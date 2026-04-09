import dataclasses
import datetime as dt
from enum import Enum
from typing import Any, Callable

UTC = dt.timezone.utc


class ProviderKind(str, Enum):
    INTEGRATION = "integration"
    AUTH = "auth"
    DATASTORE = "datastore"
    SECRETS = "secrets"
    TELEMETRY = "telemetry"


@dataclasses.dataclass(slots=True)
class ProviderMetadata:
    kind: ProviderKind | str
    name: str = ""
    display_name: str = ""
    description: str = ""
    version: str = ""


class RuntimeProvider:
    def configure(self, name: str, config: dict[str, Any]) -> None:
        pass


class MetadataProvider:
    def metadata(self) -> ProviderMetadata:
        raise NotImplementedError


class HealthChecker:
    def health_check(self) -> None:
        raise NotImplementedError


class WarningsProvider:
    def warnings(self) -> list[str]:
        raise NotImplementedError


class Closer:
    def close(self) -> None:
        raise NotImplementedError


RegisterServices = Callable[[Any, RuntimeProvider], None]


@dataclasses.dataclass(slots=True)
class RuntimeProviderAdapter:
    kind: ProviderKind | str
    provider: RuntimeProvider
    register_services: RegisterServices

    def serve(self) -> None:
        from . import _runtime

        _runtime.serve(self)


RuntimePlugin = RuntimeProviderAdapter


@dataclasses.dataclass(slots=True)
class BeginLoginRequest:
    callback_url: str = ""
    host_state: str = ""
    scopes: list[str] = dataclasses.field(default_factory=list)
    options: dict[str, str] = dataclasses.field(default_factory=dict)


@dataclasses.dataclass(slots=True)
class BeginLoginResponse:
    authorization_url: str
    provider_state: bytes = b""


@dataclasses.dataclass(slots=True)
class CompleteLoginRequest:
    query: dict[str, str] = dataclasses.field(default_factory=dict)
    provider_state: bytes = b""
    callback_url: str = ""


@dataclasses.dataclass(slots=True)
class AuthenticatedUser:
    subject: str = ""
    email: str = ""
    email_verified: bool = False
    display_name: str = ""
    avatar_url: str = ""
    claims: dict[str, str] = dataclasses.field(default_factory=dict)


class AuthProvider(RuntimeProvider):
    def begin_login(self, request: BeginLoginRequest) -> BeginLoginResponse:
        raise NotImplementedError

    def complete_login(self, request: CompleteLoginRequest) -> AuthenticatedUser:
        raise NotImplementedError

    def serve(self) -> None:
        from . import _runtime

        _runtime.serve(self, runtime_kind=ProviderKind.AUTH)


class ExternalTokenValidator:
    def validate_external_token(self, token: str) -> AuthenticatedUser | None:
        raise NotImplementedError


class SessionTTLProvider:
    def session_ttl(self) -> dt.timedelta:
        raise NotImplementedError


@dataclasses.dataclass(slots=True)
class StoredUser:
    id: str = ""
    email: str = ""
    display_name: str = ""
    created_at: dt.datetime = dataclasses.field(
        default_factory=lambda: dt.datetime.fromtimestamp(0, tz=UTC)
    )
    updated_at: dt.datetime = dataclasses.field(
        default_factory=lambda: dt.datetime.fromtimestamp(0, tz=UTC)
    )


@dataclasses.dataclass(slots=True)
class StoredIntegrationToken:
    id: str = ""
    user_id: str = ""
    integration: str = ""
    connection: str = ""
    instance: str = ""
    access_token_sealed: bytes = b""
    refresh_token_sealed: bytes = b""
    scopes: str = ""
    expires_at: dt.datetime | None = None
    last_refreshed_at: dt.datetime | None = None
    refresh_error_count: int = 0
    connection_params: dict[str, str] = dataclasses.field(default_factory=dict)
    created_at: dt.datetime = dataclasses.field(
        default_factory=lambda: dt.datetime.fromtimestamp(0, tz=UTC)
    )
    updated_at: dt.datetime = dataclasses.field(
        default_factory=lambda: dt.datetime.fromtimestamp(0, tz=UTC)
    )


@dataclasses.dataclass(slots=True)
class StoredAPIToken:
    id: str = ""
    user_id: str = ""
    name: str = ""
    hashed_token: str = ""
    scopes: str = ""
    expires_at: dt.datetime | None = None
    created_at: dt.datetime = dataclasses.field(
        default_factory=lambda: dt.datetime.fromtimestamp(0, tz=UTC)
    )
    updated_at: dt.datetime = dataclasses.field(
        default_factory=lambda: dt.datetime.fromtimestamp(0, tz=UTC)
    )


@dataclasses.dataclass(slots=True)
class OAuthRegistration:
    auth_server_url: str = ""
    redirect_uri: str = ""
    client_id: str = ""
    client_secret_sealed: bytes = b""
    expires_at: dt.datetime | None = None
    authorization_endpoint: str = ""
    token_endpoint: str = ""
    scopes_supported: str = ""
    discovered_at: dt.datetime = dataclasses.field(
        default_factory=lambda: dt.datetime.fromtimestamp(0, tz=UTC)
    )


class DatastoreProvider(RuntimeProvider):
    def migrate(self) -> None:
        raise NotImplementedError

    def get_user(self, id: str) -> StoredUser | None:
        raise NotImplementedError

    def find_or_create_user(self, email: str) -> StoredUser:
        raise NotImplementedError

    def put_integration_token(self, token: StoredIntegrationToken) -> None:
        raise NotImplementedError

    def get_integration_token(
        self,
        user_id: str,
        integration: str,
        connection: str,
        instance: str,
    ) -> StoredIntegrationToken | None:
        raise NotImplementedError

    def list_integration_tokens(
        self,
        user_id: str,
        integration: str,
        connection: str,
    ) -> list[StoredIntegrationToken]:
        raise NotImplementedError

    def delete_integration_token(self, id: str) -> None:
        raise NotImplementedError

    def put_api_token(self, token: StoredAPIToken) -> None:
        raise NotImplementedError

    def get_api_token_by_hash(self, hashed_token: str) -> StoredAPIToken | None:
        raise NotImplementedError

    def list_api_tokens(self, user_id: str) -> list[StoredAPIToken]:
        raise NotImplementedError

    def revoke_api_token(self, user_id: str, id: str) -> None:
        raise NotImplementedError

    def revoke_all_api_tokens(self, user_id: str) -> int:
        raise NotImplementedError

    def serve(self) -> None:
        from . import _runtime

        _runtime.serve(self, runtime_kind=ProviderKind.DATASTORE)


class OAuthRegistrationStore:
    def get_oauth_registration(
        self,
        auth_server_url: str,
        redirect_uri: str,
    ) -> OAuthRegistration | None:
        raise NotImplementedError

    def put_oauth_registration(self, registration: OAuthRegistration) -> None:
        raise NotImplementedError

    def delete_oauth_registration(self, auth_server_url: str, redirect_uri: str) -> None:
        raise NotImplementedError
