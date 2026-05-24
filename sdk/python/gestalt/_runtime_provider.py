from __future__ import annotations

import datetime as _dt
from collections.abc import Iterable, Mapping
from dataclasses import dataclass, field
from http import HTTPStatus
from typing import Any

from . import _agent as _agent_native
from ._api import Error
from ._gen.v1 import runtime_provider_pb2 as _pb
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

RUNTIME_EGRESS_MODE_UNSPECIFIED = pb.RUNTIME_EGRESS_MODE_UNSPECIFIED
RUNTIME_EGRESS_MODE_NONE = pb.RUNTIME_EGRESS_MODE_NONE
RUNTIME_EGRESS_MODE_CIDR = pb.RUNTIME_EGRESS_MODE_CIDR
RUNTIME_EGRESS_MODE_HOSTNAME = pb.RUNTIME_EGRESS_MODE_HOSTNAME


@dataclass(slots=True)
class GetRuntimeSupportRequest:
    """Request passed to ``RuntimeProvider.get_support``."""


@dataclass(slots=True)
class RuntimeSupport:
    """Capabilities returned by a runtime provider."""

    can_host_apps: bool = False
    egress_mode: int | str = RUNTIME_EGRESS_MODE_UNSPECIFIED
    supports_prepare_workspace: bool = False


@dataclass(slots=True)
class RuntimeSessionLifecycle:
    """Lifecycle timestamps for a runtime session."""

    started_at: _dt.datetime | None = None
    recommended_drain_at: _dt.datetime | None = None
    expires_at: _dt.datetime | None = None


@dataclass(slots=True)
class RuntimeSession:
    """Runtime session returned by a runtime provider."""

    id: str = ""
    state: str = ""
    metadata: Mapping[str, str] = field(default_factory=dict)
    lifecycle: RuntimeSessionLifecycle | Mapping[str, Any] | None = None
    state_reason: str = ""
    state_message: str = ""


@dataclass(slots=True)
class RuntimeImagePullAuth:
    """Container registry auth for a runtime image pull."""

    docker_config_json: str = ""


@dataclass(slots=True)
class StartRuntimeSessionRequest:
    """Request passed to ``RuntimeProvider.start_session``."""

    app_name: str = ""
    template: str = ""
    image: str = ""
    metadata: Mapping[str, str] = field(default_factory=dict)
    image_pull_auth: RuntimeImagePullAuth | Mapping[str, Any] | None = None


@dataclass(slots=True)
class GetRuntimeSessionRequest:
    """Request passed to ``RuntimeProvider.get_session``."""

    session_id: str = ""


@dataclass(slots=True)
class ListRuntimeSessionsRequest:
    """Request passed to ``RuntimeProvider.list_sessions``."""

    page_size: int = 0
    page_token: str = ""


@dataclass(slots=True)
class ListRuntimeSessionsResponse:
    """Sessions returned by ``RuntimeProvider.list_sessions``."""

    sessions: Iterable[RuntimeSession | Mapping[str, Any]] = field(default_factory=list)
    next_page_token: str = ""


@dataclass(slots=True)
class StopRuntimeSessionRequest:
    """Request passed to ``RuntimeProvider.stop_session``."""

    session_id: str = ""


@dataclass(slots=True)
class PrepareRuntimeWorkspaceRequest:
    """Request passed to ``RuntimeProvider.prepare_workspace``."""

    session_id: str = ""
    agent_session_id: str = ""
    workspace: _agent_native.AgentWorkspace | Mapping[str, Any] | None = None


@dataclass(slots=True)
class PrepareRuntimeWorkspaceResponse:
    """Workspace returned by ``RuntimeProvider.prepare_workspace``."""

    workspace: _agent_native.AgentPreparedWorkspace | Mapping[str, Any] | None = None


@dataclass(slots=True)
class RemoveRuntimeWorkspaceRequest:
    """Request passed to ``RuntimeProvider.remove_workspace``."""

    session_id: str = ""
    agent_session_id: str = ""


@dataclass(slots=True)
class StartHostedAppRequest:
    """Request passed to ``RuntimeProvider.start_app``."""

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
    """Hosted app returned by ``RuntimeProvider.start_app``."""

    id: str = ""
    session_id: str = ""
    app_name: str = ""
    dial_target: str = ""


def get_runtime_provider_support_request_from_proto(
    _value: Any,
) -> GetRuntimeSupportRequest:
    return GetRuntimeSupportRequest()


def runtime_provider_support_to_proto(value: Any) -> Any:
    if isinstance(value, pb.RuntimeSupport):
        return _copy(value)
    support = _coerce(value, RuntimeSupport, "RuntimeSupport")
    return pb.RuntimeSupport(
        can_host_apps=support.can_host_apps,
        egress_mode=support.egress_mode,
        supports_prepare_workspace=support.supports_prepare_workspace,
    )


def runtime_provider_session_from_proto(value: Any) -> RuntimeSession:
    return RuntimeSession(
        id=value.id,
        state=value.state,
        metadata=dict(value.metadata),
        lifecycle=runtime_provider_session_lifecycle_from_proto(value.lifecycle)
        if has_field(value, "lifecycle")
        else None,
        state_reason=value.state_reason,
        state_message=value.state_message,
    )


def runtime_provider_session_to_proto(value: Any) -> Any:
    if isinstance(value, pb.RuntimeSession):
        return _copy(value)
    session = _coerce(value, RuntimeSession, "RuntimeSession")
    out = pb.RuntimeSession(
        id=session.id,
        state=session.state,
        metadata=dict(session.metadata),
        state_reason=session.state_reason,
        state_message=session.state_message,
    )
    lifecycle = runtime_provider_session_lifecycle_to_proto(session.lifecycle)
    if lifecycle is not None:
        out.lifecycle.CopyFrom(lifecycle)
    return out


def runtime_provider_session_lifecycle_from_proto(
    value: Any,
) -> RuntimeSessionLifecycle:
    return RuntimeSessionLifecycle(
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


def runtime_provider_session_lifecycle_to_proto(value: Any) -> Any | None:
    if value is None:
        return None
    if isinstance(value, pb.RuntimeSessionLifecycle):
        return _copy(value)
    lifecycle = _coerce(
        value,
        RuntimeSessionLifecycle,
        "RuntimeSessionLifecycle",
    )
    out = pb.RuntimeSessionLifecycle()
    _copy_timestamp(out, "started_at", lifecycle.started_at)
    _copy_timestamp(out, "recommended_drain_at", lifecycle.recommended_drain_at)
    _copy_timestamp(out, "expires_at", lifecycle.expires_at)
    return out


def runtime_provider_image_pull_auth_from_proto(
    value: Any,
) -> RuntimeImagePullAuth:
    return RuntimeImagePullAuth(docker_config_json=value.docker_config_json)


def runtime_provider_image_pull_auth_to_proto(value: Any) -> Any | None:
    if value is None:
        return None
    if isinstance(value, pb.RuntimeImagePullAuth):
        return _copy(value)
    auth = _coerce(value, RuntimeImagePullAuth, "RuntimeImagePullAuth")
    return pb.RuntimeImagePullAuth(docker_config_json=auth.docker_config_json)


def start_runtime_provider_session_request_from_proto(
    value: Any,
) -> StartRuntimeSessionRequest:
    return StartRuntimeSessionRequest(
        app_name=value.app_name,
        template=value.template,
        image=value.image,
        metadata=dict(value.metadata),
        image_pull_auth=runtime_provider_image_pull_auth_from_proto(
            value.image_pull_auth
        )
        if has_field(value, "image_pull_auth")
        else None,
    )


def get_runtime_provider_session_request_from_proto(
    value: Any,
) -> GetRuntimeSessionRequest:
    return GetRuntimeSessionRequest(session_id=value.session_id)


def list_runtime_provider_sessions_request_from_proto(
    value: Any,
) -> ListRuntimeSessionsRequest:
    page_size = value.page_size
    if page_size < 0:
        raise Error(HTTPStatus.BAD_REQUEST, "page_size must be non-negative")
    if page_size == 0:
        page_size = 100
    if page_size > 200:
        page_size = 200
    return ListRuntimeSessionsRequest(
        page_size=page_size,
        page_token=value.page_token,
    )


def list_runtime_provider_sessions_response_to_proto(value: Any) -> Any:
    if isinstance(value, pb.ListRuntimeSessionsResponse):
        return _copy(value)
    response = _coerce(
        value,
        ListRuntimeSessionsResponse,
        "ListRuntimeSessionsResponse",
    )
    return pb.ListRuntimeSessionsResponse(
        sessions=[
            runtime_provider_session_to_proto(session) for session in response.sessions
        ],
        next_page_token=response.next_page_token,
    )


def stop_runtime_provider_session_request_from_proto(
    value: Any,
) -> StopRuntimeSessionRequest:
    return StopRuntimeSessionRequest(session_id=value.session_id)


def prepare_runtime_provider_workspace_request_from_proto(
    value: Any,
) -> PrepareRuntimeWorkspaceRequest:
    return PrepareRuntimeWorkspaceRequest(
        session_id=value.session_id,
        agent_session_id=value.agent_session_id,
        workspace=agent_workspace_input_from_proto(value.workspace)
        if has_field(value, "workspace")
        else None,
    )


def prepare_runtime_provider_workspace_response_to_proto(value: Any) -> Any:
    if isinstance(value, pb.PrepareRuntimeWorkspaceResponse):
        return _copy(value)
    response = _coerce(
        value,
        PrepareRuntimeWorkspaceResponse,
        "PrepareRuntimeWorkspaceResponse",
    )
    out = pb.PrepareRuntimeWorkspaceResponse()
    workspace = agent_prepared_workspace_to_proto(response.workspace)
    if workspace is not None:
        out.workspace.CopyFrom(workspace)
    return out


def remove_runtime_provider_workspace_request_from_proto(
    value: Any,
) -> RemoveRuntimeWorkspaceRequest:
    return RemoveRuntimeWorkspaceRequest(
        session_id=value.session_id,
        agent_session_id=value.agent_session_id,
    )


def start_hosted_app_request_from_proto(value: Any) -> StartHostedAppRequest:
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


def hosted_app_to_proto(value: Any) -> Any:
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
    return _agent_native.pb.PreparedAgentWorkspace(
        root=workspace.root, cwd=workspace.cwd
    )


def _copy_timestamp(target: Any, field_name: str, value: _dt.datetime | None) -> None:
    timestamp = timestamp_from_datetime(value)
    if timestamp is not None:
        getattr(target, field_name).CopyFrom(timestamp)
