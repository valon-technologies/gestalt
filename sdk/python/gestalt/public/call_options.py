"""Per-call cancellation and deadline helpers for public transports."""

from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class UnaryCallOptions:
    timeout: float | None = None
