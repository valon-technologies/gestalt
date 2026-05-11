from __future__ import annotations

import datetime as _dt
import os
from collections.abc import Iterable, Mapping, Sequence
from dataclasses import dataclass, field
from typing import Any, TypeAlias

import grpc

from ._gen.v1 import agent_pb2 as _pb
from ._gen.v1 import agent_pb2_grpc as _pb_grpc
from ._grpc_transport import host_service_channel
from ._protocol import (
    JsonObjectInput,
    datetime_from_timestamp,
    has_field,
    message_from_dict,
    message_to_dict,
    struct_from_dict,
    struct_to_dict,
    timestamp_from_datetime,
    value_from_json,
)
from ._protocol import (
    copy_message as _copy,
)
from ._protocol import (
    dataclass_mapping as _dataclass_mapping,
)
from ._protocol import (
    input_data as _data,
)

pb: Any = _pb
pb_grpc: Any = _pb_grpc

ENV_AGENT_HOST_SOCKET = "GESTALT_AGENT_HOST_SOCKET"
ENV_AGENT_HOST_SOCKET_TOKEN = f"{ENV_AGENT_HOST_SOCKET}_TOKEN"
ENV_AGENT_MANAGER_SOCKET = "GESTALT_AGENT_MANAGER_SOCKET"
ENV_AGENT_MANAGER_SOCKET_TOKEN = f"{ENV_AGENT_MANAGER_SOCKET}_TOKEN"

AGENT_EXECUTION_STATUS_UNSPECIFIED = pb.AGENT_EXECUTION_STATUS_UNSPECIFIED
AGENT_EXECUTION_STATUS_PENDING = pb.AGENT_EXECUTION_STATUS_PENDING
AGENT_EXECUTION_STATUS_RUNNING = pb.AGENT_EXECUTION_STATUS_RUNNING
AGENT_EXECUTION_STATUS_SUCCEEDED = pb.AGENT_EXECUTION_STATUS_SUCCEEDED
AGENT_EXECUTION_STATUS_FAILED = pb.AGENT_EXECUTION_STATUS_FAILED
AGENT_EXECUTION_STATUS_CANCELED = pb.AGENT_EXECUTION_STATUS_CANCELED
AGENT_EXECUTION_STATUS_WAITING_FOR_INPUT = pb.AGENT_EXECUTION_STATUS_WAITING_FOR_INPUT

AGENT_INTERACTION_STATE_UNSPECIFIED = pb.AGENT_INTERACTION_STATE_UNSPECIFIED
AGENT_INTERACTION_STATE_PENDING = pb.AGENT_INTERACTION_STATE_PENDING
AGENT_INTERACTION_STATE_RESOLVED = pb.AGENT_INTERACTION_STATE_RESOLVED
AGENT_INTERACTION_STATE_CANCELED = pb.AGENT_INTERACTION_STATE_CANCELED

AGENT_INTERACTION_TYPE_UNSPECIFIED = pb.AGENT_INTERACTION_TYPE_UNSPECIFIED
AGENT_INTERACTION_TYPE_INPUT = pb.AGENT_INTERACTION_TYPE_INPUT
AGENT_INTERACTION_TYPE_APPROVAL = pb.AGENT_INTERACTION_TYPE_APPROVAL
AGENT_INTERACTION_TYPE_CLARIFICATION = pb.AGENT_INTERACTION_TYPE_CLARIFICATION

AGENT_MESSAGE_PART_TYPE_UNSPECIFIED = pb.AGENT_MESSAGE_PART_TYPE_UNSPECIFIED
AGENT_MESSAGE_PART_TYPE_TEXT = pb.AGENT_MESSAGE_PART_TYPE_TEXT
AGENT_MESSAGE_PART_TYPE_JSON = pb.AGENT_MESSAGE_PART_TYPE_JSON
AGENT_MESSAGE_PART_TYPE_TOOL_CALL = pb.AGENT_MESSAGE_PART_TYPE_TOOL_CALL
AGENT_MESSAGE_PART_TYPE_TOOL_RESULT = pb.AGENT_MESSAGE_PART_TYPE_TOOL_RESULT
AGENT_MESSAGE_PART_TYPE_IMAGE_REF = pb.AGENT_MESSAGE_PART_TYPE_IMAGE_REF

AGENT_SESSION_STATE_UNSPECIFIED = pb.AGENT_SESSION_STATE_UNSPECIFIED
AGENT_SESSION_STATE_ACTIVE = pb.AGENT_SESSION_STATE_ACTIVE
AGENT_SESSION_STATE_ARCHIVED = pb.AGENT_SESSION_STATE_ARCHIVED

AGENT_TOOL_SOURCE_MODE_UNSPECIFIED = pb.AGENT_TOOL_SOURCE_MODE_UNSPECIFIED
AGENT_TOOL_SOURCE_MODE_MCP_CATALOG = pb.AGENT_TOOL_SOURCE_MODE_MCP_CATALOG

TimestampInput: TypeAlias = _dt.datetime | None
JsonObject: TypeAlias = dict[str, Any]


@dataclass(slots=True)
class AgentMessageInput:
    role: str = ""
    text: str = ""
    parts: Sequence[Any] | None = None
    metadata: Any | None = None


@dataclass(slots=True)
class AgentMessagePartInput:
    type: int = AGENT_MESSAGE_PART_TYPE_UNSPECIFIED
    text: str = ""
    json: Any | None = None
    tool_call: Any | None = None
    tool_result: Any | None = None
    image_ref: Any | None = None


@dataclass(slots=True)
class AgentMessagePartToolCallInput:
    id: str = ""
    tool_id: str = ""
    arguments: Any | None = None


@dataclass(slots=True)
class AgentMessagePartToolResultInput:
    tool_call_id: str = ""
    status: int = 0
    content: str = ""
    output: Any | None = None


@dataclass(slots=True)
class AgentMessagePartImageRefInput:
    uri: str = ""
    mime_type: str = ""


@dataclass(slots=True)
class AgentToolRefInput:
    plugin: str = ""
    operation: str = ""
    connection: str = ""
    instance: str = ""
    title: str = ""
    description: str = ""
    system: str = ""


@dataclass(slots=True)
class AgentWorkspaceGitCheckoutInput:
    url: str = ""
    ref: str = ""
    path: str = ""


@dataclass(slots=True)
class AgentWorkspaceInput:
    checkouts: Sequence[Any] | None = None
    cwd: str = ""


@dataclass(slots=True)
class AgentManagerCreateSessionInput:
    provider_name: str = ""
    model: str = ""
    client_ref: str = ""
    metadata: Any | None = None
    idempotency_key: str = ""
    workspace: Any | None = None


@dataclass(slots=True)
class AgentManagerGetSessionInput:
    session_id: str = ""


@dataclass(slots=True)
class AgentManagerListSessionsInput:
    provider_name: str = ""
    state: int = AGENT_SESSION_STATE_UNSPECIFIED
    limit: int = 0
    summary_only: bool = False


@dataclass(slots=True)
class AgentManagerUpdateSessionInput:
    session_id: str = ""
    client_ref: str = ""
    state: int = AGENT_SESSION_STATE_UNSPECIFIED
    metadata: Any | None = None


@dataclass(slots=True)
class AgentManagerCreateTurnInput:
    session_id: str = ""
    model: str = ""
    messages: Sequence[Any] | None = None
    tool_refs: Sequence[Any] | None = None
    tool_source: int = AGENT_TOOL_SOURCE_MODE_UNSPECIFIED
    response_schema: Any | None = None
    metadata: Any | None = None
    idempotency_key: str = ""
    model_options: Any | None = None


@dataclass(slots=True)
class AgentManagerGetTurnInput:
    turn_id: str = ""


@dataclass(slots=True)
class AgentManagerListTurnsInput:
    session_id: str = ""
    status: int = AGENT_EXECUTION_STATUS_UNSPECIFIED
    limit: int = 0
    summary_only: bool = False


@dataclass(slots=True)
class AgentManagerCancelTurnInput:
    turn_id: str = ""
    reason: str = ""


@dataclass(slots=True)
class AgentManagerListTurnEventsInput:
    turn_id: str = ""
    after_seq: int = 0
    limit: int = 0


@dataclass(slots=True)
class AgentManagerListInteractionsInput:
    turn_id: str = ""


@dataclass(slots=True)
class AgentManagerResolveInteractionInput:
    turn_id: str = ""
    interaction_id: str = ""
    resolution: Any | None = None


@dataclass(slots=True)
class AgentMessagePartToolCall:
    id: str = ""
    tool_id: str = ""
    arguments: JsonObjectInput | None = None


@dataclass(slots=True)
class AgentMessagePartToolResult:
    tool_call_id: str = ""
    status: int = 0
    content: str = ""
    output: JsonObjectInput | None = None


@dataclass(slots=True)
class AgentMessagePartImageRef:
    uri: str = ""
    mime_type: str = ""


@dataclass(slots=True)
class AgentMessagePart:
    type: int = AGENT_MESSAGE_PART_TYPE_UNSPECIFIED
    text: str = ""
    json: JsonObjectInput | None = None
    tool_call: AgentMessagePartToolCall | Mapping[str, Any] | None = None
    tool_result: AgentMessagePartToolResult | Mapping[str, Any] | None = None
    image_ref: AgentMessagePartImageRef | Mapping[str, Any] | None = None


@dataclass(slots=True)
class AgentMessage:
    role: str = ""
    text: str = ""
    parts: Iterable[AgentMessagePart | Mapping[str, Any]] = field(default_factory=list)
    metadata: JsonObjectInput | None = None


@dataclass(slots=True)
class AgentActor:
    subject_id: str = ""
    subject_kind: str = ""
    display_name: str = ""
    auth_source: str = ""


@dataclass(slots=True)
class AgentSubjectContext:
    subject_id: str = ""
    subject_kind: str = ""
    credential_subject_id: str = ""
    display_name: str = ""
    auth_source: str = ""


@dataclass(slots=True)
class AgentToolRef:
    plugin: str = ""
    operation: str = ""
    connection: str = ""
    instance: str = ""
    title: str = ""
    description: str = ""
    system: str = ""


@dataclass(slots=True)
class AgentProviderCapabilities:
    streaming_text: bool = False
    tool_calls: bool = False
    parallel_tool_calls: bool = False
    structured_output: bool = False
    interactions: bool = False
    resumable_turns: bool = False
    reasoning_summaries: bool = False
    bounded_list_hydration: bool = False
    supported_tool_sources: Iterable[int] = field(default_factory=list)
    supports_session_start: bool = False
    supports_prepared_workspace: bool = False


@dataclass(slots=True)
class AgentSession:
    id: str = ""
    provider_name: str = ""
    model: str = ""
    client_ref: str = ""
    state: int = AGENT_SESSION_STATE_UNSPECIFIED
    metadata: JsonObjectInput | None = None
    created_by: AgentActor | Mapping[str, Any] | None = None
    created_at: TimestampInput = None
    updated_at: TimestampInput = None
    last_turn_at: TimestampInput = None


@dataclass(slots=True)
class AgentSessionStartHookOutput:
    additional_context: bool = False
    metadata: bool = False


@dataclass(slots=True)
class AgentSessionStartHook:
    id: str = ""
    type: str = ""
    command: Iterable[str] = field(default_factory=list)
    cwd: str = ""
    timeout: str = ""
    env: Mapping[str, str] = field(default_factory=dict)
    output: AgentSessionStartHookOutput | Mapping[str, Any] | None = None


@dataclass(slots=True)
class AgentSessionStartConfig:
    hooks: Iterable[AgentSessionStartHook | Mapping[str, Any]] = field(
        default_factory=list
    )


@dataclass(slots=True)
class AgentPreparedWorkspace:
    root: str = ""
    cwd: str = ""


@dataclass(slots=True)
class CreateAgentProviderSessionRequest:
    session_id: str = ""
    idempotency_key: str = ""
    model: str = ""
    client_ref: str = ""
    metadata: JsonObject | None = None
    created_by: AgentActor | None = None
    subject: AgentSubjectContext | None = None
    session_start: AgentSessionStartConfig | None = None
    prepared_workspace: AgentPreparedWorkspace | None = None


@dataclass(slots=True)
class GetAgentProviderSessionRequest:
    session_id: str = ""
    subject: AgentSubjectContext | None = None


@dataclass(slots=True)
class ListAgentProviderSessionsRequest:
    subject: AgentSubjectContext | None = None
    session_ids: Iterable[str] = field(default_factory=list)
    state: int = AGENT_SESSION_STATE_UNSPECIFIED
    limit: int = 0
    summary_only: bool = False


@dataclass(slots=True)
class ListAgentProviderSessionsResponse:
    sessions: Iterable[AgentSession | Mapping[str, Any]] = field(default_factory=list)


@dataclass(slots=True)
class UpdateAgentProviderSessionRequest:
    session_id: str = ""
    client_ref: str = ""
    state: int = AGENT_SESSION_STATE_UNSPECIFIED
    metadata: JsonObject | None = None
    subject: AgentSubjectContext | None = None


@dataclass(slots=True)
class AgentTurn:
    id: str = ""
    session_id: str = ""
    provider_name: str = ""
    model: str = ""
    status: int = AGENT_EXECUTION_STATUS_UNSPECIFIED
    messages: Iterable[AgentMessage | Mapping[str, Any]] = field(default_factory=list)
    output_text: str = ""
    structured_output: JsonObjectInput | None = None
    status_message: str = ""
    created_by: AgentActor | Mapping[str, Any] | None = None
    created_at: TimestampInput = None
    started_at: TimestampInput = None
    completed_at: TimestampInput = None
    execution_ref: str = ""


@dataclass(slots=True)
class AgentTurnDisplay:
    kind: str = ""
    phase: str = ""
    text: str = ""
    label: str = ""
    ref: str = ""
    parent_ref: str = ""
    input: Any = None
    output: Any = None
    error: Any = None
    action: str = ""
    format: str = ""
    language: str = ""


@dataclass(slots=True)
class ResolvedAgentTool:
    id: str = ""
    name: str = ""
    description: str = ""
    parameters_schema: JsonObjectInput | None = None


@dataclass(slots=True)
class CreateAgentProviderTurnRequest:
    turn_id: str = ""
    session_id: str = ""
    idempotency_key: str = ""
    model: str = ""
    messages: Iterable[AgentMessage] = field(default_factory=list)
    tools: Iterable[ResolvedAgentTool] = field(default_factory=list)
    response_schema: JsonObject | None = None
    metadata: JsonObject | None = None
    created_by: AgentActor | None = None
    execution_ref: str = ""
    tool_refs: Iterable[AgentToolRef] = field(default_factory=list)
    tool_source: int = AGENT_TOOL_SOURCE_MODE_UNSPECIFIED
    subject: AgentSubjectContext | None = None
    model_options: JsonObject | None = None
    run_grant: str = ""


@dataclass(slots=True)
class GetAgentProviderTurnRequest:
    turn_id: str = ""
    subject: AgentSubjectContext | None = None


@dataclass(slots=True)
class ListAgentProviderTurnsRequest:
    session_id: str = ""
    subject: AgentSubjectContext | None = None
    turn_ids: Iterable[str] = field(default_factory=list)
    status: int = AGENT_EXECUTION_STATUS_UNSPECIFIED
    limit: int = 0
    summary_only: bool = False


@dataclass(slots=True)
class ListAgentProviderTurnsResponse:
    turns: Iterable[AgentTurn | Mapping[str, Any]] = field(default_factory=list)


@dataclass(slots=True)
class CancelAgentProviderTurnRequest:
    turn_id: str = ""
    reason: str = ""
    subject: AgentSubjectContext | None = None


@dataclass(slots=True)
class AgentTurnEvent:
    id: str = ""
    turn_id: str = ""
    seq: int = 0
    type: str = ""
    source: str = ""
    visibility: str = ""
    data: JsonObjectInput | None = None
    created_at: TimestampInput = None
    display: AgentTurnDisplay | Mapping[str, Any] | None = None


@dataclass(slots=True)
class ListAgentProviderTurnEventsRequest:
    turn_id: str = ""
    after_seq: int = 0
    limit: int = 0
    subject: AgentSubjectContext | None = None


@dataclass(slots=True)
class ListAgentProviderTurnEventsResponse:
    events: Iterable[AgentTurnEvent | Mapping[str, Any]] = field(default_factory=list)


@dataclass(slots=True)
class AgentInteraction:
    id: str = ""
    type: int = AGENT_INTERACTION_TYPE_UNSPECIFIED
    state: int = AGENT_INTERACTION_STATE_UNSPECIFIED
    title: str = ""
    prompt: str = ""
    request: JsonObjectInput | None = None
    resolution: JsonObjectInput | None = None
    created_at: TimestampInput = None
    resolved_at: TimestampInput = None
    turn_id: str = ""
    session_id: str = ""


@dataclass(slots=True)
class GetAgentProviderInteractionRequest:
    interaction_id: str = ""
    subject: AgentSubjectContext | None = None


@dataclass(slots=True)
class ListAgentProviderInteractionsRequest:
    turn_id: str = ""
    subject: AgentSubjectContext | None = None


@dataclass(slots=True)
class ListAgentProviderInteractionsResponse:
    interactions: Iterable[AgentInteraction | Mapping[str, Any]] = field(
        default_factory=list
    )


@dataclass(slots=True)
class ResolveAgentProviderInteractionRequest:
    interaction_id: str = ""
    resolution: JsonObject | None = None
    subject: AgentSubjectContext | None = None


@dataclass(slots=True)
class GetAgentProviderCapabilitiesRequest:
    pass


@dataclass(slots=True)
class ExecuteAgentToolRequest:
    session_id: str = ""
    turn_id: str = ""
    tool_call_id: str = ""
    tool_id: str = ""
    arguments: JsonObjectInput | None = None
    idempotency_key: str = ""
    run_grant: str = ""


@dataclass(slots=True)
class ExecuteAgentToolResponse:
    status: int = 0
    body: str = ""


@dataclass(slots=True)
class AgentToolAnnotations:
    read_only_hint: bool | None = None
    idempotent_hint: bool | None = None
    destructive_hint: bool | None = None
    open_world_hint: bool | None = None


@dataclass(slots=True)
class ListedAgentTool:
    id: str = ""
    mcp_name: str = ""
    title: str = ""
    description: str = ""
    input_schema: str = ""
    output_schema: str = ""
    annotations: AgentToolAnnotations | None = None
    ref: AgentToolRef | None = None
    tags: Iterable[str] = field(default_factory=list)
    search_text: str = ""


@dataclass(slots=True)
class ListAgentToolsResponse:
    tools: Iterable[ListedAgentTool] = field(default_factory=list)
    next_page_token: str = ""


@dataclass(slots=True)
class ListAgentToolsRequest:
    session_id: str = ""
    turn_id: str = ""
    page_size: int = 0
    page_token: str = ""
    run_grant: str = ""
    query: str = ""


@dataclass(slots=True)
class ResolveAgentConnectionRequest:
    session_id: str = ""
    turn_id: str = ""
    connection: str = ""
    instance: str = ""
    run_grant: str = ""


@dataclass(slots=True)
class ResolvedAgentConnection:
    connection_id: str = ""
    connection: str = ""
    instance: str = ""
    mode: str = ""
    headers: Mapping[str, str] = field(default_factory=dict)
    params: Mapping[str, str] = field(default_factory=dict)
    expires_at: TimestampInput = None


def AgentManagerCreateSessionRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-manager create-session request."""

    return pb.AgentManagerCreateSessionRequest(*args, **kwargs)


def AgentManagerGetSessionRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-manager get-session request."""

    return pb.AgentManagerGetSessionRequest(*args, **kwargs)


def AgentManagerListSessionsRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-manager list-sessions request."""

    return pb.AgentManagerListSessionsRequest(*args, **kwargs)


def AgentManagerUpdateSessionRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-manager update-session request."""

    return pb.AgentManagerUpdateSessionRequest(*args, **kwargs)


def AgentManagerCreateTurnRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-manager create-turn request."""

    return pb.AgentManagerCreateTurnRequest(*args, **kwargs)


def AgentManagerGetTurnRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-manager get-turn request."""

    return pb.AgentManagerGetTurnRequest(*args, **kwargs)


def AgentManagerListTurnsRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-manager list-turns request."""

    return pb.AgentManagerListTurnsRequest(*args, **kwargs)


def AgentManagerCancelTurnRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-manager cancel-turn request."""

    return pb.AgentManagerCancelTurnRequest(*args, **kwargs)


def AgentManagerListTurnEventsRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-manager list-turn-events request."""

    return pb.AgentManagerListTurnEventsRequest(*args, **kwargs)


def AgentManagerListInteractionsRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-manager list-interactions request."""

    return pb.AgentManagerListInteractionsRequest(*args, **kwargs)


def AgentManagerResolveInteractionRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-manager resolve-interaction request."""

    return pb.AgentManagerResolveInteractionRequest(*args, **kwargs)


def create_agent_provider_session_request_from_proto(
    request: Any,
) -> CreateAgentProviderSessionRequest:
    return CreateAgentProviderSessionRequest(
        session_id=request.session_id,
        idempotency_key=request.idempotency_key,
        model=request.model,
        client_ref=request.client_ref,
        metadata=struct_to_dict(request.metadata)
        if has_field(request, "metadata")
        else None,
        created_by=agent_actor_from_proto(request.created_by)
        if has_field(request, "created_by")
        else None,
        subject=agent_subject_context_from_proto(request.subject)
        if has_field(request, "subject")
        else None,
        session_start=agent_session_start_config_from_proto(request.session_start)
        if has_field(request, "session_start")
        else None,
        prepared_workspace=prepared_workspace_from_proto(request.prepared_workspace)
        if has_field(request, "prepared_workspace")
        else None,
    )


def get_agent_provider_session_request_from_proto(
    request: Any,
) -> GetAgentProviderSessionRequest:
    return GetAgentProviderSessionRequest(
        session_id=request.session_id,
        subject=agent_subject_context_from_proto(request.subject)
        if has_field(request, "subject")
        else None,
    )


def list_agent_provider_sessions_request_from_proto(
    request: Any,
) -> ListAgentProviderSessionsRequest:
    return ListAgentProviderSessionsRequest(
        subject=agent_subject_context_from_proto(request.subject)
        if has_field(request, "subject")
        else None,
        session_ids=list(request.session_ids),
        state=request.state,
        limit=request.limit,
        summary_only=request.summary_only,
    )


def update_agent_provider_session_request_from_proto(
    request: Any,
) -> UpdateAgentProviderSessionRequest:
    return UpdateAgentProviderSessionRequest(
        session_id=request.session_id,
        client_ref=request.client_ref,
        state=request.state,
        metadata=struct_to_dict(request.metadata)
        if has_field(request, "metadata")
        else None,
        subject=agent_subject_context_from_proto(request.subject)
        if has_field(request, "subject")
        else None,
    )


def create_agent_provider_turn_request_from_proto(
    request: Any,
) -> CreateAgentProviderTurnRequest:
    return CreateAgentProviderTurnRequest(
        turn_id=request.turn_id,
        session_id=request.session_id,
        idempotency_key=request.idempotency_key,
        model=request.model,
        messages=[agent_message_from_proto(message) for message in request.messages],
        tools=[resolved_agent_tool_from_proto(tool) for tool in request.tools],
        response_schema=struct_to_dict(request.response_schema)
        if has_field(request, "response_schema")
        else None,
        metadata=struct_to_dict(request.metadata)
        if has_field(request, "metadata")
        else None,
        created_by=agent_actor_from_proto(request.created_by)
        if has_field(request, "created_by")
        else None,
        execution_ref=request.execution_ref,
        tool_refs=[agent_tool_ref_from_proto(ref) for ref in request.tool_refs],
        tool_source=request.tool_source,
        subject=agent_subject_context_from_proto(request.subject)
        if has_field(request, "subject")
        else None,
        model_options=struct_to_dict(request.model_options)
        if has_field(request, "model_options")
        else None,
        run_grant=request.run_grant,
    )


def get_agent_provider_turn_request_from_proto(
    request: Any,
) -> GetAgentProviderTurnRequest:
    return GetAgentProviderTurnRequest(
        turn_id=request.turn_id,
        subject=agent_subject_context_from_proto(request.subject)
        if has_field(request, "subject")
        else None,
    )


def list_agent_provider_turns_request_from_proto(
    request: Any,
) -> ListAgentProviderTurnsRequest:
    return ListAgentProviderTurnsRequest(
        session_id=request.session_id,
        subject=agent_subject_context_from_proto(request.subject)
        if has_field(request, "subject")
        else None,
        turn_ids=list(request.turn_ids),
        status=request.status,
        limit=request.limit,
        summary_only=request.summary_only,
    )


def cancel_agent_provider_turn_request_from_proto(
    request: Any,
) -> CancelAgentProviderTurnRequest:
    return CancelAgentProviderTurnRequest(
        turn_id=request.turn_id,
        reason=request.reason,
        subject=agent_subject_context_from_proto(request.subject)
        if has_field(request, "subject")
        else None,
    )


def list_agent_provider_turn_events_request_from_proto(
    request: Any,
) -> ListAgentProviderTurnEventsRequest:
    return ListAgentProviderTurnEventsRequest(
        turn_id=request.turn_id,
        after_seq=request.after_seq,
        limit=request.limit,
        subject=agent_subject_context_from_proto(request.subject)
        if has_field(request, "subject")
        else None,
    )


def get_agent_provider_interaction_request_from_proto(
    request: Any,
) -> GetAgentProviderInteractionRequest:
    return GetAgentProviderInteractionRequest(
        interaction_id=request.interaction_id,
        subject=agent_subject_context_from_proto(request.subject)
        if has_field(request, "subject")
        else None,
    )


def list_agent_provider_interactions_request_from_proto(
    request: Any,
) -> ListAgentProviderInteractionsRequest:
    return ListAgentProviderInteractionsRequest(
        turn_id=request.turn_id,
        subject=agent_subject_context_from_proto(request.subject)
        if has_field(request, "subject")
        else None,
    )


def resolve_agent_provider_interaction_request_from_proto(
    request: Any,
) -> ResolveAgentProviderInteractionRequest:
    return ResolveAgentProviderInteractionRequest(
        interaction_id=request.interaction_id,
        resolution=struct_to_dict(request.resolution)
        if has_field(request, "resolution")
        else None,
        subject=agent_subject_context_from_proto(request.subject)
        if has_field(request, "subject")
        else None,
    )


def agent_session_to_proto(value: AgentSession | Mapping[str, Any]) -> Any:
    session = _coerce(value, AgentSession, "AgentSession")
    out = pb.AgentSession(
        id=session.id,
        provider_name=session.provider_name,
        model=session.model,
        client_ref=session.client_ref,
        state=_int_field(session.state),
    )
    _copy_struct(out, "metadata", session.metadata)
    _copy_message(out, "created_by", agent_actor_to_proto(session.created_by))
    _copy_timestamp(out, "created_at", session.created_at)
    _copy_timestamp(out, "updated_at", session.updated_at)
    _copy_timestamp(out, "last_turn_at", session.last_turn_at)
    return out


def list_agent_provider_sessions_response_to_proto(
    value: ListAgentProviderSessionsResponse | Mapping[str, Any],
) -> Any:
    response = _coerce(
        value,
        ListAgentProviderSessionsResponse,
        "ListAgentProviderSessionsResponse",
    )
    return pb.ListAgentProviderSessionsResponse(
        sessions=[agent_session_to_proto(session) for session in response.sessions]
    )


def agent_turn_to_proto(value: AgentTurn | Mapping[str, Any]) -> Any:
    turn = _coerce(value, AgentTurn, "AgentTurn")
    out = pb.AgentTurn(
        id=turn.id,
        session_id=turn.session_id,
        provider_name=turn.provider_name,
        model=turn.model,
        status=_int_field(turn.status),
        messages=[agent_message_to_proto(message) for message in turn.messages],
        output_text=turn.output_text,
        status_message=turn.status_message,
        execution_ref=turn.execution_ref,
    )
    _copy_struct(out, "structured_output", turn.structured_output)
    _copy_message(out, "created_by", agent_actor_to_proto(turn.created_by))
    _copy_timestamp(out, "created_at", turn.created_at)
    _copy_timestamp(out, "started_at", turn.started_at)
    _copy_timestamp(out, "completed_at", turn.completed_at)
    return out


def list_agent_provider_turns_response_to_proto(
    value: ListAgentProviderTurnsResponse | Mapping[str, Any],
) -> Any:
    response = _coerce(
        value, ListAgentProviderTurnsResponse, "ListAgentProviderTurnsResponse"
    )
    return pb.ListAgentProviderTurnsResponse(
        turns=[agent_turn_to_proto(turn) for turn in response.turns]
    )


def agent_turn_event_to_proto(value: AgentTurnEvent | Mapping[str, Any]) -> Any:
    event = _coerce(value, AgentTurnEvent, "AgentTurnEvent")
    out = pb.AgentTurnEvent(
        id=event.id,
        turn_id=event.turn_id,
        seq=_int_field(event.seq),
        type=event.type,
        source=event.source,
        visibility=event.visibility,
    )
    _copy_struct(out, "data", event.data)
    _copy_timestamp(out, "created_at", event.created_at)
    _copy_message(out, "display", agent_turn_display_to_proto(event.display))
    return out


def list_agent_provider_turn_events_response_to_proto(
    value: ListAgentProviderTurnEventsResponse | Mapping[str, Any],
) -> Any:
    response = _coerce(
        value,
        ListAgentProviderTurnEventsResponse,
        "ListAgentProviderTurnEventsResponse",
    )
    return pb.ListAgentProviderTurnEventsResponse(
        events=[agent_turn_event_to_proto(event) for event in response.events]
    )


def agent_interaction_to_proto(value: AgentInteraction | Mapping[str, Any]) -> Any:
    interaction = _coerce(value, AgentInteraction, "AgentInteraction")
    out = pb.AgentInteraction(
        id=interaction.id,
        type=_int_field(interaction.type),
        state=_int_field(interaction.state),
        title=interaction.title,
        prompt=interaction.prompt,
        turn_id=interaction.turn_id,
        session_id=interaction.session_id,
    )
    _copy_struct(out, "request", interaction.request)
    _copy_struct(out, "resolution", interaction.resolution)
    _copy_timestamp(out, "created_at", interaction.created_at)
    _copy_timestamp(out, "resolved_at", interaction.resolved_at)
    return out


def list_agent_provider_interactions_response_to_proto(
    value: ListAgentProviderInteractionsResponse | Mapping[str, Any],
) -> Any:
    response = _coerce(
        value,
        ListAgentProviderInteractionsResponse,
        "ListAgentProviderInteractionsResponse",
    )
    return pb.ListAgentProviderInteractionsResponse(
        interactions=[
            agent_interaction_to_proto(interaction)
            for interaction in response.interactions
        ]
    )


def agent_provider_capabilities_to_proto(
    value: AgentProviderCapabilities | Mapping[str, Any],
) -> Any:
    capabilities = _coerce(
        value, AgentProviderCapabilities, "AgentProviderCapabilities"
    )
    return pb.AgentProviderCapabilities(
        streaming_text=capabilities.streaming_text,
        tool_calls=capabilities.tool_calls,
        parallel_tool_calls=capabilities.parallel_tool_calls,
        structured_output=capabilities.structured_output,
        interactions=capabilities.interactions,
        resumable_turns=capabilities.resumable_turns,
        reasoning_summaries=capabilities.reasoning_summaries,
        bounded_list_hydration=capabilities.bounded_list_hydration,
        supported_tool_sources=[
            _int_field(source) for source in capabilities.supported_tool_sources
        ],
        supports_session_start=capabilities.supports_session_start,
        supports_prepared_workspace=capabilities.supports_prepared_workspace,
    )


def agent_message_from_proto(value: Any) -> AgentMessage:
    return AgentMessage(
        role=value.role,
        text=value.text,
        parts=[agent_message_part_from_proto(part) for part in value.parts],
        metadata=struct_to_dict(value.metadata)
        if has_field(value, "metadata")
        else None,
    )


def agent_message_to_proto(value: AgentMessage | Mapping[str, Any]) -> Any:
    message = _coerce(value, AgentMessage, "AgentMessage")
    out = pb.AgentMessage(
        role=message.role,
        text=message.text,
        parts=[agent_message_part_to_proto(part) for part in message.parts],
    )
    _copy_struct(out, "metadata", message.metadata)
    return out


def agent_message_part_from_proto(value: Any) -> AgentMessagePart:
    return AgentMessagePart(
        type=value.type,
        text=value.text,
        json=struct_to_dict(value.json) if has_field(value, "json") else None,
        tool_call=agent_tool_call_from_proto(value.tool_call)
        if has_field(value, "tool_call")
        else None,
        tool_result=agent_tool_result_from_proto(value.tool_result)
        if has_field(value, "tool_result")
        else None,
        image_ref=agent_image_ref_from_proto(value.image_ref)
        if has_field(value, "image_ref")
        else None,
    )


def agent_message_part_to_proto(value: AgentMessagePart | Mapping[str, Any]) -> Any:
    part = _coerce(value, AgentMessagePart, "AgentMessagePart")
    part_type = _int_field(part.type)
    if part_type == pb.AGENT_MESSAGE_PART_TYPE_UNSPECIFIED:
        part_type = _infer_agent_message_part_type(part)
    out = pb.AgentMessagePart(type=part_type, text=part.text)
    _copy_struct(out, "json", part.json)
    _copy_message(out, "tool_call", agent_tool_call_to_proto(part.tool_call))
    _copy_message(out, "tool_result", agent_tool_result_to_proto(part.tool_result))
    _copy_message(out, "image_ref", agent_image_ref_to_proto(part.image_ref))
    return out


def agent_tool_call_from_proto(value: Any) -> AgentMessagePartToolCall:
    return AgentMessagePartToolCall(
        id=value.id,
        tool_id=value.tool_id,
        arguments=struct_to_dict(value.arguments)
        if has_field(value, "arguments")
        else None,
    )


def agent_tool_call_to_proto(
    value: AgentMessagePartToolCall | Mapping[str, Any] | None,
) -> Any | None:
    if value is None:
        return None
    tool_call = _coerce(value, AgentMessagePartToolCall, "tool_call")
    out = pb.AgentMessagePartToolCall(id=tool_call.id, tool_id=tool_call.tool_id)
    _copy_struct(out, "arguments", tool_call.arguments)
    return out


def agent_tool_result_from_proto(value: Any) -> AgentMessagePartToolResult:
    return AgentMessagePartToolResult(
        tool_call_id=value.tool_call_id,
        status=value.status,
        content=value.content,
        output=struct_to_dict(value.output) if has_field(value, "output") else None,
    )


def agent_tool_result_to_proto(
    value: AgentMessagePartToolResult | Mapping[str, Any] | None,
) -> Any | None:
    if value is None:
        return None
    tool_result = _coerce(value, AgentMessagePartToolResult, "tool_result")
    out = pb.AgentMessagePartToolResult(
        tool_call_id=tool_result.tool_call_id,
        status=_int_field(tool_result.status),
        content=tool_result.content,
    )
    _copy_struct(out, "output", tool_result.output)
    return out


def agent_image_ref_from_proto(value: Any) -> AgentMessagePartImageRef:
    return AgentMessagePartImageRef(uri=value.uri, mime_type=value.mime_type)


def agent_image_ref_to_proto(
    value: AgentMessagePartImageRef | Mapping[str, Any] | None,
) -> Any | None:
    if value is None:
        return None
    image_ref = _coerce(value, AgentMessagePartImageRef, "image_ref")
    return pb.AgentMessagePartImageRef(uri=image_ref.uri, mime_type=image_ref.mime_type)


def agent_actor_from_proto(value: Any) -> AgentActor:
    return AgentActor(
        subject_id=value.subject_id,
        subject_kind=value.subject_kind,
        display_name=value.display_name,
        auth_source=value.auth_source,
    )


def agent_actor_to_proto(value: AgentActor | Mapping[str, Any] | None) -> Any | None:
    if value is None:
        return None
    actor = _coerce(value, AgentActor, "AgentActor")
    return pb.AgentActor(
        subject_id=actor.subject_id,
        subject_kind=actor.subject_kind,
        display_name=actor.display_name,
        auth_source=actor.auth_source,
    )


def agent_subject_context_from_proto(value: Any) -> AgentSubjectContext:
    return AgentSubjectContext(
        subject_id=value.subject_id,
        subject_kind=value.subject_kind,
        credential_subject_id=value.credential_subject_id,
        display_name=value.display_name,
        auth_source=value.auth_source,
    )


def agent_subject_context_to_proto(
    value: AgentSubjectContext | Mapping[str, Any] | None,
) -> Any | None:
    if value is None:
        return None
    subject = _coerce(value, AgentSubjectContext, "AgentSubjectContext")
    return pb.AgentSubjectContext(
        subject_id=subject.subject_id,
        subject_kind=subject.subject_kind,
        credential_subject_id=subject.credential_subject_id,
        display_name=subject.display_name,
        auth_source=subject.auth_source,
    )


def prepared_workspace_from_proto(value: Any) -> AgentPreparedWorkspace:
    return AgentPreparedWorkspace(root=value.root, cwd=value.cwd)


def agent_session_start_config_from_proto(value: Any) -> AgentSessionStartConfig:
    return AgentSessionStartConfig(
        hooks=[agent_session_start_hook_from_proto(hook) for hook in value.hooks]
    )


def agent_session_start_hook_from_proto(value: Any) -> AgentSessionStartHook:
    return AgentSessionStartHook(
        id=value.id,
        type=value.type,
        command=list(value.command),
        cwd=value.cwd,
        timeout=value.timeout,
        env=dict(value.env),
        output=AgentSessionStartHookOutput(
            additional_context=value.output.additional_context,
            metadata=value.output.metadata,
        )
        if has_field(value, "output")
        else None,
    )


def resolved_agent_tool_from_proto(value: Any) -> ResolvedAgentTool:
    return ResolvedAgentTool(
        id=value.id,
        name=value.name,
        description=value.description,
        parameters_schema=struct_to_dict(value.parameters_schema)
        if has_field(value, "parameters_schema")
        else None,
    )


def agent_tool_ref_from_proto(value: Any) -> AgentToolRef:
    return AgentToolRef(
        plugin=value.plugin,
        operation=value.operation,
        connection=value.connection,
        instance=value.instance,
        title=value.title,
        description=value.description,
        system=value.system,
    )


def agent_tool_ref_to_proto(
    value: AgentToolRef | Mapping[str, Any] | None,
) -> Any | None:
    if value is None:
        return None
    ref = _coerce(value, AgentToolRef, "AgentToolRef")
    return pb.AgentToolRef(
        plugin=ref.plugin,
        operation=ref.operation,
        connection=ref.connection,
        instance=ref.instance,
        title=ref.title,
        description=ref.description,
        system=ref.system,
    )


def agent_turn_display_to_proto(
    value: AgentTurnDisplay | Mapping[str, Any] | None,
) -> Any | None:
    if value is None:
        return None
    display = _coerce(value, AgentTurnDisplay, "AgentTurnDisplay")
    out = pb.AgentTurnDisplay(
        kind=display.kind,
        phase=display.phase,
        text=display.text,
        label=display.label,
        ref=display.ref,
        parent_ref=display.parent_ref,
        action=display.action,
        format=display.format,
        language=display.language,
    )
    _copy_value(out, "input", display.input)
    _copy_value(out, "output", display.output)
    _copy_value(out, "error", display.error)
    return out


def execute_agent_tool_response_from_proto(value: Any) -> ExecuteAgentToolResponse:
    return ExecuteAgentToolResponse(status=_int_field(value.status), body=value.body)


def list_agent_tools_response_from_proto(value: Any) -> ListAgentToolsResponse:
    return ListAgentToolsResponse(
        tools=[listed_agent_tool_from_proto(tool) for tool in value.tools],
        next_page_token=value.next_page_token,
    )


def listed_agent_tool_from_proto(value: Any) -> ListedAgentTool:
    return ListedAgentTool(
        id=value.id,
        mcp_name=value.mcp_name,
        title=value.title,
        description=value.description,
        input_schema=value.input_schema,
        output_schema=value.output_schema,
        annotations=agent_tool_annotations_from_proto(value.annotations)
        if has_field(value, "annotations")
        else None,
        ref=agent_tool_ref_from_proto(value.ref) if has_field(value, "ref") else None,
        tags=list(value.tags),
        search_text=value.search_text,
    )


def agent_tool_annotations_from_proto(value: Any) -> AgentToolAnnotations:
    return AgentToolAnnotations(
        read_only_hint=value.read_only_hint
        if has_field(value, "read_only_hint")
        else None,
        idempotent_hint=value.idempotent_hint
        if has_field(value, "idempotent_hint")
        else None,
        destructive_hint=value.destructive_hint
        if has_field(value, "destructive_hint")
        else None,
        open_world_hint=value.open_world_hint
        if has_field(value, "open_world_hint")
        else None,
    )


def resolved_agent_connection_from_proto(value: Any) -> ResolvedAgentConnection:
    return ResolvedAgentConnection(
        connection_id=value.connection_id,
        connection=value.connection,
        instance=value.instance,
        mode=value.mode,
        headers=dict(value.headers),
        params=dict(value.params),
        expires_at=datetime_from_timestamp(value.expires_at)
        if has_field(value, "expires_at")
        else None,
    )


def execute_agent_tool_request_to_proto(
    value: ExecuteAgentToolRequest | Mapping[str, Any],
) -> Any:
    request = _coerce(value, ExecuteAgentToolRequest, "ExecuteAgentToolRequest")
    out = pb.ExecuteAgentToolRequest(
        session_id=request.session_id,
        turn_id=request.turn_id,
        tool_call_id=request.tool_call_id,
        tool_id=request.tool_id,
        idempotency_key=request.idempotency_key,
        run_grant=request.run_grant,
    )
    _copy_struct(out, "arguments", request.arguments)
    return out


def list_agent_tools_request_to_proto(
    value: ListAgentToolsRequest | Mapping[str, Any],
) -> Any:
    request = _coerce(value, ListAgentToolsRequest, "ListAgentToolsRequest")
    return pb.ListAgentToolsRequest(
        session_id=request.session_id,
        turn_id=request.turn_id,
        page_size=_int_field(request.page_size),
        page_token=request.page_token,
        run_grant=request.run_grant,
        query=request.query,
    )


def resolve_agent_connection_request_to_proto(
    value: ResolveAgentConnectionRequest | Mapping[str, Any],
) -> Any:
    request = _coerce(
        value,
        ResolveAgentConnectionRequest,
        "ResolveAgentConnectionRequest",
    )
    return pb.ResolveAgentConnectionRequest(
        session_id=request.session_id,
        turn_id=request.turn_id,
        connection=request.connection,
        instance=request.instance,
        run_grant=request.run_grant,
    )


def _coerce(value: Any, cls: type[Any], field_name: str) -> Any:
    if isinstance(value, cls):
        return value
    mapping = _dataclass_mapping(value)
    if mapping is not None:
        return cls(**dict(mapping))
    if isinstance(value, Mapping):
        return cls(**dict(value))
    raise TypeError(f"{field_name} must be {cls.__name__} or a mapping")


def _copy_message(target: Any, field: str, value: Any | None) -> None:
    if value is not None:
        getattr(target, field).CopyFrom(value)


def _copy_struct(target: Any, field: str, value: JsonObjectInput | None) -> None:
    if value is not None:
        getattr(target, field).CopyFrom(struct_from_dict(value))


def _copy_timestamp(target: Any, field: str, value: TimestampInput) -> None:
    timestamp = timestamp_from_datetime(value)
    if timestamp is not None:
        getattr(target, field).CopyFrom(timestamp)


def _copy_value(target: Any, field: str, value: Any) -> None:
    if value is not None:
        getattr(target, field).CopyFrom(value_from_json(value))


def agent_actor_to_dict(actor: Any) -> dict[str, Any]:
    """Convert an ``AgentActor`` protocol value to a plain dictionary."""

    return _message_fields(
        actor,
        ("subject_id", "subject_kind", "display_name", "auth_source"),
    )


def agent_actor_from_dict(value: Mapping[str, Any] | None) -> Any:
    """Create an ``AgentActor`` from a plain dictionary."""

    data = dict(value or {})
    return AgentActor(
        subject_id=data.get("subject_id", ""),
        subject_kind=data.get("subject_kind", ""),
        display_name=data.get("display_name", ""),
        auth_source=data.get("auth_source", ""),
    )


def agent_subject_context_to_dict(subject: Any) -> dict[str, Any]:
    """Convert an ``AgentSubjectContext`` protocol value to a dictionary."""

    return _message_fields(
        subject,
        (
            "subject_id",
            "subject_kind",
            "credential_subject_id",
            "display_name",
            "auth_source",
        ),
    )


def agent_subject_context_from_dict(value: Mapping[str, Any] | None) -> Any:
    """Create an ``AgentSubjectContext`` from a dictionary."""

    data = dict(value or {})
    return AgentSubjectContext(
        subject_id=data.get("subject_id", ""),
        subject_kind=data.get("subject_kind", ""),
        credential_subject_id=data.get("credential_subject_id", ""),
        display_name=data.get("display_name", ""),
        auth_source=data.get("auth_source", ""),
    )


def prepared_workspace_to_dict(workspace: Any) -> dict[str, Any]:
    """Convert ``PreparedAgentWorkspace`` to a dictionary."""

    return _message_fields(workspace, ("root", "cwd"))


def prepared_workspace_from_dict(value: Mapping[str, Any] | None) -> Any:
    """Create ``PreparedAgentWorkspace`` from a dictionary."""

    data = (
        dict(_mapping_value(value, "prepared_workspace")) if value is not None else {}
    )
    return AgentPreparedWorkspace(
        root=data.get("root", ""),
        cwd=data.get("cwd", ""),
    )


def agent_tool_ref_to_dict(tool_ref: Any) -> dict[str, Any]:
    """Convert an ``AgentToolRef`` protocol value to a dictionary."""

    return _message_fields(
        tool_ref,
        (
            "plugin",
            "operation",
            "connection",
            "instance",
            "title",
            "description",
            "system",
        ),
    )


def agent_tool_ref_from_dict(value: Mapping[str, Any] | None) -> Any:
    """Create an ``AgentToolRef`` from a dictionary."""

    data = dict(_mapping_value(value, "tool_ref")) if value is not None else {}
    return AgentToolRef(
        plugin=data.get("plugin", ""),
        operation=data.get("operation", ""),
        connection=data.get("connection", ""),
        instance=data.get("instance", ""),
        title=data.get("title", ""),
        description=data.get("description", ""),
        system=data.get("system", ""),
    )


def agent_message_part_to_dict(part: Any) -> dict[str, Any]:
    """Convert an ``AgentMessagePart`` to a lower-snake-case dictionary."""

    value: dict[str, Any] = {"type": part.type}
    if part.text:
        value["text"] = part.text
    if has_field(part, "json"):
        value["json"] = struct_to_dict(part.json)
    elif getattr(part, "json", None) is not None:
        value["json"] = part.json
    if has_field(part, "tool_call"):
        value["tool_call"] = _tool_call_to_dict(part.tool_call)
    elif getattr(part, "tool_call", None) is not None:
        value["tool_call"] = _tool_call_to_dict(part.tool_call)
    if has_field(part, "tool_result"):
        value["tool_result"] = _tool_result_to_dict(part.tool_result)
    elif getattr(part, "tool_result", None) is not None:
        value["tool_result"] = _tool_result_to_dict(part.tool_result)
    if has_field(part, "image_ref"):
        value["image_ref"] = _image_ref_to_dict(part.image_ref)
    elif getattr(part, "image_ref", None) is not None:
        value["image_ref"] = _image_ref_to_dict(part.image_ref)
    return value


def agent_message_part_from_dict(value: Any) -> Any:
    """Create an ``AgentMessagePart`` from a lower-snake-case dictionary."""

    data = dict(_mapping_value(value, "AgentMessagePart"))
    part = AgentMessagePart(
        type=data.get("type", pb.AGENT_MESSAGE_PART_TYPE_UNSPECIFIED),
        text=str(data.get("text", "")),
        json=data.get("json"),
        tool_call=_tool_call_from_dict(data["tool_call"])
        if data.get("tool_call") is not None
        else None,
        tool_result=_tool_result_from_dict(data["tool_result"])
        if data.get("tool_result") is not None
        else None,
        image_ref=_image_ref_from_dict(data["image_ref"])
        if data.get("image_ref") is not None
        else None,
    )
    if _int_field(part.type) == pb.AGENT_MESSAGE_PART_TYPE_UNSPECIFIED:
        part.type = _infer_agent_message_part_type(part)
    return part


def _infer_agent_message_part_type(part: AgentMessagePart) -> int:
    if part.tool_call is not None:
        return pb.AGENT_MESSAGE_PART_TYPE_TOOL_CALL
    if part.tool_result is not None:
        return pb.AGENT_MESSAGE_PART_TYPE_TOOL_RESULT
    if part.image_ref is not None:
        return pb.AGENT_MESSAGE_PART_TYPE_IMAGE_REF
    if part.json is not None:
        return pb.AGENT_MESSAGE_PART_TYPE_JSON
    if part.text:
        return pb.AGENT_MESSAGE_PART_TYPE_TEXT
    return pb.AGENT_MESSAGE_PART_TYPE_UNSPECIFIED


def agent_message_to_dict(message: Any) -> dict[str, Any]:
    """Convert an ``AgentMessage`` protocol value to a dictionary."""

    value = _message_fields(message, ("role", "text"))
    if message.parts:
        value["parts"] = [agent_message_part_to_dict(part) for part in message.parts]
    if has_field(message, "metadata"):
        value["metadata"] = struct_to_dict(message.metadata)
    elif getattr(message, "metadata", None) is not None:
        value["metadata"] = message.metadata
    return value


def agent_messages_to_dicts(messages: Iterable[Any]) -> list[dict[str, Any]]:
    """Convert agent protocol messages to dictionaries."""

    return [agent_message_to_dict(message) for message in messages]


def agent_message_from_dict(value: Any) -> Any:
    """Create an ``AgentMessage`` protocol value from a dictionary."""

    data = dict(_mapping_value(value, "AgentMessage"))
    return AgentMessage(
        role=data.get("role", ""),
        text=data.get("text", ""),
        parts=[
            agent_message_part_from_dict(_mapping_value(part, "parts[]"))
            for part in data.get("parts", []) or []
        ],
        metadata=data.get("metadata"),
    )


def agent_messages_from_dicts(messages: Iterable[Mapping[str, Any]]) -> list[Any]:
    """Create agent protocol messages from dictionaries."""

    return [agent_message_from_dict(message) for message in messages]


def agent_message_to_proto_dict(message: Any) -> dict[str, Any]:
    """Convert an ``AgentMessage`` to protobuf JSON dictionary form."""

    value = message_to_dict(
        agent_message_to_proto(message),
        preserving_proto_field_name=True,
    )
    if not isinstance(value, dict):
        raise TypeError("AgentMessage protobuf JSON projection must be an object")
    return value


def agent_message_from_proto_dict(value: Mapping[str, Any]) -> Any:
    """Create an ``AgentMessage`` from protobuf JSON dictionary form."""

    return agent_message_from_proto(message_from_dict(dict(value), pb.AgentMessage()))


def agent_messages_to_proto_dicts(messages: Iterable[Any]) -> list[dict[str, Any]]:
    """Convert agent protocol messages to protobuf JSON dictionaries."""

    return [agent_message_to_proto_dict(message) for message in messages]


def agent_messages_from_proto_dicts(messages: Iterable[Mapping[str, Any]]) -> list[Any]:
    """Create agent protocol messages from protobuf JSON dictionaries."""

    return [agent_message_from_proto_dict(message) for message in messages]


class AgentHost:
    """Client for the agent host service available inside agent providers.

    ``AgentHost`` reads ``GESTALT_AGENT_HOST_SOCKET`` and its optional relay
    token from the environment and exposes the host RPCs that agent providers
    use to discover and call tools during a turn.
    """

    def __init__(self) -> None:
        target = os.environ.get(ENV_AGENT_HOST_SOCKET, "")
        if not target:
            raise RuntimeError(f"{ENV_AGENT_HOST_SOCKET} is not set")
        relay_token = os.environ.get(ENV_AGENT_HOST_SOCKET_TOKEN, "")
        self._channel = host_service_channel("agent host", target, token=relay_token)
        self._stub = pb_grpc.AgentHostStub(self._channel)

    def close(self) -> None:
        """Close the underlying gRPC channel."""

        self._channel.close()

    def execute_tool(
        self,
        request: ExecuteAgentToolRequest | Mapping[str, Any],
        *,
        timeout_seconds: float | None = None,
    ) -> ExecuteAgentToolResponse:
        """Execute a host tool using native request fields."""

        response = _grpc_call(
            self._stub.ExecuteTool,
            execute_agent_tool_request_to_proto(request),
            timeout_seconds=timeout_seconds,
        )
        return execute_agent_tool_response_from_proto(response)

    def execute_tool_for_turn(
        self,
        session_id: str,
        turn_id: str,
        *,
        tool_call_id: str,
        tool_id: str,
        arguments: JsonObjectInput | None = None,
        run_grant: str = "",
        idempotency_key: str = "",
        timeout_seconds: float | None = None,
    ) -> ExecuteAgentToolResponse:
        """Execute a host tool using plain Python request fields."""

        return self.execute_tool(
            ExecuteAgentToolRequest(
                session_id=session_id,
                turn_id=turn_id,
                tool_call_id=tool_call_id,
                tool_id=tool_id,
                arguments=arguments,
                run_grant=run_grant,
                idempotency_key=idempotency_key,
            ),
            timeout_seconds=timeout_seconds,
        )

    def list_tools(
        self,
        request: ListAgentToolsRequest | Mapping[str, Any],
        *,
        timeout_seconds: float | None = None,
    ) -> ListAgentToolsResponse:
        """List host tools visible to the current agent request."""

        response = _grpc_call(
            self._stub.ListTools,
            list_agent_tools_request_to_proto(request),
            timeout_seconds=timeout_seconds,
        )
        return list_agent_tools_response_from_proto(response)

    def list_tools_for_turn(
        self,
        session_id: str,
        turn_id: str,
        *,
        run_grant: str = "",
        page_size: int = 0,
        page_token: str = "",
        query: str = "",
        timeout_seconds: float | None = None,
    ) -> ListAgentToolsResponse:
        """List host tools using plain Python request fields."""

        return self.list_tools(
            ListAgentToolsRequest(
                session_id=session_id,
                turn_id=turn_id,
                run_grant=run_grant,
                page_size=page_size,
                page_token=page_token,
                query=query,
            ),
            timeout_seconds=timeout_seconds,
        )

    def resolve_connection(
        self,
        request: ResolveAgentConnectionRequest | Mapping[str, Any],
        *,
        timeout_seconds: float | None = None,
    ) -> ResolvedAgentConnection:
        """Resolve an agent connection for the current turn."""

        response = _grpc_call(
            self._stub.ResolveConnection,
            resolve_agent_connection_request_to_proto(request),
            timeout_seconds=timeout_seconds,
        )
        return resolved_agent_connection_from_proto(response)

    def resolve_connection_for_turn(
        self,
        session_id: str,
        turn_id: str,
        *,
        connection: str,
        instance: str = "",
        run_grant: str = "",
        timeout_seconds: float | None = None,
    ) -> ResolvedAgentConnection:
        """Resolve an agent connection using plain Python request fields."""

        return self.resolve_connection(
            ResolveAgentConnectionRequest(
                session_id=session_id,
                turn_id=turn_id,
                connection=connection,
                instance=instance,
                run_grant=run_grant,
            ),
            timeout_seconds=timeout_seconds,
        )

    def __enter__(self) -> AgentHost:
        """Return the client for ``with`` statements."""

        return self

    def __exit__(self, *args: Any) -> None:
        """Close the client at the end of a context manager block."""

        self.close()


def _agent_message_value(value: Any) -> Any:
    if isinstance(value, pb.AgentMessage):
        return _copy(value)
    return agent_message_to_proto(value)


def _agent_tool_ref_value(value: Any) -> Any:
    if isinstance(value, pb.AgentToolRef):
        return _copy(value)
    return agent_tool_ref_to_proto(value)


def _agent_workspace_value(value: Any | None) -> Any | None:
    if value is None:
        return None
    if isinstance(value, pb.AgentWorkspace):
        return _copy(value)
    data = _data(value, {})
    return pb.AgentWorkspace(
        checkouts=[
            pb.AgentWorkspaceGitCheckout(
                url=dict(_mapping_value(item, "checkouts[]")).get("url", ""),
                ref=dict(_mapping_value(item, "checkouts[]")).get("ref", ""),
                path=dict(_mapping_value(item, "checkouts[]")).get("path", ""),
            )
            for item in (data.get("checkouts") or [])
        ],
        cwd=data.get("cwd", ""),
    )


def _agent_manager_create_session_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.AgentManagerCreateSessionRequest):
        return _copy(value)
    data = _data(value, kwargs)
    request = pb.AgentManagerCreateSessionRequest(
        provider_name=data.get("provider_name", ""),
        model=data.get("model", ""),
        client_ref=data.get("client_ref", ""),
        idempotency_key=data.get("idempotency_key", ""),
        workspace=_agent_workspace_value(data.get("workspace")),
    )
    if data.get("metadata") is not None:
        request.metadata.CopyFrom(struct_from_dict(data["metadata"]))
    return request


def _agent_manager_get_session_request(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.AgentManagerGetSessionRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.AgentManagerGetSessionRequest(session_id=data.get("session_id", ""))


def _agent_manager_list_sessions_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.AgentManagerListSessionsRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.AgentManagerListSessionsRequest(
        provider_name=data.get("provider_name", ""),
        state=data.get("state", AGENT_SESSION_STATE_UNSPECIFIED),
        limit=data.get("limit", 0),
        summary_only=data.get("summary_only", False),
    )


def _agent_manager_update_session_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.AgentManagerUpdateSessionRequest):
        return _copy(value)
    data = _data(value, kwargs)
    request = pb.AgentManagerUpdateSessionRequest(
        session_id=data.get("session_id", ""),
        client_ref=data.get("client_ref", ""),
        state=data.get("state", AGENT_SESSION_STATE_UNSPECIFIED),
    )
    if data.get("metadata") is not None:
        request.metadata.CopyFrom(struct_from_dict(data["metadata"]))
    return request


def _agent_manager_create_turn_request(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.AgentManagerCreateTurnRequest):
        return _copy(value)
    data = _data(value, kwargs)
    request = pb.AgentManagerCreateTurnRequest(
        session_id=data.get("session_id", ""),
        model=data.get("model", ""),
        messages=[_agent_message_value(item) for item in (data.get("messages") or [])],
        tool_refs=[
            _agent_tool_ref_value(item) for item in (data.get("tool_refs") or [])
        ],
        tool_source=data.get("tool_source", AGENT_TOOL_SOURCE_MODE_UNSPECIFIED),
        idempotency_key=data.get("idempotency_key", ""),
    )
    if data.get("response_schema") is not None:
        request.response_schema.CopyFrom(struct_from_dict(data["response_schema"]))
    if data.get("metadata") is not None:
        request.metadata.CopyFrom(struct_from_dict(data["metadata"]))
    if data.get("model_options") is not None:
        request.model_options.CopyFrom(struct_from_dict(data["model_options"]))
    return request


def _agent_manager_get_turn_request(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.AgentManagerGetTurnRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.AgentManagerGetTurnRequest(turn_id=data.get("turn_id", ""))


def _agent_manager_list_turns_request(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.AgentManagerListTurnsRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.AgentManagerListTurnsRequest(
        session_id=data.get("session_id", ""),
        status=data.get("status", AGENT_EXECUTION_STATUS_UNSPECIFIED),
        limit=data.get("limit", 0),
        summary_only=data.get("summary_only", False),
    )


def _agent_manager_cancel_turn_request(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.AgentManagerCancelTurnRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.AgentManagerCancelTurnRequest(
        turn_id=data.get("turn_id", ""),
        reason=data.get("reason", ""),
    )


def _agent_manager_list_turn_events_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.AgentManagerListTurnEventsRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.AgentManagerListTurnEventsRequest(
        turn_id=data.get("turn_id", ""),
        after_seq=data.get("after_seq", 0),
        limit=data.get("limit", 0),
    )


def _agent_manager_list_interactions_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.AgentManagerListInteractionsRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.AgentManagerListInteractionsRequest(turn_id=data.get("turn_id", ""))


def _agent_manager_resolve_interaction_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.AgentManagerResolveInteractionRequest):
        return _copy(value)
    data = _data(value, kwargs)
    request = pb.AgentManagerResolveInteractionRequest(
        turn_id=data.get("turn_id", ""),
        interaction_id=data.get("interaction_id", ""),
    )
    if data.get("resolution") is not None:
        request.resolution.CopyFrom(struct_from_dict(data["resolution"]))
    return request


class AgentManager:
    """Client for managing agent sessions, turns, events, and interactions.

    The manager is for provider code that receives an invocation token and then
    needs to call the host's agent-management API. Each request passed to a
    method is mutated to include that invocation token before the RPC is sent.
    """

    def __init__(self, invocation_token: str) -> None:
        trimmed_token = invocation_token.strip()
        if not trimmed_token:
            raise RuntimeError("agent manager: invocation token is not available")

        target = os.environ.get(ENV_AGENT_MANAGER_SOCKET, "")
        if not target:
            raise RuntimeError(f"agent manager: {ENV_AGENT_MANAGER_SOCKET} is not set")
        relay_token = os.environ.get(ENV_AGENT_MANAGER_SOCKET_TOKEN, "")

        self._channel = host_service_channel("agent manager", target, token=relay_token)
        self._stub = pb_grpc.AgentManagerHostStub(self._channel)
        self._invocation_token = trimmed_token

    def close(self) -> None:
        """Close the underlying gRPC channel."""

        self._channel.close()

    def create_session(self, request: Any | None = None, **kwargs: Any) -> Any:
        """Create an agent session."""

        request = _agent_manager_create_session_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.CreateSession, request)

    def get_session(self, request: Any | None = None, **kwargs: Any) -> Any:
        """Fetch one agent session."""

        request = _agent_manager_get_session_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.GetSession, request)

    def list_sessions(self, request: Any | None = None, **kwargs: Any) -> Any:
        """List agent sessions visible to the invocation token."""

        request = _agent_manager_list_sessions_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.ListSessions, request)

    def update_session(self, request: Any | None = None, **kwargs: Any) -> Any:
        """Update mutable fields on an agent session."""

        request = _agent_manager_update_session_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.UpdateSession, request)

    def create_turn(self, request: Any | None = None, **kwargs: Any) -> Any:
        """Create an agent turn."""

        request = _agent_manager_create_turn_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.CreateTurn, request)

    def get_turn(self, request: Any | None = None, **kwargs: Any) -> Any:
        """Fetch one agent turn."""

        request = _agent_manager_get_turn_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.GetTurn, request)

    def list_turns(self, request: Any | None = None, **kwargs: Any) -> Any:
        """List turns for an agent session."""

        request = _agent_manager_list_turns_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.ListTurns, request)

    def cancel_turn(self, request: Any | None = None, **kwargs: Any) -> Any:
        """Cancel an in-progress agent turn."""

        request = _agent_manager_cancel_turn_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.CancelTurn, request)

    def list_turn_events(self, request: Any | None = None, **kwargs: Any) -> Any:
        """List events emitted for an agent turn."""

        request = _agent_manager_list_turn_events_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.ListTurnEvents, request)

    def list_interactions(self, request: Any | None = None, **kwargs: Any) -> Any:
        """List pending or completed agent interactions."""

        request = _agent_manager_list_interactions_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.ListInteractions, request)

    def resolve_interaction(self, request: Any | None = None, **kwargs: Any) -> Any:
        """Resolve an agent interaction with a host response."""

        request = _agent_manager_resolve_interaction_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.ResolveInteraction, request)

    def __enter__(self) -> AgentManager:
        """Return the client for ``with`` statements."""

        return self

    def __exit__(self, *args: Any) -> None:
        """Close the client at the end of a context manager block."""

        self.close()


def _grpc_call(
    method: Any, request: Any, *, timeout_seconds: float | None = None
) -> Any:
    try:
        if timeout_seconds is None:
            return method(request)
        timeout = _positive_float(timeout_seconds)
        if timeout is None:
            return method(request)
        return method(request, timeout=timeout)
    except grpc.RpcError:
        raise


def _message_fields(message: Any, fields: tuple[str, ...]) -> dict[str, Any]:
    value: dict[str, Any] = {}
    for field_name in fields:
        if isinstance(message, Mapping):
            current = message.get(field_name)
        else:
            current = getattr(message, field_name, None)
        if current is not None and current != "":
            value[field_name] = current
    return value


def _mapping_value(value: Any, field: str) -> Mapping[str, Any]:
    mapping = _dataclass_mapping(value)
    if mapping is not None:
        return mapping
    if not isinstance(value, Mapping):
        raise TypeError(f"{field} must be a mapping")
    return value


def _int_field(value: Any) -> int:
    if isinstance(value, str):
        text = value.strip()
        if not text:
            return 0
        if text.removeprefix("-").isdigit():
            return int(text)
        enum_value = getattr(pb, text, None)
        if isinstance(enum_value, int):
            return enum_value
    return int(value or 0)


def _positive_float(value: float) -> float | None:
    timeout = float(value)
    return timeout if timeout > 0 else None


def _tool_call_to_dict(tool_call: Any) -> dict[str, Any]:
    value = _message_fields(tool_call, ("id", "tool_id"))
    if has_field(tool_call, "arguments"):
        value["arguments"] = struct_to_dict(tool_call.arguments)
    elif _message_value(tool_call, "arguments") is not None:
        value["arguments"] = _message_value(tool_call, "arguments")
    return value


def _tool_call_from_dict(value: Any) -> Any:
    data = dict(_mapping_value(value, "tool_call"))
    return AgentMessagePartToolCall(
        id=data.get("id", ""),
        tool_id=data.get("tool_id", ""),
        arguments=data.get("arguments"),
    )


def _tool_result_to_dict(tool_result: Any) -> dict[str, Any]:
    value = _message_fields(tool_result, ("tool_call_id", "content"))
    value["status"] = _message_value(tool_result, "status", 0)
    if has_field(tool_result, "output"):
        value["output"] = struct_to_dict(tool_result.output)
    elif _message_value(tool_result, "output") is not None:
        value["output"] = _message_value(tool_result, "output")
    return value


def _tool_result_from_dict(value: Any) -> Any:
    data = dict(_mapping_value(value, "tool_result"))
    return AgentMessagePartToolResult(
        tool_call_id=data.get("tool_call_id", ""),
        status=data.get("status", 0),
        content=data.get("content", ""),
        output=data.get("output"),
    )


def _image_ref_to_dict(image_ref: Any) -> dict[str, Any]:
    return _message_fields(image_ref, ("uri", "mime_type"))


def _image_ref_from_dict(value: Any) -> Any:
    data = dict(_mapping_value(value, "image_ref"))
    return AgentMessagePartImageRef(
        uri=data.get("uri", ""),
        mime_type=data.get("mime_type", ""),
    )


def _message_value(message: Any, field: str, default: Any = None) -> Any:
    if isinstance(message, Mapping):
        return message.get(field, default)
    return getattr(message, field, default)
