export class OrcalError extends Error {
  readonly code: string;
  readonly statusCode: number;
  readonly requestId?: string;

  constructor(code: string, message: string, statusCode: number, requestId?: string) {
    super(message);
    this.name = new.target.name;
    this.code = code;
    this.statusCode = statusCode;
    this.requestId = requestId;
  }
}

export class InvalidRequest extends OrcalError {}
export class Unauthorized extends OrcalError {}
export class Forbidden extends OrcalError {}
export class NotFound extends OrcalError {}
export class TokenNotFound extends NotFound {}
export class SandboxNotFound extends NotFound {}
export class SnapshotNotFound extends NotFound {}
export class ExecNotFound extends NotFound {}
export class PathNotFound extends NotFound {}
export class Conflict extends OrcalError {}
export class ResourceExhausted extends OrcalError {}
export class RuntimeUnavailable extends OrcalError {}
export class InternalError extends OrcalError {}

export const CODE_TYPES: Record<string, typeof OrcalError> = {
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

export function errorFromResponse(statusCode: number, body: string): OrcalError {
  let code = "internal_error";
  let message = "";
  let requestId: string | undefined;
  try {
    const parsed = JSON.parse(body) as { error?: { code?: string; message?: string; details?: Record<string, unknown> } };
    const envelope = parsed.error ?? {};
    code = envelope.code ?? code;
    message = envelope.message ?? "";
    const details = envelope.details ?? {};
    const id = details["request_id"];
    requestId = typeof id === "string" ? id : undefined;
  } catch {
    message = body;
  }
  const Type = CODE_TYPES[code] ?? OrcalError;
  return new Type(code, message, statusCode, requestId);
}
