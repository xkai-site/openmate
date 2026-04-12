"""Python-side errors for the Go-backed pool adapter."""

from __future__ import annotations

from .models import InvokeResponse


class PoolError(Exception):
    """Base error for pool adapter operations."""


class PoolTransportError(PoolError):
    """Raised when invoking the Go CLI fails structurally."""


class NoCapacityError(PoolError):
    """Raised when the gateway cannot reserve any provider slot."""


class InvocationFailedError(PoolError):
    """Raised when the gateway reaches provider execution but the call fails."""

    def __init__(self, response: InvokeResponse) -> None:
        self.response = response
        message = response.error.message if response.error is not None else "invocation failed"
        super().__init__(message)
