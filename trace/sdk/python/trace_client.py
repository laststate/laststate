"""Last State Trace — minimal Python ingest client."""

from __future__ import annotations

import json
import urllib.error
import urllib.request
from typing import Any, Mapping, Optional


class TraceClient:
    def __init__(
        self,
        base_url: str,
        token: str,
        project_id: str = "",
        timeout: float = 30.0,
    ) -> None:
        if not base_url or not token:
            raise ValueError("base_url and token required")
        self.base_url = base_url.rstrip("/")
        self.token = token
        self.project_id = project_id
        self.timeout = timeout

    def _headers(self, extra: Optional[Mapping[str, str]] = None) -> dict[str, str]:
        h = {"Authorization": f"Bearer {self.token}"}
        if self.project_id:
            h["X-Project-ID"] = self.project_id
        if extra:
            h.update(extra)
        return h

    def _request(
        self,
        method: str,
        path: str,
        data: bytes | None = None,
        headers: Optional[Mapping[str, str]] = None,
    ) -> Any:
        req = urllib.request.Request(
            self.base_url + path,
            data=data,
            headers=self._headers(headers),
            method=method,
        )
        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                body = resp.read()
                if not body:
                    return {}
                ctype = resp.headers.get("Content-Type", "")
                if "json" in ctype:
                    return json.loads(body.decode("utf-8"))
                return body
        except urllib.error.HTTPError as e:
            err = e.read().decode("utf-8", errors="replace")
            raise RuntimeError(f"{method} {path} -> {e.code}: {err}") from e

    def ingest_binary(self, payload: bytes, content_type: str = "application/octet-stream") -> Any:
        return self._request(
            "POST",
            "/v1/ingest",
            data=payload,
            headers={"Content-Type": content_type},
        )

    def ingest_batch(self, items: list[dict[str, Any]]) -> Any:
        raw = json.dumps({"items": items}).encode("utf-8")
        return self._request(
            "POST",
            "/v1/events:batch",
            data=raw,
            headers={"Content-Type": "application/json"},
        )

    def upload_artifact(
        self,
        data: bytes,
        name: str = "firmware.elf",
        build_id: str = "",
    ) -> Any:
        return self._request(
            "POST",
            "/v1/artifacts",
            data=data,
            headers={
                "Content-Type": "application/octet-stream",
                "X-Artifact-Name": name,
                "X-Build-Id": build_id,
            },
        )

    def heartbeat(self, body: Optional[dict[str, Any]] = None) -> Any:
        raw = json.dumps(body or {}).encode("utf-8")
        return self._request(
            "POST",
            "/v1/relay/heartbeat",
            data=raw,
            headers={"Content-Type": "application/json"},
        )

    def capabilities(self) -> Any:
        return self._request("GET", "/v1/relay/capabilities")
