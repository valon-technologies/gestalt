import datetime as dt
from enum import Enum
from typing import Any, Callable

from ._cache import CacheEntry
from .gen.v1 import auth_pb2 as _auth_pb2
from .gen.v1 import s3_pb2_grpc as _s3_pb2_grpc

AuthenticatedUser: Any = _auth_pb2.AuthenticatedUser  # ty: ignore[unresolved-attribute]
BeginLoginRequest: Any = _auth_pb2.BeginLoginRequest  # ty: ignore[unresolved-attribute]
BeginLoginResponse: Any = _auth_pb2.BeginLoginResponse  # ty: ignore[unresolved-attribute]
CompleteLoginRequest: Any = _auth_pb2.CompleteLoginRequest  # ty: ignore[unresolved-attribute]


class ProviderKind(str, Enum):
    INTEGRATION = "integration"
    AUTH = "auth"
    CACHE = "cache"
    S3 = "s3"
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


class CacheProvider(PluginProvider):
    def get(self, key: str) -> bytes | None:
        raise NotImplementedError

    def get_many(self, keys: list[str]) -> dict[str, bytes]:
        values: dict[str, bytes] = {}
        for key in keys:
            value = self.get(key)
            if value is not None:
                values[key] = bytes(value)
        return values

    def set(self, key: str, value: bytes, ttl: dt.timedelta | None = None) -> None:
        raise NotImplementedError

    def set_many(self, entries: list[CacheEntry], ttl: dt.timedelta | None = None) -> None:
        for entry in entries:
            self.set(entry.key, entry.value, ttl)

    def delete(self, key: str) -> bool:
        raise NotImplementedError

    def delete_many(self, keys: list[str]) -> int:
        deleted = 0
        seen: set[str] = set()
        for key in keys:
            if key in seen:
                continue
            seen.add(key)
            if self.delete(key):
                deleted += 1
        return deleted

    def touch(self, key: str, ttl: dt.timedelta) -> bool:
        raise NotImplementedError

    def serve(self) -> None:
        from . import _runtime

        _runtime.serve(self, runtime_kind=ProviderKind.CACHE)


class S3Provider(PluginProvider, _s3_pb2_grpc.S3Servicer):
    def serve(self) -> None:
        from . import _runtime

        _runtime.serve(self, runtime_kind=ProviderKind.S3)
