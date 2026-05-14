from google.protobuf import struct_pb2 as _struct_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class ModelMessagePartType(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    MODEL_MESSAGE_PART_TYPE_UNSPECIFIED: _ClassVar[ModelMessagePartType]
    MODEL_MESSAGE_PART_TYPE_TEXT: _ClassVar[ModelMessagePartType]
MODEL_MESSAGE_PART_TYPE_UNSPECIFIED: ModelMessagePartType
MODEL_MESSAGE_PART_TYPE_TEXT: ModelMessagePartType

class ModelMessage(_message.Message):
    __slots__ = ()
    ROLE_FIELD_NUMBER: _ClassVar[int]
    TEXT_FIELD_NUMBER: _ClassVar[int]
    PARTS_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    role: str
    text: str
    parts: _containers.RepeatedCompositeFieldContainer[ModelMessagePart]
    metadata: _struct_pb2.Struct
    def __init__(self, role: _Optional[str] = ..., text: _Optional[str] = ..., parts: _Optional[_Iterable[_Union[ModelMessagePart, _Mapping]]] = ..., metadata: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ...) -> None: ...

class ModelMessagePart(_message.Message):
    __slots__ = ()
    TYPE_FIELD_NUMBER: _ClassVar[int]
    TEXT_FIELD_NUMBER: _ClassVar[int]
    type: ModelMessagePartType
    text: str
    def __init__(self, type: _Optional[_Union[ModelMessagePartType, str]] = ..., text: _Optional[str] = ...) -> None: ...

class ModelSubjectContext(_message.Message):
    __slots__ = ()
    SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_KIND_FIELD_NUMBER: _ClassVar[int]
    CREDENTIAL_SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    DISPLAY_NAME_FIELD_NUMBER: _ClassVar[int]
    AUTH_SOURCE_FIELD_NUMBER: _ClassVar[int]
    subject_id: str
    subject_kind: str
    credential_subject_id: str
    display_name: str
    auth_source: str
    def __init__(self, subject_id: _Optional[str] = ..., subject_kind: _Optional[str] = ..., credential_subject_id: _Optional[str] = ..., display_name: _Optional[str] = ..., auth_source: _Optional[str] = ...) -> None: ...

class ModelUsage(_message.Message):
    __slots__ = ()
    INPUT_TOKENS_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_TOKENS_FIELD_NUMBER: _ClassVar[int]
    TOTAL_TOKENS_FIELD_NUMBER: _ClassVar[int]
    input_tokens: int
    output_tokens: int
    total_tokens: int
    def __init__(self, input_tokens: _Optional[int] = ..., output_tokens: _Optional[int] = ..., total_tokens: _Optional[int] = ...) -> None: ...

class ModelProviderCapabilities(_message.Message):
    __slots__ = ()
    TEXT_OUTPUT_FIELD_NUMBER: _ClassVar[int]
    STRUCTURED_OUTPUT_FIELD_NUMBER: _ClassVar[int]
    USAGE_FIELD_NUMBER: _ClassVar[int]
    PARALLEL_REQUESTS_FIELD_NUMBER: _ClassVar[int]
    text_output: bool
    structured_output: bool
    usage: bool
    parallel_requests: bool
    def __init__(self, text_output: _Optional[bool] = ..., structured_output: _Optional[bool] = ..., usage: _Optional[bool] = ..., parallel_requests: _Optional[bool] = ...) -> None: ...

class GetModelProviderCapabilitiesRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class GenerateModelRequest(_message.Message):
    __slots__ = ()
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    MESSAGES_FIELD_NUMBER: _ClassVar[int]
    RESPONSE_SCHEMA_FIELD_NUMBER: _ClassVar[int]
    MODEL_OPTIONS_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    CALLER_PLUGIN_NAME_FIELD_NUMBER: _ClassVar[int]
    provider_name: str
    model: str
    messages: _containers.RepeatedCompositeFieldContainer[ModelMessage]
    response_schema: _struct_pb2.Struct
    model_options: _struct_pb2.Struct
    metadata: _struct_pb2.Struct
    subject: ModelSubjectContext
    caller_plugin_name: str
    def __init__(self, provider_name: _Optional[str] = ..., model: _Optional[str] = ..., messages: _Optional[_Iterable[_Union[ModelMessage, _Mapping]]] = ..., response_schema: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., model_options: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., metadata: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., subject: _Optional[_Union[ModelSubjectContext, _Mapping]] = ..., caller_plugin_name: _Optional[str] = ...) -> None: ...

class GenerateModelResponse(_message.Message):
    __slots__ = ()
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_TEXT_FIELD_NUMBER: _ClassVar[int]
    STRUCTURED_OUTPUT_FIELD_NUMBER: _ClassVar[int]
    FINISH_REASON_FIELD_NUMBER: _ClassVar[int]
    USAGE_FIELD_NUMBER: _ClassVar[int]
    PROVIDER_METADATA_FIELD_NUMBER: _ClassVar[int]
    message: ModelMessage
    output_text: str
    structured_output: _struct_pb2.Struct
    finish_reason: str
    usage: ModelUsage
    provider_metadata: _struct_pb2.Struct
    def __init__(self, message: _Optional[_Union[ModelMessage, _Mapping]] = ..., output_text: _Optional[str] = ..., structured_output: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., finish_reason: _Optional[str] = ..., usage: _Optional[_Union[ModelUsage, _Mapping]] = ..., provider_metadata: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ...) -> None: ...

class ModelManagerGenerateRequest(_message.Message):
    __slots__ = ()
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    MESSAGES_FIELD_NUMBER: _ClassVar[int]
    RESPONSE_SCHEMA_FIELD_NUMBER: _ClassVar[int]
    MODEL_OPTIONS_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    provider_name: str
    model: str
    messages: _containers.RepeatedCompositeFieldContainer[ModelMessage]
    response_schema: _struct_pb2.Struct
    model_options: _struct_pb2.Struct
    metadata: _struct_pb2.Struct
    invocation_token: str
    def __init__(self, provider_name: _Optional[str] = ..., model: _Optional[str] = ..., messages: _Optional[_Iterable[_Union[ModelMessage, _Mapping]]] = ..., response_schema: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., model_options: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., metadata: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., invocation_token: _Optional[str] = ...) -> None: ...
