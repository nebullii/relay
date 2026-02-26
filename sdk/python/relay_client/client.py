"""relay Python client."""

from __future__ import annotations

import json
import time
from typing import Any, Dict, List, Optional
from urllib import request, error
from urllib.error import HTTPError

from relay_client.errors import RelayAPIError, RelayError


class RelayClient:
    """
    Thin HTTP client for the relay daemon.

    Args:
        base_url: Daemon URL, e.g. "http://localhost:7474"
        api_token: Optional API token for authentication
        timeout: Request timeout in seconds (default: 30)
    """

    def __init__(
        self,
        base_url: str = "http://localhost:7474",
        api_token: Optional[str] = None,
        timeout: int = 30,
    ):
        self.base_url = base_url.rstrip("/")
        self.api_token = api_token
        self.timeout = timeout

    # ---- Threads ----

    def thread_new(self, name: str = "") -> Dict[str, Any]:
        """Create a new thread and return its metadata."""
        return self._post("/threads", {"name": name})

    def thread_list(self) -> List[Dict[str, Any]]:
        """List all threads."""
        return self._get("/threads")["threads"]

    def thread_get(self, thread_id: str) -> Dict[str, Any]:
        """Get thread metadata."""
        return self._get(f"/threads/{thread_id}")

    # ---- State ----

    def state_header(self, thread_id: str) -> Dict[str, Any]:
        """
        Get the bounded state header for a thread.

        This is the token-efficient view: bounded facts, constraints,
        open questions, next plan steps, and artifact refs.
        """
        return self._get(f"/threads/{thread_id}/state/header")

    def state_full(self, thread_id: str) -> Dict[str, Any]:
        """Get the full state for a thread (use sparingly in prompts)."""
        return self._get(f"/threads/{thread_id}/state")

    def state_patch(self, thread_id: str, ops: List[Dict[str, Any]]) -> Dict[str, Any]:
        """
        Apply a JSON Patch (RFC 6902) to thread state.

        Args:
            thread_id: Thread ID
            ops: List of patch operations, e.g.:
                [{"op": "add", "path": "/facts/-", "value": {"id": "f1", "key": "status", "value": "done"}}]

        Returns:
            Updated state metadata with version and state_ref.
        """
        return self._post(f"/threads/{thread_id}/state/patch", ops)

    # ---- Artifacts ----

    def artifact_put(
        self,
        thread_id: str,
        name: str,
        content: str,
        artifact_type: str = "text",
        mime: str = "text/plain",
    ) -> Dict[str, Any]:
        """
        Store text content as an artifact.

        Returns artifact metadata including ref, size, hash, preview.
        The ref can be used instead of the content in all further calls.
        """
        return self._post(
            f"/threads/{thread_id}/artifacts",
            {
                "name": name,
                "type": artifact_type,
                "mime": mime,
                "content": content,
            },
        )

    def artifact_get(self, thread_id: str, ref: str) -> Dict[str, Any]:
        """Get artifact metadata (without content)."""
        return self._get(f"/threads/{thread_id}/artifacts/{ref}")

    def artifact_content(self, thread_id: str, ref: str) -> bytes:
        """Download the full artifact content."""
        return self._get_raw(f"/threads/{thread_id}/artifacts/{ref}?raw=1")

    def artifact_list(self, thread_id: str) -> List[Dict[str, Any]]:
        """List all artifacts for a thread."""
        return self._get(f"/threads/{thread_id}/artifacts")["artifacts"]

    # ---- Capabilities ----

    def cap_invoke(
        self,
        thread_id: str,
        capability: str,
        args: Dict[str, Any],
        idempotency_key: Optional[str] = None,
    ) -> Dict[str, Any]:
        """
        Invoke a capability (tool).

        Returns:
            preview: small JSON summary
            artifact_ref: ref to full output (use instead of pasting)
            cache_hit: whether result came from cache
            duration_ms: invocation time
        """
        payload = {
            "capability": capability,
            "thread_id": thread_id,
            "args": args,
        }
        if idempotency_key:
            payload["idempotency_key"] = idempotency_key
        return self._post("/cap/invoke", payload)

    def cap_list(self) -> List[Dict[str, Any]]:
        """List all available capabilities."""
        return self._get("/cap/list")["capabilities"]

    # ---- Reports ----

    def report(self, thread_id: str, fmt: str = "md") -> Dict[str, Any]:
        """
        Generate a report artifact for a thread.

        Returns artifact_ref pointing to the report, plus token savings stats.
        """
        return self._post(f"/reports/{thread_id}", {"format": fmt})

    # ---- Events ----

    def events(self, thread_id: str, after: Optional[str] = None) -> List[Dict[str, Any]]:
        """List events for a thread."""
        path = f"/threads/{thread_id}/events"
        if after:
            path += f"?after={after}"
        return self._get(path)["events"]

    def tail(self, thread_id: str, poll_interval: float = 1.0):
        """
        Generator that yields new events as they arrive (like tail -f).

        Args:
            thread_id: Thread to tail
            poll_interval: Seconds between polls

        Yields:
            Event dicts as they appear
        """
        last_id = None
        while True:
            evs = self.events(thread_id, after=last_id)
            for ev in evs:
                yield ev
                last_id = ev["id"]
            time.sleep(poll_interval)

    # ---- Health ----

    def health(self) -> Dict[str, Any]:
        """Check daemon health."""
        return self._get("/health")

    def version(self) -> Dict[str, Any]:
        """Get daemon version."""
        return self._get("/version")

    # ---- Helpers ----

    def _headers(self) -> Dict[str, str]:
        h = {"Content-Type": "application/json", "Accept": "application/json"}
        if self.api_token:
            h["Authorization"] = f"Bearer {self.api_token}"
        return h

    def _get(self, path: str) -> Any:
        req = request.Request(
            self.base_url + path,
            headers=self._headers(),
            method="GET",
        )
        return self._execute(req)

    def _get_raw(self, path: str) -> bytes:
        req = request.Request(
            self.base_url + path,
            headers=self._headers(),
            method="GET",
        )
        try:
            with request.urlopen(req, timeout=self.timeout) as resp:
                return resp.read()
        except HTTPError as e:
            body = e.read().decode()
            raise RelayAPIError(e.code, body)
        except Exception as e:
            raise RelayError(f"request failed: {e}")

    def _post(self, path: str, body: Any) -> Any:
        data = json.dumps(body).encode()
        req = request.Request(
            self.base_url + path,
            data=data,
            headers=self._headers(),
            method="POST",
        )
        return self._execute(req)

    def _execute(self, req: request.Request) -> Any:
        try:
            with request.urlopen(req, timeout=self.timeout) as resp:
                body = resp.read().decode()
                return json.loads(body)
        except HTTPError as e:
            body = e.read().decode()
            try:
                err_data = json.loads(body)
                msg = err_data.get("error", body)
            except Exception:
                msg = body
            raise RelayAPIError(e.code, msg)
        except Exception as e:
            raise RelayError(f"request failed: {e}") from e
