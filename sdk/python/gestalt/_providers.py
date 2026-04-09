import dataclasses
import datetime as dt
from enum import Enum
from typing import Any, Callable


class ProviderKind(str, Enum):
    INTEGRATION = "integration"
    AUTH = "auth"
    SECRETS = "secrets"
    TELEMETRY = "telemetry"


@dataclasses.dataclass(slots=True)
class ProviderMetadata:
    kind: ProviderKind | str
    name: str = ""
    display_name: str = ""
    description: str = ""
    version: str = ""


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


@dataclasses.dataclass(slots=True)
class PluginProviderAdapter:
    kind: ProviderKind | str
    provider: PluginProvider
    register_services: RegisterServices

    def serve(self) -> None:
        from . import _runtime

        _runtime.serve(self)


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


class AuthProvider(PluginProvider):
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


class SecretsProvider(PluginProvider):
    def get_secret(self, name: str) -> str:
        raise NotImplementedError

    def serve(self) -> None:
        from . import _runtime

        _runtime.serve(self, runtime_kind=ProviderKind.SECRETS)

