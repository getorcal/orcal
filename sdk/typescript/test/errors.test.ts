import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";
import { load } from "js-yaml";
import {
  errorFromResponse,
  CODE_TYPES,
  OrcalError,
  InvalidRequest,
  Unauthorized,
  Forbidden,
  NotFound,
  TokenNotFound,
  SandboxNotFound,
  SnapshotNotFound,
  ExecNotFound,
  PathNotFound,
  Conflict,
  ResourceExhausted,
  RuntimeUnavailable,
  InternalError,
} from "../src/errors.js";
import type { ErrorBody } from "../src/types.js";

const here = path.dirname(fileURLToPath(import.meta.url));
const SPEC = path.resolve(here, "..", "..", "..", "spec", "openapi.yaml");

const body = (code: string, requestId = "req-1") =>
  JSON.stringify({ error: { code, message: "boom", details: { request_id: requestId } } });

const EXPECTED_TYPES: Record<ErrorBody["code"], typeof OrcalError> = {
  invalid_request: InvalidRequest,
  unauthorized: Unauthorized,
  forbidden: Forbidden,
  token_not_found: TokenNotFound,
  sandbox_not_found: SandboxNotFound,
  snapshot_not_found: SnapshotNotFound,
  exec_not_found: ExecNotFound,
  path_not_found: PathNotFound,
  name_taken: Conflict,
  invalid_state: Conflict,
  resource_exhausted: ResourceExhausted,
  runtime_unavailable: RuntimeUnavailable,
  internal_error: InternalError,
};

function specErrorCodes(): string[] {
  const doc = load(readFileSync(SPEC, "utf8")) as {
    components?: { schemas?: { ErrorBody?: { properties?: { code?: { enum?: string[] } } } } };
  };
  const codes = doc.components?.schemas?.ErrorBody?.properties?.code?.enum ?? [];
  assert.ok(codes.length > 0, "parsed zero error codes; the spec is not being read");
  return codes;
}

const SPEC_CODES = specErrorCodes();

test("the generated enum carries the same vocabulary as the spec", () => {
  assert.deepEqual([...Object.keys(EXPECTED_TYPES)].sort(), [...SPEC_CODES].sort());
});

test("the error table claims every spec code and no other", () => {
  assert.deepEqual([...Object.keys(CODE_TYPES)].sort(), [...SPEC_CODES].sort());
});

test("every code in the spec maps to its specific type", () => {
  for (const code of SPEC_CODES) {
    const expected = EXPECTED_TYPES[code as ErrorBody["code"]];
    assert.ok(expected, `${code} is in the spec's ErrorBody enum but no SDK type claims it`);
    const err = errorFromResponse(400, body(code));
    assert.ok(err instanceof OrcalError);
    assert.equal(err.code, code);
    assert.ok(err instanceof expected, `${code} should be an instance of ${expected.name}`);
    assert.equal(err.constructor, expected, `${code} should construct exactly ${expected.name}`);
  }
});

test("every mapped type descends from OrcalError", () => {
  for (const [code, type] of Object.entries(CODE_TYPES)) {
    assert.ok(type.prototype instanceof OrcalError || type === OrcalError, `${code} maps outside the OrcalError tree`);
  }
});

test("not-found codes share a base", () => {
  const notFound = SPEC_CODES.filter((code) => code.endsWith("_not_found"));
  assert.ok(notFound.length >= 5, `expected the spec to carry the not-found family, saw ${notFound.join(", ")}`);
  for (const code of notFound) {
    assert.ok(errorFromResponse(404, body(code)) instanceof NotFound, `${code} should be a NotFound`);
  }
});

test("conflict codes share a type", () => {
  assert.ok(errorFromResponse(409, body("name_taken")) instanceof Conflict);
  assert.ok(errorFromResponse(409, body("invalid_state")) instanceof Conflict);
});

test("an unknown code degrades instead of throwing", () => {
  const err = errorFromResponse(418, body("teapot_not_found"));
  assert.equal(err.constructor, OrcalError);
  assert.equal(err.code, "teapot_not_found");
});

test("request id and status survive", () => {
  const err = errorFromResponse(404, body("sandbox_not_found", "req-42"));
  assert.ok(err instanceof SandboxNotFound);
  assert.equal(err.requestId, "req-42");
  assert.equal(err.statusCode, 404);
});

test("an unparseable body still produces an error", () => {
  const err = errorFromResponse(502, "<html>gateway</html>");
  assert.ok(err instanceof OrcalError);
  assert.equal(err.statusCode, 502);
});
