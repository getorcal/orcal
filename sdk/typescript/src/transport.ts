import { errorFromResponse } from "./errors.js";

export interface TransportOptions {
  url: string;
  token: string;
  timeoutMs?: number;
  fetch?: typeof fetch;
  backoffMs?: number;
}

export interface RequestOptions {
  params?: Record<string, string | number | undefined>;
  body?: unknown;
  raw?: BodyInit;
  headers?: Record<string, string>;
}

const MAX_ATTEMPTS = 3;
const RETRYABLE_METHODS = new Set(["GET", "HEAD"]);

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

export class Transport {
  private readonly base: string;
  private readonly token: string;
  private readonly timeoutMs: number;
  private readonly backoffMs: number;
  private readonly fetchImpl: typeof fetch;

  constructor(options: TransportOptions) {
    this.base = options.url.replace(/\/+$/, "");
    this.token = options.token;
    this.timeoutMs = options.timeoutMs ?? 30_000;
    this.backoffMs = options.backoffMs ?? 100;
    this.fetchImpl = options.fetch ?? globalThis.fetch;
  }

  escape(segment: string): string {
    return encodeURIComponent(segment);
  }

  private url(path: string, params?: RequestOptions["params"]): string {
    const query = new URLSearchParams();
    for (const [key, value] of Object.entries(params ?? {})) {
      if (value !== undefined) query.set(key, String(value));
    }
    const suffix = query.toString();
    return this.base + path + (suffix ? `?${suffix}` : "");
  }

  private init(method: string, options: RequestOptions): RequestInit {
    const headers: Record<string, string> = { authorization: `Bearer ${this.token}`, ...(options.headers ?? {}) };
    let body: BodyInit | undefined;
    if (options.raw !== undefined) {
      body = options.raw;
    } else if (options.body !== undefined) {
      body = JSON.stringify(options.body);
      headers["content-type"] = "application/json";
    }
    return { method, headers, body, signal: AbortSignal.timeout(this.timeoutMs) };
  }

  async request(method: string, path: string, options: RequestOptions = {}): Promise<Response> {
    const retryable = RETRYABLE_METHODS.has(method.toUpperCase());
    for (let attempt = 1; ; attempt += 1) {
      let response: Response;
      try {
        response = await this.fetchImpl(this.url(path, options.params), this.init(method, options));
      } catch (cause) {
        if (!retryable || attempt >= MAX_ATTEMPTS) throw cause;
        await sleep(this.backoffMs * attempt);
        continue;
      }
      if (response.status < 400) return response;
      if (response.status === 503 && retryable && attempt < MAX_ATTEMPTS) {
        await sleep(this.backoffMs * attempt);
        continue;
      }
      throw errorFromResponse(response.status, await response.text());
    }
  }

  async stream(path: string, params?: RequestOptions["params"]): Promise<Response> {
    const response = await this.fetchImpl(this.url(path, params), {
      method: "GET",
      headers: { authorization: `Bearer ${this.token}` },
    });
    if (response.status >= 400) throw errorFromResponse(response.status, await response.text());
    return response;
  }
}
