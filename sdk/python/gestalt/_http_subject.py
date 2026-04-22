"""Hosted HTTP subject-resolution types for authored plugins."""
from __future__ import annotations

import dataclasses
from collections.abc import Mapping
from http import HTTPStatus
from typing import Any


@dataclasses.dataclass(slots=True)
class HTTPSubjectRequest:
    """Verified hosted HTTP request metadata passed into subject resolution."""

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


class HTTPSubjectResolutionError(Exception):
    """Explicit HTTP rejection surfaced from a hosted HTTP subject resolver."""

    def __init__(self, status: int | HTTPStatus, message: str = "") -> None:
        self.status = int(status)
        self.message = message or HTTPStatus(self.status).phrase
        super().__init__(self.message)


def http_subject_error(
    status: int | HTTPStatus,
    message: str = "",
) -> HTTPSubjectResolutionError:
    """Create an explicit hosted HTTP subject-resolution rejection."""

    return HTTPSubjectResolutionError(status, message)


def clone_http_subject_request(request: HTTPSubjectRequest) -> HTTPSubjectRequest:
    """Return a detached copy of one hosted HTTP subject-resolution request."""

    return HTTPSubjectRequest(
        binding=request.binding,
        method=request.method,
        path=request.path,
        content_type=request.content_type,
        headers=_clone_string_lists(request.headers),
        query=_clone_string_lists(request.query),
        params=dict(request.params),
        raw_body=bytes(request.raw_body),
        security_scheme=request.security_scheme,
        verified_subject=request.verified_subject,
        verified_claims=dict(request.verified_claims),
    )


def _clone_string_lists(values: Mapping[str, list[str]]) -> dict[str, list[str]]:
    return {key: list(items) for key, items in values.items()}
