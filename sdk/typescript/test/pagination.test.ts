import { test } from "node:test";
import assert from "node:assert/strict";
import { Orcal } from "../src/client.js";

const sandboxPayload = (id: string) => ({
  id,
  image: "alpine:3.20",
  state: "running",
  runtime: "docker",
  network: "full",
  resources: { cpu_millis: 1, memory_bytes: 1, pids_limit: 1 },
  created_at: "x",
  updated_at: "x",
});

const snapshotPayload = (id: string) => ({
  id,
  sandbox_id: "sb-1",
  runtime_ref: "ref-1",
  image: "alpine:3.20",
  size_bytes: 1024,
  created_at: "x",
});

const eventPayload = (id: string) => ({
  id,
  ts: "2026-08-20T10:00:00Z",
  action: "sandbox.create",
  request_id: "req-1",
  status: 201,
});

const execPayload = (id: string) => ({
  id,
  sandbox_id: "sb-1",
  command: ["sh", "-c", "echo hi"],
  state: "running",
  output_bytes: 0,
  truncated: false,
  started_at: "x",
});

function paged(path: string, pages: Array<{ items: unknown[]; next_cursor?: string }>) {
  const calls: string[] = [];
  const impl = (async (input: string | URL) => {
    const url = new URL(String(input));
    if (url.pathname !== path) return new Response("{}", { status: 200 });
    calls.push(url.searchParams.get("cursor") ?? "");
    const index = url.searchParams.get("cursor") ? 1 : 0;
    return new Response(JSON.stringify(pages[Math.min(index, pages.length - 1)]), {
      status: 200,
      headers: { "content-type": "application/json" },
    });
  }) as unknown as typeof fetch;
  return { client: new Orcal({ url: "http://example.test", token: "x", fetch: impl }), calls };
}

test("iterating follows the cursor without overlap or gap", async () => {
  const { client, calls } = paged("/v1/sandboxes", [
    { items: [sandboxPayload("sb-1"), sandboxPayload("sb-2")], next_cursor: "sb-2" },
    { items: [sandboxPayload("sb-3")] },
  ]);
  const ids: string[] = [];
  for await (const sb of client.sandboxes()) ids.push(sb.id);
  assert.deepEqual(ids, ["sb-1", "sb-2", "sb-3"]);
  assert.equal(calls.length, 2);
});

test("a single page terminates", async () => {
  const { client } = paged("/v1/sandboxes", [{ items: [sandboxPayload("sb-1")] }]);
  const ids: string[] = [];
  for await (const sb of client.sandboxes()) ids.push(sb.id);
  assert.deepEqual(ids, ["sb-1"]);
});

test("an empty page yields nothing", async () => {
  const { client, calls } = paged("/v1/sandboxes", [{ items: [] }]);
  const ids: string[] = [];
  for await (const sb of client.sandboxes()) ids.push(sb.id);
  assert.deepEqual(ids, []);
  assert.equal(calls.length, 1);
});

test("a server that echoes its cursor does not loop forever", async () => {
  const calls: string[] = [];
  const impl = (async (input: string | URL) => {
    const url = new URL(String(input));
    calls.push(url.searchParams.get("cursor") ?? "");
    return new Response(JSON.stringify({ items: [sandboxPayload("sb-1")], next_cursor: "same" }), {
      status: 200,
      headers: { "content-type": "application/json" },
    });
  }) as unknown as typeof fetch;
  const client = new Orcal({ url: "http://example.test", token: "x", fetch: impl });

  const ids: string[] = [];
  const deadline = Date.now() + 5000;
  for await (const sb of client.sandboxes()) {
    ids.push(sb.id);
    if (Date.now() > deadline) throw new Error("paginate did not terminate against an echoing cursor");
  }
  assert.deepEqual(ids, ["sb-1", "sb-1"]);
  assert.equal(calls.length, 2);
});

test("orcal snapshots paginates", async () => {
  const { client } = paged("/v1/snapshots", [{ items: [snapshotPayload("sn-1"), snapshotPayload("sn-2")] }]);
  const ids: string[] = [];
  for await (const sn of client.snapshots()) ids.push(sn.id);
  assert.deepEqual(ids, ["sn-1", "sn-2"]);
});

test("orcal events preserves the server's newest-first order", async () => {
  const { client } = paged("/v1/events", [{ items: [eventPayload("ev-b"), eventPayload("ev-a")] }]);
  const ids: string[] = [];
  for await (const ev of client.events()) ids.push(ev.id);
  assert.deepEqual(ids, ["ev-b", "ev-a"]);
});

test("sandbox execs paginates", async () => {
  const { client } = paged("/v1/sandboxes/sb-1/execs", [{ items: [execPayload("ex-1"), execPayload("ex-2")] }]);
  const sb = client._fromPayload(sandboxPayload("sb-1") as never);
  const ids: string[] = [];
  for await (const ex of sb.execs()) ids.push(ex.id);
  assert.deepEqual(ids, ["ex-1", "ex-2"]);
});

test("sandbox snapshots paginates", async () => {
  const { client } = paged("/v1/sandboxes/sb-1/snapshots", [{ items: [snapshotPayload("sn-1"), snapshotPayload("sn-2")] }]);
  const sb = client._fromPayload(sandboxPayload("sb-1") as never);
  const ids: string[] = [];
  for await (const sn of sb.snapshots()) ids.push(sn.id);
  assert.deepEqual(ids, ["sn-1", "sn-2"]);
});
