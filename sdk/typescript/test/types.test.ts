import { test } from "node:test";
import assert from "node:assert/strict";
import type { Sandbox, SandboxList, CreatedToken, EventList } from "../src/types.js";

test("sandbox type carries the wire fields", () => {
  const sandbox: Sandbox = {
    id: "sb-1",
    image: "alpine:3.20",
    state: "running",
    runtime: "docker",
    network: "full",
    resources: { cpu_millis: 1000, memory_bytes: 1, pids_limit: 512 },
    created_at: "2026-08-20T10:00:00Z",
    updated_at: "2026-08-20T10:00:00Z",
  };
  assert.equal(sandbox.id, "sb-1");
});

test("list types expose a cursor", () => {
  const list: SandboxList = { items: [], next_cursor: "sb-9" };
  const events: EventList = { items: [] };
  assert.equal(list.next_cursor, "sb-9");
  assert.deepEqual(events.items, []);
});

test("created token carries the plaintext", () => {
  const created: CreatedToken = {
    id: "tok-1",
    name: "ci",
    prefix: "orcal_abcdef",
    scopes: ["exec"],
    created_at: "2026-08-20T10:00:00Z",
    token: "orcal_secret",
  };
  assert.equal(created.token, "orcal_secret");
});
