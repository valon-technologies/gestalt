"""Provider base classes for non-integration Gestalt runtimes.

The generated request and response protobuf messages for authentication and
catalog data remain available through the public :mod:`gestalt` package, but
these helpers document the handwritten provider interfaces that wrap those
messages.
"""

from __future__ import annotations

import datetime as dt
from enum import Enum
from importlib import import_module
from typing import TYPE_CHECKING, Any, Callable

from .gen.v1 import authentication_pb2 as _authentication_pb2

if TYPE_CHECKING:
    from ._cache import CacheEntry

_s3_pb2_grpc: Any | None
_agent_pb2_grpc: Any | None
_workflow_pb2_grpc: Any | None

try:
    _agent_pb2_grpc = import_module(".gen.v1.agent_pb2_grpc", __package__)
    _s3_pb2_grpc = import_module(".gen.v1.s3_pb2_grpc", __package__)
    _workflow_pb2_grpc = import_module(".gen.v1.workflow_pb2_grpc", __package__)
except ModuleNotFoundError as exc:
    if exc.name != "grpc":
        raise
    _agent_pb2_grpc = None
    _s3_pb2_grpc = None
    _workflow_pb2_grpc = None

AuthenticatedUser: Any = _authentication_pb2.AuthenticatedUser  # ty: ignore[unresolved-attribute]
BeginLoginRequest: Any = _authentication_pb2.BeginLoginRequest  # ty: ignore[unresolved-attribute]
BeginLoginResponse: Any = _authentication_pb2.BeginLoginResponse  # ty: ignore[unresolved-attribute]
CompleteLoginRequest: Any = _authentication_pb2.CompleteLoginRequest  # ty: ignore[unresolved-attribute]

if _s3_pb2_grpc is None:
    _S3ServicerBase = object
else:
    _S3ServicerBase = _s3_pb2_grpc.S3Servicer

if _agent_pb2_grpc is None:
    _AgentProviderServicerBase = object
else:
    _AgentProviderServicerBase = _agent_pb2_grpc.AgentProviderServicer

if _workflow_pb2_grpc is None:
    _WorkflowProviderServicerBase = object
else:
    _WorkflowProviderServicerBase = _workflow_pb2_grpc.WorkflowProviderServicer


class ProviderKind(str, Enum):
    """Runtime kinds supported by the Python SDK."""

    INTEGRATION = "integration"
    AUTHENTICATION = "authentication"
    CACHE = "cache"
    S3 = "s3"
    AGENT = "agent"
    WORKFLOW = "workflow"
    SECRETS = "secrets"
    TELEMETRY = "telemetry"


class ProviderMetadata:
    """Descriptive metadata returned by :class:`MetadataProvider`."""

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
    """Base interface shared by provider-style runtimes."""

    def configure(self, name: str, config: dict[str, Any]) -> None:
        """Apply the host-provided provider name and parsed configuration."""

        pass


class MetadataProvider:
    """Optional mixin for providers that expose descriptive metadata."""

    def metadata(self) -> ProviderMetadata:
        """Return metadata for the running provider instance."""

        raise NotImplementedError


class HealthChecker:
    """Optional mixin for providers that support health checks."""

    def health_check(self) -> None:
        """Raise if the provider is unhealthy."""

        raise NotImplementedError


class WarningsProvider:
    """Optional mixin for providers that emit startup warnings."""

    def warnings(self) -> list[str]:
        """Return human-readable warnings for the host to surface."""

        raise NotImplementedError


class Closer:
    """Optional mixin for providers with explicit shutdown work."""

    def close(self) -> None:
        """Release any provider resources before the process exits."""

        raise NotImplementedError


RegisterServices = Callable[[Any, PluginProvider], None]


class PluginProviderAdapter:
    """Wrap a provider and registration callback for integration runtimes."""

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
        """Start the provider's gRPC runtime."""

        from . import _runtime

        _runtime.serve(self)


class AuthenticationProvider(PluginProvider):
    """Base class for authentication providers."""

    def begin_login(self, request: Any) -> Any:
        """Begin an interactive login flow."""

        raise NotImplementedError

    def complete_login(self, request: Any) -> Any:
        """Complete an interactive login flow."""

        raise NotImplementedError

    def serve(self) -> None:
        """Start the authentication runtime."""

        from . import _runtime

        _runtime.serve(self, runtime_kind=ProviderKind.AUTHENTICATION)

class ExternalTokenValidator:
    """Optional mixin for providers that validate external bearer tokens."""

    def validate_external_token(self, token: str) -> Any:
        """Validate a bearer token and return the authenticated subject."""

        raise NotImplementedError


class SessionTTLProvider:
    """Optional mixin for providers that control session lifetimes."""

    def session_ttl(self) -> dt.timedelta:
        """Return the requested session time-to-live."""

        raise NotImplementedError


class SecretsProvider(PluginProvider):
    """Base class for secret-provider runtimes."""

    def get_secret(self, name: str) -> str:
        """Return a secret value by name."""

        raise NotImplementedError

    def serve(self) -> None:
        """Start the secrets runtime."""

        from . import _runtime

        _runtime.serve(self, runtime_kind=ProviderKind.SECRETS)


class CacheProvider(PluginProvider):
    """Base class for cache-provider runtimes."""

    def get(self, key: str) -> bytes | None:
        """Return a cached value or ``None`` if the key is missing."""

        raise NotImplementedError

    def get_many(self, keys: list[str]) -> dict[str, bytes]:
        """Return the subset of ``keys`` that currently exist."""

        values: dict[str, bytes] = {}
        for key in keys:
            value = self.get(key)
            if value is not None:
                values[key] = bytes(value)
        return values

    def set(self, key: str, value: bytes, ttl: dt.timedelta | None = None) -> None:
        """Store ``value`` for ``key`` with an optional time-to-live."""

        raise NotImplementedError

    def set_many(self, entries: list[CacheEntry], ttl: dt.timedelta | None = None) -> None:
        """Store multiple cache entries using repeated :meth:`set` calls."""

        for entry in entries:
            self.set(entry.key, entry.value, ttl)

    def delete(self, key: str) -> bool:
        """Delete a cache entry and report whether it existed."""

        raise NotImplementedError

    def delete_many(self, keys: list[str]) -> int:
        """Delete a batch of cache keys and return the number removed."""

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
        """Refresh the TTL for an existing key."""

        raise NotImplementedError

    def serve(self) -> None:
        """Start the cache runtime."""

        from . import _runtime

        _runtime.serve(self, runtime_kind=ProviderKind.CACHE)


class S3Provider(PluginProvider, _S3ServicerBase):
    """Base class for S3-compatible object store runtimes."""

    def serve(self) -> None:
        """Start the S3 runtime."""

        from . import _runtime

        _runtime.serve(self, runtime_kind=ProviderKind.S3)


class AgentProvider(PluginProvider, _AgentProviderServicerBase):
    """Base class for agent-provider runtimes."""

    def serve(self) -> None:
        """Start the agent runtime."""

        from . import _runtime

        _runtime.serve(self, runtime_kind=ProviderKind.AGENT)


class WorkflowProvider(PluginProvider, _WorkflowProviderServicerBase):
    def serve(self) -> None:
        from . import _runtime

        _runtime.serve(self, runtime_kind=ProviderKind.WORKFLOW)
