import { test } from "node:test";
import assert from "node:assert/strict";
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

const body = (code: string, requestId = "req-1") =>
  JSON.stringify({ error: { code, message: "boom", details: { request_id: requestId } } });

const EXPECTED_TYPES: Record<string, typeof OrcalError> = {
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

test("every wire code maps to its specific type", () => {
  assert.deepEqual(Object.keys(CODE_TYPES).sort(), Object.keys(EXPECTED_TYPES).sort());
  for (const [code, expected] of Object.entries(EXPECTED_TYPES)) {
    const err = errorFromResponse(400, body(code));
    assert.ok(err instanceof OrcalError);
    assert.equal(err.code, code);
    assert.ok(err instanceof expected, `${code} should be an instance of ${expected.name}`);
    assert.equal(err.constructor, expected, `${code} should construct exactly ${expected.name}`);
  }
});

test("not-found codes share a base", () => {
  for (const code of ["token_not_found", "sandbox_not_found", "snapshot_not_found", "exec_not_found", "path_not_found"]) {
    assert.ok(errorFromResponse(404, body(code)) instanceof NotFound);
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
