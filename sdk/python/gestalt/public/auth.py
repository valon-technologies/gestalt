"""Authentication helpers for the public Gestalt transport client."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Protocol


class Auth(Protocol):
    def authorization_header(self) -> str | None: ...


@dataclass(frozen=True, slots=True)
class BearerAuth:
    """Bearer token authentication for REST and gRPC."""

    token: str

    def authorization_header(self) -> str | None:
        token = self.token.strip()
        return f"Bearer {token}" if token else None


@dataclass(frozen=True, slots=True)
class NoAuth:
    """Unauthenticated requests."""

    def authorization_header(self) -> str | None:
        return None
