"""External credential client contracts."""

from __future__ import annotations

import dataclasses as _dataclasses
import datetime as _dt
from collections.abc import Mapping, Sequence
from typing import Protocol


@_dataclasses.dataclass(slots=True)
class ExternalCredential:
    id: str = ""
    subject_id: str = ""
    instance: str = ""
    access_token: str = ""
    refresh_token: str = ""
    scopes: str = ""
    expires_at: _dt.datetime | None = None
    last_refreshed_at: _dt.datetime | None = None
    refresh_error_count: int = 0
    metadata_json: str = ""
    created_at: _dt.datetime | None = None
    updated_at: _dt.datetime | None = None
    connection_id: str = ""


@_dataclasses.dataclass(slots=True)
class ExternalCredentialLookup:
    subject_id: str = ""
    instance: str = ""
    connection_id: str = ""


@_dataclasses.dataclass(slots=True)
class UpsertExternalCredentialRequest:
    credential: ExternalCredential | None = None
    preserve_timestamps: bool = False


@_dataclasses.dataclass(slots=True)
class GetExternalCredentialRequest:
    lookup: ExternalCredentialLookup | None = None


@_dataclasses.dataclass(slots=True)
class ListExternalCredentialsRequest:
    subject_id: str = ""
    instance: str = ""
    connection_id: str = ""


@_dataclasses.dataclass(slots=True)
class ListExternalCredentialsResponse:
    credentials: Sequence[ExternalCredential] = ()


@_dataclasses.dataclass(slots=True)
class DeleteExternalCredentialRequest:
    id: str = ""


@_dataclasses.dataclass(slots=True)
class ExternalCredentialTokenExchangeDriver:
    type: str = ""
    target_principal: str = ""
    scopes: Sequence[str] = ()
    lifetime_seconds: int = 0
    endpoint: str = ""
    params: Mapping[str, str] | None = None


@_dataclasses.dataclass(slots=True)
class ExternalCredentialAuthConfig:
    type: str = ""
    token: str = ""
    token_prefix: str = ""
    grant_type: str = ""
    token_url: str = ""
    client_id: str = ""
    client_secret: str = ""
    client_auth: str = ""
    token_exchange: str = ""
    scopes: Sequence[str] = ()
    scope_param: str = ""
    scope_separator: str = ""
    token_params: Mapping[str, str] | None = None
    refresh_params: Mapping[str, str] | None = None
    accept_header: str = ""
    access_token_path: str = ""
    token_exchange_drivers: Sequence[ExternalCredentialTokenExchangeDriver] = ()
    refresh_token: str = ""


@_dataclasses.dataclass(slots=True)
class ValidateExternalCredentialConfigRequest:
    provider: str = ""
    connection: str = ""
    connection_id: str = ""
    mode: str = ""
    auth: ExternalCredentialAuthConfig | None = None
    connection_params: Mapping[str, str] | None = None


@_dataclasses.dataclass(slots=True)
class ResolveExternalCredentialRequest:
    provider: str = ""
    connection: str = ""
    connection_id: str = ""
    mode: str = ""
    credential_subject_id: str = ""
    actor_subject_id: str = ""
    instance: str = ""
    auth: ExternalCredentialAuthConfig | None = None
    connection_params: Mapping[str, str] | None = None


@_dataclasses.dataclass(slots=True)
class ResolveExternalCredentialResponse:
    token: str = ""
    expires_at: _dt.datetime | None = None
    metadata_json: str = ""
    params: Mapping[str, str] | None = None
    credential: ExternalCredential | None = None


@_dataclasses.dataclass(slots=True)
class ExternalCredentialTokenResponse:
    access_token: str = ""
    refresh_token: str = ""
    expires_in: int = 0
    token_type: str = ""
    extra_json: str = ""
    refresh_source: str = ""


@_dataclasses.dataclass(slots=True)
class ExchangeExternalCredentialRequest:
    provider: str = ""
    connection: str = ""
    connection_id: str = ""
    credential_subject_id: str = ""
    actor_subject_id: str = ""
    instance: str = ""
    auth: ExternalCredentialAuthConfig | None = None
    credential_json: str = ""
    connection_params: Mapping[str, str] | None = None


@_dataclasses.dataclass(slots=True)
class ExchangeExternalCredentialResponse:
    token_response: ExternalCredentialTokenResponse | None = None


class ExternalCredentials(Protocol):
    """Fakeable external-credential client contract."""

    def upsert_credential(
        self, request: UpsertExternalCredentialRequest
    ) -> ExternalCredential:
        """Create or update an external credential."""

    def get_credential(self, request: GetExternalCredentialRequest) -> ExternalCredential:
        """Fetch one external credential."""

    def list_credentials(
        self, request: ListExternalCredentialsRequest
    ) -> ListExternalCredentialsResponse:
        """List external credentials."""

    def delete_credential(self, request: DeleteExternalCredentialRequest) -> None:
        """Delete one external credential."""

    def validate_credential_config(
        self, request: ValidateExternalCredentialConfigRequest
    ) -> None:
        """Validate an external credential configuration."""

    def resolve_credential(
        self, request: ResolveExternalCredentialRequest
    ) -> ResolveExternalCredentialResponse:
        """Resolve a usable credential token."""

    def exchange_credential(
        self, request: ExchangeExternalCredentialRequest
    ) -> ExchangeExternalCredentialResponse:
        """Exchange external credential material."""
