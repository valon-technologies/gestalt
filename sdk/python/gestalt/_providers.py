import datetime as dt
from enum import Enum
from typing import Any, Callable

from .gen.v1 import auth_pb2 as _auth_pb2
from .gen.v1 import fileapi_pb2 as _fileapi_pb2

AuthenticatedUser: Any = _auth_pb2.AuthenticatedUser  # ty: ignore[unresolved-attribute]
BeginLoginRequest: Any = _auth_pb2.BeginLoginRequest  # ty: ignore[unresolved-attribute]
BeginLoginResponse: Any = _auth_pb2.BeginLoginResponse  # ty: ignore[unresolved-attribute]
CompleteLoginRequest: Any = _auth_pb2.CompleteLoginRequest  # ty: ignore[unresolved-attribute]
BlobPart: Any = _fileapi_pb2.BlobPart  # ty: ignore[unresolved-attribute]
BlobOptions: Any = _fileapi_pb2.BlobOptions  # ty: ignore[unresolved-attribute]
BytesResponse: Any = _fileapi_pb2.BytesResponse  # ty: ignore[unresolved-attribute]
CreateBlobRequest: Any = _fileapi_pb2.CreateBlobRequest  # ty: ignore[unresolved-attribute]
CreateFileRequest: Any = _fileapi_pb2.CreateFileRequest  # ty: ignore[unresolved-attribute]
CreateObjectURLRequest: Any = _fileapi_pb2.CreateObjectURLRequest  # ty: ignore[unresolved-attribute]
FileObject: Any = _fileapi_pb2.FileObject  # ty: ignore[unresolved-attribute]
FileObjectRequest: Any = _fileapi_pb2.FileObjectRequest  # ty: ignore[unresolved-attribute]
FileObjectResponse: Any = _fileapi_pb2.FileObjectResponse  # ty: ignore[unresolved-attribute]
ObjectURLRequest: Any = _fileapi_pb2.ObjectURLRequest  # ty: ignore[unresolved-attribute]
ObjectURLResponse: Any = _fileapi_pb2.ObjectURLResponse  # ty: ignore[unresolved-attribute]
ReadChunk: Any = _fileapi_pb2.ReadChunk  # ty: ignore[unresolved-attribute]
ReadStreamRequest: Any = _fileapi_pb2.ReadStreamRequest  # ty: ignore[unresolved-attribute]
SliceRequest: Any = _fileapi_pb2.SliceRequest  # ty: ignore[unresolved-attribute]


class ProviderKind(str, Enum):
    INTEGRATION = "integration"
    AUTH = "auth"
    FILEAPI = "fileapi"
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


class FileAPIProvider(PluginProvider):
    def create_blob(self, request: Any) -> Any:
        raise NotImplementedError

    def create_file(self, request: Any) -> Any:
        raise NotImplementedError

    def stat(self, request: Any) -> Any:
        raise NotImplementedError

    def slice(self, request: Any) -> Any:
        raise NotImplementedError

    def read_bytes(self, request: Any) -> Any:
        raise NotImplementedError

    def open_read_stream(self, request: Any) -> Any:
        raise NotImplementedError

    def create_object_url(self, request: Any) -> Any:
        raise NotImplementedError

    def resolve_object_url(self, request: Any) -> Any:
        raise NotImplementedError

    def revoke_object_url(self, request: Any) -> None:
        raise NotImplementedError

    def serve(self) -> None:
        from . import _runtime

        _runtime.serve(self, runtime_kind=ProviderKind.FILEAPI)
