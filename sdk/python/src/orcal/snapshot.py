from . import models
from ._build import build


class Snapshot:
    def __init__(self, client, raw):
        self._client = client
        self.raw = raw

    @classmethod
    def _from_payload(cls, client, payload):
        return cls(client, build(models.Snapshot, payload))

    @property
    def id(self):
        return self.raw.id

    @property
    def name(self):
        return self.raw.name

    def delete(self):
        ref = self._client._transport.escape(self.raw.id)
        self._client._transport.request("DELETE", f"/v1/snapshots/{ref}")

    def fork(self, **opts):
        return self._client.sandbox(snapshot=self.raw.id, **opts)
