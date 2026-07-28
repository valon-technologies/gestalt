import datetime

from google.api import annotations_pb2 as _annotations_pb2
from google.api import visibility_pb2 as _visibility_pb2
from google.protobuf import duration_pb2 as _duration_pb2
from google.protobuf import empty_pb2 as _empty_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf import struct_pb2 as _struct_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class TunnelEndpoint(_message.Message):
    __slots__ = ()
    HOST_FIELD_NUMBER: _ClassVar[int]
    CERTIFICATE_FIELD_NUMBER: _ClassVar[int]
    SERVER_SPKI_SHA256_FIELD_NUMBER: _ClassVar[int]
    host: str
    certificate: bytes
    server_spki_sha256: str
    def __init__(self, host: _Optional[str] = ..., certificate: _Optional[bytes] = ..., server_spki_sha256: _Optional[str] = ...) -> None: ...

class ProviderRef(_message.Message):
    __slots__ = ()
    KIND_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    kind: str
    name: str
    def __init__(self, kind: _Optional[str] = ..., name: _Optional[str] = ...) -> None: ...

class RemoteProviderDefinition(_message.Message):
    __slots__ = ()
    KIND_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    DEFINITION_FIELD_NUMBER: _ClassVar[int]
    kind: str
    name: str
    definition: _struct_pb2.Struct
    def __init__(self, kind: _Optional[str] = ..., name: _Optional[str] = ..., definition: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ...) -> None: ...

class RemoteProviderSummary(_message.Message):
    __slots__ = ()
    KIND_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    kind: str
    name: str
    def __init__(self, kind: _Optional[str] = ..., name: _Optional[str] = ...) -> None: ...

class CreateRemoteRequest(_message.Message):
    __slots__ = ()
    TUNNEL_FIELD_NUMBER: _ClassVar[int]
    PROVIDERS_FIELD_NUMBER: _ClassVar[int]
    EXPECTED_GENERATION_FIELD_NUMBER: _ClassVar[int]
    tunnel: TunnelEndpoint
    providers: _containers.RepeatedCompositeFieldContainer[RemoteProviderDefinition]
    expected_generation: int
    def __init__(self, tunnel: _Optional[_Union[TunnelEndpoint, _Mapping]] = ..., providers: _Optional[_Iterable[_Union[RemoteProviderDefinition, _Mapping]]] = ..., expected_generation: _Optional[int] = ...) -> None: ...

class DeleteRemoteRequest(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    EXPECTED_GENERATION_FIELD_NUMBER: _ClassVar[int]
    id: str
    expected_generation: int
    def __init__(self, id: _Optional[str] = ..., expected_generation: _Optional[int] = ...) -> None: ...

class ListRemotesRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class RemoteReachability(_message.Message):
    __slots__ = ()
    REACHABLE_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    reachable: bool
    message: str
    def __init__(self, reachable: _Optional[bool] = ..., message: _Optional[str] = ...) -> None: ...

class Remote(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    OWNER_SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    GENERATION_FIELD_NUMBER: _ClassVar[int]
    PROVIDERS_FIELD_NUMBER: _ClassVar[int]
    REACHABILITY_FIELD_NUMBER: _ClassVar[int]
    SERVER_SPKI_SHA256_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    LAST_CHECKED_AT_FIELD_NUMBER: _ClassVar[int]
    LAST_SUCCESSFUL_HEARTBEAT_AT_FIELD_NUMBER: _ClassVar[int]
    LAST_ERROR_FIELD_NUMBER: _ClassVar[int]
    LEASE_EXPIRES_AT_FIELD_NUMBER: _ClassVar[int]
    CONNECT_URL_FIELD_NUMBER: _ClassVar[int]
    id: str
    owner_subject_id: str
    generation: int
    providers: _containers.RepeatedCompositeFieldContainer[RemoteProviderSummary]
    reachability: RemoteReachability
    server_spki_sha256: str
    created_at: _timestamp_pb2.Timestamp
    updated_at: _timestamp_pb2.Timestamp
    last_checked_at: _timestamp_pb2.Timestamp
    last_successful_heartbeat_at: _timestamp_pb2.Timestamp
    last_error: str
    lease_expires_at: _timestamp_pb2.Timestamp
    connect_url: str
    def __init__(self, id: _Optional[str] = ..., owner_subject_id: _Optional[str] = ..., generation: _Optional[int] = ..., providers: _Optional[_Iterable[_Union[RemoteProviderSummary, _Mapping]]] = ..., reachability: _Optional[_Union[RemoteReachability, _Mapping]] = ..., server_spki_sha256: _Optional[str] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., last_checked_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., last_successful_heartbeat_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., last_error: _Optional[str] = ..., lease_expires_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., connect_url: _Optional[str] = ...) -> None: ...

class ServerIdentity(_message.Message):
    __slots__ = ()
    CLIENT_SPKI_SHA256_FIELD_NUMBER: _ClassVar[int]
    client_spki_sha256: str
    def __init__(self, client_spki_sha256: _Optional[str] = ...) -> None: ...

class TunnelBootstrap(_message.Message):
    __slots__ = ()
    FRPS_ADDRESS_FIELD_NUMBER: _ClassVar[int]
    LEASE_DURATION_FIELD_NUMBER: _ClassVar[int]
    frps_address: str
    lease_duration: _duration_pb2.Duration
    def __init__(self, frps_address: _Optional[str] = ..., lease_duration: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ...) -> None: ...

class ListRemotesResponse(_message.Message):
    __slots__ = ()
    REMOTES_FIELD_NUMBER: _ClassVar[int]
    SERVER_IDENTITY_FIELD_NUMBER: _ClassVar[int]
    TUNNEL_FIELD_NUMBER: _ClassVar[int]
    LEASE_DURATION_FIELD_NUMBER: _ClassVar[int]
    remotes: _containers.RepeatedCompositeFieldContainer[Remote]
    server_identity: ServerIdentity
    tunnel: TunnelBootstrap
    lease_duration: _duration_pb2.Duration
    def __init__(self, remotes: _Optional[_Iterable[_Union[Remote, _Mapping]]] = ..., server_identity: _Optional[_Union[ServerIdentity, _Mapping]] = ..., tunnel: _Optional[_Union[TunnelBootstrap, _Mapping]] = ..., lease_duration: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ...) -> None: ...

class RegistrationCheckRequest(_message.Message):
    __slots__ = ()
    REGISTRATION_ID_FIELD_NUMBER: _ClassVar[int]
    GENERATION_FIELD_NUMBER: _ClassVar[int]
    PROVIDERS_FIELD_NUMBER: _ClassVar[int]
    registration_id: str
    generation: int
    providers: _containers.RepeatedCompositeFieldContainer[ProviderRef]
    def __init__(self, registration_id: _Optional[str] = ..., generation: _Optional[int] = ..., providers: _Optional[_Iterable[_Union[ProviderRef, _Mapping]]] = ...) -> None: ...

class RegistrationCheckResponse(_message.Message):
    __slots__ = ()
    READY_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    ready: bool
    message: str
    def __init__(self, ready: _Optional[bool] = ..., message: _Optional[str] = ...) -> None: ...
