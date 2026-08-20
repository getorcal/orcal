import base64
from dataclasses import dataclass, field

from . import models
from ._sse import SSEParser


@dataclass
class Frame:
    offset: int
    stream: str
    data: bytes


@dataclass
class ExecResult:
    id: str
    stdout: str
    stderr: str
    exit_code: int | None
    truncated: bool
    raw: models.Exec
    gaps: list[int] = field(default_factory=list)


class ExecStream:
    def __init__(self, client, exec_id, from_offset=0):
        self._client = client
        self.id = exec_id
        self._from = from_offset
        self.exit_code = None
        self.truncated = False
        self.state = None
        self.gaps = []

    def __iter__(self):
        self.gaps = []
        parser = SSEParser()
        path = f"/v1/execs/{self._client._transport.escape(self.id)}/output"
        with self._client._transport.stream("GET", path, params={"from": self._from}) as response:
            if response.status_code >= 400:
                response.read()
                from . import errors

                raise errors.from_response(response.status_code, response.content)
            for chunk in response.iter_bytes():
                for name, payload in parser.feed(chunk):
                    if name == "output":
                        self._from = payload["offset"]
                        yield Frame(payload["offset"], payload.get("stream", "stdout"), base64.b64decode(payload["data"]))
                    elif name == "gap":
                        self._from = payload["offset"]
                        self.gaps.append(payload["offset"])
                    elif name == "exit":
                        self.state = payload.get("state")
                        self.exit_code = payload.get("exit_code")
                        self.truncated = bool(payload.get("truncated"))
                        return
