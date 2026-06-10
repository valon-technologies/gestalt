from google.protobuf import empty_pb2 as _empty_pb2
from google.protobuf import struct_pb2 as _struct_pb2
from . import annotations_pb2 as _annotations_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class ProviderKind(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    PROVIDER_KIND_UNSPECIFIED: _ClassVar[ProviderKind]
    PROVIDER_KIND_APP: _ClassVar[ProviderKind]
    PROVIDER_KIND_AUTHENTICATION: _ClassVar[ProviderKind]
    PROVIDER_KIND_INDEXEDDB: _ClassVar[ProviderKind]
    PROVIDER_KIND_SECRETS: _ClassVar[ProviderKind]
    PROVIDER_KIND_TELEMETRY: _ClassVar[ProviderKind]
    PROVIDER_KIND_CACHE: _ClassVar[ProviderKind]
    PROVIDER_KIND_S3: _ClassVar[ProviderKind]
    PROVIDER_KIND_WORKFLOW: _ClassVar[ProviderKind]
    PROVIDER_KIND_AUTHORIZATION: _ClassVar[ProviderKind]
    PROVIDER_KIND_RUNTIME: _ClassVar[ProviderKind]
    PROVIDER_KIND_AGENT: _ClassVar[ProviderKind]
    PROVIDER_KIND_EXTERNAL_CREDENTIAL: _ClassVar[ProviderKind]
    PROVIDER_KIND_TEST: _ClassVar[ProviderKind]
PROVIDER_KIND_UNSPECIFIED: ProviderKind
PROVIDER_KIND_APP: ProviderKind
PROVIDER_KIND_AUTHENTICATION: ProviderKind
PROVIDER_KIND_INDEXEDDB: ProviderKind
PROVIDER_KIND_SECRETS: ProviderKind
PROVIDER_KIND_TELEMETRY: ProviderKind
PROVIDER_KIND_CACHE: ProviderKind
PROVIDER_KIND_S3: ProviderKind
PROVIDER_KIND_WORKFLOW: ProviderKind
PROVIDER_KIND_AUTHORIZATION: ProviderKind
PROVIDER_KIND_RUNTIME: ProviderKind
PROVIDER_KIND_AGENT: ProviderKind
PROVIDER_KIND_EXTERNAL_CREDENTIAL: ProviderKind
PROVIDER_KIND_TEST: ProviderKind

class ProviderIdentity(_message.Message):
    __slots__ = ()
    KIND_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    DISPLAY_NAME_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    WARNINGS_FIELD_NUMBER: _ClassVar[int]
    MIN_PROTOCOL_VERSION_FIELD_NUMBER: _ClassVar[int]
    MAX_PROTOCOL_VERSION_FIELD_NUMBER: _ClassVar[int]
    kind: ProviderKind
    name: str
    display_name: str
    description: str
    version: str
    warnings: _containers.RepeatedScalarFieldContainer[str]
    min_protocol_version: int
    max_protocol_version: int
    def __init__(self, kind: _Optional[_Union[ProviderKind, str]] = ..., name: _Optional[str] = ..., display_name: _Optional[str] = ..., description: _Optional[str] = ..., version: _Optional[str] = ..., warnings: _Optional[_Iterable[str]] = ..., min_protocol_version: _Optional[int] = ..., max_protocol_version: _Optional[int] = ...) -> None: ...

class ConfigureProviderRequest(_message.Message):
    __slots__ = ()
    NAME_FIELD_NUMBER: _ClassVar[int]
    CONFIG_FIELD_NUMBER: _ClassVar[int]
    PROTOCOL_VERSION_FIELD_NUMBER: _ClassVar[int]
    name: str
    config: _struct_pb2.Struct
    protocol_version: int
    def __init__(self, name: _Optional[str] = ..., config: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., protocol_version: _Optional[int] = ...) -> None: ...

class ConfigureProviderResponse(_message.Message):
    __slots__ = ()
    PROTOCOL_VERSION_FIELD_NUMBER: _ClassVar[int]
    protocol_version: int
    def __init__(self, protocol_version: _Optional[int] = ...) -> None: ...

class HealthCheckResponse(_message.Message):
    __slots__ = ()
    READY_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    ready: bool
    message: str
    def __init__(self, ready: _Optional[bool] = ..., message: _Optional[str] = ...) -> None: ...

class StartRuntimeProviderResponse(_message.Message):
    __slots__ = ()
    PROTOCOL_VERSION_FIELD_NUMBER: _ClassVar[int]
    protocol_version: int
    def __init__(self, protocol_version: _Optional[int] = ...) -> None: ...
