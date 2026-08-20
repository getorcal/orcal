import itertools

import httpx

import orcal
from tests.conftest import sandbox_payload


def snapshot_payload(snapshot_id="sn-1"):
    return {
        "id": snapshot_id,
        "sandbox_id": "sb-1",
        "runtime_ref": "ref-1",
        "image": "alpine:3.20",
        "size_bytes": 1024,
        "created_at": "2026-08-20T10:00:00Z",
    }


def event_payload(event_id="ev-1"):
    return {
        "id": event_id,
        "ts": "2026-08-20T10:00:00Z",
        "action": "sandbox.create",
        "request_id": "req-1",
        "status": 201,
    }


def exec_payload(exec_id="ex-1"):
    return {
        "id": exec_id,
        "sandbox_id": "sb-1",
        "command": ["sh", "-c", "echo hi"],
        "state": "running",
        "output_bytes": 0,
        "truncated": False,
        "started_at": "2026-08-20T10:00:00Z",
    }


def test_iterating_follows_the_cursor_without_overlap_or_gap(make_client):
    def page(request):
        cursor = request.url.params.get("cursor")
        if cursor is None:
            return httpx.Response(200, json={"items": [sandbox_payload("sb-1"), sandbox_payload("sb-2")], "next_cursor": "sb-2"})
        if cursor == "sb-2":
            return httpx.Response(200, json={"items": [sandbox_payload("sb-3")]})
        raise AssertionError(f"unexpected cursor {cursor}")

    client, recorder = make_client({("GET", "/v1/sandboxes"): page})
    ids = [sb.id for sb in client.sandboxes()]
    assert ids == ["sb-1", "sb-2", "sb-3"]
    assert len([c for c in recorder.calls if c[1] == "/v1/sandboxes"]) == 2


def test_a_single_page_terminates(make_client):
    client, _ = make_client(
        {("GET", "/v1/sandboxes"): lambda r: httpx.Response(200, json={"items": [sandbox_payload("sb-1")]})}
    )
    assert [sb.id for sb in client.sandboxes()] == ["sb-1"]


def test_an_empty_page_yields_nothing(make_client):
    client, _ = make_client({("GET", "/v1/sandboxes"): lambda r: httpx.Response(200, json={"items": []})})
    assert list(client.sandboxes()) == []


def test_a_server_that_echoes_its_cursor_does_not_loop_forever(make_client):
    def page(request):
        return httpx.Response(200, json={"items": [sandbox_payload("sb-1")], "next_cursor": "same"})

    client, recorder = make_client({("GET", "/v1/sandboxes"): page})
    ids = [sb.id for sb in itertools.islice(client.sandboxes(), 10)]
    assert ids == ["sb-1", "sb-1"]
    assert len([c for c in recorder.calls if c[1] == "/v1/sandboxes"]) == 2


def test_orcal_snapshots_paginates(make_client):
    client, _ = make_client(
        {
            ("GET", "/v1/snapshots"): lambda r: httpx.Response(
                200, json={"items": [snapshot_payload("sn-1"), snapshot_payload("sn-2")]}
            )
        }
    )
    assert [sn.id for sn in client.snapshots()] == ["sn-1", "sn-2"]


def test_orcal_events_preserves_the_servers_order(make_client):
    items = [event_payload("ev-b"), event_payload("ev-a")]
    client, _ = make_client({("GET", "/v1/events"): lambda r: httpx.Response(200, json={"items": items})})
    assert [e.id for e in client.events()] == ["ev-b", "ev-a"]


def test_sandbox_execs_paginates(make_client):
    routes = {
        ("GET", "/v1/sandboxes/sb-1/execs"): lambda r: httpx.Response(
            200, json={"items": [exec_payload("ex-1"), exec_payload("ex-2")]}
        )
    }
    client, _ = make_client(routes)
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    assert [ex.id for ex in sb.execs()] == ["ex-1", "ex-2"]
