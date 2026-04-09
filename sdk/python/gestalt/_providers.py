import datetime as dt
from enum import Enum
from typing import Any, Callable

from .gen.v1 import auth_pb2 as _auth_pb2
from .gen.v1 import datastore_pb2 as _datastore_pb2

AuthenticatedUser: Any = _auth_pb2.AuthenticatedUser  # ty: ignore[unresolved-attribute]
BeginLoginRequest: Any = _auth_pb2.BeginLoginRequest  # ty: ignore[unresolved-attribute]
BeginLoginResponse: Any = _auth_pb2.BeginLoginResponse  # ty: ignore[unresolved-attribute]
CompleteLoginRequest: Any = _auth_pb2.CompleteLoginRequest  # ty: ignore[unresolved-attribute]
StoredUser: Any = _datastore_pb2.StoredUser  # ty: ignore[unresolved-attribute]
StoredIntegrationToken: Any = _datastore_pb2.StoredIntegrationToken  # ty: ignore[unresolved-attribute]
StoredAPIToken: Any = _datastore_pb2.StoredAPIToken  # ty: ignore[unresolved-attribute]
OAuthRegistration: Any = _datastore_pb2.OAuthRegistration  # ty: ignore[unresolved-attribute]


class ProviderKind(str, Enum):
    INTEGRATION = "integration"
    AUTH = "auth"
    DATASTORE = "datastore"
    SECRETS = "secrets"
    TELEMETRY = "telemetry"


class ProviderMetadata:
    __slots__ = ("kind", "name", "display_name", "description", "version")

    def __init__(
        self,
        kind: ProviderKind | str,
        name: str = "",
        display_name: str = "",
        description: str = "",
        version: str = "",
    ) -> None:
        self.kind = kind
        self.name = name
        self.display_name = display_name
        self.description = description
        self.version = version


class PluginProvider:
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


RegisterServices = Callable[[Any, PluginProvider], None]


class PluginProviderAdapter:
    __slots__ = ("kind", "provider", "register_services")

    def __init__(
        self,
        kind: ProviderKind | str,
        provider: PluginProvider,
        register_services: RegisterServices,
    ) -> None:
        self.kind = kind
        self.provider = provider
        self.register_services = register_services

    def serve(self) -> None:
        from . import _runtime

        _runtime.serve(self)


class AuthProvider(PluginProvider):
    def begin_login(self, request: Any) -> Any:
        raise NotImplementedError

    def complete_login(self, request: Any) -> Any:
        raise NotImplementedError

    def serve(self) -> None:
        from . import _runtime

        _runtime.serve(self, runtime_kind=ProviderKind.AUTH)


class ExternalTokenValidator:
    def validate_external_token(self, token: str) -> Any:
        raise NotImplementedError


class SessionTTLProvider:
    def session_ttl(self) -> dt.timedelta:
        raise NotImplementedError


class SecretsProvider(PluginProvider):
    def get_secret(self, name: str) -> str:
        raise NotImplementedError

    def serve(self) -> None:
        from . import _runtime

        _runtime.serve(self, runtime_kind=ProviderKind.SECRETS)


class DatastoreProvider(PluginProvider):
    def migrate(self) -> None:
        raise NotImplementedError

    def get_user(self, id: str) -> Any:
        raise NotImplementedError

    def find_or_create_user(self, email: str) -> Any:
        raise NotImplementedError

    def put_integration_token(self, token: Any) -> None:
        raise NotImplementedError

    def get_integration_token(
        self,
        user_id: str,
        integration: str,
        connection: str,
        instance: str,
    ) -> Any:
        raise NotImplementedError

    def list_integration_tokens(
        self,
        user_id: str,
        integration: str,
        connection: str,
    ) -> list[Any]:
        raise NotImplementedError

    def delete_integration_token(self, id: str) -> None:
        raise NotImplementedError

    def put_api_token(self, token: Any) -> None:
        raise NotImplementedError

    def get_api_token_by_hash(self, hashed_token: str) -> Any:
        raise NotImplementedError

    def list_api_tokens(self, user_id: str) -> list[Any]:
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
    ) -> Any:
        raise NotImplementedError

    def put_oauth_registration(self, registration: Any) -> None:
        raise NotImplementedError

    def delete_oauth_registration(self, auth_server_url: str, redirect_uri: str) -> None:
        raise NotImplementedError
