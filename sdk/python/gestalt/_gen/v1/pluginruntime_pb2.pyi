import datetime

from google.protobuf import empty_pb2 as _empty_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from . import agent_pb2 as _agent_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class PluginRuntimeEgressMode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    PLUGIN_RUNTIME_EGRESS_MODE_UNSPECIFIED: _ClassVar[PluginRuntimeEgressMode]
    PLUGIN_RUNTIME_EGRESS_MODE_NONE: _ClassVar[PluginRuntimeEgressMode]
    PLUGIN_RUNTIME_EGRESS_MODE_CIDR: _ClassVar[PluginRuntimeEgressMode]
    PLUGIN_RUNTIME_EGRESS_MODE_HOSTNAME: _ClassVar[PluginRuntimeEgressMode]

class PluginRuntimeLogStream(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    PLUGIN_RUNTIME_LOG_STREAM_UNSPECIFIED: _ClassVar[PluginRuntimeLogStream]
    PLUGIN_RUNTIME_LOG_STREAM_STDOUT: _ClassVar[PluginRuntimeLogStream]
    PLUGIN_RUNTIME_LOG_STREAM_STDERR: _ClassVar[PluginRuntimeLogStream]
    PLUGIN_RUNTIME_LOG_STREAM_RUNTIME: _ClassVar[PluginRuntimeLogStream]
PLUGIN_RUNTIME_EGRESS_MODE_UNSPECIFIED: PluginRuntimeEgressMode
PLUGIN_RUNTIME_EGRESS_MODE_NONE: PluginRuntimeEgressMode
PLUGIN_RUNTIME_EGRESS_MODE_CIDR: PluginRuntimeEgressMode
PLUGIN_RUNTIME_EGRESS_MODE_HOSTNAME: PluginRuntimeEgressMode
PLUGIN_RUNTIME_LOG_STREAM_UNSPECIFIED: PluginRuntimeLogStream
PLUGIN_RUNTIME_LOG_STREAM_STDOUT: PluginRuntimeLogStream
PLUGIN_RUNTIME_LOG_STREAM_STDERR: PluginRuntimeLogStream
PLUGIN_RUNTIME_LOG_STREAM_RUNTIME: PluginRuntimeLogStream

class PluginRuntimeSupport(_message.Message):
    __slots__ = ()
    CAN_HOST_PLUGINS_FIELD_NUMBER: _ClassVar[int]
    EGRESS_MODE_FIELD_NUMBER: _ClassVar[int]
    SUPPORTS_PREPARE_WORKSPACE_FIELD_NUMBER: _ClassVar[int]
    can_host_plugins: bool
    egress_mode: PluginRuntimeEgressMode
    supports_prepare_workspace: bool
    def __init__(self, can_host_plugins: _Optional[bool] = ..., egress_mode: _Optional[_Union[PluginRuntimeEgressMode, str]] = ..., supports_prepare_workspace: _Optional[bool] = ...) -> None: ...

class PluginRuntimeSession(_message.Message):
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
    lifecycle: PluginRuntimeSessionLifecycle
    state_reason: str
    state_message: str
    def __init__(self, id: _Optional[str] = ..., state: _Optional[str] = ..., metadata: _Optional[_Mapping[str, str]] = ..., lifecycle: _Optional[_Union[PluginRuntimeSessionLifecycle, _Mapping]] = ..., state_reason: _Optional[str] = ..., state_message: _Optional[str] = ...) -> None: ...

class PluginRuntimeSessionLifecycle(_message.Message):
    __slots__ = ()
    STARTED_AT_FIELD_NUMBER: _ClassVar[int]
    RECOMMENDED_DRAIN_AT_FIELD_NUMBER: _ClassVar[int]
    EXPIRES_AT_FIELD_NUMBER: _ClassVar[int]
    started_at: _timestamp_pb2.Timestamp
    recommended_drain_at: _timestamp_pb2.Timestamp
    expires_at: _timestamp_pb2.Timestamp
    def __init__(self, started_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., recommended_drain_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., expires_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class PluginRuntimeImagePullAuth(_message.Message):
    __slots__ = ()
    DOCKER_CONFIG_JSON_FIELD_NUMBER: _ClassVar[int]
    docker_config_json: str
    def __init__(self, docker_config_json: _Optional[str] = ...) -> None: ...

class StartPluginRuntimeSessionRequest(_message.Message):
    __slots__ = ()
    class MetadataEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    PLUGIN_NAME_FIELD_NUMBER: _ClassVar[int]
    TEMPLATE_FIELD_NUMBER: _ClassVar[int]
    IMAGE_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    IMAGE_PULL_AUTH_FIELD_NUMBER: _ClassVar[int]
    plugin_name: str
    template: str
    image: str
    metadata: _containers.ScalarMap[str, str]
    image_pull_auth: PluginRuntimeImagePullAuth
    def __init__(self, plugin_name: _Optional[str] = ..., template: _Optional[str] = ..., image: _Optional[str] = ..., metadata: _Optional[_Mapping[str, str]] = ..., image_pull_auth: _Optional[_Union[PluginRuntimeImagePullAuth, _Mapping]] = ...) -> None: ...

class GetPluginRuntimeSessionRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    def __init__(self, session_id: _Optional[str] = ...) -> None: ...

class ListPluginRuntimeSessionsRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ListPluginRuntimeSessionsResponse(_message.Message):
    __slots__ = ()
    SESSIONS_FIELD_NUMBER: _ClassVar[int]
    sessions: _containers.RepeatedCompositeFieldContainer[PluginRuntimeSession]
    def __init__(self, sessions: _Optional[_Iterable[_Union[PluginRuntimeSession, _Mapping]]] = ...) -> None: ...

class StopPluginRuntimeSessionRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    def __init__(self, session_id: _Optional[str] = ...) -> None: ...

class PreparePluginRuntimeWorkspaceRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    AGENT_SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    WORKSPACE_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    agent_session_id: str
    workspace: _agent_pb2.AgentWorkspace
    def __init__(self, session_id: _Optional[str] = ..., agent_session_id: _Optional[str] = ..., workspace: _Optional[_Union[_agent_pb2.AgentWorkspace, _Mapping]] = ...) -> None: ...

class PreparePluginRuntimeWorkspaceResponse(_message.Message):
    __slots__ = ()
    WORKSPACE_FIELD_NUMBER: _ClassVar[int]
    workspace: _agent_pb2.PreparedAgentWorkspace
    def __init__(self, workspace: _Optional[_Union[_agent_pb2.PreparedAgentWorkspace, _Mapping]] = ...) -> None: ...

class RemovePluginRuntimeWorkspaceRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    AGENT_SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    agent_session_id: str
    def __init__(self, session_id: _Optional[str] = ..., agent_session_id: _Optional[str] = ...) -> None: ...

class StartHostedPluginRequest(_message.Message):
    __slots__ = ()
    class EnvEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    PLUGIN_NAME_FIELD_NUMBER: _ClassVar[int]
    COMMAND_FIELD_NUMBER: _ClassVar[int]
    ARGS_FIELD_NUMBER: _ClassVar[int]
    ENV_FIELD_NUMBER: _ClassVar[int]
    ALLOWED_HOSTS_FIELD_NUMBER: _ClassVar[int]
    DEFAULT_ACTION_FIELD_NUMBER: _ClassVar[int]
    HOST_BINARY_FIELD_NUMBER: _ClassVar[int]
    WORKDIR_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    plugin_name: str
    command: str
    args: _containers.RepeatedScalarFieldContainer[str]
    env: _containers.ScalarMap[str, str]
    allowed_hosts: _containers.RepeatedScalarFieldContainer[str]
    default_action: str
    host_binary: str
    workdir: str
    def __init__(self, session_id: _Optional[str] = ..., plugin_name: _Optional[str] = ..., command: _Optional[str] = ..., args: _Optional[_Iterable[str]] = ..., env: _Optional[_Mapping[str, str]] = ..., allowed_hosts: _Optional[_Iterable[str]] = ..., default_action: _Optional[str] = ..., host_binary: _Optional[str] = ..., workdir: _Optional[str] = ...) -> None: ...

class HostedPlugin(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    PLUGIN_NAME_FIELD_NUMBER: _ClassVar[int]
    DIAL_TARGET_FIELD_NUMBER: _ClassVar[int]
    id: str
    session_id: str
    plugin_name: str
    dial_target: str
    def __init__(self, id: _Optional[str] = ..., session_id: _Optional[str] = ..., plugin_name: _Optional[str] = ..., dial_target: _Optional[str] = ...) -> None: ...

class PluginRuntimeLogEntry(_message.Message):
    __slots__ = ()
    STREAM_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    OBSERVED_AT_FIELD_NUMBER: _ClassVar[int]
    SOURCE_SEQ_FIELD_NUMBER: _ClassVar[int]
    stream: PluginRuntimeLogStream
    message: str
    observed_at: _timestamp_pb2.Timestamp
    source_seq: int
    def __init__(self, stream: _Optional[_Union[PluginRuntimeLogStream, str]] = ..., message: _Optional[str] = ..., observed_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., source_seq: _Optional[int] = ...) -> None: ...

class AppendPluginRuntimeLogsRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    LOGS_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    logs: _containers.RepeatedCompositeFieldContainer[PluginRuntimeLogEntry]
    def __init__(self, session_id: _Optional[str] = ..., logs: _Optional[_Iterable[_Union[PluginRuntimeLogEntry, _Mapping]]] = ...) -> None: ...

class AppendPluginRuntimeLogsResponse(_message.Message):
    __slots__ = ()
    LAST_SEQ_FIELD_NUMBER: _ClassVar[int]
    last_seq: int
    def __init__(self, last_seq: _Optional[int] = ...) -> None: ...
