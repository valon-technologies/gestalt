from __future__ import annotations

import datetime as dt
import os
import threading
from collections.abc import Mapping, Sequence
from dataclasses import dataclass, field
from typing import Any, Protocol

from google.protobuf import empty_pb2 as _empty_pb2

from ._gen.v1 import authorization_pb2 as _pb
from ._gen.v1 import authorization_pb2_grpc as _pb_grpc
from ._grpc_transport import (
    ENV_HOST_SERVICE_SOCKET,
    ENV_HOST_SERVICE_TOKEN,
    host_service_channel,
)
from ._protocol import coerce_model as _coerce
from ._protocol import copy_message as _copy
from ._protocol import datetime_from_timestamp as _datetime_from_timestamp
from ._protocol import has_field as _has_field
from ._protocol import struct_from_dict as _struct_from_dict
from ._protocol import struct_to_dict as _struct_to_dict
from ._protocol import timestamp_from_datetime as _timestamp_from_datetime
from ._protocol import which_oneof as _which_oneof

pb: Any = _pb
pb_grpc: Any = _pb_grpc

_shared_authorization_transport: dict[str, Any] = {}
_shared_authorization_lock = threading.Lock()

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


class AuthorizationProtocol(Protocol):
    """Fakeable contract for host authorization calls."""

    def __enter__(self) -> "AuthorizationProtocol":
        """Return the client for ``with`` statements."""

    def __exit__(self, *args: Any) -> None:
        """Close the client at the end of a context manager block."""

    def close(self) -> None:
        """Close the client."""

    def check_access(self, request: CheckAccessRequest) -> CheckAccessResponse:
        """Return whether one authorization request is allowed."""

    def check_access_many(
        self, request: CheckAccessManyRequest
    ) -> CheckAccessManyResponse:
        """Return decisions for a batch of authorization requests."""

    def list_relationships(
        self, request: ListRelationshipsRequest
    ) -> ListRelationshipsResponse:
        """List relationships matching a filter."""

    def add_relationship(
        self, request: AddRelationshipRequest
    ) -> AddRelationshipResponse:
        """Add one relationship."""

    def delete_relationship(
        self, request: DeleteRelationshipRequest
    ) -> DeleteRelationshipResponse:
        """Delete one relationship tuple."""

    def set_authorization_state(
        self, request: SetAuthorizationStateRequest
    ) -> SetAuthorizationStateResponse:
        """Atomically replace authorization state."""

    def get_active_model_ref(self) -> GetActiveModelRefResponse:
        """Return the active authorization model reference."""

    def set_active_model(self, request: SetActiveModelRequest) -> SetActiveModelResponse:
        """Set the active authorization model."""

    def list_active_model_resource_types(
        self, request: ListActiveModelResourceTypesRequest
    ) -> ListActiveModelResourceTypesResponse:
        """List resource types in the active authorization model."""


class Authorization:
    """Transport-backed implementation for host authorization calls."""

    def __new__(
        cls,
        socket_target: str | None = None,
        relay_token: str | None = None,
        *,
        _shared: bool = False,
    ) -> "Authorization":
        if not _shared and socket_target is None and relay_token is None:
            return _shared_authorization_client()
        return super().__new__(cls)

    def __init__(
        self,
        socket_target: str | None = None,
        relay_token: str | None = None,
        *,
        _shared: bool = False,
    ) -> None:
        if getattr(self, "_authorization_initialized", False):
            return
        target = _resolve_authorization_socket_target(socket_target)
        token = (
            relay_token
            if relay_token is not None
            else os.environ.get(ENV_HOST_SERVICE_TOKEN, "")
        ).strip()
        self._channel = host_service_channel("authorization", target, token=token)
        self._stub = pb_grpc.AuthorizationStub(self._channel)
        self._closed = False
        self._shared = _shared
        self._authorization_initialized = True

    def close(self) -> None:
        """Close the underlying gRPC channel."""

        if self._shared:
            return
        self._close_channel()

    def _close_channel(self) -> None:
        if self._closed:
            return
        self._closed = True
        self._channel.close()

    def check_access(self, request: CheckAccessRequest) -> CheckAccessResponse:
        """Return whether one authorization request is allowed."""

        return check_access_response_from_proto(
            self._stub.CheckAccess(check_access_request_to_proto(request))
        )

    def check_access_many(
        self, request: CheckAccessManyRequest
    ) -> CheckAccessManyResponse:
        """Return decisions for a batch of authorization requests."""

        return check_access_many_response_from_proto(
            self._stub.CheckAccessMany(check_access_many_request_to_proto(request))
        )

    def list_relationships(
        self, request: ListRelationshipsRequest
    ) -> ListRelationshipsResponse:
        """List relationships matching a filter."""

        return list_relationships_response_from_proto(
            self._stub.ListRelationships(list_relationships_request_to_proto(request))
        )

    def add_relationship(
        self, request: AddRelationshipRequest
    ) -> AddRelationshipResponse:
        """Add one relationship."""

        return add_relationship_response_from_proto(
            self._stub.AddRelationship(add_relationship_request_to_proto(request))
        )

    def delete_relationship(
        self, request: DeleteRelationshipRequest
    ) -> DeleteRelationshipResponse:
        """Delete one relationship tuple."""

        return delete_relationship_response_from_proto(
            self._stub.DeleteRelationship(delete_relationship_request_to_proto(request))
        )

    def set_authorization_state(
        self, request: SetAuthorizationStateRequest
    ) -> SetAuthorizationStateResponse:
        """Atomically replace authorization state."""

        return set_authorization_state_response_from_proto(
            self._stub.SetAuthorizationState(
                set_authorization_state_request_to_proto(request)
            )
        )

    def get_active_model_ref(self) -> GetActiveModelRefResponse:
        """Return the active authorization model reference."""

        return get_active_model_ref_response_from_proto(
            self._stub.GetActiveModelRef(getattr(_empty_pb2, "Empty")())
        )

    def set_active_model(self, request: SetActiveModelRequest) -> SetActiveModelResponse:
        """Set the active authorization model."""

        return set_active_model_response_from_proto(
            self._stub.SetActiveModel(set_active_model_request_to_proto(request))
        )

    def list_active_model_resource_types(
        self, request: ListActiveModelResourceTypesRequest
    ) -> ListActiveModelResourceTypesResponse:
        """List resource types in the active authorization model."""

        return list_active_model_resource_types_response_from_proto(
            self._stub.ListActiveModelResourceTypes(
                list_active_model_resource_types_request_to_proto(request)
            )
        )

    def __enter__(self) -> "Authorization":
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()


def _shared_authorization_client() -> Authorization:
    target = _resolve_authorization_socket_target()
    token = os.environ.get(ENV_HOST_SERVICE_TOKEN, "").strip()
    with _shared_authorization_lock:
        client = _shared_authorization_transport.get("client")
        if (
            client is not None
            and _shared_authorization_transport.get("target") == target
            and _shared_authorization_transport.get("token") == token
        ):
            return client

        client = Authorization(target, token, _shared=True)
        stale = _shared_authorization_transport.get("client")
        _shared_authorization_transport["target"] = target
        _shared_authorization_transport["token"] = token
        _shared_authorization_transport["client"] = client
        if stale is not None:
            stale._close_channel()
        return client


def _resolve_authorization_socket_target(socket_target: str | None = None) -> str:
    target = (
        socket_target
        if socket_target is not None
        else os.environ.get(ENV_HOST_SERVICE_SOCKET, "")
    ).strip()
    if not target:
        raise RuntimeError(f"authorization: {ENV_HOST_SERVICE_SOCKET} is not set")
    return target


def check_access_request_to_proto(value: Any) -> Any:
    if isinstance(value, pb.CheckAccessRequest):
        return _copy(value)
    request = _coerce(value, CheckAccessRequest, "CheckAccessRequest")
    return pb.CheckAccessRequest(
        subject=(
            _subject_to_proto(request.subject)
            if request.subject is not None
            else None
        ),
        action=(
            _action_to_proto(request.action)
            if request.action is not None
            else None
        ),
        resource=(
            _resource_to_proto(request.resource)
            if request.resource is not None
            else None
        ),
    )


def check_access_request_from_proto(value: Any) -> CheckAccessRequest:
    return CheckAccessRequest(
        subject=_subject_from_proto(value.subject) if _has_field(value, "subject") else None,
        action=_action_from_proto(value.action) if _has_field(value, "action") else None,
        resource=_resource_from_proto(value.resource) if _has_field(value, "resource") else None,
    )


def check_access_response_from_proto(value: Any) -> CheckAccessResponse:
    return CheckAccessResponse(allowed=value.allowed, model_id=value.model_id)


def check_access_response_to_proto(value: Any) -> Any:
    if isinstance(value, pb.CheckAccessResponse):
        return _copy(value)
    response = _coerce(value, CheckAccessResponse, "CheckAccessResponse")
    return pb.CheckAccessResponse(
        allowed=response.allowed,
        model_id=response.model_id,
    )


def check_access_many_request_to_proto(value: Any) -> Any:
    if isinstance(value, pb.CheckAccessManyRequest):
        return _copy(value)
    request = _coerce(value, CheckAccessManyRequest, "CheckAccessManyRequest")
    return pb.CheckAccessManyRequest(
        requests=[check_access_request_to_proto(item) for item in request.requests]
    )


def check_access_many_request_from_proto(value: Any) -> CheckAccessManyRequest:
    return CheckAccessManyRequest(
        requests=[check_access_request_from_proto(request) for request in value.requests]
    )


def check_access_many_response_from_proto(value: Any) -> CheckAccessManyResponse:
    return CheckAccessManyResponse(
        decisions=[check_access_response_from_proto(item) for item in value.decisions]
    )


def check_access_many_response_to_proto(value: Any) -> Any:
    if isinstance(value, pb.CheckAccessManyResponse):
        return _copy(value)
    response = _coerce(value, CheckAccessManyResponse, "CheckAccessManyResponse")
    return pb.CheckAccessManyResponse(
        decisions=[check_access_response_to_proto(decision) for decision in response.decisions]
    )


def list_relationships_request_to_proto(value: Any) -> Any:
    if isinstance(value, pb.ListRelationshipsRequest):
        return _copy(value)
    request = _coerce(value, ListRelationshipsRequest, "ListRelationshipsRequest")
    return pb.ListRelationshipsRequest(
        filter=(
            relationship_filter_to_proto(request.filter)
            if request.filter is not None
            else None
        ),
        page_size=request.page_size,
        page_token=request.page_token,
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


def list_relationships_response_from_proto(value: Any) -> ListRelationshipsResponse:
    return ListRelationshipsResponse(
        relationships=[relationship_from_proto(item) for item in value.relationships],
        next_page_token=value.next_page_token,
    )


def list_relationships_response_to_proto(value: Any) -> Any:
    if isinstance(value, pb.ListRelationshipsResponse):
        return _copy(value)
    response = _coerce(value, ListRelationshipsResponse, "ListRelationshipsResponse")
    return pb.ListRelationshipsResponse(
        relationships=[relationship_to_proto(item) for item in response.relationships],
        next_page_token=response.next_page_token,
    )


def add_relationship_request_to_proto(value: Any) -> Any:
    if isinstance(value, pb.AddRelationshipRequest):
        return _copy(value)
    request = _coerce(value, AddRelationshipRequest, "AddRelationshipRequest")
    return pb.AddRelationshipRequest(
        relationship=(
            relationship_to_proto(request.relationship)
            if request.relationship is not None
            else None
        )
    )


def add_relationship_request_from_proto(value: Any) -> AddRelationshipRequest:
    return AddRelationshipRequest(
        relationship=(
            relationship_from_proto(value.relationship)
            if _has_field(value, "relationship")
            else None
        )
    )


def add_relationship_response_from_proto(value: Any) -> AddRelationshipResponse:
    return AddRelationshipResponse(
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


def delete_relationship_request_to_proto(value: Any) -> Any:
    if isinstance(value, pb.DeleteRelationshipRequest):
        return _copy(value)
    request = _coerce(value, DeleteRelationshipRequest, "DeleteRelationshipRequest")
    return pb.DeleteRelationshipRequest(
        relationship_tuple=(
            relationship_tuple_to_proto(request.relationship_tuple)
            if request.relationship_tuple is not None
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


def delete_relationship_response_from_proto(_value: Any) -> DeleteRelationshipResponse:
    return DeleteRelationshipResponse()


def delete_relationship_response_to_proto(value: Any) -> Any:
    if isinstance(value, pb.DeleteRelationshipResponse):
        return _copy(value)
    return pb.DeleteRelationshipResponse()


def set_authorization_state_request_to_proto(value: Any) -> Any:
    if isinstance(value, pb.SetAuthorizationStateRequest):
        return _copy(value)
    request = _coerce(
        value, SetAuthorizationStateRequest, "SetAuthorizationStateRequest"
    )
    return pb.SetAuthorizationStateRequest(
        model=(
            authorization_model_to_proto(request.model)
            if request.model is not None
            else None
        ),
        relationships=[relationship_to_proto(item) for item in request.relationships],
    )


def set_authorization_state_request_from_proto(
    value: Any,
) -> SetAuthorizationStateRequest:
    return SetAuthorizationStateRequest(
        model=authorization_model_from_proto(value.model)
        if _has_field(value, "model")
        else None,
        relationships=[relationship_from_proto(item) for item in value.relationships]
    )


def set_authorization_state_response_from_proto(
    value: Any,
) -> SetAuthorizationStateResponse:
    return SetAuthorizationStateResponse(
        active_model=(
            _authorization_model_ref_from_proto(value.active_model)
            if _has_field(value, "active_model")
            else None
        )
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


def get_active_model_ref_response_from_proto(value: Any) -> GetActiveModelRefResponse:
    return GetActiveModelRefResponse(
        model=(
            _authorization_model_ref_from_proto(value.model)
            if _has_field(value, "model")
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


def set_active_model_request_to_proto(value: Any) -> Any:
    if isinstance(value, pb.SetActiveModelRequest):
        return _copy(value)
    request = _coerce(value, SetActiveModelRequest, "SetActiveModelRequest")
    return pb.SetActiveModelRequest(
        model=(
            authorization_model_to_proto(request.model)
            if request.model is not None
            else None
        )
    )


def set_active_model_request_from_proto(value: Any) -> SetActiveModelRequest:
    return SetActiveModelRequest(
        model=authorization_model_from_proto(value.model) if _has_field(value, "model") else None
    )


def set_active_model_response_from_proto(value: Any) -> SetActiveModelResponse:
    return SetActiveModelResponse(
        model=(
            _authorization_model_ref_from_proto(value.model)
            if _has_field(value, "model")
            else None
        )
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


def list_active_model_resource_types_request_to_proto(value: Any) -> Any:
    if isinstance(value, pb.ListActiveModelResourceTypesRequest):
        return _copy(value)
    request = _coerce(
        value,
        ListActiveModelResourceTypesRequest,
        "ListActiveModelResourceTypesRequest",
    )
    return pb.ListActiveModelResourceTypesRequest(
        filter=(
            pb.AuthorizationModelResourceTypeFilter(
                name=request.filter.name,
                source_layer=request.filter.source_layer,
            )
            if request.filter is not None
            else None
        ),
        page_size=request.page_size,
        page_token=request.page_token,
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


def list_active_model_resource_types_response_from_proto(
    value: Any,
) -> ListActiveModelResourceTypesResponse:
    return ListActiveModelResourceTypesResponse(
        resource_types=[
            authorization_model_resource_type_from_proto(item)
            for item in value.resource_types
        ],
        next_page_token=value.next_page_token,
        model_id=value.model_id,
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


def relationship_filter_to_proto(value: Any) -> Any:
    if isinstance(value, pb.RelationshipFilter):
        return _copy(value)
    relationship_filter = _coerce(value, RelationshipFilter, "RelationshipFilter")
    return pb.RelationshipFilter(
        target=(
            relationship_target_to_proto(relationship_filter.target)
            if relationship_filter.target is not None
            else None
        ),
        relation=relationship_filter.relation,
        resource=(
            _resource_to_proto(relationship_filter.resource)
            if relationship_filter.resource is not None
            else None
        ),
        target_type=relationship_filter.target_type,
        target_entity_type=relationship_filter.target_entity_type,
        resource_type=relationship_filter.resource_type,
        source_layer=relationship_filter.source_layer,
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


def authorization_model_to_proto(value: Any) -> Any:
    if isinstance(value, pb.AuthorizationModel):
        return _copy(value)
    model = _coerce(value, AuthorizationModel, "AuthorizationModel")
    return pb.AuthorizationModel(
        id=model.id,
        version=model.version,
        resource_types=[
            authorization_model_resource_type_to_proto(item)
            for item in model.resource_types
        ],
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


def _action_to_proto(value: Any) -> Any:
    if isinstance(value, pb.Action):
        return _copy(value)
    action = _coerce(value, AuthorizationAction, "AuthorizationAction")
    return pb.Action(
        name=action.name,
        properties=_struct_from_dict(action.properties),
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
