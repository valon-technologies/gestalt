from __future__ import annotations

import datetime as _dt
from collections.abc import Iterable, Mapping
from dataclasses import dataclass, field
from typing import Any

from . import _agent as _agent_native
from ._gen.v1 import pluginruntime_pb2 as _pb
from ._protocol import (
    coerce_model as _coerce,
)
from ._protocol import (
    copy_message as _copy,
)
from ._protocol import (
    datetime_from_timestamp,
    has_field,
    timestamp_from_datetime,
)

pb: Any = _pb

PLUGIN_RUNTIME_EGRESS_MODE_UNSPECIFIED = pb.PLUGIN_RUNTIME_EGRESS_MODE_UNSPECIFIED
PLUGIN_RUNTIME_EGRESS_MODE_NONE = pb.PLUGIN_RUNTIME_EGRESS_MODE_NONE
PLUGIN_RUNTIME_EGRESS_MODE_CIDR = pb.PLUGIN_RUNTIME_EGRESS_MODE_CIDR
PLUGIN_RUNTIME_EGRESS_MODE_HOSTNAME = pb.PLUGIN_RUNTIME_EGRESS_MODE_HOSTNAME


@dataclass(slots=True)
class GetPluginRuntimeSupportRequest:
    """Request passed to ``PluginRuntimeProvider.get_support``."""


@dataclass(slots=True)
class PluginRuntimeSupport:
    """Capabilities returned by a plugin-runtime provider."""

    can_host_plugins: bool = False
    egress_mode: int | str = PLUGIN_RUNTIME_EGRESS_MODE_UNSPECIFIED
    supports_prepare_workspace: bool = False


@dataclass(slots=True)
class PluginRuntimeSessionLifecycle:
    """Lifecycle timestamps for a plugin-runtime session."""

    started_at: _dt.datetime | None = None
    recommended_drain_at: _dt.datetime | None = None
    expires_at: _dt.datetime | None = None


@dataclass(slots=True)
class PluginRuntimeSession:
    """Plugin-runtime session returned by a runtime provider."""

    id: str = ""
    state: str = ""
    metadata: Mapping[str, str] = field(default_factory=dict)
    lifecycle: PluginRuntimeSessionLifecycle | Mapping[str, Any] | None = None
    state_reason: str = ""
    state_message: str = ""


@dataclass(slots=True)
class PluginRuntimeImagePullAuth:
    """Container registry auth for a runtime image pull."""

    docker_config_json: str = ""


@dataclass(slots=True)
class StartPluginRuntimeSessionRequest:
    """Request passed to ``PluginRuntimeProvider.start_session``."""

    plugin_name: str = ""
    template: str = ""
    image: str = ""
    metadata: Mapping[str, str] = field(default_factory=dict)
    image_pull_auth: PluginRuntimeImagePullAuth | Mapping[str, Any] | None = None


@dataclass(slots=True)
class GetPluginRuntimeSessionRequest:
    """Request passed to ``PluginRuntimeProvider.get_session``."""

    session_id: str = ""


@dataclass(slots=True)
class ListPluginRuntimeSessionsRequest:
    """Request passed to ``PluginRuntimeProvider.list_sessions``."""


@dataclass(slots=True)
class ListPluginRuntimeSessionsResponse:
    """Sessions returned by ``PluginRuntimeProvider.list_sessions``."""

    sessions: Iterable[PluginRuntimeSession | Mapping[str, Any]] = field(
        default_factory=list
    )


@dataclass(slots=True)
class StopPluginRuntimeSessionRequest:
    """Request passed to ``PluginRuntimeProvider.stop_session``."""

    session_id: str = ""


@dataclass(slots=True)
class PreparePluginRuntimeWorkspaceRequest:
    """Request passed to ``PluginRuntimeProvider.prepare_workspace``."""

    session_id: str = ""
    agent_session_id: str = ""
    workspace: _agent_native.AgentWorkspace | Mapping[str, Any] | None = None


@dataclass(slots=True)
class PreparePluginRuntimeWorkspaceResponse:
    """Workspace returned by ``PluginRuntimeProvider.prepare_workspace``."""

    workspace: _agent_native.AgentPreparedWorkspace | Mapping[str, Any] | None = None


@dataclass(slots=True)
class RemovePluginRuntimeWorkspaceRequest:
    """Request passed to ``PluginRuntimeProvider.remove_workspace``."""

    session_id: str = ""
    agent_session_id: str = ""


@dataclass(slots=True)
class StartHostedPluginRequest:
    """Request passed to ``PluginRuntimeProvider.start_plugin``."""

    session_id: str = ""
    plugin_name: str = ""
    command: str = ""
    args: Iterable[str] = field(default_factory=list)
    env: Mapping[str, str] = field(default_factory=dict)
    allowed_hosts: Iterable[str] = field(default_factory=list)
    default_action: str = ""
    host_binary: str = ""
    workdir: str = ""


@dataclass(slots=True)
class HostedPlugin:
    """Hosted plugin returned by ``PluginRuntimeProvider.start_plugin``."""

    id: str = ""
    session_id: str = ""
    plugin_name: str = ""
    dial_target: str = ""


def get_plugin_runtime_support_request_from_proto(
    _value: Any,
) -> GetPluginRuntimeSupportRequest:
    return GetPluginRuntimeSupportRequest()


def plugin_runtime_support_to_proto(value: Any) -> Any:
    if isinstance(value, pb.PluginRuntimeSupport):
        return _copy(value)
    support = _coerce(value, PluginRuntimeSupport, "PluginRuntimeSupport")
    return pb.PluginRuntimeSupport(
        can_host_plugins=support.can_host_plugins,
        egress_mode=support.egress_mode,
        supports_prepare_workspace=support.supports_prepare_workspace,
    )


def plugin_runtime_session_from_proto(value: Any) -> PluginRuntimeSession:
    return PluginRuntimeSession(
        id=value.id,
        state=value.state,
        metadata=dict(value.metadata),
        lifecycle=plugin_runtime_session_lifecycle_from_proto(value.lifecycle)
        if has_field(value, "lifecycle")
        else None,
        state_reason=value.state_reason,
        state_message=value.state_message,
    )


def plugin_runtime_session_to_proto(value: Any) -> Any:
    if isinstance(value, pb.PluginRuntimeSession):
        return _copy(value)
    session = _coerce(value, PluginRuntimeSession, "PluginRuntimeSession")
    out = pb.PluginRuntimeSession(
        id=session.id,
        state=session.state,
        metadata=dict(session.metadata),
        state_reason=session.state_reason,
        state_message=session.state_message,
    )
    lifecycle = plugin_runtime_session_lifecycle_to_proto(session.lifecycle)
    if lifecycle is not None:
        out.lifecycle.CopyFrom(lifecycle)
    return out


def plugin_runtime_session_lifecycle_from_proto(
    value: Any,
) -> PluginRuntimeSessionLifecycle:
    return PluginRuntimeSessionLifecycle(
        started_at=datetime_from_timestamp(value.started_at)
        if has_field(value, "started_at")
        else None,
        recommended_drain_at=datetime_from_timestamp(value.recommended_drain_at)
        if has_field(value, "recommended_drain_at")
        else None,
        expires_at=datetime_from_timestamp(value.expires_at)
        if has_field(value, "expires_at")
        else None,
    )


def plugin_runtime_session_lifecycle_to_proto(value: Any) -> Any | None:
    if value is None:
        return None
    if isinstance(value, pb.PluginRuntimeSessionLifecycle):
        return _copy(value)
    lifecycle = _coerce(
        value,
        PluginRuntimeSessionLifecycle,
        "PluginRuntimeSessionLifecycle",
    )
    out = pb.PluginRuntimeSessionLifecycle()
    _copy_timestamp(out, "started_at", lifecycle.started_at)
    _copy_timestamp(out, "recommended_drain_at", lifecycle.recommended_drain_at)
    _copy_timestamp(out, "expires_at", lifecycle.expires_at)
    return out


def plugin_runtime_image_pull_auth_from_proto(
    value: Any,
) -> PluginRuntimeImagePullAuth:
    return PluginRuntimeImagePullAuth(docker_config_json=value.docker_config_json)


def plugin_runtime_image_pull_auth_to_proto(value: Any) -> Any | None:
    if value is None:
        return None
    if isinstance(value, pb.PluginRuntimeImagePullAuth):
        return _copy(value)
    auth = _coerce(value, PluginRuntimeImagePullAuth, "PluginRuntimeImagePullAuth")
    return pb.PluginRuntimeImagePullAuth(docker_config_json=auth.docker_config_json)


def start_plugin_runtime_session_request_from_proto(
    value: Any,
) -> StartPluginRuntimeSessionRequest:
    return StartPluginRuntimeSessionRequest(
        plugin_name=value.plugin_name,
        template=value.template,
        image=value.image,
        metadata=dict(value.metadata),
        image_pull_auth=plugin_runtime_image_pull_auth_from_proto(
            value.image_pull_auth
        )
        if has_field(value, "image_pull_auth")
        else None,
    )


def get_plugin_runtime_session_request_from_proto(
    value: Any,
) -> GetPluginRuntimeSessionRequest:
    return GetPluginRuntimeSessionRequest(session_id=value.session_id)


def list_plugin_runtime_sessions_request_from_proto(
    _value: Any,
) -> ListPluginRuntimeSessionsRequest:
    return ListPluginRuntimeSessionsRequest()


def list_plugin_runtime_sessions_response_to_proto(value: Any) -> Any:
    if isinstance(value, pb.ListPluginRuntimeSessionsResponse):
        return _copy(value)
    response = _coerce(
        value,
        ListPluginRuntimeSessionsResponse,
        "ListPluginRuntimeSessionsResponse",
    )
    return pb.ListPluginRuntimeSessionsResponse(
        sessions=[
            plugin_runtime_session_to_proto(session) for session in response.sessions
        ]
    )


def stop_plugin_runtime_session_request_from_proto(
    value: Any,
) -> StopPluginRuntimeSessionRequest:
    return StopPluginRuntimeSessionRequest(session_id=value.session_id)


def prepare_plugin_runtime_workspace_request_from_proto(
    value: Any,
) -> PreparePluginRuntimeWorkspaceRequest:
    return PreparePluginRuntimeWorkspaceRequest(
        session_id=value.session_id,
        agent_session_id=value.agent_session_id,
        workspace=agent_workspace_input_from_proto(value.workspace)
        if has_field(value, "workspace")
        else None,
    )


def prepare_plugin_runtime_workspace_response_to_proto(value: Any) -> Any:
    if isinstance(value, pb.PreparePluginRuntimeWorkspaceResponse):
        return _copy(value)
    response = _coerce(
        value,
        PreparePluginRuntimeWorkspaceResponse,
        "PreparePluginRuntimeWorkspaceResponse",
    )
    out = pb.PreparePluginRuntimeWorkspaceResponse()
    workspace = agent_prepared_workspace_to_proto(response.workspace)
    if workspace is not None:
        out.workspace.CopyFrom(workspace)
    return out


def remove_plugin_runtime_workspace_request_from_proto(
    value: Any,
) -> RemovePluginRuntimeWorkspaceRequest:
    return RemovePluginRuntimeWorkspaceRequest(
        session_id=value.session_id,
        agent_session_id=value.agent_session_id,
    )


def start_hosted_plugin_request_from_proto(value: Any) -> StartHostedPluginRequest:
    return StartHostedPluginRequest(
        session_id=value.session_id,
        plugin_name=value.plugin_name,
        command=value.command,
        args=list(value.args),
        env=dict(value.env),
        allowed_hosts=list(value.allowed_hosts),
        default_action=value.default_action,
        host_binary=value.host_binary,
        workdir=value.workdir,
    )


def hosted_plugin_to_proto(value: Any) -> Any:
    if isinstance(value, pb.HostedPlugin):
        return _copy(value)
    plugin = _coerce(value, HostedPlugin, "HostedPlugin")
    return pb.HostedPlugin(
        id=plugin.id,
        session_id=plugin.session_id,
        plugin_name=plugin.plugin_name,
        dial_target=plugin.dial_target,
    )


def agent_workspace_input_from_proto(value: Any) -> _agent_native.AgentWorkspace:
    return _agent_native.AgentWorkspace(
        checkouts=[
            _agent_native.AgentWorkspaceGitCheckout(
                url=checkout.url,
                ref=checkout.ref,
                path=checkout.path,
            )
            for checkout in value.checkouts
        ],
        cwd=value.cwd,
    )


def agent_prepared_workspace_to_proto(value: Any) -> Any | None:
    if value is None:
        return None
    if isinstance(value, _agent_native.pb.PreparedAgentWorkspace):
        return _copy(value)
    workspace = _coerce(value, _agent_native.AgentPreparedWorkspace, "workspace")
    return _agent_native.pb.PreparedAgentWorkspace(root=workspace.root, cwd=workspace.cwd)


def _copy_timestamp(target: Any, field_name: str, value: _dt.datetime | None) -> None:
    timestamp = timestamp_from_datetime(value)
    if timestamp is not None:
        getattr(target, field_name).CopyFrom(timestamp)
