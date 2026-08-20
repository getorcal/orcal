import { SSEParser } from "./sse.js";
import type { Transport } from "./transport.js";
import type { Exec as ExecModel } from "./types.js";

export interface Frame {
  offset: number;
  stream: string;
  data: Uint8Array;
}

export interface ExecResult {
  id: string;
  stdout: string;
  stderr: string;
  exitCode: number | null;
  truncated: boolean;
  raw: ExecModel;
  gaps: number[];
}

export interface ExecOptions {
  env?: Record<string, string>;
  workingDir?: string;
  user?: string;
}

function decodeBase64(value: string): Uint8Array {
  return new Uint8Array(Buffer.from(value, "base64"));
}

function concatBytes(parts: Uint8Array[]): Uint8Array {
  const total = parts.reduce((sum, part) => sum + part.length, 0);
  const result = new Uint8Array(total);
  let offset = 0;
  for (const part of parts) {
    result.set(part, offset);
    offset += part.length;
  }
  return result;
}

export class ExecStream implements AsyncIterable<Frame> {
  exitCode: number | null = null;
  truncated = false;
  state: string | null = null;
  readonly gaps: number[] = [];

  constructor(
    private readonly transport: Transport,
    readonly id: string,
    private from = 0,
  ) {}

  async *[Symbol.asyncIterator](): AsyncIterator<Frame> {
    this.gaps.length = 0;
    const parser = new SSEParser();
    const response = await this.transport.stream(`/v1/execs/${this.transport.escape(this.id)}/output`, {
      from: this.from,
    });
    const reader = response.body?.getReader();
    if (!reader) return;
    try {
      for (;;) {
        const { done, value } = await reader.read();
        if (done) return;
        for (const [name, payload] of parser.feed(value)) {
          if (name === "output") {
            const offset = payload.offset as number;
            this.from = offset;
            yield {
              offset,
              stream: (payload.stream as string) ?? "stdout",
              data: decodeBase64(payload.data as string),
            };
          } else if (name === "gap") {
            const offset = payload.offset as number;
            this.from = offset;
            this.gaps.push(offset);
          } else if (name === "exit") {
            this.state = (payload.state as string) ?? null;
            this.exitCode = (payload.exit_code as number | null) ?? null;
            this.truncated = Boolean(payload.truncated);
            return;
          }
        }
      }
    } finally {
      await reader.cancel();
    }
  }
}

export async function collectExec(transport: Transport, raw: ExecModel): Promise<ExecResult> {
  const stream = new ExecStream(transport, raw.id);
  const out: Uint8Array[] = [];
  const err: Uint8Array[] = [];
  for await (const frame of stream) {
    (frame.stream === "stdout" ? out : err).push(frame.data);
  }
  const decoder = new TextDecoder();
  return {
    id: stream.id,
    stdout: decoder.decode(concatBytes(out)),
    stderr: decoder.decode(concatBytes(err)),
    exitCode: stream.exitCode,
    truncated: stream.truncated,
    raw,
    gaps: [...stream.gaps],
  };
}
