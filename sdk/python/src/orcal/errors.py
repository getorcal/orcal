import json


class OrcalError(Exception):
    def __init__(self, code, message, status_code, request_id=None):
        super().__init__(message)
        self.code = code
        self.message = message
        self.status_code = status_code
        self.request_id = request_id

    def __str__(self):
        if self.request_id:
            return f"{self.code} ({self.status_code}): {self.message} [request_id={self.request_id}]"
        return f"{self.code} ({self.status_code}): {self.message}"


class InvalidRequest(OrcalError):
    pass


class Unauthorized(OrcalError):
    pass


class Forbidden(OrcalError):
    pass


class NotFound(OrcalError):
    pass


class TokenNotFound(NotFound):
    pass


class SandboxNotFound(NotFound):
    pass


class SnapshotNotFound(NotFound):
    pass


class ExecNotFound(NotFound):
    pass


class PathNotFound(NotFound):
    pass


class Conflict(OrcalError):
    pass


class ResourceExhausted(OrcalError):
    pass


class RuntimeUnavailable(OrcalError):
    pass


class InternalError(OrcalError):
    pass


CODE_TYPES = {
    "invalid_request": InvalidRequest,
    "unauthorized": Unauthorized,
    "forbidden": Forbidden,
    "token_not_found": TokenNotFound,
    "sandbox_not_found": SandboxNotFound,
    "snapshot_not_found": SnapshotNotFound,
    "exec_not_found": ExecNotFound,
    "path_not_found": PathNotFound,
    "name_taken": Conflict,
    "invalid_state": Conflict,
    "resource_exhausted": ResourceExhausted,
    "runtime_unavailable": RuntimeUnavailable,
    "internal_error": InternalError,
}


def from_response(status_code, body):
    code = "internal_error"
    message = ""
    request_id = None
    try:
        payload = json.loads(body)
        envelope = payload.get("error") or {}
        code = envelope.get("code") or code
        message = envelope.get("message") or ""
        details = envelope.get("details") or {}
        request_id = details.get("request_id")
    except (ValueError, AttributeError):
        message = body.decode(errors="replace") if isinstance(body, bytes) else str(body)
    return CODE_TYPES.get(code, OrcalError)(code, message, status_code, request_id)
