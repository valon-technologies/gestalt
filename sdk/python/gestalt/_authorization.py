from __future__ import annotations

import datetime as dt
from collections.abc import Mapping, Sequence
from dataclasses import dataclass, field
from typing import Any

from ._gen.v1 import authorization_pb2 as _pb
from ._protocol import coerce_model as _coerce
from ._protocol import copy_message as _copy
from ._protocol import datetime_from_timestamp as _datetime_from_timestamp
from ._protocol import has_field as _has_field
from ._protocol import struct_from_dict as _struct_from_dict
from ._protocol import struct_to_dict as _struct_to_dict
from ._protocol import timestamp_from_datetime as _timestamp_from_datetime
from ._protocol import which_oneof as _which_oneof

pb: Any = _pb

RELATIONSHIP_TARGET_TYPE_UNSPECIFIED = _pb.RELATIONSHIP_TARGET_TYPE_UNSPECIFIED
RELATIONSHIP_TARGET_TYPE_SUBJECT = _pb.RELATIONSHIP_TARGET_TYPE_SUBJECT
RELATIONSHIP_TARGET_TYPE_RESOURCE = _pb.RELATIONSHIP_TARGET_TYPE_RESOURCE
RELATIONSHIP_TARGET_TYPE_SUBJECT_SET = _pb.RELATIONSHIP_TARGET_TYPE_SUBJECT_SET

SOURCE_LAYER_UNSPECIFIED = _pb.SOURCE_LAYER_UNSPECIFIED
SOURCE_LAYER_STATIC_CONFIG = _pb.SOURCE_LAYER_STATIC_CONFIG
SOURCE_LAYER_RUNTIME = _pb.SOURCE_LAYER_RUNTIME

DEFAULT_ACCESS_POLICY_DENY = _pb.DEFAULT_ACCESS_POLICY_DENY
DEFAULT_ACCESS_POLICY_ALLOW = _pb.DEFAULT_ACCESS_POLICY_ALLOW


@dataclass(slots=True)
class AuthorizationSubject:
    type: str = ""
    id: str = ""
    properties: Mapping[str, Any] = field(default_factory=dict)


@dataclass(slots=True)
class AuthorizationAction:
    name: str = ""
    properties: Mapping[str, Any] = field(default_factory=dict)


@dataclass(slots=True)
class AuthorizationResource:
    type: str = ""
    id: str = ""
    properties: Mapping[str, Any] = field(default_factory=dict)


@dataclass(slots=True)
class CheckAccessRequest:
    subject: AuthorizationSubject | None = None
    action: AuthorizationAction | None = None
    resource: AuthorizationResource | None = None


@dataclass(slots=True)
class CheckAccessResponse:
    allowed: bool = False
    model_id: str = ""


@dataclass(slots=True)
class CheckAccessManyRequest:
    requests: list[CheckAccessRequest] = field(default_factory=list)


@dataclass(slots=True)
class CheckAccessManyResponse:
    decisions: list[CheckAccessResponse] = field(default_factory=list)


@dataclass(slots=True)
class RelationshipFilter:
    target: "RelationshipTarget | None" = None
    relation: str = ""
    resource: AuthorizationResource | None = None
    target_type: int = RELATIONSHIP_TARGET_TYPE_UNSPECIFIED
    target_entity_type: str = ""
    resource_type: str = ""
    source_layer: int = SOURCE_LAYER_UNSPECIFIED


@dataclass(slots=True)
class ListRelationshipsRequest:
    filter: RelationshipFilter | None = None
    page_size: int = 0
    page_token: str = ""


@dataclass(slots=True)
class ListRelationshipsResponse:
    relationships: list["Relationship"] = field(default_factory=list)
    next_page_token: str = ""


@dataclass(slots=True)
class AddRelationshipRequest:
    relationship: "Relationship | None" = None


@dataclass(slots=True)
class AddRelationshipResponse:
    relationship: "Relationship | None" = None


@dataclass(slots=True)
class DeleteRelationshipRequest:
    relationship_tuple: "RelationshipTuple | None" = None


@dataclass(slots=True)
class DeleteRelationshipResponse:
    pass


@dataclass(slots=True)
class SetAuthorizationStateRequest:
    model: "AuthorizationModel | None" = None
    relationships: list["Relationship"] = field(default_factory=list)


@dataclass(slots=True)
class SetAuthorizationStateResponse:
    active_model: "AuthorizationModelRef | None" = None


@dataclass(slots=True)
class Relationship:
    tuple: "RelationshipTuple | None" = None
    properties: Mapping[str, Any] = field(default_factory=dict)
    source_layer: int = SOURCE_LAYER_UNSPECIFIED


@dataclass(slots=True)
class RelationshipTuple:
    target: "RelationshipTarget | None" = None
    relation: str = ""
    resource: AuthorizationResource | None = None


@dataclass(slots=True)
class RelationshipTarget:
    subject: AuthorizationSubject | None = None
    resource: AuthorizationResource | None = None
    subject_set: "SubjectSet | None" = None


@dataclass(slots=True)
class SubjectSet:
    resource: AuthorizationResource | None = None
    relation: str = ""


@dataclass(slots=True)
class AuthorizationModel:
    id: str = ""
    version: str = ""
    resource_types: list["AuthorizationModelResourceType"] = field(
        default_factory=list
    )


@dataclass(slots=True)
class AuthorizationModelResourceType:
    name: str = ""
    relations: list["ModelRelation"] = field(default_factory=list)
    actions: list["ModelAction"] = field(default_factory=list)
    source_layer: int = SOURCE_LAYER_UNSPECIFIED
    default_access_policy: int = DEFAULT_ACCESS_POLICY_DENY


@dataclass(slots=True)
class ModelRelation:
    name: str = ""
    allowed_targets: list["ModelAllowedTarget"] = field(default_factory=list)


@dataclass(slots=True)
class ModelAction:
    name: str = ""
    relations: list[str] = field(default_factory=list)


@dataclass(slots=True)
class ModelAllowedTarget:
    subject_type: str | None = None
    resource_type: str | None = None
    subject_set_type: "SubjectSetType | None" = None


@dataclass(slots=True)
class SubjectSetType:
    resource_type: str = ""
    relation: str = ""


@dataclass(slots=True)
class AuthorizationModelRef:
    id: str = ""
    version: str = ""
    created_at: dt.datetime | None = None


@dataclass(slots=True)
class GetActiveModelRefResponse:
    model: AuthorizationModelRef | None = None


@dataclass(slots=True)
class SetActiveModelRequest:
    model: AuthorizationModel | None = None


@dataclass(slots=True)
class SetActiveModelResponse:
    model: AuthorizationModelRef | None = None


@dataclass(slots=True)
class AuthorizationModelResourceTypeFilter:
    name: str = ""
    source_layer: int = SOURCE_LAYER_UNSPECIFIED


@dataclass(slots=True)
class ListActiveModelResourceTypesRequest:
    filter: AuthorizationModelResourceTypeFilter | None = None
    page_size: int = 0
    page_token: str = ""


@dataclass(slots=True)
class ListActiveModelResourceTypesResponse:
    resource_types: list[AuthorizationModelResourceType] = field(default_factory=list)
    next_page_token: str = ""
    model_id: str = ""


def check_access_request_from_proto(value: Any) -> CheckAccessRequest:
    return CheckAccessRequest(
        subject=_subject_from_proto(value.subject) if _has_field(value, "subject") else None,
        action=_action_from_proto(value.action) if _has_field(value, "action") else None,
        resource=_resource_from_proto(value.resource) if _has_field(value, "resource") else None,
    )


def check_access_response_to_proto(value: Any) -> Any:
    if isinstance(value, pb.CheckAccessResponse):
        return _copy(value)
    response = _coerce(value, CheckAccessResponse, "CheckAccessResponse")
    return pb.CheckAccessResponse(
        allowed=response.allowed,
        model_id=response.model_id,
    )


def check_access_many_request_from_proto(value: Any) -> CheckAccessManyRequest:
    return CheckAccessManyRequest(
        requests=[check_access_request_from_proto(request) for request in value.requests]
    )


def check_access_many_response_to_proto(value: Any) -> Any:
    if isinstance(value, pb.CheckAccessManyResponse):
        return _copy(value)
    response = _coerce(value, CheckAccessManyResponse, "CheckAccessManyResponse")
    return pb.CheckAccessManyResponse(
        decisions=[check_access_response_to_proto(decision) for decision in response.decisions]
    )


def list_relationships_request_from_proto(value: Any) -> ListRelationshipsRequest:
    return ListRelationshipsRequest(
        filter=(
            relationship_filter_from_proto(value.filter)
            if _has_field(value, "filter")
            else None
        ),
        page_size=value.page_size,
        page_token=value.page_token,
    )


def list_relationships_response_to_proto(value: Any) -> Any:
    if isinstance(value, pb.ListRelationshipsResponse):
        return _copy(value)
    response = _coerce(value, ListRelationshipsResponse, "ListRelationshipsResponse")
    return pb.ListRelationshipsResponse(
        relationships=[relationship_to_proto(item) for item in response.relationships],
        next_page_token=response.next_page_token,
    )


def add_relationship_request_from_proto(value: Any) -> AddRelationshipRequest:
    return AddRelationshipRequest(
        relationship=(
            relationship_from_proto(value.relationship)
            if _has_field(value, "relationship")
            else None
        )
    )


def add_relationship_response_to_proto(value: Any) -> Any:
    if isinstance(value, pb.AddRelationshipResponse):
        return _copy(value)
    response = _coerce(value, AddRelationshipResponse, "AddRelationshipResponse")
    return pb.AddRelationshipResponse(
        relationship=(
            relationship_to_proto(response.relationship)
            if response.relationship is not None
            else None
        )
    )


def delete_relationship_request_from_proto(value: Any) -> DeleteRelationshipRequest:
    return DeleteRelationshipRequest(
        relationship_tuple=(
            relationship_tuple_from_proto(value.relationship_tuple)
            if _has_field(value, "relationship_tuple")
            else None
        )
    )


def delete_relationship_response_to_proto(value: Any) -> Any:
    if isinstance(value, pb.DeleteRelationshipResponse):
        return _copy(value)
    return pb.DeleteRelationshipResponse()


def set_authorization_state_request_from_proto(
    value: Any,
) -> SetAuthorizationStateRequest:
    return SetAuthorizationStateRequest(
        model=authorization_model_from_proto(value.model)
        if _has_field(value, "model")
        else None,
        relationships=[relationship_from_proto(item) for item in value.relationships]
    )


def set_authorization_state_response_to_proto(value: Any) -> Any:
    if isinstance(value, pb.SetAuthorizationStateResponse):
        return _copy(value)
    response = _coerce(
        value,
        SetAuthorizationStateResponse,
        "SetAuthorizationStateResponse",
    )
    return pb.SetAuthorizationStateResponse(
        active_model=(
            authorization_model_ref_to_proto(response.active_model)
            if response.active_model is not None
            else None
        )
    )


def get_active_model_ref_response_to_proto(value: Any) -> Any:
    if isinstance(value, pb.GetActiveModelRefResponse):
        return _copy(value)
    response = _coerce(value, GetActiveModelRefResponse, "GetActiveModelRefResponse")
    return pb.GetActiveModelRefResponse(
        model=(
            authorization_model_ref_to_proto(response.model)
            if response.model is not None
            else None
        )
    )


def set_active_model_request_from_proto(value: Any) -> SetActiveModelRequest:
    return SetActiveModelRequest(
        model=authorization_model_from_proto(value.model) if _has_field(value, "model") else None
    )


def set_active_model_response_to_proto(value: Any) -> Any:
    if isinstance(value, pb.SetActiveModelResponse):
        return _copy(value)
    response = _coerce(value, SetActiveModelResponse, "SetActiveModelResponse")
    return pb.SetActiveModelResponse(
        model=(
            authorization_model_ref_to_proto(response.model)
            if response.model is not None
            else None
        )
    )


def list_active_model_resource_types_request_from_proto(
    value: Any,
) -> ListActiveModelResourceTypesRequest:
    return ListActiveModelResourceTypesRequest(
        filter=(
            AuthorizationModelResourceTypeFilter(
                name=value.filter.name,
                source_layer=value.filter.source_layer,
            )
            if _has_field(value, "filter")
            else None
        ),
        page_size=value.page_size,
        page_token=value.page_token,
    )


def list_active_model_resource_types_response_to_proto(value: Any) -> Any:
    if isinstance(value, pb.ListActiveModelResourceTypesResponse):
        return _copy(value)
    response = _coerce(
        value,
        ListActiveModelResourceTypesResponse,
        "ListActiveModelResourceTypesResponse",
    )
    return pb.ListActiveModelResourceTypesResponse(
        resource_types=[
            authorization_model_resource_type_to_proto(item)
            for item in response.resource_types
        ],
        next_page_token=response.next_page_token,
        model_id=response.model_id,
    )


def relationship_filter_from_proto(value: Any) -> RelationshipFilter:
    return RelationshipFilter(
        target=relationship_target_from_proto(value.target)
        if _has_field(value, "target")
        else None,
        relation=value.relation,
        resource=_resource_from_proto(value.resource)
        if _has_field(value, "resource")
        else None,
        target_type=value.target_type,
        target_entity_type=value.target_entity_type,
        resource_type=value.resource_type,
        source_layer=value.source_layer,
    )


def relationship_from_proto(value: Any) -> Relationship:
    return Relationship(
        tuple=relationship_tuple_from_proto(value.tuple)
        if _has_field(value, "tuple")
        else None,
        properties=_struct_to_dict(value.properties)
        if _has_field(value, "properties")
        else {},
        source_layer=value.source_layer,
    )


def relationship_to_proto(value: Any) -> Any:
    if isinstance(value, pb.Relationship):
        return _copy(value)
    relationship = _coerce(value, Relationship, "Relationship")
    return pb.Relationship(
        tuple=(
            relationship_tuple_to_proto(relationship.tuple)
            if relationship.tuple is not None
            else None
        ),
        properties=_struct_from_dict(relationship.properties),
        source_layer=relationship.source_layer,
    )


def relationship_tuple_from_proto(value: Any) -> RelationshipTuple:
    return RelationshipTuple(
        target=relationship_target_from_proto(value.target)
        if _has_field(value, "target")
        else None,
        relation=value.relation,
        resource=_resource_from_proto(value.resource)
        if _has_field(value, "resource")
        else None,
    )


def relationship_tuple_to_proto(value: Any) -> Any:
    if isinstance(value, pb.RelationshipTuple):
        return _copy(value)
    relationship_tuple = _coerce(value, RelationshipTuple, "RelationshipTuple")
    return pb.RelationshipTuple(
        target=(
            relationship_target_to_proto(relationship_tuple.target)
            if relationship_tuple.target is not None
            else None
        ),
        relation=relationship_tuple.relation,
        resource=(
            _resource_to_proto(relationship_tuple.resource)
            if relationship_tuple.resource is not None
            else None
        ),
    )


def relationship_target_from_proto(value: Any) -> RelationshipTarget:
    selected = _which_oneof(value, "kind")
    if selected == "subject":
        return RelationshipTarget(subject=_subject_from_proto(value.subject))
    if selected == "resource":
        return RelationshipTarget(resource=_resource_from_proto(value.resource))
    if selected == "subject_set":
        return RelationshipTarget(subject_set=subject_set_from_proto(value.subject_set))
    return RelationshipTarget()


def relationship_target_to_proto(value: Any) -> Any:
    if isinstance(value, pb.RelationshipTarget):
        return _copy(value)
    target = _coerce(value, RelationshipTarget, "RelationshipTarget")
    message = pb.RelationshipTarget()
    if target.subject is not None:
        message.subject.CopyFrom(_subject_to_proto(target.subject))
    elif target.resource is not None:
        message.resource.CopyFrom(_resource_to_proto(target.resource))
    elif target.subject_set is not None:
        message.subject_set.CopyFrom(subject_set_to_proto(target.subject_set))
    return message


def subject_set_from_proto(value: Any) -> SubjectSet:
    return SubjectSet(
        resource=_resource_from_proto(value.resource)
        if _has_field(value, "resource")
        else None,
        relation=value.relation,
    )


def subject_set_to_proto(value: Any) -> Any:
    if isinstance(value, pb.SubjectSet):
        return _copy(value)
    subject_set = _coerce(value, SubjectSet, "SubjectSet")
    return pb.SubjectSet(
        resource=(
            _resource_to_proto(subject_set.resource)
            if subject_set.resource is not None
            else None
        ),
        relation=subject_set.relation,
    )


def authorization_model_from_proto(value: Any) -> AuthorizationModel:
    return AuthorizationModel(
        id=value.id,
        version=value.version,
        resource_types=[
            authorization_model_resource_type_from_proto(item)
            for item in value.resource_types
        ],
    )


def authorization_model_resource_type_from_proto(
    value: Any,
) -> AuthorizationModelResourceType:
    return AuthorizationModelResourceType(
        name=value.name,
        relations=[
            ModelRelation(
                name=relation.name,
                allowed_targets=[
                    model_allowed_target_from_proto(target)
                    for target in relation.allowed_targets
                ],
            )
            for relation in value.relations
        ],
        actions=[
            ModelAction(name=action.name, relations=list(action.relations))
            for action in value.actions
        ],
        source_layer=value.source_layer,
        default_access_policy=value.default_access_policy,
    )


def authorization_model_resource_type_to_proto(value: Any) -> Any:
    if isinstance(value, pb.AuthorizationModelResourceType):
        return _copy(value)
    resource_type = _coerce(
        value, AuthorizationModelResourceType, "AuthorizationModelResourceType"
    )
    return pb.AuthorizationModelResourceType(
        name=resource_type.name,
        relations=[
            pb.ModelRelation(
                name=relation.name,
                allowed_targets=[
                    model_allowed_target_to_proto(target)
                    for target in relation.allowed_targets
                ],
            )
            for relation in resource_type.relations
        ],
        actions=[
            pb.ModelAction(name=action.name, relations=list(action.relations))
            for action in resource_type.actions
        ],
        source_layer=resource_type.source_layer,
        default_access_policy=resource_type.default_access_policy,
    )


def authorization_model_ref_to_proto(value: Any) -> Any:
    if isinstance(value, pb.AuthorizationModelRef):
        return _copy(value)
    model = _coerce(value, AuthorizationModelRef, "AuthorizationModelRef")
    return pb.AuthorizationModelRef(
        id=model.id,
        version=model.version,
        created_at=_timestamp_from_datetime(model.created_at),
    )


def model_allowed_target_from_proto(value: Any) -> ModelAllowedTarget:
    selected = _which_oneof(value, "kind")
    if selected == "subject_type":
        return ModelAllowedTarget(subject_type=value.subject_type)
    if selected == "resource_type":
        return ModelAllowedTarget(resource_type=value.resource_type)
    if selected == "subject_set_type":
        return ModelAllowedTarget(
            subject_set_type=SubjectSetType(
                resource_type=value.subject_set_type.resource_type,
                relation=value.subject_set_type.relation,
            )
        )
    return ModelAllowedTarget()


def model_allowed_target_to_proto(value: Any) -> Any:
    if isinstance(value, pb.ModelAllowedTarget):
        return _copy(value)
    target = _coerce(value, ModelAllowedTarget, "ModelAllowedTarget")
    message = pb.ModelAllowedTarget()
    if target.subject_type is not None:
        message.subject_type = target.subject_type
    elif target.resource_type is not None:
        message.resource_type = target.resource_type
    elif target.subject_set_type is not None:
        message.subject_set_type.CopyFrom(
            pb.SubjectSetType(
                resource_type=target.subject_set_type.resource_type,
                relation=target.subject_set_type.relation,
            )
        )
    return message


def _subject_from_proto(value: Any) -> AuthorizationSubject:
    return AuthorizationSubject(
        type=value.type,
        id=value.id,
        properties=_struct_to_dict(value.properties)
        if _has_field(value, "properties")
        else {},
    )


def _subject_to_proto(value: Any) -> Any:
    if isinstance(value, pb.Subject):
        return _copy(value)
    subject = _coerce(value, AuthorizationSubject, "AuthorizationSubject")
    return pb.Subject(
        type=subject.type,
        id=subject.id,
        properties=_struct_from_dict(subject.properties),
    )


def _action_from_proto(value: Any) -> AuthorizationAction:
    return AuthorizationAction(
        name=value.name,
        properties=_struct_to_dict(value.properties)
        if _has_field(value, "properties")
        else {},
    )


def _resource_from_proto(value: Any) -> AuthorizationResource:
    return AuthorizationResource(
        type=value.type,
        id=value.id,
        properties=_struct_to_dict(value.properties)
        if _has_field(value, "properties")
        else {},
    )


def _resource_to_proto(value: Any) -> Any:
    if isinstance(value, pb.Resource):
        return _copy(value)
    resource = _coerce(value, AuthorizationResource, "AuthorizationResource")
    return pb.Resource(
        type=resource.type,
        id=resource.id,
        properties=_struct_from_dict(resource.properties),
    )


def _authorization_model_ref_from_proto(value: Any) -> AuthorizationModelRef:
    return AuthorizationModelRef(
        id=value.id,
        version=value.version,
        created_at=_datetime_from_timestamp(value.created_at)
        if _has_field(value, "created_at")
        else None,
    )


def _sequence(value: Sequence[Any] | None) -> list[Any]:
    return list(value or ())

_sequence
_authorization_model_ref_from_proto
