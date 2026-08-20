import httpx
import pytest

from orcal import errors
from orcal._transport import Transport


def transport_with(handler):
    return Transport("http://example.test/", "orcal_secret", transport=httpx.MockTransport(handler))


def test_bearer_token_is_sent():
    seen = {}

    def handler(request):
        seen["auth"] = request.headers.get("authorization")
        return httpx.Response(200, json={})

    transport_with(handler).request("GET", "/v1/sandboxes")
    assert seen["auth"] == "Bearer orcal_secret"


def test_base_url_trailing_slash_does_not_double():
    seen = {}

    def handler(request):
        seen["url"] = str(request.url)
        return httpx.Response(200, json={})

    transport_with(handler).request("GET", "/v1/sandboxes")
    assert seen["url"] == "http://example.test/v1/sandboxes"


def test_path_segments_are_escaped():
    seen = {}

    def handler(request):
        seen["path"] = request.url.raw_path.decode()
        return httpx.Response(200, json={})

    t = transport_with(handler)
    t.request("GET", f"/v1/sandboxes/{t.escape('a/b?c')}")
    assert seen["path"] == "/v1/sandboxes/a%2Fb%3Fc"


def test_error_status_raises_the_mapped_type():
    def handler(request):
        return httpx.Response(404, json={"error": {"code": "sandbox_not_found", "message": "nope", "details": {}}})

    with pytest.raises(errors.SandboxNotFound):
        transport_with(handler).request("GET", "/v1/sandboxes/x")


def test_a_4xx_is_never_retried():
    calls = []

    def handler(request):
        calls.append(1)
        return httpx.Response(409, json={"error": {"code": "name_taken", "message": "taken", "details": {}}})

    with pytest.raises(errors.Conflict):
        transport_with(handler).request("POST", "/v1/sandboxes", json={"image": "alpine"})
    assert len(calls) == 1


def test_503_is_retried_then_succeeds():
    calls = []

    def handler(request):
        calls.append(1)
        if len(calls) < 3:
            return httpx.Response(503, json={"error": {"code": "runtime_unavailable", "message": "busy", "details": {}}})
        return httpx.Response(200, json={"ok": True})

    resp = transport_with(handler).request("GET", "/v1/sandboxes")
    assert resp.status_code == 200
    assert len(calls) == 3


def test_503_gives_up_after_three_attempts():
    calls = []

    def handler(request):
        calls.append(1)
        return httpx.Response(503, json={"error": {"code": "runtime_unavailable", "message": "busy", "details": {}}})

    with pytest.raises(errors.RuntimeUnavailable):
        transport_with(handler).request("GET", "/v1/sandboxes")
    assert len(calls) == 3


def test_a_lost_sandbox_create_is_never_retried():
    calls = []

    def handler(request):
        calls.append(1)
        raise httpx.ConnectError("dropped")

    with pytest.raises(httpx.ConnectError):
        transport_with(handler).request("POST", "/v1/sandboxes", json={"image": "alpine"})
    assert len(calls) == 1, "a create whose response was lost may have landed; retrying it makes a second sandbox"


def test_connection_failure_is_retried_for_a_get():
    calls = []

    def handler(request):
        calls.append(1)
        if len(calls) < 2:
            raise httpx.ConnectError("dropped")
        return httpx.Response(200, json={})

    transport_with(handler).request("GET", "/v1/sandboxes")
    assert len(calls) == 2
