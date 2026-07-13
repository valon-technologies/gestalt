from google.protobuf import descriptor_pb2 as _descriptor_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class ProviderKind(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    PROVIDER_KIND_UNSPECIFIED: _ClassVar[ProviderKind]
    PROVIDER_KIND_APP: _ClassVar[ProviderKind]
    PROVIDER_KIND_IDENTITY: _ClassVar[ProviderKind]
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

class ProviderInput(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    PROVIDER_INPUT_FULL_REQUEST: _ClassVar[ProviderInput]
    PROVIDER_INPUT_CLIENT_SIGNATURE: _ClassVar[ProviderInput]
PROVIDER_KIND_UNSPECIFIED: ProviderKind
PROVIDER_KIND_APP: ProviderKind
PROVIDER_KIND_IDENTITY: ProviderKind
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
PROVIDER_INPUT_FULL_REQUEST: ProviderInput
PROVIDER_INPUT_CLIENT_SIGNATURE: ProviderInput
SIGNATURE_FIELD_NUMBER: _ClassVar[int]
signature: _descriptor.FieldDescriptor
INITIAL_FIELD_NUMBER: _ClassVar[int]
initial: _descriptor.FieldDescriptor
JSON_RESULT_FIELD_NUMBER: _ClassVar[int]
json_result: _descriptor.FieldDescriptor
OPTIONAL_SIGNATURE_FIELD_NUMBER: _ClassVar[int]
optional_signature: _descriptor.FieldDescriptor
PROVIDER_INPUT_FIELD_NUMBER: _ClassVar[int]
provider_input: _descriptor.FieldDescriptor
PUBLIC_FIELD_NUMBER: _ClassVar[int]
public: _descriptor.FieldDescriptor
OPTIONAL_RESULT_FIELD_NUMBER: _ClassVar[int]
optional_result: _descriptor.FieldDescriptor
KEYED_FIELD_NUMBER: _ClassVar[int]
keyed: _descriptor.FieldDescriptor
UNWRAP_FIELD_NUMBER: _ClassVar[int]
unwrap: _descriptor.FieldDescriptor
HOST_BINDING_FIELD_NUMBER: _ClassVar[int]
host_binding: _descriptor.FieldDescriptor
PROVIDER_KIND_FIELD_NUMBER: _ClassVar[int]
provider_kind: _descriptor.FieldDescriptor

class OptionalResult(_message.Message):
    __slots__ = ()
    GUARD_FIELD_NUMBER: _ClassVar[int]
    VALUE_FIELD_NUMBER: _ClassVar[int]
    guard: str
    value: str
    def __init__(self, guard: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...

class Keyed(_message.Message):
    __slots__ = ()
    ENTRIES_FIELD_NUMBER: _ClassVar[int]
    KEY_FIELD_NUMBER: _ClassVar[int]
    PRESENT_FIELD_NUMBER: _ClassVar[int]
    VALUE_FIELD_NUMBER: _ClassVar[int]
    entries: str
    key: str
    present: str
    value: str
    def __init__(self, entries: _Optional[str] = ..., key: _Optional[str] = ..., present: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...

class Initial(_message.Message):
    __slots__ = ()
    HEADER_FIELD_NUMBER: _ClassVar[int]
    CHUNK_FIELD_NUMBER: _ClassVar[int]
    header: str
    chunk: str
    def __init__(self, header: _Optional[str] = ..., chunk: _Optional[str] = ...) -> None: ...

class JsonResult(_message.Message):
    __slots__ = ()
    STATUS_FIELD_NUMBER: _ClassVar[int]
    BODY_FIELD_NUMBER: _ClassVar[int]
    status: str
    body: str
    def __init__(self, status: _Optional[str] = ..., body: _Optional[str] = ...) -> None: ...

class PublicPolicy(_message.Message):
    __slots__ = ()
    FILL_FIELD_NUMBER: _ClassVar[int]
    REJECT_FIELD_NUMBER: _ClassVar[int]
    fill: _containers.RepeatedScalarFieldContainer[str]
    reject: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, fill: _Optional[_Iterable[str]] = ..., reject: _Optional[_Iterable[str]] = ...) -> None: ...
