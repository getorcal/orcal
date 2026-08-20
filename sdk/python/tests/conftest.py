import json
import os

import httpx
import pytest

import orcal


def sandbox_payload(sandbox_id="sb-1", name="demo", network="full"):
    return {
        "id": sandbox_id,
        "name": name,
        "image": "alpine:3.20",
        "state": "running",
        "runtime": "docker",
        "network": network,
        "oci_runtime": "runc",
        "resources": {"cpu_millis": 1000, "memory_bytes": 1073741824, "pids_limit": 512},
        "created_at": "2026-08-20T10:00:00Z",
        "updated_at": "2026-08-20T10:00:00Z",
    }


class Recorder:
    def __init__(self, routes):
        self.routes = routes
        self.calls = []

    def __call__(self, request):
        path = request.url.raw_path.decode().split("?", 1)[0]
        self.calls.append((request.method, path, request.content))
        key = (request.method, path)
        handler = self.routes.get(key)
        if handler is None:
            return httpx.Response(404, json={"error": {"code": "sandbox_not_found", "message": str(key), "details": {}}})
        return handler(request)


@pytest.fixture
def make_client():
    def factory(routes):
        recorder = Recorder(routes)
        client = orcal.Orcal("http://example.test", "orcal_secret", transport=httpx.MockTransport(recorder))
        return client, recorder

    return factory


@pytest.fixture(scope="session")
def live():
    url = os.environ.get("ORCAL_URL")
    token = os.environ.get("ORCAL_TOKEN")
    if not url or not token:
        pytest.fail(
            "ORCAL_URL and ORCAL_TOKEN must be set. These tests run against a real orcald and "
            "must fail rather than skip when one is unreachable: a suite that skips quietly is "
            "a suite that never ran."
        )
    client = orcal.Orcal(url, token)
    try:
        client.version()
    except Exception as exc:
        pytest.fail(f"no orcald reachable at {url}: {exc}")
    yield client
    client.close()


@pytest.fixture
def image():
    return os.environ.get("ORCAL_TEST_IMAGE", "alpine:3.20")
