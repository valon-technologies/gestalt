"""Hosted HTTP subject-resolution helpers for authored Python plugins."""

from __future__ import annotations

import dataclasses
from typing import Any

from ._api import Error


@dataclasses.dataclass(slots=True)
class HTTPSubjectRequest:
    """Verified hosted HTTP request metadata passed into subject resolvers."""

    binding: str = ""
    method: str = ""
    path: str = ""
    content_type: str = ""
    headers: dict[str, list[str]] = dataclasses.field(default_factory=dict)
    query: dict[str, list[str]] = dataclasses.field(default_factory=dict)
    params: dict[str, Any] = dataclasses.field(default_factory=dict)
    raw_body: bytes = b""
    security_scheme: str = ""
    verified_subject: str = ""
    verified_claims: dict[str, str] = dataclasses.field(default_factory=dict)


class HTTPSubjectResolutionError(Error):
    """Explicit hosted HTTP subject-resolution rejection."""


def http_subject_error(status: int, message: str = "") -> HTTPSubjectResolutionError:
    """Create an explicit hosted HTTP subject-resolution rejection."""

    return HTTPSubjectResolutionError(status, message)
