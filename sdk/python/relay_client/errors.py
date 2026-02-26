class RelayError(Exception):
    """Base error for relay SDK."""
    pass


class RelayAPIError(RelayError):
    """API returned an error response."""

    def __init__(self, status: int, message: str):
        self.status = status
        self.message = message
        super().__init__(f"relay API error {status}: {message}")
