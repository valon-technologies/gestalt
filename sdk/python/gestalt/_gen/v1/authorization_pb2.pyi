import datetime

from google.protobuf import empty_pb2 as _empty_pb2
from google.protobuf import struct_pb2 as _struct_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class Subject(_message.Message):
    __slots__ = ()
    TYPE_FIELD_NUMBER: _ClassVar[int]
    ID_FIELD_NUMBER: _ClassVar[int]
    PROPERTIES_FIELD_NUMBER: _ClassVar[int]
    type: str
    id: str
    properties: _struct_pb2.Struct
    def __init__(self, type: _Optional[str] = ..., id: _Optional[str] = ..., properties: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ...) -> None: ...

class Resource(_message.Message):
    __slots__ = ()
    TYPE_FIELD_NUMBER: _ClassVar[int]
    ID_FIELD_NUMBER: _ClassVar[int]
    PROPERTIES_FIELD_NUMBER: _ClassVar[int]
    type: str
    id: str
    properties: _struct_pb2.Struct
    def __init__(self, type: _Optional[str] = ..., id: _Optional[str] = ..., properties: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ...) -> None: ...

class Action(_message.Message):
    __slots__ = ()
    NAME_FIELD_NUMBER: _ClassVar[int]
    PROPERTIES_FIELD_NUMBER: _ClassVar[int]
    name: str
    properties: _struct_pb2.Struct
    def __init__(self, name: _Optional[str] = ..., properties: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ...) -> None: ...

class AccessEvaluationRequest(_message.Message):
    __slots__ = ()
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    ACTION_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    subject: Subject
    action: Action
    resource: Resource
    context: _struct_pb2.Struct
    def __init__(self, subject: _Optional[_Union[Subject, _Mapping]] = ..., action: _Optional[_Union[Action, _Mapping]] = ..., resource: _Optional[_Union[Resource, _Mapping]] = ..., context: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ...) -> None: ...

class AccessDecision(_message.Message):
    __slots__ = ()
    ALLOWED_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    MODEL_ID_FIELD_NUMBER: _ClassVar[int]
    allowed: bool
    context: _struct_pb2.Struct
    model_id: str
    def __init__(self, allowed: _Optional[bool] = ..., context: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., model_id: _Optional[str] = ...) -> None: ...

class AccessEvaluationsRequest(_message.Message):
    __slots__ = ()
    REQUESTS_FIELD_NUMBER: _ClassVar[int]
    requests: _containers.RepeatedCompositeFieldContainer[AccessEvaluationRequest]
    def __init__(self, requests: _Optional[_Iterable[_Union[AccessEvaluationRequest, _Mapping]]] = ...) -> None: ...

class AccessEvaluationsResponse(_message.Message):
    __slots__ = ()
    DECISIONS_FIELD_NUMBER: _ClassVar[int]
    decisions: _containers.RepeatedCompositeFieldContainer[AccessDecision]
    def __init__(self, decisions: _Optional[_Iterable[_Union[AccessDecision, _Mapping]]] = ...) -> None: ...

class ResourceSearchRequest(_message.Message):
    __slots__ = ()
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    ACTION_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_TYPE_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    subject: Subject
    action: Action
    resource_type: str
    context: _struct_pb2.Struct
    page_size: int
    page_token: str
    def __init__(self, subject: _Optional[_Union[Subject, _Mapping]] = ..., action: _Optional[_Union[Action, _Mapping]] = ..., resource_type: _Optional[str] = ..., context: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., page_size: _Optional[int] = ..., page_token: _Optional[str] = ...) -> None: ...

class ResourceSearchResponse(_message.Message):
    __slots__ = ()
    RESOURCES_FIELD_NUMBER: _ClassVar[int]
    NEXT_PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    MODEL_ID_FIELD_NUMBER: _ClassVar[int]
    resources: _containers.RepeatedCompositeFieldContainer[Resource]
    next_page_token: str
    model_id: str
    def __init__(self, resources: _Optional[_Iterable[_Union[Resource, _Mapping]]] = ..., next_page_token: _Optional[str] = ..., model_id: _Optional[str] = ...) -> None: ...

class SubjectSearchRequest(_message.Message):
    __slots__ = ()
    RESOURCE_FIELD_NUMBER: _ClassVar[int]
    ACTION_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_TYPE_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    resource: Resource
    action: Action
    subject_type: str
    context: _struct_pb2.Struct
    page_size: int
    page_token: str
    def __init__(self, resource: _Optional[_Union[Resource, _Mapping]] = ..., action: _Optional[_Union[Action, _Mapping]] = ..., subject_type: _Optional[str] = ..., context: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., page_size: _Optional[int] = ..., page_token: _Optional[str] = ...) -> None: ...

class SubjectSearchResponse(_message.Message):
    __slots__ = ()
    SUBJECTS_FIELD_NUMBER: _ClassVar[int]
    NEXT_PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    MODEL_ID_FIELD_NUMBER: _ClassVar[int]
    subjects: _containers.RepeatedCompositeFieldContainer[Subject]
    next_page_token: str
    model_id: str
    def __init__(self, subjects: _Optional[_Iterable[_Union[Subject, _Mapping]]] = ..., next_page_token: _Optional[str] = ..., model_id: _Optional[str] = ...) -> None: ...

class ActionSearchRequest(_message.Message):
    __slots__ = ()
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    subject: Subject
    resource: Resource
    context: _struct_pb2.Struct
    page_size: int
    page_token: str
    def __init__(self, subject: _Optional[_Union[Subject, _Mapping]] = ..., resource: _Optional[_Union[Resource, _Mapping]] = ..., context: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., page_size: _Optional[int] = ..., page_token: _Optional[str] = ...) -> None: ...

class ActionSearchResponse(_message.Message):
    __slots__ = ()
    ACTIONS_FIELD_NUMBER: _ClassVar[int]
    NEXT_PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    MODEL_ID_FIELD_NUMBER: _ClassVar[int]
    actions: _containers.RepeatedCompositeFieldContainer[Action]
    next_page_token: str
    model_id: str
    def __init__(self, actions: _Optional[_Iterable[_Union[Action, _Mapping]]] = ..., next_page_token: _Optional[str] = ..., model_id: _Optional[str] = ...) -> None: ...

class AuthorizationMetadata(_message.Message):
    __slots__ = ()
    CAPABILITIES_FIELD_NUMBER: _ClassVar[int]
    ACTIVE_MODEL_ID_FIELD_NUMBER: _ClassVar[int]
    capabilities: _containers.RepeatedScalarFieldContainer[str]
    active_model_id: str
    def __init__(self, capabilities: _Optional[_Iterable[str]] = ..., active_model_id: _Optional[str] = ...) -> None: ...

class Relationship(_message.Message):
    __slots__ = ()
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    RELATION_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_FIELD_NUMBER: _ClassVar[int]
    PROPERTIES_FIELD_NUMBER: _ClassVar[int]
    subject: Subject
    relation: str
    resource: Resource
    properties: _struct_pb2.Struct
    def __init__(self, subject: _Optional[_Union[Subject, _Mapping]] = ..., relation: _Optional[str] = ..., resource: _Optional[_Union[Resource, _Mapping]] = ..., properties: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ...) -> None: ...

class RelationshipKey(_message.Message):
    __slots__ = ()
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    RELATION_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_FIELD_NUMBER: _ClassVar[int]
    subject: Subject
    relation: str
    resource: Resource
    def __init__(self, subject: _Optional[_Union[Subject, _Mapping]] = ..., relation: _Optional[str] = ..., resource: _Optional[_Union[Resource, _Mapping]] = ...) -> None: ...

class ReadRelationshipsRequest(_message.Message):
    __slots__ = ()
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    RELATION_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_FIELD_NUMBER: _ClassVar[int]
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    MODEL_ID_FIELD_NUMBER: _ClassVar[int]
    subject: Subject
    relation: str
    resource: Resource
    page_size: int
    page_token: str
    model_id: str
    def __init__(self, subject: _Optional[_Union[Subject, _Mapping]] = ..., relation: _Optional[str] = ..., resource: _Optional[_Union[Resource, _Mapping]] = ..., page_size: _Optional[int] = ..., page_token: _Optional[str] = ..., model_id: _Optional[str] = ...) -> None: ...

class ReadRelationshipsResponse(_message.Message):
    __slots__ = ()
    RELATIONSHIPS_FIELD_NUMBER: _ClassVar[int]
    NEXT_PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    MODEL_ID_FIELD_NUMBER: _ClassVar[int]
    relationships: _containers.RepeatedCompositeFieldContainer[Relationship]
    next_page_token: str
    model_id: str
    def __init__(self, relationships: _Optional[_Iterable[_Union[Relationship, _Mapping]]] = ..., next_page_token: _Optional[str] = ..., model_id: _Optional[str] = ...) -> None: ...

class WriteRelationshipsRequest(_message.Message):
    __slots__ = ()
    WRITES_FIELD_NUMBER: _ClassVar[int]
    DELETES_FIELD_NUMBER: _ClassVar[int]
    MODEL_ID_FIELD_NUMBER: _ClassVar[int]
    writes: _containers.RepeatedCompositeFieldContainer[Relationship]
    deletes: _containers.RepeatedCompositeFieldContainer[RelationshipKey]
    model_id: str
    def __init__(self, writes: _Optional[_Iterable[_Union[Relationship, _Mapping]]] = ..., deletes: _Optional[_Iterable[_Union[RelationshipKey, _Mapping]]] = ..., model_id: _Optional[str] = ...) -> None: ...

class AuthorizationModel(_message.Message):
    __slots__ = ()
    VERSION_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_TYPES_FIELD_NUMBER: _ClassVar[int]
    version: int
    resource_types: _containers.RepeatedCompositeFieldContainer[AuthorizationModelResourceType]
    def __init__(self, version: _Optional[int] = ..., resource_types: _Optional[_Iterable[_Union[AuthorizationModelResourceType, _Mapping]]] = ...) -> None: ...

class AuthorizationModelResourceType(_message.Message):
    __slots__ = ()
    NAME_FIELD_NUMBER: _ClassVar[int]
    RELATIONS_FIELD_NUMBER: _ClassVar[int]
    ACTIONS_FIELD_NUMBER: _ClassVar[int]
    name: str
    relations: _containers.RepeatedCompositeFieldContainer[AuthorizationModelRelation]
    actions: _containers.RepeatedCompositeFieldContainer[AuthorizationModelAction]
    def __init__(self, name: _Optional[str] = ..., relations: _Optional[_Iterable[_Union[AuthorizationModelRelation, _Mapping]]] = ..., actions: _Optional[_Iterable[_Union[AuthorizationModelAction, _Mapping]]] = ...) -> None: ...

class AuthorizationModelRelation(_message.Message):
    __slots__ = ()
    NAME_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_TYPES_FIELD_NUMBER: _ClassVar[int]
    name: str
    subject_types: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, name: _Optional[str] = ..., subject_types: _Optional[_Iterable[str]] = ...) -> None: ...

class AuthorizationModelAction(_message.Message):
    __slots__ = ()
    NAME_FIELD_NUMBER: _ClassVar[int]
    RELATIONS_FIELD_NUMBER: _ClassVar[int]
    name: str
    relations: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, name: _Optional[str] = ..., relations: _Optional[_Iterable[str]] = ...) -> None: ...

class AuthorizationModelRef(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    id: str
    version: str
    created_at: _timestamp_pb2.Timestamp
    def __init__(self, id: _Optional[str] = ..., version: _Optional[str] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class GetActiveModelResponse(_message.Message):
    __slots__ = ()
    MODEL_FIELD_NUMBER: _ClassVar[int]
    model: AuthorizationModelRef
    def __init__(self, model: _Optional[_Union[AuthorizationModelRef, _Mapping]] = ...) -> None: ...

class ListModelsRequest(_message.Message):
    __slots__ = ()
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    page_size: int
    page_token: str
    def __init__(self, page_size: _Optional[int] = ..., page_token: _Optional[str] = ...) -> None: ...

class ListModelsResponse(_message.Message):
    __slots__ = ()
    MODELS_FIELD_NUMBER: _ClassVar[int]
    NEXT_PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    models: _containers.RepeatedCompositeFieldContainer[AuthorizationModelRef]
    next_page_token: str
    def __init__(self, models: _Optional[_Iterable[_Union[AuthorizationModelRef, _Mapping]]] = ..., next_page_token: _Optional[str] = ...) -> None: ...

class WriteModelRequest(_message.Message):
    __slots__ = ()
    MODEL_FIELD_NUMBER: _ClassVar[int]
    model: AuthorizationModel
    def __init__(self, model: _Optional[_Union[AuthorizationModel, _Mapping]] = ...) -> None: ...
