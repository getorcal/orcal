import json


class SSEParser:
    def __init__(self):
        self._buffer = b""

    def feed(self, chunk):
        self._buffer += chunk
        events = []
        while b"\n\n" in self._buffer:
            block, self._buffer = self._buffer.split(b"\n\n", 1)
            parsed = self._parse_block(block)
            if parsed is not None:
                events.append(parsed)
        return events

    @staticmethod
    def _parse_block(block):
        name = None
        data_lines = []
        for raw in block.split(b"\n"):
            line = raw.decode("utf-8", errors="replace")
            if not line or line.startswith(":"):
                continue
            field, _, value = line.partition(":")
            value = value[1:] if value.startswith(" ") else value
            if field == "event":
                name = value
            elif field == "data":
                data_lines.append(value)
        if name is None or not data_lines:
            return None
        try:
            payload = json.loads("\n".join(data_lines))
        except ValueError:
            return None
        return (name, payload)
