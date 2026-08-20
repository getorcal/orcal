import json

import httpx
import pytest

import orcal
from tests.conftest import file_info_payload, sandbox_payload, snapshot_payload, token_payload


def verbs(recorder):
    return [(method, path) for method, path, _ in recorder.calls]


def body_of(recorder, method, path):
    for seen_method, seen_path, content in recorder.calls:
        if (seen_method, seen_path) == (method, path):
            return json.loads(content)
    raise AssertionError(f"no {method} {path} in {verbs(recorder)}")


def exec_payload(exec_id="ex-1"):
    return {
        "id": exec_id,
        "sandbox_id": "sb-1",
        "command": ["sh", "-c", "echo hi"],
        "state": "exited",
        "output_bytes": 2,
        "truncated": False,
        "started_at": "2026-08-20T10:00:00Z",
        "exit_code": 0,
    }


def event_payload(event_id="ev-1"):
    return {
        "id": event_id,
        "ts": "2026-08-20T10:00:00Z",
        "action": "sandbox.create",
        "request_id": "req-1",
        "status": 201,
    }


def extra(payload):
    return dict(payload, brand_new_field="x")


def test_sandbox_context_manager_destroys_on_exit(make_client):
    routes = {
        ("POST", "/v1/sandboxes"): lambda r: httpx.Response(201, json=sandbox_payload()),
        ("DELETE", "/v1/sandboxes/sb-1"): lambda r: httpx.Response(200, json=sandbox_payload()),
    }
    client, recorder = make_client(routes)
    with client.sandbox(image="alpine:3.20") as sb:
        assert sb.id == "sb-1"
    assert ("DELETE", "/v1/sandboxes/sb-1") in verbs(recorder)


def test_sandbox_is_destroyed_even_when_the_body_raises(make_client):
    routes = {
        ("POST", "/v1/sandboxes"): lambda r: httpx.Response(201, json=sandbox_payload()),
        ("DELETE", "/v1/sandboxes/sb-1"): lambda r: httpx.Response(200, json=sandbox_payload()),
    }
    client, recorder = make_client(routes)
    with pytest.raises(ZeroDivisionError):
        with client.sandbox(image="alpine:3.20"):
            1 / 0
    assert ("DELETE", "/v1/sandboxes/sb-1") in verbs(recorder)


def test_healthz_gets_the_status(make_client):
    routes = {("GET", "/v1/healthz"): lambda r: httpx.Response(200, json={"status": "ok"})}
    client, recorder = make_client(routes)
    assert client.healthz() == {"status": "ok"}
    assert recorder.calls == [("GET", "/v1/healthz", b"")]


def test_version_gets_the_version_resource(make_client):
    routes = {("GET", "/v1/version"): lambda r: httpx.Response(200, json={"version": "1.2.3", "api_versions": ["v1"]})}
    client, recorder = make_client(routes)
    result = client.version()
    assert result.version == "1.2.3"
    assert result.api_versions == ["v1"]
    assert recorder.calls == [("GET", "/v1/version", b"")]


def test_create_posts_every_supplied_option_to_the_sandbox_collection(make_client):
    client, recorder = make_client({("POST", "/v1/sandboxes"): lambda r: httpx.Response(201, json=sandbox_payload())})
    client.sandbox(
        name="demo",
        image="alpine:3.20",
        snapshot="snap-1",
        network="none",
        cpu_millis=500,
        memory_bytes=268435456,
        pids_limit=32,
        env={"A": "1"},
        labels={"team": "x"},
    )
    assert verbs(recorder) == [("POST", "/v1/sandboxes")]
    assert body_of(recorder, "POST", "/v1/sandboxes") == {
        "name": "demo",
        "image": "alpine:3.20",
        "snapshot": "snap-1",
        "network": "none",
        "cpu_millis": 500,
        "memory_bytes": 268435456,
        "pids_limit": 32,
        "env": {"A": "1"},
        "labels": {"team": "x"},
    }


def test_create_omits_the_options_that_were_not_supplied(make_client):
    client, recorder = make_client({("POST", "/v1/sandboxes"): lambda r: httpx.Response(201, json=sandbox_payload())})
    client.sandbox(image="alpine:3.20", memory_bytes=None)
    assert body_of(recorder, "POST", "/v1/sandboxes") == {"image": "alpine:3.20"}


def test_network_none_is_sent_on_create(make_client):
    client, recorder = make_client(
        {("POST", "/v1/sandboxes"): lambda r: httpx.Response(201, json=sandbox_payload(network="none"))}
    )
    sb = client.sandbox(image="alpine:3.20", network="none")
    assert body_of(recorder, "POST", "/v1/sandboxes")["network"] == "none"
    assert sb.network == "none"


def test_get_sandbox_gets_the_sandbox_resource(make_client):
    client, recorder = make_client({("GET", "/v1/sandboxes/sb-1"): lambda r: httpx.Response(200, json=sandbox_payload())})
    sb = client.get_sandbox("sb-1")
    assert sb.id == "sb-1"
    assert sb.state == "running"
    assert sb.oci_runtime == "runc"
    assert sb.raw.image == "alpine:3.20"
    assert recorder.calls == [("GET", "/v1/sandboxes/sb-1", b"")]


def test_sandbox_ref_is_escaped_in_the_path(make_client):
    client, recorder = make_client({("GET", "/v1/sandboxes/a%2Fb"): lambda r: httpx.Response(200, json=sandbox_payload())})
    client.get_sandbox("a/b")
    assert verbs(recorder) == [("GET", "/v1/sandboxes/a%2Fb")]


def test_get_snapshot_gets_the_snapshot_resource(make_client):
    client, recorder = make_client({("GET", "/v1/snapshots/snap-1"): lambda r: httpx.Response(200, json=snapshot_payload())})
    snap = client.get_snapshot("snap-1")
    assert snap.id == "snap-1"
    assert snap.name == "v1"
    assert recorder.calls == [("GET", "/v1/snapshots/snap-1", b"")]


def test_snapshot_ref_is_escaped_in_the_path(make_client):
    client, recorder = make_client({("GET", "/v1/snapshots/a%2Fb"): lambda r: httpx.Response(200, json=snapshot_payload())})
    client.get_snapshot("a/b")
    assert verbs(recorder) == [("GET", "/v1/snapshots/a%2Fb")]


def test_get_exec_fetches_by_id(make_client):
    client, recorder = make_client({("GET", "/v1/execs/ex-1"): lambda r: httpx.Response(200, json=exec_payload())})
    ex = client.get_exec("ex-1")
    assert ex.id == "ex-1"
    assert ex.exit_code == 0
    assert recorder.calls == [("GET", "/v1/execs/ex-1", b"")]


def test_exec_id_is_escaped_in_the_path(make_client):
    client, recorder = make_client({("GET", "/v1/execs/a%2Fb"): lambda r: httpx.Response(200, json=exec_payload())})
    client.get_exec("a/b")
    assert verbs(recorder) == [("GET", "/v1/execs/a%2Fb")]


def test_create_token_posts_the_name_scopes_and_expiry(make_client):
    created = dict(token_payload(), token="orcal_secretvalue")
    client, recorder = make_client({("POST", "/v1/tokens"): lambda r: httpx.Response(201, json=created)})
    result = client.create_token("ci", ("admin",), 3600)
    assert result.token == "orcal_secretvalue"
    assert result.id == "tok-1"
    assert verbs(recorder) == [("POST", "/v1/tokens")]
    assert body_of(recorder, "POST", "/v1/tokens") == {"name": "ci", "scopes": ["admin"], "expires_in_seconds": 3600}


def test_create_token_omits_the_expiry_when_it_is_not_supplied(make_client):
    created = dict(token_payload(), token="orcal_secretvalue")
    client, recorder = make_client({("POST", "/v1/tokens"): lambda r: httpx.Response(201, json=created)})
    client.create_token("ci", ["admin"])
    assert body_of(recorder, "POST", "/v1/tokens") == {"name": "ci", "scopes": ["admin"]}


def test_create_token_posts_every_scope_it_was_given(make_client):
    created = dict(token_payload(), token="orcal_secretvalue")
    client, recorder = make_client({("POST", "/v1/tokens"): lambda r: httpx.Response(201, json=created)})
    client.create_token("ci", ["sandboxes:read", "exec", "files:write"])
    assert body_of(recorder, "POST", "/v1/tokens")["scopes"] == ["sandboxes:read", "exec", "files:write"]


def test_list_tokens_gets_the_collection_and_unwraps_items(make_client):
    client, recorder = make_client({("GET", "/v1/tokens"): lambda r: httpx.Response(200, json={"items": [token_payload()]})})
    tokens = client.list_tokens()
    assert [t.id for t in tokens] == ["tok-1"]
    assert tokens[0].prefix == "orcal_"
    assert recorder.calls == [("GET", "/v1/tokens", b"")]


def test_list_tokens_is_empty_when_the_daemon_sends_no_items(make_client):
    client, _ = make_client({("GET", "/v1/tokens"): lambda r: httpx.Response(200, json={})})
    assert client.list_tokens() == []


def test_revoke_token_deletes_the_token_resource(make_client):
    client, recorder = make_client({("DELETE", "/v1/tokens/tok%2F1"): lambda r: httpx.Response(204)})
    client.revoke_token("tok/1")
    assert verbs(recorder) == [("DELETE", "/v1/tokens/tok%2F1")]


def test_start_posts_to_the_start_action(make_client):
    started = dict(sandbox_payload(), state="running")
    client, recorder = make_client({("POST", "/v1/sandboxes/sb-1/start"): lambda r: httpx.Response(200, json=started)})
    sb = orcal.Sandbox._from_payload(client, dict(sandbox_payload(), state="stopped"))
    sb.start()
    assert verbs(recorder) == [("POST", "/v1/sandboxes/sb-1/start")]
    assert sb.state == "running"


def test_stop_posts_to_the_stop_action(make_client):
    stopped = dict(sandbox_payload(), state="stopped")
    client, recorder = make_client({("POST", "/v1/sandboxes/sb-1/stop"): lambda r: httpx.Response(200, json=stopped)})
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    sb.stop()
    assert verbs(recorder) == [("POST", "/v1/sandboxes/sb-1/stop")]
    assert sb.state == "stopped"


def test_refresh_gets_the_sandbox_resource(make_client):
    fresh = dict(sandbox_payload(), state="stopped")
    client, recorder = make_client({("GET", "/v1/sandboxes/sb-1"): lambda r: httpx.Response(200, json=fresh)})
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    sb.refresh()
    assert verbs(recorder) == [("GET", "/v1/sandboxes/sb-1")]
    assert sb.state == "stopped"


def test_destroy_deletes_the_sandbox_resource(make_client):
    gone = dict(sandbox_payload(), state="destroyed")
    client, recorder = make_client({("DELETE", "/v1/sandboxes/sb-1"): lambda r: httpx.Response(200, json=gone)})
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    sb.destroy()
    assert verbs(recorder) == [("DELETE", "/v1/sandboxes/sb-1")]
    assert sb.state == "destroyed"


def test_snapshot_posts_the_name_to_the_sandbox_snapshots_collection(make_client):
    client, recorder = make_client(
        {("POST", "/v1/sandboxes/sb-1/snapshots"): lambda r: httpx.Response(201, json=snapshot_payload())}
    )
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    snap = sb.snapshot(name="v1")
    assert snap.id == "snap-1"
    assert verbs(recorder) == [("POST", "/v1/sandboxes/sb-1/snapshots")]
    assert body_of(recorder, "POST", "/v1/sandboxes/sb-1/snapshots") == {"name": "v1"}


def test_snapshot_posts_an_empty_body_when_no_name_is_given(make_client):
    client, recorder = make_client(
        {("POST", "/v1/sandboxes/sb-1/snapshots"): lambda r: httpx.Response(201, json=snapshot_payload(name=None))}
    )
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    sb.snapshot()
    assert body_of(recorder, "POST", "/v1/sandboxes/sb-1/snapshots") == {}


def test_restore_posts_a_plain_reference(make_client):
    client, recorder = make_client(
        {("POST", "/v1/sandboxes/sb-1/restore"): lambda r: httpx.Response(200, json=sandbox_payload())}
    )
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    sb.restore("snap-1")
    assert verbs(recorder) == [("POST", "/v1/sandboxes/sb-1/restore")]
    assert body_of(recorder, "POST", "/v1/sandboxes/sb-1/restore") == {"snapshot": "snap-1"}


def test_restore_posts_the_id_of_a_snapshot_object(make_client):
    routes = {
        ("GET", "/v1/snapshots/snap-1"): lambda r: httpx.Response(200, json=snapshot_payload()),
        ("POST", "/v1/sandboxes/sb-1/restore"): lambda r: httpx.Response(200, json=sandbox_payload()),
    }
    client, recorder = make_client(routes)
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    sb.restore(client.get_snapshot("snap-1"))
    assert body_of(recorder, "POST", "/v1/sandboxes/sb-1/restore") == {"snapshot": "snap-1"}


def test_restore_replaces_the_local_sandbox_state(make_client):
    restored = dict(sandbox_payload(), state="stopped")
    client, _ = make_client({("POST", "/v1/sandboxes/sb-1/restore"): lambda r: httpx.Response(200, json=restored)})
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    assert sb.restore("snap-1") is sb
    assert sb.state == "stopped"


def test_snapshot_then_fork_posts_the_snapshot_reference(make_client):
    routes = {
        ("POST", "/v1/sandboxes"): lambda r: httpx.Response(201, json=sandbox_payload(sandbox_id="sb-2", name="forked")),
        ("POST", "/v1/sandboxes/sb-1/snapshots"): lambda r: httpx.Response(201, json=snapshot_payload()),
    }
    client, recorder = make_client(routes)
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    snap = sb.snapshot(name="v1")
    forked = snap.fork(name="forked")
    assert forked.id == "sb-2"
    assert body_of(recorder, "POST", "/v1/sandboxes") == {"name": "forked", "snapshot": "snap-1"}


def test_snapshot_delete_deletes_the_snapshot_resource(make_client):
    client, recorder = make_client({("DELETE", "/v1/snapshots/snap%2F1"): lambda r: httpx.Response(204)})
    snap = orcal.Snapshot._from_payload(client, snapshot_payload(snapshot_id="snap/1"))
    snap.delete()
    assert verbs(recorder) == [("DELETE", "/v1/snapshots/snap%2F1")]


def test_files_reach_every_endpoint_with_the_sandbox_ref_escaped(make_client):
    routes = {
        ("GET", "/v1/sandboxes/a%2Fb/files"): lambda r: httpx.Response(200, content=b"\x01"),
        ("PUT", "/v1/sandboxes/a%2Fb/files"): lambda r: httpx.Response(204),
        ("GET", "/v1/sandboxes/a%2Fb/files/stat"): lambda r: httpx.Response(200, json=file_info_payload()),
        ("GET", "/v1/sandboxes/a%2Fb/files/list"): lambda r: httpx.Response(200, json={"items": [], "truncated": False}),
        ("GET", "/v1/sandboxes/a%2Fb/archive"): lambda r: httpx.Response(200, content=b"\x02"),
        ("PUT", "/v1/sandboxes/a%2Fb/archive"): lambda r: httpx.Response(204),
    }
    client, recorder = make_client(routes)
    sb = orcal.Sandbox._from_payload(client, sandbox_payload(sandbox_id="a/b"))
    sb.files.read("/x")
    sb.files.write("/x", "hi")
    sb.files.stat("/x")
    sb.files.list("/x")
    sb.files.download("/x")
    sb.files.upload("/x", b"\x01")
    assert verbs(recorder) == [
        ("GET", "/v1/sandboxes/a%2Fb/files"),
        ("PUT", "/v1/sandboxes/a%2Fb/files"),
        ("GET", "/v1/sandboxes/a%2Fb/files/stat"),
        ("GET", "/v1/sandboxes/a%2Fb/files/list"),
        ("GET", "/v1/sandboxes/a%2Fb/archive"),
        ("PUT", "/v1/sandboxes/a%2Fb/archive"),
    ]


def test_the_file_path_travels_as_an_encoded_query_parameter(make_client):
    client, recorder = make_client({("GET", "/v1/sandboxes/sb-1/files"): lambda r: httpx.Response(200, content=b"x")})
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    sb.files.read("/tmp/a b/../c.txt")
    request = recorder.requests[-1]
    assert request.url.params.get("path") == "/tmp/a b/../c.txt"
    assert request.url.raw_path.decode().endswith("?path=%2Ftmp%2Fa+b%2F..%2Fc.txt")


def test_files_read_returns_bytes(make_client):
    client, recorder = make_client(
        {("GET", "/v1/sandboxes/sb-1/files"): lambda r: httpx.Response(200, content=b"\x00\x01binary")}
    )
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    assert sb.files.read("/app/a.bin") == b"\x00\x01binary"
    assert verbs(recorder) == [("GET", "/v1/sandboxes/sb-1/files")]


def test_files_write_puts_str_and_bytes_unchanged(make_client):
    client, recorder = make_client({("PUT", "/v1/sandboxes/sb-1/files"): lambda r: httpx.Response(204)})
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    sb.files.write("/app/a.txt", "hello")
    sb.files.write("/app/b.txt", b"hello")
    assert [content for _, _, content in recorder.calls] == [b"hello", b"hello"]
    assert verbs(recorder) == [("PUT", "/v1/sandboxes/sb-1/files")] * 2


def test_files_stat_returns_the_entry(make_client):
    client, recorder = make_client(
        {("GET", "/v1/sandboxes/sb-1/files/stat"): lambda r: httpx.Response(200, json=file_info_payload())}
    )
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    info = sb.files.stat("/a.txt")
    assert info.name == "a.txt"
    assert info.size == 5
    assert info.is_dir is False
    assert verbs(recorder) == [("GET", "/v1/sandboxes/sb-1/files/stat")]


def test_files_list_returns_the_entries(make_client):
    listing = {"items": [file_info_payload("a.txt"), file_info_payload("b", is_dir=True)], "truncated": False}
    client, recorder = make_client({("GET", "/v1/sandboxes/sb-1/files/list"): lambda r: httpx.Response(200, json=listing)})
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    entries = sb.files.list("/")
    assert [entry.name for entry in entries] == ["a.txt", "b"]
    assert verbs(recorder) == [("GET", "/v1/sandboxes/sb-1/files/list")]


def test_files_list_reports_a_truncated_listing(make_client):
    listing = {"items": [file_info_payload("a.txt")], "truncated": True}
    client, _ = make_client({("GET", "/v1/sandboxes/sb-1/files/list"): lambda r: httpx.Response(200, json=listing)})
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    assert sb.files.list("/").truncated is True


def test_files_list_reports_a_complete_listing(make_client):
    listing = {"items": [file_info_payload("a.txt")], "truncated": False}
    client, _ = make_client({("GET", "/v1/sandboxes/sb-1/files/list"): lambda r: httpx.Response(200, json=listing)})
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    assert sb.files.list("/").truncated is False


def test_files_download_gets_the_archive_endpoint(make_client):
    client, recorder = make_client(
        {("GET", "/v1/sandboxes/sb-1/archive"): lambda r: httpx.Response(200, content=b"\x09\x08\x07")}
    )
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    assert sb.files.download("/dir") == b"\x09\x08\x07"
    assert verbs(recorder) == [("GET", "/v1/sandboxes/sb-1/archive")]


def test_files_upload_puts_the_tar_bytes_to_the_archive_endpoint(make_client):
    client, recorder = make_client({("PUT", "/v1/sandboxes/sb-1/archive"): lambda r: httpx.Response(204)})
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    sb.files.upload("/dir", b"\x01\x02\x03")
    assert recorder.calls == [("PUT", "/v1/sandboxes/sb-1/archive", b"\x01\x02\x03")]


def test_a_file_listing_equals_the_plain_list_of_its_entries(make_client):
    entries = [file_info_payload("a.txt"), file_info_payload("b.txt")]
    listing = {"items": entries, "truncated": True}
    client, _ = make_client({("GET", "/v1/sandboxes/sb-1/files/list"): lambda r: httpx.Response(200, json=listing)})
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    result = sb.files.list("/")
    assert result == [orcal.models.FileInfo(**entry) for entry in entries]
    assert result.truncated is True


FILE_OPERATIONS = {
    "read": (
        ("GET", "/v1/sandboxes/sb-1/files"),
        lambda r: httpx.Response(200, content=b"x"),
        lambda files, path: files.read(path),
    ),
    "write": (
        ("PUT", "/v1/sandboxes/sb-1/files"),
        lambda r: httpx.Response(204),
        lambda files, path: files.write(path, "hi"),
    ),
    "stat": (
        ("GET", "/v1/sandboxes/sb-1/files/stat"),
        lambda r: httpx.Response(200, json=file_info_payload()),
        lambda files, path: files.stat(path),
    ),
    "list": (
        ("GET", "/v1/sandboxes/sb-1/files/list"),
        lambda r: httpx.Response(200, json={"items": [], "truncated": False}),
        lambda files, path: files.list(path),
    ),
    "download": (
        ("GET", "/v1/sandboxes/sb-1/archive"),
        lambda r: httpx.Response(200, content=b"x"),
        lambda files, path: files.download(path),
    ),
    "upload": (
        ("PUT", "/v1/sandboxes/sb-1/archive"),
        lambda r: httpx.Response(204),
        lambda files, path: files.upload(path, b"\x01"),
    ),
}


@pytest.mark.parametrize("operation", sorted(FILE_OPERATIONS))
def test_a_file_operation_asks_for_the_path_it_was_given(make_client, operation):
    route, responder, call = FILE_OPERATIONS[operation]
    method, path = route
    client, recorder = make_client({route: responder})
    sb = orcal.Sandbox._from_payload(client, sandbox_payload())
    call(sb.files, "/app/config.json")
    assert recorder.targets == [(method, f"{path}?path=%2Fapp%2Fconfig.json")]


ADDITIVE_CASES = {
    "version": (
        {("GET", "/v1/version"): lambda r: httpx.Response(200, json=extra({"version": "1", "api_versions": ["v1"]}))},
        lambda c: c.version(),
    ),
    "sandbox": (
        {("POST", "/v1/sandboxes"): lambda r: httpx.Response(201, json=extra(sandbox_payload()))},
        lambda c: c.sandbox(image="alpine:3.20"),
    ),
    "get_sandbox": (
        {("GET", "/v1/sandboxes/sb-1"): lambda r: httpx.Response(200, json=extra(sandbox_payload()))},
        lambda c: c.get_sandbox("sb-1"),
    ),
    "get_snapshot": (
        {("GET", "/v1/snapshots/snap-1"): lambda r: httpx.Response(200, json=extra(snapshot_payload()))},
        lambda c: c.get_snapshot("snap-1"),
    ),
    "get_exec": (
        {("GET", "/v1/execs/ex-1"): lambda r: httpx.Response(200, json=extra(exec_payload()))},
        lambda c: c.get_exec("ex-1"),
    ),
    "create_token": (
        {("POST", "/v1/tokens"): lambda r: httpx.Response(201, json=extra(dict(token_payload(), token="t")))},
        lambda c: c.create_token("ci", ["admin"]),
    ),
    "list_tokens": (
        {("GET", "/v1/tokens"): lambda r: httpx.Response(200, json={"items": [extra(token_payload())]})},
        lambda c: c.list_tokens(),
    ),
    "sandboxes": (
        {("GET", "/v1/sandboxes"): lambda r: httpx.Response(200, json={"items": [extra(sandbox_payload())]})},
        lambda c: list(c.sandboxes()),
    ),
    "snapshots": (
        {("GET", "/v1/snapshots"): lambda r: httpx.Response(200, json={"items": [extra(snapshot_payload())]})},
        lambda c: list(c.snapshots()),
    ),
    "events": (
        {("GET", "/v1/events"): lambda r: httpx.Response(200, json={"items": [extra(event_payload())]})},
        lambda c: list(c.events()),
    ),
    "refresh": (
        {("GET", "/v1/sandboxes/sb-1"): lambda r: httpx.Response(200, json=extra(sandbox_payload()))},
        lambda c: orcal.Sandbox._from_payload(c, sandbox_payload()).refresh(),
    ),
    "start": (
        {("POST", "/v1/sandboxes/sb-1/start"): lambda r: httpx.Response(200, json=extra(sandbox_payload()))},
        lambda c: orcal.Sandbox._from_payload(c, sandbox_payload()).start(),
    ),
    "stop": (
        {("POST", "/v1/sandboxes/sb-1/stop"): lambda r: httpx.Response(200, json=extra(sandbox_payload()))},
        lambda c: orcal.Sandbox._from_payload(c, sandbox_payload()).stop(),
    ),
    "destroy": (
        {("DELETE", "/v1/sandboxes/sb-1"): lambda r: httpx.Response(200, json=extra(sandbox_payload()))},
        lambda c: orcal.Sandbox._from_payload(c, sandbox_payload()).destroy(),
    ),
    "restore": (
        {("POST", "/v1/sandboxes/sb-1/restore"): lambda r: httpx.Response(200, json=extra(sandbox_payload()))},
        lambda c: orcal.Sandbox._from_payload(c, sandbox_payload()).restore("snap-1"),
    ),
    "sandbox_snapshot": (
        {("POST", "/v1/sandboxes/sb-1/snapshots"): lambda r: httpx.Response(201, json=extra(snapshot_payload()))},
        lambda c: orcal.Sandbox._from_payload(c, sandbox_payload()).snapshot(),
    ),
    "sandbox_snapshots": (
        {("GET", "/v1/sandboxes/sb-1/snapshots"): lambda r: httpx.Response(200, json={"items": [extra(snapshot_payload())]})},
        lambda c: list(orcal.Sandbox._from_payload(c, sandbox_payload()).snapshots()),
    ),
    "sandbox_execs": (
        {("GET", "/v1/sandboxes/sb-1/execs"): lambda r: httpx.Response(200, json={"items": [extra(exec_payload())]})},
        lambda c: list(orcal.Sandbox._from_payload(c, sandbox_payload()).execs()),
    ),
    "sandbox_exec": (
        {
            ("POST", "/v1/sandboxes/sb-1/execs"): lambda r: httpx.Response(201, json=extra(exec_payload())),
            ("GET", "/v1/execs/ex-1/output"): lambda r: httpx.Response(
                200,
                content=b'event: exit\ndata: {"state": "exited", "exit_code": 0, "truncated": false}\n\n',
                headers={"content-type": "text/event-stream"},
            ),
        },
        lambda c: orcal.Sandbox._from_payload(c, sandbox_payload()).exec("echo hi"),
    ),
    "files_stat": (
        {("GET", "/v1/sandboxes/sb-1/files/stat"): lambda r: httpx.Response(200, json=extra(file_info_payload()))},
        lambda c: orcal.Sandbox._from_payload(c, sandbox_payload()).files.stat("/a.txt"),
    ),
    "files_list": (
        {
            ("GET", "/v1/sandboxes/sb-1/files/list"): lambda r: httpx.Response(
                200, json={"items": [extra(file_info_payload())], "truncated": False, "brand_new_field": "x"}
            )
        },
        lambda c: orcal.Sandbox._from_payload(c, sandbox_payload()).files.list("/"),
    ),
}


@pytest.mark.parametrize("case", sorted(ADDITIVE_CASES))
def test_a_response_field_the_model_does_not_declare_does_not_break_the_call(make_client, case):
    routes, call = ADDITIVE_CASES[case]
    client, _ = make_client(routes)
    call(client)
