from __future__ import annotations

import dataclasses as _dataclasses
import datetime as _dt
import os
from collections.abc import Iterable, Mapping, Sequence
from typing import Any, TypeAlias

import grpc
from typing_extensions import TypedDict, Unpack

from ._gen.v1 import agent_pb2 as _pb
from ._gen.v1 import agent_pb2_grpc as _pb_grpc
from ._grpc_transport import host_service_channel
from ._protocol import (
    JsonObjectInput,
    has_field,
    message_from_dict,
    message_to_dict,
    struct_from_dict,
    struct_to_dict,
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

TimestampInput: TypeAlias = _dt.datetime | Mapping[str, Any] | None


@_dataclasses.dataclass(slots=True)
class AgentMessageInput:
    role: str = ""
    text: str = ""
    parts: Sequence[Any] | None = None
    metadata: Any | None = None


@_dataclasses.dataclass(slots=True)
class AgentMessagePartInput:
    type: int = AGENT_MESSAGE_PART_TYPE_UNSPECIFIED
    text: str = ""
    json: Any | None = None
    tool_call: Any | None = None
    tool_result: Any | None = None
    image_ref: Any | None = None


@_dataclasses.dataclass(slots=True)
class AgentMessagePartToolCallInput:
    id: str = ""
    tool_id: str = ""
    arguments: Any | None = None


@_dataclasses.dataclass(slots=True)
class AgentMessagePartToolResultInput:
    tool_call_id: str = ""
    status: int = 0
    content: str = ""
    output: Any | None = None


@_dataclasses.dataclass(slots=True)
class AgentMessagePartImageRefInput:
    uri: str = ""
    mime_type: str = ""


@_dataclasses.dataclass(slots=True)
class AgentToolRefInput:
    plugin: str = ""
    operation: str = ""
    connection: str = ""
    instance: str = ""
    title: str = ""
    description: str = ""
    system: str = ""


@_dataclasses.dataclass(slots=True)
class AgentWorkspaceGitCheckoutInput:
    url: str = ""
    ref: str = ""
    path: str = ""


@_dataclasses.dataclass(slots=True)
class AgentWorkspaceInput:
    checkouts: Sequence[Any] | None = None
    cwd: str = ""


@_dataclasses.dataclass(slots=True)
class AgentManagerCreateSessionInput:
    provider_name: str = ""
    model: str = ""
    client_ref: str = ""
    metadata: Any | None = None
    idempotency_key: str = ""
    workspace: Any | None = None


@_dataclasses.dataclass(slots=True)
class AgentManagerGetSessionInput:
    session_id: str = ""


@_dataclasses.dataclass(slots=True)
class AgentManagerListSessionsInput:
    provider_name: str = ""
    state: int = AGENT_SESSION_STATE_UNSPECIFIED
    limit: int = 0
    summary_only: bool = False


@_dataclasses.dataclass(slots=True)
class AgentManagerUpdateSessionInput:
    session_id: str = ""
    client_ref: str = ""
    state: int = AGENT_SESSION_STATE_UNSPECIFIED
    metadata: Any | None = None


@_dataclasses.dataclass(slots=True)
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


@_dataclasses.dataclass(slots=True)
class AgentManagerGetTurnInput:
    turn_id: str = ""


@_dataclasses.dataclass(slots=True)
class AgentManagerListTurnsInput:
    session_id: str = ""
    status: int = AGENT_EXECUTION_STATUS_UNSPECIFIED
    limit: int = 0
    summary_only: bool = False


@_dataclasses.dataclass(slots=True)
class AgentManagerCancelTurnInput:
    turn_id: str = ""
    reason: str = ""


@_dataclasses.dataclass(slots=True)
class AgentManagerListTurnEventsInput:
    turn_id: str = ""
    after_seq: int = 0
    limit: int = 0


@_dataclasses.dataclass(slots=True)
class AgentManagerListInteractionsInput:
    turn_id: str = ""


@_dataclasses.dataclass(slots=True)
class AgentManagerResolveInteractionInput:
    turn_id: str = ""
    interaction_id: str = ""
    resolution: Any | None = None


class AgentSessionInput(TypedDict, total=False):
    id: str
    provider_name: str
    model: str
    client_ref: str
    state: int | str
    metadata: JsonObjectInput | None
    created_by: Any
    created_at: TimestampInput
    updated_at: TimestampInput
    last_turn_at: TimestampInput


class AgentTurnInput(TypedDict, total=False):
    id: str
    session_id: str
    provider_name: str
    model: str
    status: int | str
    messages: Iterable[Any]
    output_text: str
    structured_output: JsonObjectInput | None
    status_message: str
    execution_ref: str
    created_by: Any
    created_at: TimestampInput
    started_at: TimestampInput
    completed_at: TimestampInput


class AgentTurnEventInput(TypedDict, total=False):
    id: str
    turn_id: str
    seq: int
    type: str
    source: str
    visibility: str
    data: JsonObjectInput | None
    created_at: TimestampInput


def AgentMessage(*args: Any, **kwargs: Any) -> Any:
    """Create an agent message protocol value."""

    return pb.AgentMessage(*args, **kwargs)


def AgentMessagePart(*args: Any, **kwargs: Any) -> Any:
    """Create an agent message part protocol value."""

    return pb.AgentMessagePart(*args, **kwargs)


def AgentMessagePartToolCall(*args: Any, **kwargs: Any) -> Any:
    """Create an agent tool-call message part payload."""

    return pb.AgentMessagePartToolCall(*args, **kwargs)


def AgentMessagePartToolResult(*args: Any, **kwargs: Any) -> Any:
    """Create an agent tool-result message part payload."""

    return pb.AgentMessagePartToolResult(*args, **kwargs)


def AgentMessagePartImageRef(*args: Any, **kwargs: Any) -> Any:
    """Create an agent image-reference message part payload."""

    return pb.AgentMessagePartImageRef(*args, **kwargs)


def AgentActor(*args: Any, **kwargs: Any) -> Any:
    """Create an agent actor protocol value."""

    return pb.AgentActor(*args, **kwargs)


def AgentSubjectContext(*args: Any, **kwargs: Any) -> Any:
    """Create an agent subject context protocol value."""

    return pb.AgentSubjectContext(*args, **kwargs)


def AgentToolRef(*args: Any, **kwargs: Any) -> Any:
    """Create an agent tool reference protocol value."""

    return pb.AgentToolRef(*args, **kwargs)


def AgentProviderCapabilities(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider capabilities protocol value."""

    return pb.AgentProviderCapabilities(*args, **kwargs)


def AgentSession(*args: Any, **kwargs: Unpack[AgentSessionInput]) -> Any:
    """Create an agent session protocol value."""

    return pb.AgentSession(*args, **kwargs)


def AgentSessionStartConfig(*args: Any, **kwargs: Any) -> Any:
    """Create an agent session start config."""

    return pb.AgentSessionStartConfig(*args, **kwargs)


def AgentSessionStartHook(*args: Any, **kwargs: Any) -> Any:
    """Create an agent session start hook."""

    return pb.AgentSessionStartHook(*args, **kwargs)


def AgentSessionStartHookOutput(*args: Any, **kwargs: Any) -> Any:
    """Create an agent session start hook output."""

    return pb.AgentSessionStartHookOutput(*args, **kwargs)


def AgentTurn(*args: Any, **kwargs: Unpack[AgentTurnInput]) -> Any:
    """Create an agent turn protocol value."""

    return pb.AgentTurn(*args, **kwargs)


def AgentTurnEvent(*args: Any, **kwargs: Unpack[AgentTurnEventInput]) -> Any:
    """Create an agent turn event protocol value."""

    return pb.AgentTurnEvent(*args, **kwargs)


def AgentTurnDisplay(*args: Any, **kwargs: Any) -> Any:
    """Create an agent turn display payload."""

    return pb.AgentTurnDisplay(*args, **kwargs)


def AgentInteraction(*args: Any, **kwargs: Any) -> Any:
    """Create an agent interaction protocol value."""

    return pb.AgentInteraction(*args, **kwargs)


def CreateAgentProviderSessionRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider session request."""

    return pb.CreateAgentProviderSessionRequest(*args, **kwargs)


def GetAgentProviderSessionRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider get-session request."""

    return pb.GetAgentProviderSessionRequest(*args, **kwargs)


def ListAgentProviderSessionsRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider list-sessions request."""

    return pb.ListAgentProviderSessionsRequest(*args, **kwargs)


def ListAgentProviderSessionsResponse(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider list-sessions response."""

    return pb.ListAgentProviderSessionsResponse(*args, **kwargs)


def UpdateAgentProviderSessionRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider update-session request."""

    return pb.UpdateAgentProviderSessionRequest(*args, **kwargs)


def CreateAgentProviderTurnRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider turn request."""

    return pb.CreateAgentProviderTurnRequest(*args, **kwargs)


def GetAgentProviderTurnRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider get-turn request."""

    return pb.GetAgentProviderTurnRequest(*args, **kwargs)


def ListAgentProviderTurnsRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider list-turns request."""

    return pb.ListAgentProviderTurnsRequest(*args, **kwargs)


def ListAgentProviderTurnsResponse(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider list-turns response."""

    return pb.ListAgentProviderTurnsResponse(*args, **kwargs)


def CancelAgentProviderTurnRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider cancel-turn request."""

    return pb.CancelAgentProviderTurnRequest(*args, **kwargs)


def ListAgentProviderTurnEventsRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider list-turn-events request."""

    return pb.ListAgentProviderTurnEventsRequest(*args, **kwargs)


def ListAgentProviderTurnEventsResponse(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider list-turn-events response."""

    return pb.ListAgentProviderTurnEventsResponse(*args, **kwargs)


def ListAgentProviderInteractionsResponse(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider list-interactions response."""

    return pb.ListAgentProviderInteractionsResponse(*args, **kwargs)


def GetAgentProviderInteractionRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider get-interaction request."""

    return pb.GetAgentProviderInteractionRequest(*args, **kwargs)


def ListAgentProviderInteractionsRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider list-interactions request."""

    return pb.ListAgentProviderInteractionsRequest(*args, **kwargs)


def ResolveAgentProviderInteractionRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider resolve-interaction request."""

    return pb.ResolveAgentProviderInteractionRequest(*args, **kwargs)


def GetAgentProviderCapabilitiesRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent-provider capabilities request."""

    return pb.GetAgentProviderCapabilitiesRequest(*args, **kwargs)


def ExecuteAgentToolRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent host ExecuteTool request."""

    return pb.ExecuteAgentToolRequest(*args, **kwargs)


def ExecuteAgentToolResponse(*args: Any, **kwargs: Any) -> Any:
    """Create an agent host ExecuteTool response."""

    return pb.ExecuteAgentToolResponse(*args, **kwargs)


def ListAgentToolsRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent host ListTools request."""

    return pb.ListAgentToolsRequest(*args, **kwargs)


def ListAgentToolsResponse(*args: Any, **kwargs: Any) -> Any:
    """Create an agent host ListTools response."""

    return pb.ListAgentToolsResponse(*args, **kwargs)


def ListedAgentTool(*args: Any, **kwargs: Any) -> Any:
    """Create an agent host listed-tool value."""

    return pb.ListedAgentTool(*args, **kwargs)


def ResolvedAgentTool(*args: Any, **kwargs: Any) -> Any:
    """Create a resolved agent tool value."""

    return pb.ResolvedAgentTool(*args, **kwargs)


def ResolveAgentConnectionRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an agent host ResolveConnection request."""

    return pb.ResolveAgentConnectionRequest(*args, **kwargs)


def ResolvedAgentConnection(*args: Any, **kwargs: Any) -> Any:
    """Create an agent host ResolveConnection response."""

    return pb.ResolvedAgentConnection(*args, **kwargs)


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


def agent_actor_to_dict(actor: Any) -> dict[str, Any]:
    """Convert an ``AgentActor`` protocol value to a plain dictionary."""

    return _message_fields(
        actor,
        ("subject_id", "subject_kind", "display_name", "auth_source"),
    )


def agent_actor_from_dict(value: Mapping[str, Any] | None) -> Any:
    """Create an ``AgentActor`` protocol value from a plain dictionary."""

    data = dict(value or {})
    return pb.AgentActor(
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
    """Create an ``AgentSubjectContext`` protocol value from a dictionary."""

    data = dict(value or {})
    return pb.AgentSubjectContext(
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

    data = dict(value or {})
    return pb.PreparedAgentWorkspace(
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


def agent_tool_ref_from_dict(value: Any | None) -> Any:
    """Create an ``AgentToolRef`` protocol value from a dictionary."""

    data = dict(_mapping_value(value, "tool_ref")) if value is not None else {}
    return pb.AgentToolRef(
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
    if has_field(part, "tool_call"):
        value["tool_call"] = _tool_call_to_dict(part.tool_call)
    if has_field(part, "tool_result"):
        value["tool_result"] = _tool_result_to_dict(part.tool_result)
    if has_field(part, "image_ref"):
        value["image_ref"] = _image_ref_to_dict(part.image_ref)
    return value


def agent_message_part_from_dict(value: Any) -> Any:
    """Create an ``AgentMessagePart`` from a lower-snake-case dictionary."""

    data = dict(_mapping_value(value, "part"))
    part_type = data.get("type", pb.AGENT_MESSAGE_PART_TYPE_UNSPECIFIED)
    if part_type == pb.AGENT_MESSAGE_PART_TYPE_UNSPECIFIED:
        part_type = _agent_message_part_type(data)
    part = pb.AgentMessagePart(type=part_type)
    if "text" in data:
        part.text = str(data["text"])
    if data.get("json") is not None:
        part.json.CopyFrom(struct_from_dict(data["json"]))
    if data.get("tool_call") is not None:
        part.tool_call.CopyFrom(_tool_call_from_dict(data["tool_call"]))
    if data.get("tool_result") is not None:
        part.tool_result.CopyFrom(_tool_result_from_dict(data["tool_result"]))
    if data.get("image_ref") is not None:
        part.image_ref.CopyFrom(_image_ref_from_dict(data["image_ref"]))
    return part


def _agent_message_part_type(data: Mapping[str, Any]) -> int:
    if data.get("tool_call") is not None:
        return pb.AGENT_MESSAGE_PART_TYPE_TOOL_CALL
    if data.get("tool_result") is not None:
        return pb.AGENT_MESSAGE_PART_TYPE_TOOL_RESULT
    if data.get("image_ref") is not None:
        return pb.AGENT_MESSAGE_PART_TYPE_IMAGE_REF
    if data.get("json") is not None:
        return pb.AGENT_MESSAGE_PART_TYPE_JSON
    if data.get("text"):
        return pb.AGENT_MESSAGE_PART_TYPE_TEXT
    return pb.AGENT_MESSAGE_PART_TYPE_UNSPECIFIED


def agent_message_to_dict(message: Any) -> dict[str, Any]:
    """Convert an ``AgentMessage`` protocol value to a dictionary."""

    value = _message_fields(message, ("role", "text"))
    if message.parts:
        value["parts"] = [agent_message_part_to_dict(part) for part in message.parts]
    if has_field(message, "metadata"):
        value["metadata"] = struct_to_dict(message.metadata)
    return value


def agent_messages_to_dicts(messages: Iterable[Any]) -> list[dict[str, Any]]:
    """Convert agent protocol messages to dictionaries."""

    return [agent_message_to_dict(message) for message in messages]


def agent_message_from_dict(value: Any) -> Any:
    """Create an ``AgentMessage`` protocol value from a dictionary."""

    data = dict(_mapping_value(value, "message"))
    message = pb.AgentMessage(
        role=data.get("role", ""),
        text=data.get("text", ""),
    )
    for part in data.get("parts", []) or []:
        message.parts.append(
            agent_message_part_from_dict(_mapping_value(part, "parts[]"))
        )
    if data.get("metadata") is not None:
        message.metadata.CopyFrom(struct_from_dict(data["metadata"]))
    return message


def agent_messages_from_dicts(messages: Iterable[Mapping[str, Any]]) -> list[Any]:
    """Create agent protocol messages from dictionaries."""

    return [agent_message_from_dict(message) for message in messages]


def agent_message_to_proto_dict(message: Any) -> dict[str, Any]:
    """Convert an ``AgentMessage`` to protobuf JSON dictionary form."""

    value = message_to_dict(message, preserving_proto_field_name=True)
    if not isinstance(value, dict):
        raise TypeError("AgentMessage protobuf JSON projection must be an object")
    return value


def agent_message_from_proto_dict(value: Mapping[str, Any]) -> Any:
    """Create an ``AgentMessage`` from protobuf JSON dictionary form."""

    return message_from_dict(dict(value), pb.AgentMessage())


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

    def execute_tool(self, request: Any) -> Any:
        """Execute a host tool using an agent protocol request message."""

        return _grpc_call(self._stub.ExecuteTool, request)

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
    ) -> Any:
        """Execute a host tool using plain Python request fields."""

        request = pb.ExecuteAgentToolRequest(
            session_id=session_id,
            turn_id=turn_id,
            tool_call_id=tool_call_id,
            tool_id=tool_id,
            run_grant=run_grant,
            idempotency_key=idempotency_key,
        )
        if arguments is not None:
            request.arguments.CopyFrom(struct_from_dict(arguments))
        return self.execute_tool(request)

    def list_tools(self, request: Any) -> Any:
        """List host tools visible to the current agent request."""

        return _grpc_call(self._stub.ListTools, request)

    def list_tools_for_turn(
        self,
        session_id: str,
        turn_id: str,
        *,
        run_grant: str = "",
        page_size: int = 0,
        page_token: str = "",
        query: str = "",
    ) -> Any:
        """List host tools using plain Python request fields."""

        return self.list_tools(
            pb.ListAgentToolsRequest(
                session_id=session_id,
                turn_id=turn_id,
                run_grant=run_grant,
                page_size=page_size,
                page_token=page_token,
                query=query,
            )
        )

    def resolve_connection(self, request: Any) -> Any:
        """Resolve an agent connection for the current turn."""

        return _grpc_call(self._stub.ResolveConnection, request)

    def resolve_connection_for_turn(
        self,
        session_id: str,
        turn_id: str,
        *,
        connection: str,
        instance: str = "",
        run_grant: str = "",
    ) -> Any:
        """Resolve an agent connection using plain Python request fields."""

        return self.resolve_connection(
            pb.ResolveAgentConnectionRequest(
                session_id=session_id,
                turn_id=turn_id,
                connection=connection,
                instance=instance,
                run_grant=run_grant,
            )
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
    return agent_message_from_dict(_mapping_value(value, "messages[]"))


def _agent_tool_ref_value(value: Any) -> Any:
    if isinstance(value, pb.AgentToolRef):
        return _copy(value)
    return agent_tool_ref_from_dict(_mapping_value(value, "tool_refs[]"))


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


def _grpc_call(method: Any, request: Any) -> Any:
    try:
        return method(request)
    except grpc.RpcError:
        raise


def _message_fields(message: Any, fields: tuple[str, ...]) -> dict[str, Any]:
    value: dict[str, Any] = {}
    for field in fields:
        current = getattr(message, field, None)
        if current:
            value[field] = current
    return value


def _mapping_value(value: Any, field: str) -> Mapping[str, Any]:
    mapping = _dataclass_mapping(value)
    if mapping is not None:
        return mapping
    if not isinstance(value, Mapping):
        raise TypeError(f"{field} must be a mapping")
    return value


def _tool_call_to_dict(tool_call: Any) -> dict[str, Any]:
    value = _message_fields(tool_call, ("id", "tool_id"))
    if has_field(tool_call, "arguments"):
        value["arguments"] = struct_to_dict(tool_call.arguments)
    return value


def _tool_call_from_dict(value: Any) -> Any:
    data = dict(_mapping_value(value, "tool_call"))
    tool_call = pb.AgentMessagePartToolCall(
        id=data.get("id", ""),
        tool_id=data.get("tool_id", ""),
    )
    if data.get("arguments") is not None:
        tool_call.arguments.CopyFrom(struct_from_dict(data["arguments"]))
    return tool_call


def _tool_result_to_dict(tool_result: Any) -> dict[str, Any]:
    value = _message_fields(tool_result, ("tool_call_id", "content"))
    value["status"] = tool_result.status
    if has_field(tool_result, "output"):
        value["output"] = struct_to_dict(tool_result.output)
    return value


def _tool_result_from_dict(value: Any) -> Any:
    data = dict(_mapping_value(value, "tool_result"))
    tool_result = pb.AgentMessagePartToolResult(
        tool_call_id=data.get("tool_call_id", ""),
        status=data.get("status", 0),
        content=data.get("content", ""),
    )
    if data.get("output") is not None:
        tool_result.output.CopyFrom(struct_from_dict(data["output"]))
    return tool_result


def _image_ref_to_dict(image_ref: Any) -> dict[str, Any]:
    return _message_fields(image_ref, ("uri", "mime_type"))


def _image_ref_from_dict(value: Any) -> Any:
    data = dict(_mapping_value(value, "image_ref"))
    return pb.AgentMessagePartImageRef(
        uri=data.get("uri", ""),
        mime_type=data.get("mime_type", ""),
    )
