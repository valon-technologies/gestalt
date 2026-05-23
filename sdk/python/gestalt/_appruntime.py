from __future__ import annotations

import datetime as _dt
from collections.abc import Iterable, Mapping
from dataclasses import dataclass, field
from typing import Any

from . import _agent as _agent_native
from ._gen.v1 import appruntime_pb2 as _pb
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

APP_RUNTIME_EGRESS_MODE_UNSPECIFIED = pb.APP_RUNTIME_EGRESS_MODE_UNSPECIFIED
APP_RUNTIME_EGRESS_MODE_NONE = pb.APP_RUNTIME_EGRESS_MODE_NONE
APP_RUNTIME_EGRESS_MODE_CIDR = pb.APP_RUNTIME_EGRESS_MODE_CIDR
APP_RUNTIME_EGRESS_MODE_HOSTNAME = pb.APP_RUNTIME_EGRESS_MODE_HOSTNAME


@dataclass(slots=True)
class GetAppRuntimeSupportRequest:
    """Request passed to ``AppRuntimeProvider.get_support``."""


@dataclass(slots=True)
class AppRuntimeSupport:
    """Capabilities returned by a plugin-runtime provider."""

    can_host_apps: bool = False
    egress_mode: int | str = APP_RUNTIME_EGRESS_MODE_UNSPECIFIED
    supports_prepare_workspace: bool = False


@dataclass(slots=True)
class AppRuntimeSessionLifecycle:
    """Lifecycle timestamps for a plugin-runtime session."""

    started_at: _dt.datetime | None = None
    recommended_drain_at: _dt.datetime | None = None
    expires_at: _dt.datetime | None = None


@dataclass(slots=True)
class AppRuntimeSession:
    """Plugin-runtime session returned by a runtime provider."""

    id: str = ""
    state: str = ""
    metadata: Mapping[str, str] = field(default_factory=dict)
    lifecycle: AppRuntimeSessionLifecycle | Mapping[str, Any] | None = None
    state_reason: str = ""
    state_message: str = ""


@dataclass(slots=True)
class AppRuntimeImagePullAuth:
    """Container registry auth for a runtime image pull."""

    docker_config_json: str = ""


@dataclass(slots=True)
class StartAppRuntimeSessionRequest:
    """Request passed to ``AppRuntimeProvider.start_session``."""

    app_name: str = ""
    template: str = ""
    image: str = ""
    metadata: Mapping[str, str] = field(default_factory=dict)
    image_pull_auth: AppRuntimeImagePullAuth | Mapping[str, Any] | None = None


@dataclass(slots=True)
class GetAppRuntimeSessionRequest:
    """Request passed to ``AppRuntimeProvider.get_session``."""

    session_id: str = ""


@dataclass(slots=True)
class ListAppRuntimeSessionsRequest:
    """Request passed to ``AppRuntimeProvider.list_sessions``."""


@dataclass(slots=True)
class ListAppRuntimeSessionsResponse:
    """Sessions returned by ``AppRuntimeProvider.list_sessions``."""

    sessions: Iterable[AppRuntimeSession | Mapping[str, Any]] = field(
        default_factory=list
    )


@dataclass(slots=True)
class StopAppRuntimeSessionRequest:
    """Request passed to ``AppRuntimeProvider.stop_session``."""

    session_id: str = ""


@dataclass(slots=True)
class PrepareAppRuntimeWorkspaceRequest:
    """Request passed to ``AppRuntimeProvider.prepare_workspace``."""

    session_id: str = ""
    agent_session_id: str = ""
    workspace: _agent_native.AgentWorkspace | Mapping[str, Any] | None = None


@dataclass(slots=True)
class PrepareAppRuntimeWorkspaceResponse:
    """Workspace returned by ``AppRuntimeProvider.prepare_workspace``."""

    workspace: _agent_native.AgentPreparedWorkspace | Mapping[str, Any] | None = None


@dataclass(slots=True)
class RemoveAppRuntimeWorkspaceRequest:
    """Request passed to ``AppRuntimeProvider.remove_workspace``."""

    session_id: str = ""
    agent_session_id: str = ""


@dataclass(slots=True)
class StartHostedAppRequest:
    """Request passed to ``AppRuntimeProvider.start_plugin``."""

    session_id: str = ""
    app_name: str = ""
    command: str = ""
    args: Iterable[str] = field(default_factory=list)
    env: Mapping[str, str] = field(default_factory=dict)
    allowed_hosts: Iterable[str] = field(default_factory=list)
    default_action: str = ""
    host_binary: str = ""
    workdir: str = ""


@dataclass(slots=True)
class HostedApp:
    """Hosted app returned by ``AppRuntimeProvider.start_plugin``."""

    id: str = ""
    session_id: str = ""
    app_name: str = ""
    dial_target: str = ""


def get_app_runtime_support_request_from_proto(
    _value: Any,
) -> GetAppRuntimeSupportRequest:
    return GetAppRuntimeSupportRequest()


def app_runtime_support_to_proto(value: Any) -> Any:
    if isinstance(value, pb.AppRuntimeSupport):
        return _copy(value)
    support = _coerce(value, AppRuntimeSupport, "AppRuntimeSupport")
    return pb.AppRuntimeSupport(
        can_host_apps=support.can_host_apps,
        egress_mode=support.egress_mode,
        supports_prepare_workspace=support.supports_prepare_workspace,
    )


def app_runtime_session_from_proto(value: Any) -> AppRuntimeSession:
    return AppRuntimeSession(
        id=value.id,
        state=value.state,
        metadata=dict(value.metadata),
        lifecycle=app_runtime_session_lifecycle_from_proto(value.lifecycle)
        if has_field(value, "lifecycle")
        else None,
        state_reason=value.state_reason,
        state_message=value.state_message,
    )


def app_runtime_session_to_proto(value: Any) -> Any:
    if isinstance(value, pb.AppRuntimeSession):
        return _copy(value)
    session = _coerce(value, AppRuntimeSession, "AppRuntimeSession")
    out = pb.AppRuntimeSession(
        id=session.id,
        state=session.state,
        metadata=dict(session.metadata),
        state_reason=session.state_reason,
        state_message=session.state_message,
    )
    lifecycle = app_runtime_session_lifecycle_to_proto(session.lifecycle)
    if lifecycle is not None:
        out.lifecycle.CopyFrom(lifecycle)
    return out


def app_runtime_session_lifecycle_from_proto(
    value: Any,
) -> AppRuntimeSessionLifecycle:
    return AppRuntimeSessionLifecycle(
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


def app_runtime_session_lifecycle_to_proto(value: Any) -> Any | None:
    if value is None:
        return None
    if isinstance(value, pb.AppRuntimeSessionLifecycle):
        return _copy(value)
    lifecycle = _coerce(
        value,
        AppRuntimeSessionLifecycle,
        "AppRuntimeSessionLifecycle",
    )
    out = pb.AppRuntimeSessionLifecycle()
    _copy_timestamp(out, "started_at", lifecycle.started_at)
    _copy_timestamp(out, "recommended_drain_at", lifecycle.recommended_drain_at)
    _copy_timestamp(out, "expires_at", lifecycle.expires_at)
    return out


def app_runtime_image_pull_auth_from_proto(
    value: Any,
) -> AppRuntimeImagePullAuth:
    return AppRuntimeImagePullAuth(docker_config_json=value.docker_config_json)


def app_runtime_image_pull_auth_to_proto(value: Any) -> Any | None:
    if value is None:
        return None
    if isinstance(value, pb.AppRuntimeImagePullAuth):
        return _copy(value)
    auth = _coerce(value, AppRuntimeImagePullAuth, "AppRuntimeImagePullAuth")
    return pb.AppRuntimeImagePullAuth(docker_config_json=auth.docker_config_json)


def start_app_runtime_session_request_from_proto(
    value: Any,
) -> StartAppRuntimeSessionRequest:
    return StartAppRuntimeSessionRequest(
        app_name=value.app_name,
        template=value.template,
        image=value.image,
        metadata=dict(value.metadata),
        image_pull_auth=app_runtime_image_pull_auth_from_proto(
            value.image_pull_auth
        )
        if has_field(value, "image_pull_auth")
        else None,
    )


def get_app_runtime_session_request_from_proto(
    value: Any,
) -> GetAppRuntimeSessionRequest:
    return GetAppRuntimeSessionRequest(session_id=value.session_id)


def list_app_runtime_sessions_request_from_proto(
    _value: Any,
) -> ListAppRuntimeSessionsRequest:
    return ListAppRuntimeSessionsRequest()


def list_app_runtime_sessions_response_to_proto(value: Any) -> Any:
    if isinstance(value, pb.ListAppRuntimeSessionsResponse):
        return _copy(value)
    response = _coerce(
        value,
        ListAppRuntimeSessionsResponse,
        "ListAppRuntimeSessionsResponse",
    )
    return pb.ListAppRuntimeSessionsResponse(
        sessions=[
            app_runtime_session_to_proto(session) for session in response.sessions
        ]
    )


def stop_app_runtime_session_request_from_proto(
    value: Any,
) -> StopAppRuntimeSessionRequest:
    return StopAppRuntimeSessionRequest(session_id=value.session_id)


def prepare_app_runtime_workspace_request_from_proto(
    value: Any,
) -> PrepareAppRuntimeWorkspaceRequest:
    return PrepareAppRuntimeWorkspaceRequest(
        session_id=value.session_id,
        agent_session_id=value.agent_session_id,
        workspace=agent_workspace_input_from_proto(value.workspace)
        if has_field(value, "workspace")
        else None,
    )


def prepare_app_runtime_workspace_response_to_proto(value: Any) -> Any:
    if isinstance(value, pb.PrepareAppRuntimeWorkspaceResponse):
        return _copy(value)
    response = _coerce(
        value,
        PrepareAppRuntimeWorkspaceResponse,
        "PrepareAppRuntimeWorkspaceResponse",
    )
    out = pb.PrepareAppRuntimeWorkspaceResponse()
    workspace = agent_prepared_workspace_to_proto(response.workspace)
    if workspace is not None:
        out.workspace.CopyFrom(workspace)
    return out


def remove_app_runtime_workspace_request_from_proto(
    value: Any,
) -> RemoveAppRuntimeWorkspaceRequest:
    return RemoveAppRuntimeWorkspaceRequest(
        session_id=value.session_id,
        agent_session_id=value.agent_session_id,
    )


def start_hosted_plugin_request_from_proto(value: Any) -> StartHostedAppRequest:
    return StartHostedAppRequest(
        session_id=value.session_id,
        app_name=value.app_name,
        command=value.command,
        args=list(value.args),
        env=dict(value.env),
        allowed_hosts=list(value.allowed_hosts),
        default_action=value.default_action,
        host_binary=value.host_binary,
        workdir=value.workdir,
    )


def hosted_plugin_to_proto(value: Any) -> Any:
    if isinstance(value, pb.HostedApp):
        return _copy(value)
    app = _coerce(value, HostedApp, "HostedApp")
    return pb.HostedApp(
        id=app.id,
        session_id=app.session_id,
        app_name=app.app_name,
        dial_target=app.dial_target,
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
