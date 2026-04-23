"""Hosted HTTP subject-resolution types for authored plugins."""

from __future__ import annotations

import dataclasses
from collections.abc import Mapping
from http import HTTPStatus
from typing import Any


@dataclasses.dataclass(slots=True)
class HTTPSubjectRequest:
    """Host-provided HTTP request data for resolving a hosted subject."""

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
    """Raised by subject handlers to reject a hosted HTTP request."""

    def __init__(self, status: int | HTTPStatus, message: str = "") -> None:
        self.status = int(status)
        if message:
            self.message = message
        else:
            try:
                self.message = HTTPStatus(self.status).phrase
            except ValueError:
                self.message = ""
        super().__init__(self.message)


def http_subject_error(
    status: int | HTTPStatus,
    message: str = "",
) -> HTTPSubjectResolutionError:
    """Return an error that rejects hosted HTTP subject resolution."""

    return HTTPSubjectResolutionError(status, message)


def clone_http_subject_request(request: HTTPSubjectRequest) -> HTTPSubjectRequest:
    """Return a defensive copy of a subject-resolution request."""

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
