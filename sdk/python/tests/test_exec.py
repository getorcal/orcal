import base64
import json

import httpx
import pytest

import orcal
from tests.conftest import sandbox_payload


def sse(*blocks):
    return b"".join(blocks)


def event(name, payload):
    return f"event: {name}\ndata: {json.dumps(payload)}\n\n".encode()


def exec_payload(exec_id="ex-1", state="running"):
    return {
        "id": exec_id,
        "sandbox_id": "sb-1",
        "command": ["sh", "-c", "echo hi"],
        "state": state,
        "output_bytes": 0,
        "truncated": False,
        "started_at": "2026-08-20T10:00:00Z",
    }


def test_a_string_command_is_wrapped_in_a_shell(make_client):
    seen = {}

    def create(request):
        seen["body"] = json.loads(request.content)
        return httpx.Response(201, json=exec_payload())

    routes = {
        ("POST", "/v1/sandboxes/sb-1/execs"): create,
        ("GET", "/v1/execs/ex-1/output"): lambda r: httpx.Response(
            200,
            content=sse(event("exit", {"state": "exited", "exit_code": 0, "truncated": False})),
            headers={"content-type": "text/event-stream"},
        ),
    }
    client, _ = make_client(routes)
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    sb.exec("echo hi")
    assert seen["body"]["command"] == ["sh", "-c", "echo hi"]


def test_a_list_command_is_passed_through_as_argv(make_client):
    seen = {}

    def create(request):
        seen["body"] = json.loads(request.content)
        return httpx.Response(201, json=exec_payload())

    routes = {
        ("POST", "/v1/sandboxes/sb-1/execs"): create,
        ("GET", "/v1/execs/ex-1/output"): lambda r: httpx.Response(
            200,
            content=sse(event("exit", {"state": "exited", "exit_code": 0, "truncated": False})),
            headers={"content-type": "text/event-stream"},
        ),
    }
    client, _ = make_client(routes)
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    sb.exec(["echo", "hi"])
    assert seen["body"]["command"] == ["echo", "hi"]


def test_buffered_exec_decodes_base64_and_splits_streams(make_client):
    stream_body = sse(
        event("output", {"offset": 5, "stream": "stdout", "data": base64.b64encode(b"out").decode()}),
        event("output", {"offset": 9, "stream": "stderr", "data": base64.b64encode(b"err").decode()}),
        event("exit", {"state": "exited", "exit_code": 3, "truncated": False}),
    )
    routes = {
        ("POST", "/v1/sandboxes/sb-1/execs"): lambda r: httpx.Response(201, json=exec_payload()),
        ("GET", "/v1/execs/ex-1/output"): lambda r: httpx.Response(
            200, content=stream_body, headers={"content-type": "text/event-stream"}
        ),
    }
    client, _ = make_client(routes)
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    result = sb.exec("whatever")
    assert result.stdout == "out"
    assert result.stderr == "err"
    assert result.exit_code == 3


def test_streaming_exec_yields_frames_and_then_carries_the_exit_code(make_client):
    stream_body = sse(
        event("output", {"offset": 3, "stream": "stdout", "data": base64.b64encode(b"a").decode()}),
        event("output", {"offset": 6, "stream": "stdout", "data": base64.b64encode(b"b").decode()}),
        event("exit", {"state": "exited", "exit_code": 0, "truncated": False}),
    )
    routes = {
        ("POST", "/v1/sandboxes/sb-1/execs"): lambda r: httpx.Response(201, json=exec_payload()),
        ("GET", "/v1/execs/ex-1/output"): lambda r: httpx.Response(
            200, content=stream_body, headers={"content-type": "text/event-stream"}
        ),
    }
    client, _ = make_client(routes)
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    stream = sb.exec("whatever", stream=True)
    frames = list(stream)
    assert [f.data for f in frames] == [b"a", b"b"]
    assert all(isinstance(f.data, bytes) for f in frames)
    assert stream.exit_code == 0


def test_a_gap_is_surfaced_not_swallowed(make_client):
    stream_body = sse(
        event("output", {"offset": 3, "stream": "stdout", "data": base64.b64encode(b"a").decode()}),
        event("gap", {"offset": 4}),
        event("exit", {"state": "exited", "exit_code": 0, "truncated": True}),
    )
    routes = {
        ("POST", "/v1/sandboxes/sb-1/execs"): lambda r: httpx.Response(201, json=exec_payload()),
        ("GET", "/v1/execs/ex-1/output"): lambda r: httpx.Response(
            200, content=stream_body, headers={"content-type": "text/event-stream"}
        ),
    }
    client, _ = make_client(routes)
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    stream = sb.exec("whatever", stream=True)
    list(stream)
    assert stream.gaps == [4]
    assert stream.truncated is True


def test_buffered_exec_result_carries_the_raw_exec(make_client):
    routes = {
        ("POST", "/v1/sandboxes/sb-1/execs"): lambda r: httpx.Response(201, json=exec_payload()),
        ("GET", "/v1/execs/ex-1/output"): lambda r: httpx.Response(
            200,
            content=sse(event("exit", {"state": "exited", "exit_code": 0, "truncated": False})),
            headers={"content-type": "text/event-stream"},
        ),
    }
    client, _ = make_client(routes)
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    result = sb.exec("echo hi")
    assert result.raw.id == "ex-1"
    assert result.raw.command == ["sh", "-c", "echo hi"]


def test_buffered_exec_result_carries_gaps(make_client):
    stream_body = sse(
        event("output", {"offset": 3, "stream": "stdout", "data": base64.b64encode(b"a").decode()}),
        event("gap", {"offset": 4}),
        event("exit", {"state": "exited", "exit_code": 0, "truncated": True}),
    )
    routes = {
        ("POST", "/v1/sandboxes/sb-1/execs"): lambda r: httpx.Response(201, json=exec_payload()),
        ("GET", "/v1/execs/ex-1/output"): lambda r: httpx.Response(
            200, content=stream_body, headers={"content-type": "text/event-stream"}
        ),
    }
    client, _ = make_client(routes)
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    result = sb.exec("whatever")
    assert result.gaps == [4]


def test_iterating_a_stream_twice_does_not_duplicate_gaps(make_client):
    stream_body = sse(
        event("output", {"offset": 3, "stream": "stdout", "data": base64.b64encode(b"a").decode()}),
        event("gap", {"offset": 4}),
        event("exit", {"state": "exited", "exit_code": 0, "truncated": True}),
    )
    routes = {
        ("POST", "/v1/sandboxes/sb-1/execs"): lambda r: httpx.Response(201, json=exec_payload()),
        ("GET", "/v1/execs/ex-1/output"): lambda r: httpx.Response(
            200, content=stream_body, headers={"content-type": "text/event-stream"}
        ),
    }
    client, _ = make_client(routes)
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    stream = sb.exec("whatever", stream=True)
    list(stream)
    list(stream)
    assert stream.gaps == [4]


def test_streaming_error_status_raises_the_mapped_error(make_client):
    routes = {
        ("POST", "/v1/sandboxes/sb-1/execs"): lambda r: httpx.Response(201, json=exec_payload()),
        ("GET", "/v1/execs/ex-1/output"): lambda r: httpx.Response(
            404, json={"error": {"code": "exec_not_found", "message": "no such exec", "details": {}}}
        ),
    }
    client, _ = make_client(routes)
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    stream = sb.exec("whatever", stream=True)
    with pytest.raises(orcal.errors.ExecNotFound):
        list(stream)
