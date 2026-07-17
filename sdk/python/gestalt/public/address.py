"""External gestaltd address normalization and validation."""

from __future__ import annotations

from urllib.parse import urlparse


def normalize_address(address: str) -> str:
    trimmed = address.strip()
    if not trimmed:
        raise ValueError("address is required")
    parsed = urlparse(trimmed)
    if not parsed.scheme or not parsed.netloc:
        raise ValueError(f"address must be an absolute URL: {address!r}")
    if parsed.scheme not in {"http", "https"}:
        raise ValueError(f"address must use http or https: {parsed.scheme!r}")
    return trimmed.rstrip("/")
