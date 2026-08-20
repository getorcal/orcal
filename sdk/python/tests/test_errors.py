import json

import pytest

from orcal import errors


def body(code, message="boom", request_id="req-1"):
    return json.dumps({"error": {"code": code, "message": message, "details": {"request_id": request_id}}}).encode()


@pytest.mark.parametrize(
    "code,expected",
    [
        ("invalid_request", errors.InvalidRequest),
        ("unauthorized", errors.Unauthorized),
        ("forbidden", errors.Forbidden),
        ("token_not_found", errors.TokenNotFound),
        ("sandbox_not_found", errors.SandboxNotFound),
        ("snapshot_not_found", errors.SnapshotNotFound),
        ("exec_not_found", errors.ExecNotFound),
        ("path_not_found", errors.PathNotFound),
        ("name_taken", errors.Conflict),
        ("invalid_state", errors.Conflict),
        ("resource_exhausted", errors.ResourceExhausted),
        ("runtime_unavailable", errors.RuntimeUnavailable),
        ("internal_error", errors.InternalError),
    ],
)
def test_every_wire_code_maps_to_its_type(code, expected):
    err = errors.from_response(400, body(code))
    assert isinstance(err, expected)
    assert err.code == code


def test_not_found_types_share_a_base():
    for code in ("token_not_found", "sandbox_not_found", "snapshot_not_found", "exec_not_found", "path_not_found"):
        assert isinstance(errors.from_response(404, body(code)), errors.NotFound)


def test_unknown_code_degrades_instead_of_raising():
    err = errors.from_response(418, body("teapot_not_found"))
    assert type(err) is errors.OrcalError
    assert err.code == "teapot_not_found"


def test_request_id_and_status_survive():
    err = errors.from_response(409, body("name_taken", request_id="req-42"))
    assert err.request_id == "req-42"
    assert err.status_code == 409


def test_unparseable_body_still_produces_an_error():
    err = errors.from_response(502, b"<html>gateway</html>")
    assert isinstance(err, errors.OrcalError)
    assert err.status_code == 502


def test_every_type_descends_from_orcal_error():
    for code in errors.CODE_TYPES:
        assert issubclass(errors.CODE_TYPES[code], errors.OrcalError)
