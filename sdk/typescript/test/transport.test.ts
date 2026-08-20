import { test } from "node:test";
import assert from "node:assert/strict";
import { Transport } from "../src/transport.js";
import { Conflict, RuntimeUnavailable, SandboxNotFound } from "../src/errors.js";

function fakeFetch(responses: Array<() => Response | Promise<Response>>) {
  const calls: Array<{ url: string; init: RequestInit }> = [];
  let index = 0;
  const impl = async (url: string | URL, init: RequestInit = {}) => {
    calls.push({ url: String(url), init });
    const next = responses[Math.min(index, responses.length - 1)];
    index += 1;
    const result = Promise.resolve().then(() => next());
    const signal = init.signal;
    if (!signal) return result;
    return new Promise<Response>((resolve, reject) => {
      const onAbort = () => reject(new Error("aborted"));
      if (signal.aborted) {
        onAbort();
        return;
      }
      signal.addEventListener("abort", onAbort, { once: true });
      result.then(
        (value) => {
          signal.removeEventListener("abort", onAbort);
          resolve(value);
        },
        (err) => {
          signal.removeEventListener("abort", onAbort);
          reject(err);
        },
      );
    });
  };
  return { impl: impl as unknown as typeof fetch, calls };
}

const json = (status: number, payload: unknown) =>
  new Response(JSON.stringify(payload), { status, headers: { "content-type": "application/json" } });

const errorBody = (code: string) => ({ error: { code, message: "boom", details: {} } });

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

test("the bearer token is sent and the base url does not double its slash", async () => {
  const { impl, calls } = fakeFetch([() => json(200, {})]);
  const t = new Transport({ url: "http://example.test/", token: "orcal_secret", fetch: impl });
  await t.request("GET", "/v1/sandboxes");
  assert.equal(calls[0].url, "http://example.test/v1/sandboxes");
  assert.equal((calls[0].init.headers as Record<string, string>)["authorization"], "Bearer orcal_secret");
});

test("path segments are escaped", () => {
  const t = new Transport({ url: "http://example.test", token: "x" });
  assert.equal(t.escape("a/b?c"), "a%2Fb%3Fc");
});

test("escaped path segments reach the request url still encoded", async () => {
  const { impl, calls } = fakeFetch([() => json(200, {})]);
  const t = new Transport({ url: "http://example.test", token: "x", fetch: impl });
  await t.request("GET", `/v1/sandboxes/${t.escape("a/../b")}`);
  assert.equal(calls[0].url, "http://example.test/v1/sandboxes/a%2F..%2Fb");
});

test("an error status throws the mapped type", async () => {
  const { impl } = fakeFetch([() => json(404, errorBody("sandbox_not_found"))]);
  const t = new Transport({ url: "http://example.test", token: "x", fetch: impl });
  await assert.rejects(() => t.request("GET", "/v1/sandboxes/x"), SandboxNotFound);
});

test("a 4xx is never retried", async () => {
  const { impl, calls } = fakeFetch([() => json(409, errorBody("name_taken"))]);
  const t = new Transport({ url: "http://example.test", token: "x", fetch: impl, backoffMs: 0 });
  await assert.rejects(() => t.request("POST", "/v1/sandboxes", { body: { image: "alpine" } }), Conflict);
  assert.equal(calls.length, 1);
});

test("a 503 on a GET is retried then succeeds", async () => {
  let n = 0;
  const { impl, calls } = fakeFetch([
    () => {
      n += 1;
      return n < 3 ? json(503, errorBody("runtime_unavailable")) : json(200, { ok: true });
    },
  ]);
  const t = new Transport({ url: "http://example.test", token: "x", fetch: impl, backoffMs: 0 });
  await t.request("GET", "/v1/sandboxes");
  assert.equal(calls.length, 3);
});

test("a non-503 4xx on a GET is not retried either", async () => {
  const { impl, calls } = fakeFetch([() => json(404, errorBody("sandbox_not_found"))]);
  const t = new Transport({ url: "http://example.test", token: "x", fetch: impl, backoffMs: 0 });
  await assert.rejects(() => t.request("GET", "/v1/sandboxes/x"), SandboxNotFound);
  assert.equal(calls.length, 1);
});

test("a 503 gives up after three attempts", async () => {
  const { impl, calls } = fakeFetch([() => json(503, errorBody("runtime_unavailable"))]);
  const t = new Transport({ url: "http://example.test", token: "x", fetch: impl, backoffMs: 0 });
  await assert.rejects(() => t.request("GET", "/v1/sandboxes"), RuntimeUnavailable);
  assert.equal(calls.length, 3);
});

test("a lost sandbox create is never retried", async () => {
  const { impl, calls } = fakeFetch([
    () => {
      throw new TypeError("network failure");
    },
  ]);
  const t = new Transport({ url: "http://example.test", token: "x", fetch: impl, backoffMs: 0 });
  await assert.rejects(() => t.request("POST", "/v1/sandboxes", { body: { image: "alpine" } }));
  assert.equal(calls.length, 1, "a create whose response was lost may have landed; retrying makes a second sandbox");
});

test("a connection failure on a GET is retried", async () => {
  let n = 0;
  const { impl, calls } = fakeFetch([
    () => {
      n += 1;
      if (n < 2) throw new TypeError("network failure");
      return json(200, {});
    },
  ]);
  const t = new Transport({ url: "http://example.test", token: "x", fetch: impl, backoffMs: 0 });
  await t.request("GET", "/v1/sandboxes");
  assert.equal(calls.length, 2);
});

test("stream requests carry no read-deadline signal", async () => {
  const { impl, calls } = fakeFetch([() => json(200, {})]);
  const t = new Transport({ url: "http://example.test", token: "x", fetch: impl });
  await t.stream("/v1/execs/x/logs");
  assert.equal(calls[0].init.signal, undefined);
});

test("a slow stream is not cut off by the request timeout", async () => {
  const { impl } = fakeFetch([
    async () => {
      await sleep(50);
      return json(200, {});
    },
  ]);
  const t = new Transport({ url: "http://example.test", token: "x", fetch: impl, timeoutMs: 5 });
  const response = await t.stream("/v1/execs/x/logs");
  assert.equal(response.status, 200);
});
