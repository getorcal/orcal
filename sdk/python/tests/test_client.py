import httpx
import pytest

import orcal
from tests.conftest import sandbox_payload


def test_sandbox_context_manager_destroys_on_exit(make_client):
    routes = {
        ("POST", "/v1/sandboxes"): lambda r: httpx.Response(201, json=sandbox_payload()),
        ("DELETE", "/v1/sandboxes/sb-1"): lambda r: httpx.Response(200, json=sandbox_payload()),
    }
    client, recorder = make_client(routes)
    with client.sandbox(image="alpine:3.20") as sb:
        assert sb.id == "sb-1"
    assert ("DELETE", "/v1/sandboxes/sb-1") in [(m, p) for m, p, _ in recorder.calls]


def test_sandbox_is_destroyed_even_when_the_body_raises(make_client):
    routes = {
        ("POST", "/v1/sandboxes"): lambda r: httpx.Response(201, json=sandbox_payload()),
        ("DELETE", "/v1/sandboxes/sb-1"): lambda r: httpx.Response(200, json=sandbox_payload()),
    }
    client, recorder = make_client(routes)
    with pytest.raises(ZeroDivisionError):
        with client.sandbox(image="alpine:3.20"):
            1 / 0
    assert ("DELETE", "/v1/sandboxes/sb-1") in [(m, p) for m, p, _ in recorder.calls]


def test_network_none_is_sent_on_create(make_client):
    seen = {}

    def create(request):
        seen["body"] = request.content
        return httpx.Response(201, json=sandbox_payload(network="none"))

    client, _ = make_client({("POST", "/v1/sandboxes"): create})
    sb = client.sandbox(image="alpine:3.20", network="none")
    assert b'"network"' in seen["body"] and b'"none"' in seen["body"]
    assert sb.network == "none"


def test_files_write_accepts_str_and_bytes(make_client):
    bodies = []

    def put(request):
        bodies.append(request.content)
        return httpx.Response(204)

    routes = {
        ("POST", "/v1/sandboxes"): lambda r: httpx.Response(201, json=sandbox_payload()),
        ("PUT", "/v1/sandboxes/sb-1/files"): put,
    }
    client, _ = make_client(routes)
    sb = client.sandbox(image="alpine:3.20")
    sb.files.write("/app/a.txt", "hello")
    sb.files.write("/app/b.txt", b"hello")
    assert bodies == [b"hello", b"hello"]


def test_files_read_returns_bytes(make_client):
    routes = {
        ("POST", "/v1/sandboxes"): lambda r: httpx.Response(201, json=sandbox_payload()),
        ("GET", "/v1/sandboxes/sb-1/files"): lambda r: httpx.Response(200, content=b"\x00\x01binary"),
    }
    client, _ = make_client(routes)
    sb = client.sandbox(image="alpine:3.20")
    assert sb.files.read("/app/a.bin") == b"\x00\x01binary"


def test_snapshot_then_fork_posts_the_snapshot_reference(make_client):
    seen = {}

    def create(request):
        seen.setdefault("bodies", []).append(request.content)
        return httpx.Response(201, json=sandbox_payload(sandbox_id="sb-2", name="forked"))

    routes = {
        ("POST", "/v1/sandboxes"): create,
        ("POST", "/v1/sandboxes/sb-1/snapshots"): lambda r: httpx.Response(
            201, json={"id": "snap-1", "name": "v1", "sandbox_id": "sb-1", "runtime_ref": "sha256:x",
                       "image": "alpine:3.20", "size_bytes": 1, "created_at": "2026-08-20T10:00:00Z"}
        ),
    }
    client, _ = make_client(routes)
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    snap = sb.snapshot(name="v1")
    assert snap.id == "snap-1"
    forked = snap.fork()
    assert forked.id == "sb-2"
    assert b"snap-1" in seen["bodies"][-1]


def test_sandbox_ref_is_escaped_in_the_path(make_client):
    seen = {}

    def get(request):
        seen["path"] = request.url.raw_path.decode()
        return httpx.Response(200, json=sandbox_payload())

    client, _ = make_client({("GET", "/v1/sandboxes/a%2Fb"): get})
    client.get_sandbox("a/b")
    assert seen["path"] == "/v1/sandboxes/a%2Fb"


def test_healthz_gets_the_status(make_client):
    routes = {("GET", "/v1/healthz"): lambda r: httpx.Response(200, json={"status": "ok"})}
    client, recorder = make_client(routes)
    assert client.healthz() == {"status": "ok"}
    assert recorder.calls == [("GET", "/v1/healthz", b"")]


def test_get_exec_fetches_by_id(make_client):
    exec_payload = {
        "id": "ex-1",
        "sandbox_id": "sb-1",
        "command": ["sh", "-c", "echo hi"],
        "state": "exited",
        "output_bytes": 2,
        "truncated": False,
        "started_at": "2026-08-20T10:00:00Z",
        "exit_code": 0,
    }
    routes = {("GET", "/v1/execs/ex-1"): lambda r: httpx.Response(200, json=exec_payload)}
    client, recorder = make_client(routes)
    ex = client.get_exec("ex-1")
    assert ex.id == "ex-1"
    assert ex.exit_code == 0
    assert recorder.calls == [("GET", "/v1/execs/ex-1", b"")]
