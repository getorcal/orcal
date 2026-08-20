import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";
import { load } from "js-yaml";
import { Orcal } from "../src/client.js";
import { Sandbox, SandboxFiles } from "../src/sandbox.js";
import { Snapshot } from "../src/snapshot.js";
import { ExecStream } from "../src/exec.js";

const here = path.dirname(fileURLToPath(import.meta.url));
const SPEC = path.resolve(here, "..", "..", "..", "spec", "openapi.yaml");

const METHODS = new Set(["get", "post", "put", "delete", "patch"]);

interface Coverage {
  owner: string;
  member: string;
}

const COVERED_BY: Record<string, Coverage> = {
  "get /healthz": { owner: "Orcal", member: "healthz" },
  "get /version": { owner: "Orcal", member: "version" },
  "get /sandboxes": { owner: "Orcal", member: "sandboxes" },
  "post /sandboxes": { owner: "Orcal", member: "sandbox" },
  "get /sandboxes/{ref}": { owner: "Orcal", member: "getSandbox" },
  "delete /sandboxes/{ref}": { owner: "Sandbox", member: "destroy" },
  "post /sandboxes/{ref}/start": { owner: "Sandbox", member: "start" },
  "post /sandboxes/{ref}/stop": { owner: "Sandbox", member: "stop" },
  "get /sandboxes/{ref}/execs": { owner: "Sandbox", member: "execs" },
  "post /sandboxes/{ref}/execs": { owner: "Sandbox", member: "exec" },
  "get /execs/{id}": { owner: "Orcal", member: "getExec" },
  "get /execs/{id}/output": { owner: "ExecStream", member: "asyncIterator" },
  "get /sandboxes/{ref}/snapshots": { owner: "Sandbox", member: "snapshots" },
  "post /sandboxes/{ref}/snapshots": { owner: "Sandbox", member: "snapshot" },
  "post /sandboxes/{ref}/restore": { owner: "Sandbox", member: "restore" },
  "get /snapshots": { owner: "Orcal", member: "snapshots" },
  "get /snapshots/{ref}": { owner: "Orcal", member: "getSnapshot" },
  "delete /snapshots/{ref}": { owner: "Snapshot", member: "delete" },
  "get /sandboxes/{ref}/files": { owner: "SandboxFiles", member: "read" },
  "put /sandboxes/{ref}/files": { owner: "SandboxFiles", member: "write" },
  "get /sandboxes/{ref}/files/stat": { owner: "SandboxFiles", member: "stat" },
  "get /sandboxes/{ref}/files/list": { owner: "SandboxFiles", member: "list" },
  "get /sandboxes/{ref}/archive": { owner: "SandboxFiles", member: "download" },
  "put /sandboxes/{ref}/archive": { owner: "SandboxFiles", member: "upload" },
  "get /tokens": { owner: "Orcal", member: "listTokens" },
  "post /tokens": { owner: "Orcal", member: "createToken" },
  "delete /tokens/{id}": { owner: "Orcal", member: "revokeToken" },
  "get /events": { owner: "Orcal", member: "events" },
};

const EXPECTED_OPERATION_COUNT = 28;

function specOperations(): Set<string> {
  const doc = load(readFileSync(SPEC, "utf8")) as { paths?: Record<string, Record<string, unknown>> };
  const paths = doc.paths ?? {};
  assert.ok(Object.keys(paths).length > 0, "parsed zero paths; the spec is not being read");
  const operations = new Set<string>();
  for (const [p, ops] of Object.entries(paths)) {
    for (const m of Object.keys(ops)) {
      if (METHODS.has(m)) operations.add(`${m} ${p}`);
    }
  }
  return operations;
}

test("every operation has a client method", () => {
  const missing = [...specOperations()].filter((op) => !(op in COVERED_BY)).sort();
  assert.deepEqual(missing, [], `operations with no SDK method: ${missing.join(", ")}`);
});

test("no stale entries in the coverage table", () => {
  const spec = specOperations();
  const stale = Object.keys(COVERED_BY)
    .filter((op) => !spec.has(op))
    .sort();
  assert.deepEqual(stale, [], `coverage table names operations the spec no longer has: ${stale.join(", ")}`);
});

test("the coverage table accounts for every spec operation exactly once", () => {
  const spec = specOperations();
  assert.equal(spec.size, EXPECTED_OPERATION_COUNT, `expected ${EXPECTED_OPERATION_COUNT} spec operations`);
  assert.equal(
    Object.keys(COVERED_BY).length,
    EXPECTED_OPERATION_COUNT,
    `expected ${EXPECTED_OPERATION_COUNT} covered operations`,
  );
});

test("every named method exists", () => {
  const owners: Record<string, { prototype: object }> = { Orcal, Sandbox, SandboxFiles, Snapshot, ExecStream };
  for (const [operation, coverage] of Object.entries(COVERED_BY)) {
    const owner = owners[coverage.owner];
    assert.ok(owner, `unknown owner ${coverage.owner} claimed by ${operation}`);
    const proto = owner.prototype as Record<PropertyKey, unknown>;
    const key = coverage.member === "asyncIterator" ? Symbol.asyncIterator : coverage.member;
    assert.equal(
      typeof proto[key],
      "function",
      `${coverage.owner}.${coverage.member} does not exist, but ${operation} claims it`,
    );
  }
});
