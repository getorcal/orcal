import { test } from "node:test";
import assert from "node:assert/strict";
import { Buffer } from "node:buffer";
import { Orcal } from "../src/client.js";
import { ExecNotFound } from "../src/errors.js";

const b64 = (s: string) => Buffer.from(s).toString("base64");
const evt = (name: string, payload: unknown) => `event: ${name}\ndata: ${JSON.stringify(payload)}\n\n`;

const execPayload = {
  id: "ex-1",
  sandbox_id: "sb-1",
  command: ["sh"],
  state: "running",
  output_bytes: 0,
  truncated: false,
  started_at: "2026-08-20T10:00:00Z",
};
const sandboxPayload = {
  id: "sb-1",
  image: "alpine:3.20",
  state: "running",
  runtime: "docker",
  network: "full",
  resources: { cpu_millis: 1, memory_bytes: 1, pids_limit: 1 },
  created_at: "x",
  updated_at: "x",
};

function cancellableStream(chunks: string[]) {
  const state = { cancelled: false };
  const pending = [...chunks];
  const encoder = new TextEncoder();
  const stream = new ReadableStream<Uint8Array>({
    pull(controller) {
      const next = pending.shift();
      if (next === undefined) controller.close();
      else controller.enqueue(encoder.encode(next));
    },
    cancel() {
      state.cancelled = true;
    },
  });
  return { stream, state };
}

function harness(streamBody: string | number | ReadableStream<Uint8Array>, onCreate?: (init: RequestInit) => void) {
  const impl = (async (input: string | URL, init: RequestInit = {}) => {
    const url = new URL(String(input));
    if (url.pathname === "/v1/sandboxes/sb-1/execs") {
      onCreate?.(init);
      return new Response(JSON.stringify(execPayload), { status: 201, headers: { "content-type": "application/json" } });
    }
    if (url.pathname === "/v1/execs/ex-1/output") {
      if (typeof streamBody === "number") {
        return new Response(JSON.stringify({ error: { code: "exec_not_found", message: "no such exec", details: {} } }), {
          status: streamBody,
          headers: { "content-type": "application/json" },
        });
      }
      return new Response(streamBody, { status: 200, headers: { "content-type": "text/event-stream" } });
    }
    return new Response("{}", { status: 200 });
  }) as unknown as typeof fetch;
  const client = new Orcal({ url: "http://example.test", token: "x", fetch: impl });
  return client._fromPayload(sandboxPayload as never);
}

test("a string command is wrapped in a shell", async () => {
  let body = "";
  const sb = harness(evt("exit", { state: "exited", exit_code: 0, truncated: false }), (init) => {
    body = String(init.body);
  });
  await sb.exec("echo hi");
  const parsed = JSON.parse(body) as { command: string[] };
  assert.deepEqual(parsed.command, ["sh", "-c", "echo hi"]);
});

test("an array command is passed through as exact argv", async () => {
  let body = "";
  const sb = harness(evt("exit", { state: "exited", exit_code: 0, truncated: false }), (init) => {
    body = String(init.body);
  });
  await sb.exec(["echo", "hi"]);
  const parsed = JSON.parse(body) as { command: string[] };
  assert.deepEqual(parsed.command, ["echo", "hi"]);
});

test("buffered exec decodes base64 and splits streams", async () => {
  const sb = harness(
    evt("output", { offset: 5, stream: "stdout", data: b64("out") }) +
      evt("output", { offset: 9, stream: "stderr", data: b64("err") }) +
      evt("exit", { state: "exited", exit_code: 3, truncated: false }),
  );
  const result = await sb.exec("whatever");
  assert.equal(result.stdout, "out");
  assert.equal(result.stderr, "err");
  assert.equal(result.exitCode, 3);
});

test("buffered exec result decodes to the exact original bytes, not merely a string", async () => {
  const original = new Uint8Array([0, 255, 1, 254, 128, 10, 13]);
  const sb = harness(
    evt("output", { offset: 7, stream: "stdout", data: Buffer.from(original).toString("base64") }) +
      evt("exit", { state: "exited", exit_code: 0, truncated: false }),
  );
  const stream = await sb.exec("whatever", { stream: true });
  const frames = [];
  for await (const frame of stream) frames.push(frame);
  assert.equal(frames.length, 1);
  assert.ok(frames[0].data instanceof Uint8Array);
  assert.deepEqual(Array.from(frames[0].data), Array.from(original));
});

test("streaming yields frames then carries the exit code", async () => {
  const sb = harness(
    evt("output", { offset: 3, stream: "stdout", data: b64("a") }) +
      evt("output", { offset: 6, stream: "stdout", data: b64("b") }) +
      evt("exit", { state: "exited", exit_code: 0, truncated: false }),
  );
  const stream = await sb.exec("whatever", { stream: true });
  const seen: string[] = [];
  for await (const frame of stream) {
    assert.ok(frame.data instanceof Uint8Array);
    seen.push(new TextDecoder().decode(frame.data));
  }
  assert.deepEqual(seen, ["a", "b"]);
  assert.equal(stream.exitCode, 0);
});

test("a gap is surfaced not swallowed", async () => {
  const sb = harness(
    evt("output", { offset: 3, stream: "stdout", data: b64("a") }) +
      evt("gap", { offset: 4 }) +
      evt("exit", { state: "exited", exit_code: 0, truncated: true }),
  );
  const stream = await sb.exec("whatever", { stream: true });
  for await (const _ of stream) {
    void _;
  }
  assert.deepEqual(stream.gaps, [4]);
  assert.equal(stream.truncated, true);
});

test("buffered exec result carries gaps through from the underlying stream", async () => {
  const sb = harness(
    evt("output", { offset: 3, stream: "stdout", data: b64("a") }) +
      evt("gap", { offset: 4 }) +
      evt("exit", { state: "exited", exit_code: 0, truncated: true }),
  );
  const result = await sb.exec("whatever");
  assert.deepEqual(result.gaps, [4]);
});

test("iterating a stream twice does not duplicate gaps", async () => {
  const sb = harness(
    evt("output", { offset: 3, stream: "stdout", data: b64("a") }) +
      evt("gap", { offset: 4 }) +
      evt("exit", { state: "exited", exit_code: 0, truncated: true }),
  );
  const stream = await sb.exec("whatever", { stream: true });
  for await (const _ of stream) {
    void _;
  }
  for await (const _ of stream) {
    void _;
  }
  assert.deepEqual(stream.gaps, [4]);
});

test("breaking out of a stream early cancels the response body", async () => {
  const { stream, state } = cancellableStream([
    evt("output", { offset: 3, stream: "stdout", data: b64("a") }),
    evt("output", { offset: 6, stream: "stdout", data: b64("b") }),
    evt("exit", { state: "exited", exit_code: 0, truncated: false }),
  ]);
  const sb = harness(stream);
  const execStream = await sb.exec("whatever", { stream: true });
  for await (const frame of execStream) {
    void frame;
    break;
  }
  assert.equal(state.cancelled, true, "an abandoned stream must cancel its reader and release the response body");
});

test("a buffered exec releases the response body once the exit frame arrives", async () => {
  const { stream, state } = cancellableStream([
    evt("output", { offset: 3, stream: "stdout", data: b64("a") }),
    evt("exit", { state: "exited", exit_code: 0, truncated: false }),
    evt("output", { offset: 9, stream: "stdout", data: b64("never") }),
  ]);
  const sb = harness(stream);
  await sb.exec("whatever");
  assert.equal(state.cancelled, true);
});

test("a buffered exec result carries the created exec record", async () => {
  const sb = harness(evt("exit", { state: "exited", exit_code: 0, truncated: false }));
  const result = await sb.exec("echo hi");
  assert.equal(result.raw.id, "ex-1");
  assert.equal(result.raw.sandbox_id, "sb-1");
  assert.deepEqual(result.raw.command, ["sh"]);
  assert.equal(result.raw.state, "running");
  assert.equal(result.raw.started_at, "2026-08-20T10:00:00Z");
  assert.equal(result.raw.output_bytes, 0);
});

test("streaming error status raises the mapped error", async () => {
  const sb = harness(404);
  const stream = await sb.exec("whatever", { stream: true });
  await assert.rejects(async () => {
    for await (const _ of stream) {
      void _;
    }
  }, ExecNotFound);
});
