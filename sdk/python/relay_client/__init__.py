"""
relay Python SDK — thin client for the relay daemon.

Example:
    from relay_client import RelayClient

    client = RelayClient("http://localhost:7474")
    thread = client.thread_new(name="my-run")
    tid = thread["thread_id"]

    ref = client.artifact_put(tid, "notes.md", "# Notes\\n\\nHello world", "markdown")
    result = client.cap_invoke(tid, "retrieval.search", {"query": "hello"})
    print(result["preview"])
"""

from relay_client.client import RelayClient
from relay_client.errors import RelayError, RelayAPIError

__version__ = "1.0.0"
__all__ = ["RelayClient", "RelayError", "RelayAPIError"]
