import datetime

from google.protobuf import empty_pb2 as _empty_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from . import agent_pb2 as _agent_pb2
from . import annotations_pb2 as _annotations_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class RuntimeEgressMode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    RUNTIME_EGRESS_MODE_UNSPECIFIED: _ClassVar[RuntimeEgressMode]
    RUNTIME_EGRESS_MODE_NONE: _ClassVar[RuntimeEgressMode]
    RUNTIME_EGRESS_MODE_CIDR: _ClassVar[RuntimeEgressMode]
    RUNTIME_EGRESS_MODE_HOSTNAME: _ClassVar[RuntimeEgressMode]
RUNTIME_EGRESS_MODE_UNSPECIFIED: RuntimeEgressMode
RUNTIME_EGRESS_MODE_NONE: RuntimeEgressMode
RUNTIME_EGRESS_MODE_CIDR: RuntimeEgressMode
RUNTIME_EGRESS_MODE_HOSTNAME: RuntimeEgressMode

class RuntimeSupport(_message.Message):
    __slots__ = ()
    CAN_HOST_APPS_FIELD_NUMBER: _ClassVar[int]
    EGRESS_MODE_FIELD_NUMBER: _ClassVar[int]
    SUPPORTS_PREPARE_WORKSPACE_FIELD_NUMBER: _ClassVar[int]
    can_host_apps: bool
    egress_mode: RuntimeEgressMode
    supports_prepare_workspace: bool
    def __init__(self, can_host_apps: _Optional[bool] = ..., egress_mode: _Optional[_Union[RuntimeEgressMode, str]] = ..., supports_prepare_workspace: _Optional[bool] = ...) -> None: ...

class RuntimeSession(_message.Message):
    __slots__ = ()
    class MetadataEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    ID_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    LIFECYCLE_FIELD_NUMBER: _ClassVar[int]
    STATE_REASON_FIELD_NUMBER: _ClassVar[int]
    STATE_MESSAGE_FIELD_NUMBER: _ClassVar[int]
    id: str
    state: str
    metadata: _containers.ScalarMap[str, str]
    lifecycle: RuntimeSessionLifecycle
    state_reason: str
    state_message: str
    def __init__(self, id: _Optional[str] = ..., state: _Optional[str] = ..., metadata: _Optional[_Mapping[str, str]] = ..., lifecycle: _Optional[_Union[RuntimeSessionLifecycle, _Mapping]] = ..., state_reason: _Optional[str] = ..., state_message: _Optional[str] = ...) -> None: ...

class RuntimeSessionLifecycle(_message.Message):
    __slots__ = ()
    STARTED_AT_FIELD_NUMBER: _ClassVar[int]
    RECOMMENDED_DRAIN_AT_FIELD_NUMBER: _ClassVar[int]
    EXPIRES_AT_FIELD_NUMBER: _ClassVar[int]
    started_at: _timestamp_pb2.Timestamp
    recommended_drain_at: _timestamp_pb2.Timestamp
    expires_at: _timestamp_pb2.Timestamp
    def __init__(self, started_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., recommended_drain_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., expires_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class RuntimeImagePullAuth(_message.Message):
    __slots__ = ()
    DOCKER_CONFIG_JSON_FIELD_NUMBER: _ClassVar[int]
    docker_config_json: str
    def __init__(self, docker_config_json: _Optional[str] = ...) -> None: ...

class StartRuntimeSessionRequest(_message.Message):
    __slots__ = ()
    class MetadataEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    APP_NAME_FIELD_NUMBER: _ClassVar[int]
    TEMPLATE_FIELD_NUMBER: _ClassVar[int]
    IMAGE_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    IMAGE_PULL_AUTH_FIELD_NUMBER: _ClassVar[int]
    app_name: str
    template: str
    image: str
    metadata: _containers.ScalarMap[str, str]
    image_pull_auth: RuntimeImagePullAuth
    def __init__(self, app_name: _Optional[str] = ..., template: _Optional[str] = ..., image: _Optional[str] = ..., metadata: _Optional[_Mapping[str, str]] = ..., image_pull_auth: _Optional[_Union[RuntimeImagePullAuth, _Mapping]] = ...) -> None: ...

class GetRuntimeSessionRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    def __init__(self, session_id: _Optional[str] = ...) -> None: ...

class ListRuntimeSessionsRequest(_message.Message):
    __slots__ = ()
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    page_size: int
    page_token: str
    def __init__(self, page_size: _Optional[int] = ..., page_token: _Optional[str] = ...) -> None: ...

class ListRuntimeSessionsResponse(_message.Message):
    __slots__ = ()
    SESSIONS_FIELD_NUMBER: _ClassVar[int]
    NEXT_PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    sessions: _containers.RepeatedCompositeFieldContainer[RuntimeSession]
    next_page_token: str
    def __init__(self, sessions: _Optional[_Iterable[_Union[RuntimeSession, _Mapping]]] = ..., next_page_token: _Optional[str] = ...) -> None: ...

class StopRuntimeSessionRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    def __init__(self, session_id: _Optional[str] = ...) -> None: ...

class PrepareRuntimeWorkspaceRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    AGENT_SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    WORKSPACE_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    agent_session_id: str
    workspace: _agent_pb2.AgentWorkspace
    def __init__(self, session_id: _Optional[str] = ..., agent_session_id: _Optional[str] = ..., workspace: _Optional[_Union[_agent_pb2.AgentWorkspace, _Mapping]] = ...) -> None: ...

class PrepareRuntimeWorkspaceResponse(_message.Message):
    __slots__ = ()
    WORKSPACE_FIELD_NUMBER: _ClassVar[int]
    workspace: _agent_pb2.PreparedAgentWorkspace
    def __init__(self, workspace: _Optional[_Union[_agent_pb2.PreparedAgentWorkspace, _Mapping]] = ...) -> None: ...

class RemoveRuntimeWorkspaceRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    AGENT_SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    agent_session_id: str
    def __init__(self, session_id: _Optional[str] = ..., agent_session_id: _Optional[str] = ...) -> None: ...

class StartHostedAppRequest(_message.Message):
    __slots__ = ()
    class EnvEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    APP_NAME_FIELD_NUMBER: _ClassVar[int]
    COMMAND_FIELD_NUMBER: _ClassVar[int]
    ARGS_FIELD_NUMBER: _ClassVar[int]
    ENV_FIELD_NUMBER: _ClassVar[int]
    ALLOWED_HOSTS_FIELD_NUMBER: _ClassVar[int]
    DEFAULT_ACTION_FIELD_NUMBER: _ClassVar[int]
    HOST_BINARY_FIELD_NUMBER: _ClassVar[int]
    WORKDIR_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    app_name: str
    command: str
    args: _containers.RepeatedScalarFieldContainer[str]
    env: _containers.ScalarMap[str, str]
    allowed_hosts: _containers.RepeatedScalarFieldContainer[str]
    default_action: str
    host_binary: str
    workdir: str
    def __init__(self, session_id: _Optional[str] = ..., app_name: _Optional[str] = ..., command: _Optional[str] = ..., args: _Optional[_Iterable[str]] = ..., env: _Optional[_Mapping[str, str]] = ..., allowed_hosts: _Optional[_Iterable[str]] = ..., default_action: _Optional[str] = ..., host_binary: _Optional[str] = ..., workdir: _Optional[str] = ...) -> None: ...

class HostedApp(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    APP_NAME_FIELD_NUMBER: _ClassVar[int]
    DIAL_TARGET_FIELD_NUMBER: _ClassVar[int]
    id: str
    session_id: str
    app_name: str
    dial_target: str
    def __init__(self, id: _Optional[str] = ..., session_id: _Optional[str] = ..., app_name: _Optional[str] = ..., dial_target: _Optional[str] = ...) -> None: ...
