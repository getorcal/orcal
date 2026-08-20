from . import models


class SandboxFiles:
    def __init__(self, client, ref):
        self._client = client
        self._ref = ref

    def _path(self, suffix=""):
        return f"/v1/sandboxes/{self._client._transport.escape(self._ref)}/files{suffix}"

    def read(self, path):
        return self._client._transport.request("GET", self._path(), params={"path": path}).content

    def write(self, path, content):
        payload = content.encode() if isinstance(content, str) else content
        self._client._transport.request("PUT", self._path(), params={"path": path}, content=payload)

    def stat(self, path):
        body = self._client._transport.request("GET", self._path("/stat"), params={"path": path}).json()
        return models.FileInfo(**body)

    def list(self, path):
        body = self._client._transport.request("GET", self._path("/list"), params={"path": path}).json()
        return [models.FileInfo(**entry) for entry in body.get("items", [])]

    def download(self, path):
        archive = f"/v1/sandboxes/{self._client._transport.escape(self._ref)}/archive"
        return self._client._transport.request("GET", archive, params={"path": path}).content

    def upload(self, path, tar_bytes):
        archive = f"/v1/sandboxes/{self._client._transport.escape(self._ref)}/archive"
        self._client._transport.request("PUT", archive, params={"path": path}, content=tar_bytes)


class Sandbox:
    def __init__(self, client, raw):
        self._client = client
        self.raw = raw
        self.files = SandboxFiles(client, raw.id)

    @classmethod
    def _from_payload(cls, client, payload):
        return cls(client, models.Sandbox(**payload))

    @property
    def id(self):
        return self.raw.id

    @property
    def name(self):
        return self.raw.name

    @property
    def state(self):
        return self.raw.state

    @property
    def network(self):
        return self.raw.network

    @property
    def oci_runtime(self):
        return getattr(self.raw, "oci_runtime", None)

    def _ref(self):
        return self._client._transport.escape(self.raw.id)

    def _replace(self, payload):
        self.raw = models.Sandbox(**payload)
        return self

    def refresh(self):
        return self._replace(self._client._transport.request("GET", f"/v1/sandboxes/{self._ref()}").json())

    def start(self):
        return self._replace(self._client._transport.request("POST", f"/v1/sandboxes/{self._ref()}/start").json())

    def stop(self):
        return self._replace(self._client._transport.request("POST", f"/v1/sandboxes/{self._ref()}/stop").json())

    def destroy(self):
        return self._replace(self._client._transport.request("DELETE", f"/v1/sandboxes/{self._ref()}").json())

    def snapshot(self, name=None):
        from .snapshot import Snapshot

        body = {} if name is None else {"name": name}
        payload = self._client._transport.request(
            "POST", f"/v1/sandboxes/{self._ref()}/snapshots", json=body
        ).json()
        return Snapshot._from_payload(self._client, payload)

    def restore(self, snapshot):
        ref = snapshot.id if hasattr(snapshot, "id") else snapshot
        payload = self._client._transport.request(
            "POST", f"/v1/sandboxes/{self._ref()}/restore", json={"snapshot": ref}
        ).json()
        return self._replace(payload)

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc, tb):
        self.destroy()
        return False
