"""Domain errors for API pool."""


class PoolError(Exception):
    """Base error for pool operations."""


class ApiExistsError(PoolError):
    """Raised when api id already exists."""


class ApiNotFoundError(PoolError):
    """Raised when api id does not exist."""


class NoCapacityError(PoolError):
    """Raised when no API capacity is available."""


class TicketNotFoundError(PoolError):
    """Raised when ticket id does not exist."""


class InvalidStateError(PoolError):
    """Raised when runtime state is invalid."""
