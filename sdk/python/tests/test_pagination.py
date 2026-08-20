import httpx

from tests.conftest import sandbox_payload


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
