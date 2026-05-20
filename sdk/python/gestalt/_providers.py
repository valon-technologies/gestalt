"""Provider base classes for non-integration Gestalt runtimes.

Handwritten helpers in :mod:`gestalt` construct transport payloads for providers
without requiring provider code to import transport modules.
"""

from __future__ import annotations

import datetime as dt
from enum import Enum
from http import HTTPStatus
from typing import (
    TYPE_CHECKING,
    Any,
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
    from ._authentication import (
        AuthenticatedUser,
        BeginLoginRequest,
        BeginLoginResponse,
        CompleteLoginRequest,
    )
    from ._cache import CacheEntry
    from ._gen.v1.authorization_pb2 import (
        AccessDecision,
        AccessEvaluationRequest,
        AccessEvaluationsRequest,
        AccessEvaluationsResponse,
        ActionSearchRequest,
        ActionSearchResponse,
        AuthorizationMetadata,
        AuthorizationModelRef,
        EffectiveSubjectSearchRequest,
        EffectiveSubjectSearchResponse,
        ExpandRequest,
        ExpandResponse,
        GetActiveModelResponse,
        ListModelsRequest,
        ListModelsResponse,
        ReadRelationshipsRequest,
        ReadRelationshipsResponse,
        ResourceSearchRequest,
        ResourceSearchResponse,
        SubjectSearchRequest,
        SubjectSearchResponse,
        WriteModelRequest,
        WriteRelationshipsRequest,
    )
    from ._pluginruntime import (
        GetPluginRuntimeSessionRequest,
        GetPluginRuntimeSupportRequest,
        HostedPlugin,
        ListPluginRuntimeSessionsRequest,
        ListPluginRuntimeSessionsResponse,
        PluginRuntimeSession,
        PluginRuntimeSupport,
        PreparePluginRuntimeWorkspaceRequest,
        PreparePluginRuntimeWorkspaceResponse,
        RemovePluginRuntimeWorkspaceRequest,
        StartHostedPluginRequest,
        StartPluginRuntimeSessionRequest,
        StopPluginRuntimeSessionRequest,
    )
    from ._s3 import (
        CopyOptions,
        ListOptions,
        ListPage,
        ObjectMeta,
        ObjectRef,
        PresignOptions,
        PresignResult,
        ProviderReadResult,
        ReadOptions,
        WriteOptions,
    )
    from ._workflow import (
        ApplyWorkflowDefinitionRequest,
        CancelWorkflowRunRequest,
        DeleteWorkflowDefinitionRequest,
        DeliverWorkflowEventRequest,
        DeliverWorkflowEventResponse,
        GetWorkflowDefinitionRequest,
        GetWorkflowRunEventsRequest,
        GetWorkflowRunOutputRequest,
        GetWorkflowRunRequest,
        ListWorkflowDefinitionsRequest,
        ListWorkflowDefinitionsResponse,
        ListWorkflowRunEventsResponse,
        ListWorkflowRunsRequest,
        ListWorkflowRunsResponse,
        SetWorkflowActivationPausedRequest,
        SetWorkflowDefinitionPausedRequest,
        SignalOrStartWorkflowRunRequest,
        SignalWorkflowRunRequest,
        StartWorkflowRunRequest,
        WorkflowDefinition,
        WorkflowRun,
        WorkflowRunOutput,
        WorkflowRunSignal,
    )

else:
    from ._api import Error


class ProviderKind(str, Enum):
    """Runtime kinds supported by the Python SDK."""

    INTEGRATION = "integration"
    AUTHENTICATION = "authentication"
    AUTHORIZATION = "authorization"
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


class PluginProvider:
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

    def begin_login(self, request: BeginLoginRequest) -> BeginLoginResponse:
        """Begin an interactive login flow."""

        raise NotImplementedError

    def complete_login(self, request: CompleteLoginRequest) -> AuthenticatedUser:
        """Complete an interactive login flow."""

        raise NotImplementedError

    def serve(self) -> None:
        """Start the authentication runtime."""

        from . import _runtime

        _runtime.serve(self, runtime_kind=ProviderKind.AUTHENTICATION)


class AuthorizationProvider(PluginProvider):
    """Base class for authorization-provider runtimes."""

    def evaluate(self, request: AccessEvaluationRequest) -> AccessDecision:
        self._unimplemented("evaluate")

    def evaluate_many(
        self, request: AccessEvaluationsRequest
    ) -> AccessEvaluationsResponse:
        self._unimplemented("evaluate_many")

    def search_resources(
        self, request: ResourceSearchRequest
    ) -> ResourceSearchResponse:
        self._unimplemented("search_resources")

    def search_subjects(self, request: SubjectSearchRequest) -> SubjectSearchResponse:
        self._unimplemented("search_subjects")

    def effective_search_resources(
        self, request: ResourceSearchRequest
    ) -> ResourceSearchResponse:
        self._unimplemented("effective_search_resources")

    def effective_search_subjects(
        self, request: EffectiveSubjectSearchRequest
    ) -> EffectiveSubjectSearchResponse:
        self._unimplemented("effective_search_subjects")

    def search_actions(self, request: ActionSearchRequest) -> ActionSearchResponse:
        self._unimplemented("search_actions")

    def expand(self, request: ExpandRequest) -> ExpandResponse:
        self._unimplemented("expand")

    def get_metadata(self) -> AuthorizationMetadata:
        self._unimplemented("get_metadata")

    def read_relationships(
        self, request: ReadRelationshipsRequest
    ) -> ReadRelationshipsResponse:
        self._unimplemented("read_relationships")

    def write_relationships(self, request: WriteRelationshipsRequest) -> None:
        self._unimplemented("write_relationships")

    def get_active_model(self) -> GetActiveModelResponse:
        self._unimplemented("get_active_model")

    def list_models(self, request: ListModelsRequest) -> ListModelsResponse:
        self._unimplemented("list_models")

    def write_model(self, request: WriteModelRequest) -> AuthorizationModelRef:
        self._unimplemented("write_model")

    def serve(self) -> None:
        """Start the authorization runtime."""

        from . import _runtime

        _runtime.serve(self, runtime_kind=ProviderKind.AUTHORIZATION)


class ExternalTokenValidator:
    """Optional mixin for providers that validate external bearer tokens."""

    def validate_external_token(self, token: str) -> AuthenticatedUser | None:
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

    def set_many(
        self, entries: list[CacheEntry], ttl: dt.timedelta | None = None
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


class S3Provider(PluginProvider):
    """Base class for S3-compatible object store runtimes."""

    def head_object(self, ref: ObjectRef) -> ObjectMeta:
        """Return object metadata without reading the object body."""

        self._unimplemented("head_object")

    def read_object(
        self,
        ref: ObjectRef,
        opts: ReadOptions | None = None,
    ) -> ProviderReadResult:
        """Return metadata and a streaming body for an object."""

        self._unimplemented("read_object")

    def write_object(
        self,
        ref: ObjectRef,
        body: Iterable[bytes],
        opts: WriteOptions | None = None,
    ) -> ObjectMeta:
        """Consume an object body stream and return committed metadata."""

        self._unimplemented("write_object")

    def delete_object(self, ref: ObjectRef) -> None:
        """Delete one object or object version."""

        self._unimplemented("delete_object")

    def list_objects(self, opts: ListOptions) -> ListPage:
        """List objects using S3-style pagination and delimiters."""

        self._unimplemented("list_objects")

    def copy_object(
        self,
        source: ObjectRef,
        destination: ObjectRef,
        opts: CopyOptions | None = None,
    ) -> ObjectMeta:
        """Copy one object to another object reference."""

        self._unimplemented("copy_object")

    def presign_object(
        self,
        ref: ObjectRef,
        opts: PresignOptions | None = None,
    ) -> PresignResult:
        """Return a presigned request URL for one object operation."""

        self._unimplemented("presign_object")

    def serve(self) -> None:
        """Start the S3 runtime."""

        from . import _runtime

        _runtime.serve(self, runtime_kind=ProviderKind.S3)


class AgentProvider(PluginProvider):
    """Base class for agent-provider runtimes.

    Subclasses implement snake_case handler methods such as
    ``create_session(request)``, ``create_turn(request)``, and
    ``get_capabilities(request)``. Request and response objects are native SDK
    dataclasses; the runtime owns transport conversion.
    """

    def create_session(
        self, request: CreateAgentProviderSessionRequest
    ) -> AgentSession:
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


class PluginRuntimeProvider(PluginProvider):
    """Base class for hosted plugin-runtime providers.

    Subclasses implement snake_case handler methods such as
    ``get_support(request)``, ``start_session(request)``, and
    ``start_plugin(request)``.
    """

    def get_support(
        self,
        request: GetPluginRuntimeSupportRequest,
    ) -> PluginRuntimeSupport:
        self._unimplemented("get_support")

    def start_session(
        self,
        request: StartPluginRuntimeSessionRequest,
    ) -> PluginRuntimeSession:
        self._unimplemented("start_session")

    def get_session(
        self,
        request: GetPluginRuntimeSessionRequest,
    ) -> PluginRuntimeSession:
        self._unimplemented("get_session")

    def list_sessions(
        self,
        request: ListPluginRuntimeSessionsRequest,
    ) -> ListPluginRuntimeSessionsResponse:
        self._unimplemented("list_sessions")

    def stop_session(self, request: StopPluginRuntimeSessionRequest) -> None:
        self._unimplemented("stop_session")

    def prepare_workspace(
        self,
        request: PreparePluginRuntimeWorkspaceRequest,
    ) -> PreparePluginRuntimeWorkspaceResponse:
        self._unimplemented("prepare_workspace")

    def remove_workspace(
        self,
        request: RemovePluginRuntimeWorkspaceRequest,
    ) -> None:
        self._unimplemented("remove_workspace")

    def start_plugin(self, request: StartHostedPluginRequest) -> HostedPlugin:
        self._unimplemented("start_plugin")

    def serve(self) -> None:
        """Start the plugin-runtime provider."""

        from . import _runtime

        _runtime.serve(self, runtime_kind=ProviderKind.RUNTIME)


class WorkflowProvider(PluginProvider):
    """Base class for workflow-provider runtimes.

    Subclasses implement snake_case handler methods such as
    ``apply_definition(request)``, ``start_run(request)``,
    ``signal_run(request)``, and ``deliver_event(request)``.
    """

    def apply_definition(
        self,
        request: ApplyWorkflowDefinitionRequest,
    ) -> WorkflowDefinition:
        self._unimplemented("apply_definition")

    def get_definition(
        self,
        request: GetWorkflowDefinitionRequest,
    ) -> WorkflowDefinition:
        self._unimplemented("get_definition")

    def list_definitions(
        self,
        request: ListWorkflowDefinitionsRequest,
    ) -> ListWorkflowDefinitionsResponse:
        self._unimplemented("list_definitions")

    def delete_definition(self, request: DeleteWorkflowDefinitionRequest) -> None:
        self._unimplemented("delete_definition")

    def set_definition_paused(
        self,
        request: SetWorkflowDefinitionPausedRequest,
    ) -> WorkflowDefinition:
        self._unimplemented("set_definition_paused")

    def set_activation_paused(
        self,
        request: SetWorkflowActivationPausedRequest,
    ) -> WorkflowDefinition:
        self._unimplemented("set_activation_paused")

    def start_run(
        self,
        request: StartWorkflowRunRequest,
    ) -> WorkflowRun:
        self._unimplemented("start_run")

    def get_run(self, request: GetWorkflowRunRequest) -> WorkflowRun:
        self._unimplemented("get_run")

    def list_runs(
        self,
        request: ListWorkflowRunsRequest,
    ) -> ListWorkflowRunsResponse:
        self._unimplemented("list_runs")

    def cancel_run(
        self,
        request: CancelWorkflowRunRequest,
    ) -> WorkflowRun:
        self._unimplemented("cancel_run")

    def signal_run(
        self,
        request: SignalWorkflowRunRequest,
    ) -> WorkflowRunSignal:
        self._unimplemented("signal_run")

    def signal_or_start_run(
        self,
        request: SignalOrStartWorkflowRunRequest,
    ) -> WorkflowRunSignal:
        self._unimplemented("signal_or_start_run")

    def deliver_event(
        self,
        request: DeliverWorkflowEventRequest,
    ) -> DeliverWorkflowEventResponse:
        self._unimplemented("deliver_event")

    def get_run_events(
        self,
        request: GetWorkflowRunEventsRequest,
    ) -> ListWorkflowRunEventsResponse:
        self._unimplemented("get_run_events")

    def get_run_output(
        self,
        request: GetWorkflowRunOutputRequest,
    ) -> WorkflowRunOutput:
        self._unimplemented("get_run_output")

    def serve(self) -> None:
        """Start the workflow runtime."""

        from . import _runtime

        _runtime.serve(self, runtime_kind=ProviderKind.WORKFLOW)
