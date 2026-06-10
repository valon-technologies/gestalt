from google.protobuf import descriptor_pb2 as _descriptor_pb2
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor
SIGNATURE_FIELD_NUMBER: _ClassVar[int]
signature: _descriptor.FieldDescriptor
INITIAL_FIELD_NUMBER: _ClassVar[int]
initial: _descriptor.FieldDescriptor
JSON_RESULT_FIELD_NUMBER: _ClassVar[int]
json_result: _descriptor.FieldDescriptor
OPTIONAL_SIGNATURE_FIELD_NUMBER: _ClassVar[int]
optional_signature: _descriptor.FieldDescriptor
OPTIONAL_RESULT_FIELD_NUMBER: _ClassVar[int]
optional_result: _descriptor.FieldDescriptor
KEYED_FIELD_NUMBER: _ClassVar[int]
keyed: _descriptor.FieldDescriptor
UNWRAP_FIELD_NUMBER: _ClassVar[int]
unwrap: _descriptor.FieldDescriptor
HOST_BINDING_FIELD_NUMBER: _ClassVar[int]
host_binding: _descriptor.FieldDescriptor

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
