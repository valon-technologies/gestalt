"""Authentication helpers for the public Gestalt transport client."""

from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass
from typing import Protocol


class AuthProvider(Protocol):
    def authorization_header(self) -> str | None: ...


@dataclass(frozen=True, slots=True)
class BearerAuth:
    """Bearer token authentication for REST and gRPC."""

    token: Callable[[], str]

    def authorization_header(self) -> str | None:
        value = self.token().strip()
        return f"Bearer {value}" if value else None


@dataclass(frozen=True, slots=True)
class Unauthenticated:
    """Unauthenticated requests."""

    def authorization_header(self) -> str | None:
        return None


Auth = BearerAuth | Unauthenticated


def bearer(token: Callable[[], str]) -> BearerAuth:
    return BearerAuth(token=token)


def unauthenticated() -> Unauthenticated:
    return Unauthenticated()
