import time
import urllib.parse

import httpx

from . import errors

MAX_ATTEMPTS = 3
BACKOFF_SECONDS = 0.1
RETRYABLE_METHODS = frozenset({"GET", "HEAD"})


class Transport:
    def __init__(self, url, token, timeout=30.0, transport=None):
        self._base = url.rstrip("/")
        self._client = httpx.Client(timeout=timeout, transport=transport)
        self._token = token

    @staticmethod
    def escape(segment):
        return urllib.parse.quote(segment, safe="")

    def close(self):
        self._client.close()

    def _headers(self, extra=None):
        headers = {"Authorization": f"Bearer {self._token}"}
        if extra:
            headers.update(extra)
        return headers

    def _send(self, method, path, params, json, content, headers):
        return self._client.request(
            method,
            self._base + path,
            params=params,
            json=json,
            content=content,
            headers=self._headers(headers),
        )

    def request(self, method, path, *, params=None, json=None, content=None, headers=None):
        retryable = method.upper() in RETRYABLE_METHODS
        attempt = 0
        while True:
            attempt += 1
            try:
                response = self._send(method, path, params, json, content, headers)
            except httpx.TransportError:
                if not retryable or attempt >= MAX_ATTEMPTS:
                    raise
                time.sleep(BACKOFF_SECONDS * attempt)
                continue
            if response.status_code < 400:
                return response
            if response.status_code == 503 and retryable and attempt < MAX_ATTEMPTS:
                time.sleep(BACKOFF_SECONDS * attempt)
                continue
            raise errors.from_response(response.status_code, response.content)

    def stream(self, method, path, *, params=None, headers=None):
        return self._client.stream(
            method,
            self._base + path,
            params=params,
            headers=self._headers(headers),
        )
