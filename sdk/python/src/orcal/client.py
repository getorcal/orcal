from . import models
from ._transport import Transport
from .sandbox import Sandbox
from .snapshot import Snapshot

_CREATE_FIELDS = ("name", "image", "snapshot", "network", "cpu_millis", "memory_bytes", "pids_limit", "env", "labels")


class Orcal:
    def __init__(self, url, token, timeout=30.0, transport=None):
        self._transport = Transport(url, token, timeout=timeout, transport=transport)

    def close(self):
        self._transport.close()

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc, tb):
        self.close()
        return False

    def version(self):
        return models.Version(**self._transport.request("GET", "/v1/version").json())

    def sandbox(self, **opts):
        body = {key: opts[key] for key in _CREATE_FIELDS if opts.get(key) is not None}
        payload = self._transport.request("POST", "/v1/sandboxes", json=body).json()
        return Sandbox._from_payload(self, payload)

    def get_sandbox(self, ref):
        payload = self._transport.request("GET", f"/v1/sandboxes/{self._transport.escape(ref)}").json()
        return Sandbox._from_payload(self, payload)

    def get_snapshot(self, ref):
        payload = self._transport.request("GET", f"/v1/snapshots/{self._transport.escape(ref)}").json()
        return Snapshot._from_payload(self, payload)

    def create_token(self, name, scopes, expires_in_seconds=None):
        body = {"name": name, "scopes": list(scopes)}
        if expires_in_seconds is not None:
            body["expires_in_seconds"] = expires_in_seconds
        return models.CreatedToken(**self._transport.request("POST", "/v1/tokens", json=body).json())

    def revoke_token(self, token_id):
        self._transport.request("DELETE", f"/v1/tokens/{self._transport.escape(token_id)}")

    def list_tokens(self):
        body = self._transport.request("GET", "/v1/tokens").json()
        return [models.Token(**item) for item in body.get("items", [])]
