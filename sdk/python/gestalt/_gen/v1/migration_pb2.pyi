from google.protobuf import struct_pb2 as _struct_pb2
from . import annotations_pb2 as _annotations_pb2
from . import workflow_pb2 as _workflow_pb2
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class ApplyWorkflowMigrationRequest(_message.Message):
    __slots__ = ()
    PROVIDER_FIELD_NUMBER: _ClassVar[int]
    REVISION_ID_FIELD_NUMBER: _ClassVar[int]
    SPEC_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    provider: str
    revision_id: str
    spec: _workflow_pb2.WorkflowDefinitionSpec
    idempotency_key: str
    def __init__(self, provider: _Optional[str] = ..., revision_id: _Optional[str] = ..., spec: _Optional[_Union[_workflow_pb2.WorkflowDefinitionSpec, _Mapping]] = ..., idempotency_key: _Optional[str] = ...) -> None: ...

class ApplyWorkflowMigrationResponse(_message.Message):
    __slots__ = ()
    DEFINITION_FIELD_NUMBER: _ClassVar[int]
    definition: _workflow_pb2.WorkflowDefinition
    def __init__(self, definition: _Optional[_Union[_workflow_pb2.WorkflowDefinition, _Mapping]] = ...) -> None: ...
