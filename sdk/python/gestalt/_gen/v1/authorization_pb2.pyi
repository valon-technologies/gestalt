import datetime

from google.protobuf import empty_pb2 as _empty_pb2
from google.protobuf import struct_pb2 as _struct_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class RelationshipTargetType(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    RELATIONSHIP_TARGET_TYPE_UNSPECIFIED: _ClassVar[RelationshipTargetType]
    RELATIONSHIP_TARGET_TYPE_SUBJECT: _ClassVar[RelationshipTargetType]
    RELATIONSHIP_TARGET_TYPE_RESOURCE: _ClassVar[RelationshipTargetType]
    RELATIONSHIP_TARGET_TYPE_SUBJECT_SET: _ClassVar[RelationshipTargetType]

class DefaultAccessPolicy(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    DEFAULT_ACCESS_POLICY_DENY: _ClassVar[DefaultAccessPolicy]
    DEFAULT_ACCESS_POLICY_ALLOW: _ClassVar[DefaultAccessPolicy]

class SourceLayer(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    SOURCE_LAYER_UNSPECIFIED: _ClassVar[SourceLayer]
    SOURCE_LAYER_STATIC_CONFIG: _ClassVar[SourceLayer]
    SOURCE_LAYER_RUNTIME: _ClassVar[SourceLayer]
RELATIONSHIP_TARGET_TYPE_UNSPECIFIED: RelationshipTargetType
RELATIONSHIP_TARGET_TYPE_SUBJECT: RelationshipTargetType
RELATIONSHIP_TARGET_TYPE_RESOURCE: RelationshipTargetType
RELATIONSHIP_TARGET_TYPE_SUBJECT_SET: RelationshipTargetType
DEFAULT_ACCESS_POLICY_DENY: DefaultAccessPolicy
DEFAULT_ACCESS_POLICY_ALLOW: DefaultAccessPolicy
SOURCE_LAYER_UNSPECIFIED: SourceLayer
SOURCE_LAYER_STATIC_CONFIG: SourceLayer
SOURCE_LAYER_RUNTIME: SourceLayer

class Subject(_message.Message):
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

class Resource(_message.Message):
    __slots__ = ()
    TYPE_FIELD_NUMBER: _ClassVar[int]
    ID_FIELD_NUMBER: _ClassVar[int]
    PROPERTIES_FIELD_NUMBER: _ClassVar[int]
    type: str
    id: str
    properties: _struct_pb2.Struct
    def __init__(self, type: _Optional[str] = ..., id: _Optional[str] = ..., properties: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ...) -> None: ...

class CheckAccessRequest(_message.Message):
    __slots__ = ()
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    ACTION_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_FIELD_NUMBER: _ClassVar[int]
    subject: Subject
    action: Action
    resource: Resource
    def __init__(self, subject: _Optional[_Union[Subject, _Mapping]] = ..., action: _Optional[_Union[Action, _Mapping]] = ..., resource: _Optional[_Union[Resource, _Mapping]] = ...) -> None: ...

class CheckAccessResponse(_message.Message):
    __slots__ = ()
    ALLOWED_FIELD_NUMBER: _ClassVar[int]
    MODEL_ID_FIELD_NUMBER: _ClassVar[int]
    allowed: bool
    model_id: str
    def __init__(self, allowed: _Optional[bool] = ..., model_id: _Optional[str] = ...) -> None: ...

class CheckAccessManyRequest(_message.Message):
    __slots__ = ()
    REQUESTS_FIELD_NUMBER: _ClassVar[int]
    requests: _containers.RepeatedCompositeFieldContainer[CheckAccessRequest]
    def __init__(self, requests: _Optional[_Iterable[_Union[CheckAccessRequest, _Mapping]]] = ...) -> None: ...

class CheckAccessManyResponse(_message.Message):
    __slots__ = ()
    DECISIONS_FIELD_NUMBER: _ClassVar[int]
    decisions: _containers.RepeatedCompositeFieldContainer[CheckAccessResponse]
    def __init__(self, decisions: _Optional[_Iterable[_Union[CheckAccessResponse, _Mapping]]] = ...) -> None: ...

class ListRelationshipsRequest(_message.Message):
    __slots__ = ()
    FILTER_FIELD_NUMBER: _ClassVar[int]
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    filter: RelationshipFilter
    page_size: int
    page_token: str
    def __init__(self, filter: _Optional[_Union[RelationshipFilter, _Mapping]] = ..., page_size: _Optional[int] = ..., page_token: _Optional[str] = ...) -> None: ...

class RelationshipFilter(_message.Message):
    __slots__ = ()
    TARGET_FIELD_NUMBER: _ClassVar[int]
    RELATION_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_FIELD_NUMBER: _ClassVar[int]
    TARGET_TYPE_FIELD_NUMBER: _ClassVar[int]
    TARGET_ENTITY_TYPE_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_TYPE_FIELD_NUMBER: _ClassVar[int]
    SOURCE_LAYER_FIELD_NUMBER: _ClassVar[int]
    target: RelationshipTarget
    relation: str
    resource: Resource
    target_type: RelationshipTargetType
    target_entity_type: str
    resource_type: str
    source_layer: SourceLayer
    def __init__(self, target: _Optional[_Union[RelationshipTarget, _Mapping]] = ..., relation: _Optional[str] = ..., resource: _Optional[_Union[Resource, _Mapping]] = ..., target_type: _Optional[_Union[RelationshipTargetType, str]] = ..., target_entity_type: _Optional[str] = ..., resource_type: _Optional[str] = ..., source_layer: _Optional[_Union[SourceLayer, str]] = ...) -> None: ...

class ListRelationshipsResponse(_message.Message):
    __slots__ = ()
    RELATIONSHIPS_FIELD_NUMBER: _ClassVar[int]
    NEXT_PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    relationships: _containers.RepeatedCompositeFieldContainer[Relationship]
    next_page_token: str
    def __init__(self, relationships: _Optional[_Iterable[_Union[Relationship, _Mapping]]] = ..., next_page_token: _Optional[str] = ...) -> None: ...

class AddRelationshipRequest(_message.Message):
    __slots__ = ()
    RELATIONSHIP_FIELD_NUMBER: _ClassVar[int]
    relationship: Relationship
    def __init__(self, relationship: _Optional[_Union[Relationship, _Mapping]] = ...) -> None: ...

class AddRelationshipResponse(_message.Message):
    __slots__ = ()
    RELATIONSHIP_FIELD_NUMBER: _ClassVar[int]
    relationship: Relationship
    def __init__(self, relationship: _Optional[_Union[Relationship, _Mapping]] = ...) -> None: ...

class DeleteRelationshipRequest(_message.Message):
    __slots__ = ()
    RELATIONSHIP_TUPLE_FIELD_NUMBER: _ClassVar[int]
    relationship_tuple: RelationshipTuple
    def __init__(self, relationship_tuple: _Optional[_Union[RelationshipTuple, _Mapping]] = ...) -> None: ...

class DeleteRelationshipResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class SetAuthorizationStateRequest(_message.Message):
    __slots__ = ()
    MODEL_FIELD_NUMBER: _ClassVar[int]
    RELATIONSHIPS_FIELD_NUMBER: _ClassVar[int]
    model: AuthorizationModel
    relationships: _containers.RepeatedCompositeFieldContainer[Relationship]
    def __init__(self, model: _Optional[_Union[AuthorizationModel, _Mapping]] = ..., relationships: _Optional[_Iterable[_Union[Relationship, _Mapping]]] = ...) -> None: ...

class SetAuthorizationStateResponse(_message.Message):
    __slots__ = ()
    ACTIVE_MODEL_FIELD_NUMBER: _ClassVar[int]
    active_model: AuthorizationModelRef
    def __init__(self, active_model: _Optional[_Union[AuthorizationModelRef, _Mapping]] = ...) -> None: ...

class Relationship(_message.Message):
    __slots__ = ()
    TUPLE_FIELD_NUMBER: _ClassVar[int]
    PROPERTIES_FIELD_NUMBER: _ClassVar[int]
    SOURCE_LAYER_FIELD_NUMBER: _ClassVar[int]
    tuple: RelationshipTuple
    properties: _struct_pb2.Struct
    source_layer: SourceLayer
    def __init__(self, tuple: _Optional[_Union[RelationshipTuple, _Mapping]] = ..., properties: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., source_layer: _Optional[_Union[SourceLayer, str]] = ...) -> None: ...

class RelationshipTuple(_message.Message):
    __slots__ = ()
    TARGET_FIELD_NUMBER: _ClassVar[int]
    RELATION_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_FIELD_NUMBER: _ClassVar[int]
    target: RelationshipTarget
    relation: str
    resource: Resource
    def __init__(self, target: _Optional[_Union[RelationshipTarget, _Mapping]] = ..., relation: _Optional[str] = ..., resource: _Optional[_Union[Resource, _Mapping]] = ...) -> None: ...

class RelationshipTarget(_message.Message):
    __slots__ = ()
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_SET_FIELD_NUMBER: _ClassVar[int]
    subject: Subject
    resource: Resource
    subject_set: SubjectSet
    def __init__(self, subject: _Optional[_Union[Subject, _Mapping]] = ..., resource: _Optional[_Union[Resource, _Mapping]] = ..., subject_set: _Optional[_Union[SubjectSet, _Mapping]] = ...) -> None: ...

class SubjectSet(_message.Message):
    __slots__ = ()
    RESOURCE_FIELD_NUMBER: _ClassVar[int]
    RELATION_FIELD_NUMBER: _ClassVar[int]
    resource: Resource
    relation: str
    def __init__(self, resource: _Optional[_Union[Resource, _Mapping]] = ..., relation: _Optional[str] = ...) -> None: ...

class AuthorizationModel(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_TYPES_FIELD_NUMBER: _ClassVar[int]
    id: str
    version: str
    resource_types: _containers.RepeatedCompositeFieldContainer[AuthorizationModelResourceType]
    def __init__(self, id: _Optional[str] = ..., version: _Optional[str] = ..., resource_types: _Optional[_Iterable[_Union[AuthorizationModelResourceType, _Mapping]]] = ...) -> None: ...

class AuthorizationModelResourceType(_message.Message):
    __slots__ = ()
    NAME_FIELD_NUMBER: _ClassVar[int]
    RELATIONS_FIELD_NUMBER: _ClassVar[int]
    ACTIONS_FIELD_NUMBER: _ClassVar[int]
    SOURCE_LAYER_FIELD_NUMBER: _ClassVar[int]
    DEFAULT_ACCESS_POLICY_FIELD_NUMBER: _ClassVar[int]
    name: str
    relations: _containers.RepeatedCompositeFieldContainer[ModelRelation]
    actions: _containers.RepeatedCompositeFieldContainer[ModelAction]
    source_layer: SourceLayer
    default_access_policy: DefaultAccessPolicy
    def __init__(self, name: _Optional[str] = ..., relations: _Optional[_Iterable[_Union[ModelRelation, _Mapping]]] = ..., actions: _Optional[_Iterable[_Union[ModelAction, _Mapping]]] = ..., source_layer: _Optional[_Union[SourceLayer, str]] = ..., default_access_policy: _Optional[_Union[DefaultAccessPolicy, str]] = ...) -> None: ...

class ModelRelation(_message.Message):
    __slots__ = ()
    NAME_FIELD_NUMBER: _ClassVar[int]
    ALLOWED_TARGETS_FIELD_NUMBER: _ClassVar[int]
    name: str
    allowed_targets: _containers.RepeatedCompositeFieldContainer[ModelAllowedTarget]
    def __init__(self, name: _Optional[str] = ..., allowed_targets: _Optional[_Iterable[_Union[ModelAllowedTarget, _Mapping]]] = ...) -> None: ...

class ModelAction(_message.Message):
    __slots__ = ()
    NAME_FIELD_NUMBER: _ClassVar[int]
    RELATIONS_FIELD_NUMBER: _ClassVar[int]
    name: str
    relations: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, name: _Optional[str] = ..., relations: _Optional[_Iterable[str]] = ...) -> None: ...

class ModelAllowedTarget(_message.Message):
    __slots__ = ()
    SUBJECT_TYPE_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_TYPE_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_SET_TYPE_FIELD_NUMBER: _ClassVar[int]
    subject_type: str
    resource_type: str
    subject_set_type: SubjectSetType
    def __init__(self, subject_type: _Optional[str] = ..., resource_type: _Optional[str] = ..., subject_set_type: _Optional[_Union[SubjectSetType, _Mapping]] = ...) -> None: ...

class SubjectSetType(_message.Message):
    __slots__ = ()
    RESOURCE_TYPE_FIELD_NUMBER: _ClassVar[int]
    RELATION_FIELD_NUMBER: _ClassVar[int]
    resource_type: str
    relation: str
    def __init__(self, resource_type: _Optional[str] = ..., relation: _Optional[str] = ...) -> None: ...

class AuthorizationModelRef(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    id: str
    version: str
    created_at: _timestamp_pb2.Timestamp
    def __init__(self, id: _Optional[str] = ..., version: _Optional[str] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class GetActiveModelRefResponse(_message.Message):
    __slots__ = ()
    MODEL_FIELD_NUMBER: _ClassVar[int]
    model: AuthorizationModelRef
    def __init__(self, model: _Optional[_Union[AuthorizationModelRef, _Mapping]] = ...) -> None: ...

class SetActiveModelRequest(_message.Message):
    __slots__ = ()
    MODEL_FIELD_NUMBER: _ClassVar[int]
    model: AuthorizationModel
    def __init__(self, model: _Optional[_Union[AuthorizationModel, _Mapping]] = ...) -> None: ...

class SetActiveModelResponse(_message.Message):
    __slots__ = ()
    MODEL_FIELD_NUMBER: _ClassVar[int]
    model: AuthorizationModelRef
    def __init__(self, model: _Optional[_Union[AuthorizationModelRef, _Mapping]] = ...) -> None: ...

class ListActiveModelResourceTypesRequest(_message.Message):
    __slots__ = ()
    FILTER_FIELD_NUMBER: _ClassVar[int]
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    filter: AuthorizationModelResourceTypeFilter
    page_size: int
    page_token: str
    def __init__(self, filter: _Optional[_Union[AuthorizationModelResourceTypeFilter, _Mapping]] = ..., page_size: _Optional[int] = ..., page_token: _Optional[str] = ...) -> None: ...

class AuthorizationModelResourceTypeFilter(_message.Message):
    __slots__ = ()
    NAME_FIELD_NUMBER: _ClassVar[int]
    SOURCE_LAYER_FIELD_NUMBER: _ClassVar[int]
    name: str
    source_layer: SourceLayer
    def __init__(self, name: _Optional[str] = ..., source_layer: _Optional[_Union[SourceLayer, str]] = ...) -> None: ...

class ListActiveModelResourceTypesResponse(_message.Message):
    __slots__ = ()
    RESOURCE_TYPES_FIELD_NUMBER: _ClassVar[int]
    NEXT_PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    MODEL_ID_FIELD_NUMBER: _ClassVar[int]
    resource_types: _containers.RepeatedCompositeFieldContainer[AuthorizationModelResourceType]
    next_page_token: str
    model_id: str
    def __init__(self, resource_types: _Optional[_Iterable[_Union[AuthorizationModelResourceType, _Mapping]]] = ..., next_page_token: _Optional[str] = ..., model_id: _Optional[str] = ...) -> None: ...
