"""Provider base classes for non-integration Gestalt runtimes.

Handwritten helpers in :mod:`gestalt` construct transport payloads for providers
without requiring provider code to import transport modules.
"""

from __future__ import annotations

import datetime as dt
from dataclasses import dataclass
from enum import Enum
from http import HTTPStatus
from typing import (
    TYPE_CHECKING,
    Any,
    BinaryIO,
    Callable,
    Iterable,
    NoReturn,
    Protocol,
    runtime_checkable,
)

if TYPE_CHECKING:
    from ._agent import (
        AgentInteraction,
        AgentProviderCapabilities,
        AgentSession,
        AgentTurn,
        CancelAgentProviderTurnRequest,
        CreateAgentProviderSessionRequest,
        CreateAgentProviderTurnRequest,
        GetAgentProviderCapabilitiesRequest,
        GetAgentProviderInteractionRequest,
        GetAgentProviderSessionRequest,
        GetAgentProviderTurnRequest,
        ListAgentProviderInteractionsRequest,
        ListAgentProviderInteractionsResponse,
        ListAgentProviderSessionsRequest,
        ListAgentProviderSessionsResponse,
        ListAgentProviderTurnEventsRequest,
        ListAgentProviderTurnEventsResponse,
        ListAgentProviderTurnsRequest,
        ListAgentProviderTurnsResponse,
        ResolveAgentProviderInteractionRequest,
        UpdateAgentProviderSessionRequest,
    )
    from ._api import Error
    from ._runtime_provider import (
        GetRuntimeSessionRequest,
        GetRuntimeSupportRequest,
        HostedApp,
        ListRuntimeSessionsRequest,
        ListRuntimeSessionsResponse,
        PrepareRuntimeWorkspaceRequest,
        PrepareRuntimeWorkspaceResponse,
        RemoveRuntimeWorkspaceRequest,
        RuntimeSession,
        RuntimeSupport,
        StartHostedAppRequest,
        StartRuntimeSessionRequest,
        StopRuntimeSessionRequest,
    )
    from ._workflow import (
        ApplyWorkflowProviderDefinitionRequest,
        CancelWorkflowProviderRunRequest,
        DeleteWorkflowProviderDefinitionRequest,
        DeliverWorkflowProviderEventRequest,
        GetWorkflowProviderDefinitionRequest,
        GetWorkflowProviderRunEventsRequest,
        GetWorkflowProviderRunEventsResponse,
        GetWorkflowProviderRunOutputRequest,
        GetWorkflowProviderRunOutputResponse,
        GetWorkflowProviderRunRequest,
        ListWorkflowProviderDefinitionsRequest,
        ListWorkflowProviderDefinitionsResponse,
        ListWorkflowProviderRunsRequest,
        ListWorkflowProviderRunsResponse,
        SetWorkflowProviderActivationPausedRequest,
        SetWorkflowProviderDefinitionPausedRequest,
        SignalOrStartWorkflowProviderRunRequest,
        SignalWorkflowProviderRunRequest,
        SignalWorkflowRunResponse,
        StartWorkflowProviderRunRequest,
        WorkflowDefinition,
        WorkflowEvent,
        WorkflowRun,
    )
    from .authorization import (
        AddRelationshipRequest,
        AddRelationshipResponse,
        CheckAccessManyRequest,
        CheckAccessManyResponse,
        CheckAccessRequest,
        CheckAccessResponse,
        DeleteRelationshipRequest,
        DeleteRelationshipResponse,
        GetActiveModelRefResponse,
        ListActiveModelResourceTypesRequest,
        ListActiveModelResourceTypesResponse,
        ListRelationshipsRequest,
        ListRelationshipsResponse,
        SetActiveModelRequest,
        SetActiveModelResponse,
        SetAuthorizationStateRequest,
        SetAuthorizationStateResponse,
    )
    from .cache import CacheSetEntry
    from .identity import (
        AuthorizeRequest,
        AuthorizeResponse,
        GetGrantRequest,
        GetGrantResponse,
        IntrospectRequest,
        IntrospectResponse,
        ListGrantsRequest,
        ListGrantsResponse,
        RevokeGrantRequest,
        RevokeGrantResponse,
        TokenRequest,
        TokenResponse,
        UserInfoRequest,
        UserInfoResponse,
    )
    from .s3 import (
        CopyObjectRequest,
        CopyObjectResponse,
        DeleteObjectRequest,
        HeadObjectRequest,
        HeadObjectResponse,
        ListObjectsRequest,
        ListObjectsResponse,
        PresignObjectRequest,
        PresignObjectResponse,
        ReadObjectRequest,
        S3ObjectMeta,
        WriteObjectOpen,
        WriteObjectResponse,
    )

else:
    from ._api import Error


#: Object body shapes an authored S3 provider may return from ``read_object``.
ObjectBody = bytes | bytearray | memoryview | BinaryIO | Iterable[bytes] | None


class S3NotFoundError(Exception):
    """Raised by S3 providers when the requested object does not exist."""

    pass


class S3PreconditionFailedError(Exception):
    """Raised by S3 providers when conditional request headers fail."""

    pass


class S3InvalidRangeError(Exception):
    """Raised by S3 providers when a requested byte range is invalid."""

    pass


@dataclass
class ProviderReadResult:
    """Object metadata and body returned by an authored S3 provider."""

    meta: S3ObjectMeta
    body: ObjectBody = None


class ProviderKind(str, Enum):
    """Runtime kinds supported by the Python SDK."""

    INTEGRATION = "integration"
    AUTHORIZATION = "authorization"
    IDENTITY = "identity"
    CACHE = "cache"
    S3 = "s3"
    AGENT = "agent"
    RUNTIME = "runtime"
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


class AppProvider:
    """Base interface shared by provider-style runtimes."""

    def configure(self, name: str, config: dict[str, Any]) -> None:
        """Apply the host-provided provider name and parsed configuration."""

        pass

    def _unimplemented(self, method: str) -> NoReturn:
        raise Error(
            HTTPStatus.NOT_IMPLEMENTED,
            f"{type(self).__name__}.{method} is not implemented",
        )


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


@runtime_checkable
class Starter(Protocol):
    """Optional mixin for providers with an explicit post-configure start phase."""

    def start(self) -> None:
        """Start provider-owned background work after host readiness."""

        ...


@runtime_checkable
class MigrationsProvider(Protocol):
    """Optional mixin for providers that run IndexedDB migrations on configure."""

    def migration_options(
        self, name: str, config: dict[str, Any]
    ) -> Any:
        """Return migration revisions or run options for this configure call."""

        ...


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


RegisterServices = Callable[[Any, AppProvider], None]

CALLER_BEARER_TOKEN_METADATA_KEY = "x-gestalt-caller-bearer-token"


@dataclass(frozen=True)
class IdentityCallContext:
    """Caller-scoped authentication metadata for grant-management RPCs."""

    caller_bearer_token: str = ""


class AppProviderAdapter:
    """Wrap a provider and registration callback for integration runtimes."""

    __slots__ = ("kind", "provider", "register_services")

    def __init__(
        self,
        kind: ProviderKind | str,
        provider: AppProvider,
        register_services: RegisterServices,
    ) -> None:
        self.kind = kind
        self.provider = provider
        self.register_services = register_services

    def serve(self) -> None:
        """Start the provider's gRPC runtime."""

        from . import _runtime

        _runtime.serve(self)


class IdentityProvider(AppProvider):
    """Base class for identity providers."""

    def authorize(self, request: AuthorizeRequest) -> AuthorizeResponse:
        """Start an RFC 6749 authorization flow."""

        raise NotImplementedError

    def token(self, request: TokenRequest) -> TokenResponse:
        """Issue or exchange tokens via the RFC 6749 token endpoint."""

        raise NotImplementedError

    def introspect(self, request: IntrospectRequest) -> IntrospectResponse:
        """Introspect a bearer token via RFC 7662."""

        raise NotImplementedError

    def user_info(
        self, request: UserInfoRequest, call: IdentityCallContext
    ) -> UserInfoResponse:
        """Return profile claims for the authenticated caller."""

        raise NotImplementedError

    def list_grants(
        self, request: ListGrantsRequest, call: IdentityCallContext
    ) -> ListGrantsResponse:
        """List grant IDs visible to the caller."""

        raise NotImplementedError

    def get_grant(
        self, request: GetGrantRequest, call: IdentityCallContext
    ) -> GetGrantResponse:
        """Return one grant owned by the caller."""

        raise NotImplementedError

    def revoke_grant(
        self, request: RevokeGrantRequest, call: IdentityCallContext
    ) -> RevokeGrantResponse:
        """Revoke one grant owned by the caller."""

        raise NotImplementedError

    def serve(self) -> None:
        """Start the authentication runtime."""

        from . import _runtime

        _runtime.serve(self, runtime_kind=ProviderKind.IDENTITY)


class AuthorizationProvider(AppProvider):
    """Base class for authorization providers."""

    def check_access(self, request: CheckAccessRequest) -> CheckAccessResponse:
        """Return whether a single access request is allowed."""

        raise NotImplementedError

    def check_access_many(
        self, request: CheckAccessManyRequest
    ) -> CheckAccessManyResponse:
        """Return decisions for a batch of access requests."""

        raise NotImplementedError

    def list_relationships(
        self, request: ListRelationshipsRequest
    ) -> ListRelationshipsResponse:
        """List relationships matching the supplied filter."""

        raise NotImplementedError

    def add_relationship(
        self, request: AddRelationshipRequest
    ) -> AddRelationshipResponse:
        """Add a relationship and return the stored relationship."""

        raise NotImplementedError

    def delete_relationship(
        self, request: DeleteRelationshipRequest
    ) -> DeleteRelationshipResponse | None:
        """Delete a relationship tuple."""

        raise NotImplementedError

    def set_authorization_state(
        self, request: SetAuthorizationStateRequest
    ) -> SetAuthorizationStateResponse:
        """Atomically replace the model and relationships."""

        raise NotImplementedError

    def get_active_model_ref(self) -> GetActiveModelRefResponse:
        """Return the active authorization model reference."""

        raise NotImplementedError

    def set_active_model(self, request: SetActiveModelRequest) -> SetActiveModelResponse:
        """Set the active authorization model."""

        raise NotImplementedError

    def list_active_model_resource_types(
        self, request: ListActiveModelResourceTypesRequest
    ) -> ListActiveModelResourceTypesResponse:
        """List resource types in the active authorization model."""

        raise NotImplementedError

    def serve(self) -> None:
        """Start the authorization runtime."""

        from . import _runtime

        _runtime.serve(self, runtime_kind=ProviderKind.AUTHORIZATION)


class ExternalTokenValidator:
    """Optional mixin for providers that validate external bearer tokens."""

    def validate_external_token(self, token: str) -> Any | None:
        """Validate a bearer token and return the authenticated subject."""

        raise NotImplementedError


class SessionTTLProvider:
    """Optional mixin for providers that control session lifetimes."""

    def session_ttl(self) -> dt.timedelta:
        """Return the requested session time-to-live."""

        raise NotImplementedError


class SecretsProvider(AppProvider):
    """Base class for secret-provider runtimes."""

    def get_secret(self, name: str) -> str:
        """Return a secret value by name."""

        raise NotImplementedError

    def serve(self) -> None:
        """Start the secrets runtime."""

        from . import _runtime

        _runtime.serve(self, runtime_kind=ProviderKind.SECRETS)


class CacheProvider(AppProvider):
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

    def set_many(
        self, entries: list[CacheSetEntry], ttl: dt.timedelta | None = None
    ) -> None:
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


class S3Provider(AppProvider):
    """Base class for S3-compatible object store runtimes.

    Handler methods accept and return the generated native request and
    response models from :mod:`gestalt.s3`.
    """

    def head_object(self, request: HeadObjectRequest) -> HeadObjectResponse:
        """Return object metadata without reading the object body."""

        self._unimplemented("head_object")

    def read_object(self, request: ReadObjectRequest) -> ProviderReadResult:
        """Return metadata and a streaming body for an object."""

        self._unimplemented("read_object")

    def write_object(
        self,
        open: WriteObjectOpen,
        body: Iterable[bytes],
    ) -> WriteObjectResponse:
        """Consume an object body stream and return committed metadata."""

        self._unimplemented("write_object")

    def delete_object(self, request: DeleteObjectRequest) -> None:
        """Delete one object or object version."""

        self._unimplemented("delete_object")

    def list_objects(self, request: ListObjectsRequest) -> ListObjectsResponse:
        """List objects using S3-style pagination and delimiters."""

        self._unimplemented("list_objects")

    def copy_object(self, request: CopyObjectRequest) -> CopyObjectResponse:
        """Copy one object to another object reference."""

        self._unimplemented("copy_object")

    def presign_object(self, request: PresignObjectRequest) -> PresignObjectResponse:
        """Return a presigned request URL for one object operation."""

        self._unimplemented("presign_object")

    def serve(self) -> None:
        """Start the S3 runtime."""

        from . import _runtime

        _runtime.serve(self, runtime_kind=ProviderKind.S3)


class AgentProvider(AppProvider):
    """Base class for agent-provider runtimes.

    Subclasses implement snake_case handler methods such as
    ``create_session(request)``, ``create_turn(request)``, and
    ``get_capabilities(request)``. Request and response objects are native SDK
    dataclasses; the runtime owns transport conversion.
    """

    def create_session(
        self, request: CreateAgentProviderSessionRequest
    ) -> AgentSession:
        """Create a session, minting its id.

        Must be idempotent on ``request.idempotency_key`` scoped per subject
        (``request.created_by_subject_id``); an empty key always creates.
        """
        self._unimplemented("create_session")

    def get_session(self, request: GetAgentProviderSessionRequest) -> AgentSession:
        self._unimplemented("get_session")

    def list_sessions(
        self, request: ListAgentProviderSessionsRequest
    ) -> ListAgentProviderSessionsResponse:
        self._unimplemented("list_sessions")

    def update_session(
        self, request: UpdateAgentProviderSessionRequest
    ) -> AgentSession:
        self._unimplemented("update_session")

    def create_turn(self, request: CreateAgentProviderTurnRequest) -> AgentTurn:
        self._unimplemented("create_turn")

    def get_turn(self, request: GetAgentProviderTurnRequest) -> AgentTurn:
        self._unimplemented("get_turn")

    def list_turns(
        self, request: ListAgentProviderTurnsRequest
    ) -> ListAgentProviderTurnsResponse:
        self._unimplemented("list_turns")

    def cancel_turn(self, request: CancelAgentProviderTurnRequest) -> AgentTurn:
        self._unimplemented("cancel_turn")

    def list_turn_events(
        self, request: ListAgentProviderTurnEventsRequest
    ) -> ListAgentProviderTurnEventsResponse:
        self._unimplemented("list_turn_events")

    def get_interaction(
        self, request: GetAgentProviderInteractionRequest
    ) -> AgentInteraction:
        self._unimplemented("get_interaction")

    def list_interactions(
        self, request: ListAgentProviderInteractionsRequest
    ) -> ListAgentProviderInteractionsResponse:
        self._unimplemented("list_interactions")

    def resolve_interaction(
        self, request: ResolveAgentProviderInteractionRequest
    ) -> AgentInteraction:
        self._unimplemented("resolve_interaction")

    def get_capabilities(
        self, request: GetAgentProviderCapabilitiesRequest
    ) -> AgentProviderCapabilities:
        self._unimplemented("get_capabilities")

    def serve(self) -> None:
        """Start the agent runtime."""

        from . import _runtime

        _runtime.serve(self, runtime_kind=ProviderKind.AGENT)


class RuntimeProvider(AppProvider):
    """Base class for hosted runtime providers.

    Subclasses implement snake_case handler methods such as
    ``get_support(request)``, ``start_session(request)``, and
    ``start_app(request)``.
    """

    def get_support(
        self,
        request: GetRuntimeSupportRequest,
    ) -> RuntimeSupport:
        self._unimplemented("get_support")

    def start_session(
        self,
        request: StartRuntimeSessionRequest,
    ) -> RuntimeSession:
        self._unimplemented("start_session")

    def get_session(
        self,
        request: GetRuntimeSessionRequest,
    ) -> RuntimeSession:
        self._unimplemented("get_session")

    def list_sessions(
        self,
        request: ListRuntimeSessionsRequest,
    ) -> ListRuntimeSessionsResponse:
        self._unimplemented("list_sessions")

    def stop_session(self, request: StopRuntimeSessionRequest) -> None:
        self._unimplemented("stop_session")

    def prepare_workspace(
        self,
        request: PrepareRuntimeWorkspaceRequest,
    ) -> PrepareRuntimeWorkspaceResponse:
        self._unimplemented("prepare_workspace")

    def remove_workspace(
        self,
        request: RemoveRuntimeWorkspaceRequest,
    ) -> None:
        self._unimplemented("remove_workspace")

    def start_app(self, request: StartHostedAppRequest) -> HostedApp:
        self._unimplemented("start_app")

    def serve(self) -> None:
        """Start the runtime provider."""

        from . import _runtime

        _runtime.serve(self, runtime_kind=ProviderKind.RUNTIME)


class WorkflowProvider(AppProvider):
    """Base class for workflow-provider runtimes.

    Subclasses implement snake_case handler methods such as
    ``start_run(request)``, ``signal_run(request)``, and
    ``deliver_event(request)``.
    """

    def apply_definition(
        self,
        request: ApplyWorkflowProviderDefinitionRequest,
    ) -> WorkflowDefinition:
        self._unimplemented("apply_definition")

    def get_definition(
        self,
        request: GetWorkflowProviderDefinitionRequest,
    ) -> WorkflowDefinition:
        self._unimplemented("get_definition")

    def list_definitions(
        self,
        request: ListWorkflowProviderDefinitionsRequest,
    ) -> ListWorkflowProviderDefinitionsResponse:
        self._unimplemented("list_definitions")

    def set_definition_paused(
        self,
        request: SetWorkflowProviderDefinitionPausedRequest,
    ) -> WorkflowDefinition:
        self._unimplemented("set_definition_paused")

    def set_activation_paused(
        self,
        request: SetWorkflowProviderActivationPausedRequest,
    ) -> WorkflowDefinition:
        self._unimplemented("set_activation_paused")

    def delete_definition(
        self,
        request: DeleteWorkflowProviderDefinitionRequest,
    ) -> None:
        self._unimplemented("delete_definition")

    def start_run(
        self,
        request: StartWorkflowProviderRunRequest,
    ) -> WorkflowRun:
        self._unimplemented("start_run")

    def get_run(self, request: GetWorkflowProviderRunRequest) -> WorkflowRun:
        self._unimplemented("get_run")

    def list_runs(
        self,
        request: ListWorkflowProviderRunsRequest,
    ) -> ListWorkflowProviderRunsResponse:
        self._unimplemented("list_runs")

    def cancel_run(
        self,
        request: CancelWorkflowProviderRunRequest,
    ) -> WorkflowRun:
        self._unimplemented("cancel_run")

    def get_run_events(
        self,
        request: GetWorkflowProviderRunEventsRequest,
    ) -> GetWorkflowProviderRunEventsResponse:
        self._unimplemented("get_run_events")

    def get_run_output(
        self,
        request: GetWorkflowProviderRunOutputRequest,
    ) -> GetWorkflowProviderRunOutputResponse:
        self._unimplemented("get_run_output")

    def signal_run(
        self,
        request: SignalWorkflowProviderRunRequest,
    ) -> SignalWorkflowRunResponse:
        self._unimplemented("signal_run")

    def signal_or_start_run(
        self,
        request: SignalOrStartWorkflowProviderRunRequest,
    ) -> SignalWorkflowRunResponse:
        self._unimplemented("signal_or_start_run")

    def deliver_event(
        self,
        request: DeliverWorkflowProviderEventRequest,
    ) -> WorkflowEvent:
        self._unimplemented("deliver_event")

    def serve(self) -> None:
        """Start the workflow runtime."""

        from . import _runtime

        _runtime.serve(self, runtime_kind=ProviderKind.WORKFLOW)
