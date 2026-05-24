"""Transport client for the host authorization provider."""

from __future__ import annotations

import dataclasses as _dataclasses
import datetime as _dt
import os
import threading
from collections.abc import Mapping, Sequence
from typing import Any, Protocol, cast
from urllib import parse as _urlparse

import grpc
from google.protobuf import empty_pb2 as _empty_pb2
from google.protobuf import json_format

from ._gen.v1 import authorization_pb2 as _authorization_pb2
from ._gen.v1 import authorization_pb2_grpc as _authorization_pb2_grpc
from ._grpc_transport import (
    ENV_HOST_SERVICE_SOCKET,
    ENV_HOST_SERVICE_TOKEN,
    insecure_internal_channel,
    internal_channel_target,
    secure_internal_channel,
)
from ._protocol import dataclass_mapping as _dataclass_mapping

empty_pb2: Any = _empty_pb2
authorization_pb2: Any = _authorization_pb2
authorization_pb2_grpc: Any = _authorization_pb2_grpc

_AUTHORIZATION_RELAY_TOKEN_HEADER = "x-gestalt-host-service-relay-token"
AUTHORIZATION_SUBJECT_TYPE_SUBJECT = "subject"

_shared_authorization_transport: dict[str, Any] = {
    "target": "",
    "token": "",
    "client": None,
}
_shared_authorization_lock = threading.Lock()


@_dataclasses.dataclass(slots=True)
class AuthorizationSubject:
    type: str = ""
    id: str = ""
    properties: Mapping[str, Any] | None = None


@_dataclasses.dataclass(slots=True)
class AuthorizationResource:
    type: str = ""
    id: str = ""
    properties: Mapping[str, Any] | None = None


@_dataclasses.dataclass(slots=True)
class AuthorizationSubjectSet:
    resource: Any | None = None
    relation: str = ""


@_dataclasses.dataclass(slots=True)
class AuthorizationRelationshipTarget:
    subject: Any | None = None
    resource: Any | None = None
    subject_set: Any | None = None


@_dataclasses.dataclass(slots=True)
class AuthorizationAction:
    name: str = ""
    properties: Mapping[str, Any] | None = None


@_dataclasses.dataclass(slots=True)
class AccessEvaluationRequest:
    subject: Any | None = None
    action: Any | None = None
    resource: Any | None = None
    context: Mapping[str, Any] | None = None


@_dataclasses.dataclass(slots=True)
class AccessDecision:
    allowed: bool = False
    context: Mapping[str, Any] | None = None
    model_id: str = ""


@_dataclasses.dataclass(slots=True)
class AccessEvaluationsRequest:
    requests: Sequence[Any] | None = None


@_dataclasses.dataclass(slots=True)
class AccessEvaluationsResponse:
    decisions: Sequence[Any] | None = None


@_dataclasses.dataclass(slots=True)
class ResourceSearchRequest:
    subject: Any | None = None
    action: Any | None = None
    resource_type: str = ""
    context: Mapping[str, Any] | None = None
    page_size: int = 0
    page_token: str = ""


@_dataclasses.dataclass(slots=True)
class ResourceSearchResponse:
    resources: Sequence[Any] | None = None
    next_page_token: str = ""
    model_id: str = ""


@_dataclasses.dataclass(slots=True)
class SubjectSearchRequest:
    resource: Any | None = None
    action: Any | None = None
    subject_type: str = ""
    context: Mapping[str, Any] | None = None
    page_size: int = 0
    page_token: str = ""


@_dataclasses.dataclass(slots=True)
class SubjectSearchResponse:
    subjects: Sequence[Any] | None = None
    next_page_token: str = ""
    model_id: str = ""


@_dataclasses.dataclass(slots=True)
class EffectiveSubjectSearchRequest:
    resource: Any | None = None
    action: Any | None = None
    context: Mapping[str, Any] | None = None
    page_size: int = 0
    page_token: str = ""


@_dataclasses.dataclass(slots=True)
class EffectiveSubjectSearchResponse:
    targets: Sequence[Any] | None = None
    next_page_token: str = ""
    model_id: str = ""
    truncated: bool = False


@_dataclasses.dataclass(slots=True)
class ActionSearchRequest:
    subject: Any | None = None
    resource: Any | None = None
    context: Mapping[str, Any] | None = None
    page_size: int = 0
    page_token: str = ""


@_dataclasses.dataclass(slots=True)
class ActionSearchResponse:
    actions: Sequence[Any] | None = None
    next_page_token: str = ""
    model_id: str = ""


@_dataclasses.dataclass(slots=True)
class AuthorizationMetadata:
    capabilities: Sequence[str] | None = None
    active_model_id: str = ""


@_dataclasses.dataclass(slots=True)
class Relationship:
    subject: Any | None = None
    relation: str = ""
    resource: Any | None = None
    properties: Mapping[str, Any] | None = None
    target: Any | None = None


@_dataclasses.dataclass(slots=True)
class RelationshipKey:
    subject: Any | None = None
    relation: str = ""
    resource: Any | None = None
    target: Any | None = None


@_dataclasses.dataclass(slots=True)
class ReadRelationshipsRequest:
    subject: Any | None = None
    relation: str = ""
    resource: Any | None = None
    page_size: int = 0
    page_token: str = ""
    model_id: str = ""
    target: Any | None = None


@_dataclasses.dataclass(slots=True)
class ReadRelationshipsResponse:
    relationships: Sequence[Any] | None = None
    next_page_token: str = ""
    model_id: str = ""


@_dataclasses.dataclass(slots=True)
class WriteRelationshipsRequest:
    writes: Sequence[Any] | None = None
    deletes: Sequence[Any] | None = None
    model_id: str = ""


@_dataclasses.dataclass(slots=True)
class AuthorizationModel:
    version: int = 0
    resource_types: Sequence[Any] | None = None


@_dataclasses.dataclass(slots=True)
class AuthorizationModelResourceType:
    name: str = ""
    relations: Sequence[Any] | None = None
    actions: Sequence[Any] | None = None


@_dataclasses.dataclass(slots=True)
class AuthorizationModelRelation:
    name: str = ""
    subject_types: Sequence[str] | None = None
    allowed_targets: Sequence[Any] | None = None
    rewrite: Any | None = None


@_dataclasses.dataclass(slots=True)
class AuthorizationModelAction:
    name: str = ""
    relations: Sequence[str] | None = None
    rewrite: Any | None = None


@_dataclasses.dataclass(slots=True)
class AuthorizationModelAllowedTarget:
    subject_type: str = ""
    resource_type: str = ""
    subject_set: Any | None = None


@_dataclasses.dataclass(slots=True)
class AuthorizationModelSubjectSetTarget:
    resource_type: str = ""
    relation: str = ""


@_dataclasses.dataclass(slots=True)
class AuthorizationModelRewrite:
    this: Any | None = None
    computed_userset: Any | None = None
    tuple_to_userset: Any | None = None
    union: Any | None = None


@_dataclasses.dataclass(slots=True)
class AuthorizationModelRewriteThis:
    pass


@_dataclasses.dataclass(slots=True)
class AuthorizationModelComputedUserset:
    relation: str = ""


@_dataclasses.dataclass(slots=True)
class AuthorizationModelTupleToUserset:
    tupleset_relation: str = ""
    computed_relation: str = ""


@_dataclasses.dataclass(slots=True)
class AuthorizationModelRewriteUnion:
    children: Sequence[Any] | None = None


@_dataclasses.dataclass(slots=True)
class AuthorizationModelRef:
    id: str = ""
    version: str = ""
    created_at: _dt.datetime | str | None = None

    def __post_init__(self) -> None:
        if isinstance(self.created_at, str):
            self.created_at = _datetime_from_proto_json(self.created_at)


@_dataclasses.dataclass(slots=True)
class GetActiveModelResponse:
    model: Any | None = None


@_dataclasses.dataclass(slots=True)
class ListModelsRequest:
    page_size: int = 0
    page_token: str = ""


@_dataclasses.dataclass(slots=True)
class ListModelsResponse:
    models: Sequence[Any] | None = None
    next_page_token: str = ""


@_dataclasses.dataclass(slots=True)
class WriteModelRequest:
    model: Any | None = None


@_dataclasses.dataclass(slots=True)
class ExpandRequest:
    resource: Any | None = None
    relation: str = ""
    context: Mapping[str, Any] | None = None
    max_depth: int = 0
    model_id: str = ""


@_dataclasses.dataclass(slots=True)
class ExpandNode:
    target: Any | None = None
    relation: str = ""
    children: Sequence[Any] | None = None


@_dataclasses.dataclass(slots=True)
class ExpandResponse:
    root: Any | None = None
    truncated: bool = False
    cycle_detected: bool = False
    max_depth_reached: bool = False
    model_id: str = ""


_NESTED_FIELD_TYPES: dict[tuple[type[Any], str], type[Any]] = {
    (AuthorizationSubjectSet, "resource"): AuthorizationResource,
    (AuthorizationRelationshipTarget, "subject"): AuthorizationSubject,
    (AuthorizationRelationshipTarget, "resource"): AuthorizationResource,
    (AuthorizationRelationshipTarget, "subject_set"): AuthorizationSubjectSet,
    (AccessEvaluationRequest, "subject"): AuthorizationSubject,
    (AccessEvaluationRequest, "action"): AuthorizationAction,
    (AccessEvaluationRequest, "resource"): AuthorizationResource,
    (ResourceSearchRequest, "subject"): AuthorizationSubject,
    (ResourceSearchRequest, "action"): AuthorizationAction,
    (SubjectSearchRequest, "resource"): AuthorizationResource,
    (SubjectSearchRequest, "action"): AuthorizationAction,
    (EffectiveSubjectSearchRequest, "resource"): AuthorizationResource,
    (EffectiveSubjectSearchRequest, "action"): AuthorizationAction,
    (ActionSearchRequest, "subject"): AuthorizationSubject,
    (ActionSearchRequest, "resource"): AuthorizationResource,
    (Relationship, "subject"): AuthorizationSubject,
    (Relationship, "resource"): AuthorizationResource,
    (Relationship, "target"): AuthorizationRelationshipTarget,
    (RelationshipKey, "subject"): AuthorizationSubject,
    (RelationshipKey, "resource"): AuthorizationResource,
    (RelationshipKey, "target"): AuthorizationRelationshipTarget,
    (ReadRelationshipsRequest, "subject"): AuthorizationSubject,
    (ReadRelationshipsRequest, "resource"): AuthorizationResource,
    (ReadRelationshipsRequest, "target"): AuthorizationRelationshipTarget,
    (AuthorizationModelRelation, "rewrite"): AuthorizationModelRewrite,
    (AuthorizationModelAction, "rewrite"): AuthorizationModelRewrite,
    (AuthorizationModelAllowedTarget, "subject_set"): AuthorizationModelSubjectSetTarget,
    (AuthorizationModelRewrite, "this"): AuthorizationModelRewriteThis,
    (AuthorizationModelRewrite, "computed_userset"): AuthorizationModelComputedUserset,
    (AuthorizationModelRewrite, "tuple_to_userset"): AuthorizationModelTupleToUserset,
    (AuthorizationModelRewrite, "union"): AuthorizationModelRewriteUnion,
    (GetActiveModelResponse, "model"): AuthorizationModelRef,
    (WriteModelRequest, "model"): AuthorizationModel,
    (ExpandRequest, "resource"): AuthorizationResource,
    (ExpandNode, "target"): AuthorizationRelationshipTarget,
    (ExpandResponse, "root"): ExpandNode,
}

_SEQUENCE_FIELD_TYPES: dict[tuple[type[Any], str], type[Any]] = {
    (AccessEvaluationsRequest, "requests"): AccessEvaluationRequest,
    (AccessEvaluationsResponse, "decisions"): AccessDecision,
    (ResourceSearchResponse, "resources"): AuthorizationResource,
    (SubjectSearchResponse, "subjects"): AuthorizationSubject,
    (EffectiveSubjectSearchResponse, "targets"): AuthorizationRelationshipTarget,
    (ActionSearchResponse, "actions"): AuthorizationAction,
    (ReadRelationshipsResponse, "relationships"): Relationship,
    (WriteRelationshipsRequest, "writes"): Relationship,
    (WriteRelationshipsRequest, "deletes"): RelationshipKey,
    (AuthorizationModel, "resource_types"): AuthorizationModelResourceType,
    (AuthorizationModelResourceType, "relations"): AuthorizationModelRelation,
    (AuthorizationModelResourceType, "actions"): AuthorizationModelAction,
    (AuthorizationModelRelation, "allowed_targets"): AuthorizationModelAllowedTarget,
    (AuthorizationModelRewriteUnion, "children"): AuthorizationModelRewrite,
    (ListModelsResponse, "models"): AuthorizationModelRef,
    (ExpandNode, "children"): ExpandNode,
}

_MESSAGE_NATIVE_TYPES: dict[type[Any], type[Any]] = {
    authorization_pb2.Subject: AuthorizationSubject,
    authorization_pb2.Resource: AuthorizationResource,
    authorization_pb2.SubjectSet: AuthorizationSubjectSet,
    authorization_pb2.RelationshipTarget: AuthorizationRelationshipTarget,
    authorization_pb2.Action: AuthorizationAction,
    authorization_pb2.AccessEvaluationRequest: AccessEvaluationRequest,
    authorization_pb2.AccessDecision: AccessDecision,
    authorization_pb2.AccessEvaluationsRequest: AccessEvaluationsRequest,
    authorization_pb2.AccessEvaluationsResponse: AccessEvaluationsResponse,
    authorization_pb2.ResourceSearchRequest: ResourceSearchRequest,
    authorization_pb2.ResourceSearchResponse: ResourceSearchResponse,
    authorization_pb2.SubjectSearchRequest: SubjectSearchRequest,
    authorization_pb2.SubjectSearchResponse: SubjectSearchResponse,
    authorization_pb2.EffectiveSubjectSearchRequest: EffectiveSubjectSearchRequest,
    authorization_pb2.EffectiveSubjectSearchResponse: EffectiveSubjectSearchResponse,
    authorization_pb2.ActionSearchRequest: ActionSearchRequest,
    authorization_pb2.ActionSearchResponse: ActionSearchResponse,
    authorization_pb2.AuthorizationMetadata: AuthorizationMetadata,
    authorization_pb2.Relationship: Relationship,
    authorization_pb2.RelationshipKey: RelationshipKey,
    authorization_pb2.ReadRelationshipsRequest: ReadRelationshipsRequest,
    authorization_pb2.ReadRelationshipsResponse: ReadRelationshipsResponse,
    authorization_pb2.WriteRelationshipsRequest: WriteRelationshipsRequest,
    authorization_pb2.AuthorizationModel: AuthorizationModel,
    authorization_pb2.AuthorizationModelResourceType: AuthorizationModelResourceType,
    authorization_pb2.AuthorizationModelRelation: AuthorizationModelRelation,
    authorization_pb2.AuthorizationModelAction: AuthorizationModelAction,
    authorization_pb2.AuthorizationModelAllowedTarget: AuthorizationModelAllowedTarget,
    authorization_pb2.AuthorizationModelSubjectSetTarget: AuthorizationModelSubjectSetTarget,
    authorization_pb2.AuthorizationModelRewrite: AuthorizationModelRewrite,
    authorization_pb2.AuthorizationModelRewriteThis: AuthorizationModelRewriteThis,
    authorization_pb2.AuthorizationModelComputedUserset: AuthorizationModelComputedUserset,
    authorization_pb2.AuthorizationModelTupleToUserset: AuthorizationModelTupleToUserset,
    authorization_pb2.AuthorizationModelRewriteUnion: AuthorizationModelRewriteUnion,
    authorization_pb2.AuthorizationModelRef: AuthorizationModelRef,
    authorization_pb2.GetActiveModelResponse: GetActiveModelResponse,
    authorization_pb2.ListModelsRequest: ListModelsRequest,
    authorization_pb2.ListModelsResponse: ListModelsResponse,
    authorization_pb2.WriteModelRequest: WriteModelRequest,
    authorization_pb2.ExpandRequest: ExpandRequest,
    authorization_pb2.ExpandNode: ExpandNode,
    authorization_pb2.ExpandResponse: ExpandResponse,
}

_ONEOF_FIELD_NAMES: dict[type[Any], tuple[str, ...]] = {
    AuthorizationRelationshipTarget: ("subject", "resource", "subject_set"),
    AuthorizationModelAllowedTarget: ("subject_type", "resource_type", "subject_set"),
    AuthorizationModelRewrite: (
        "this",
        "computed_userset",
        "tuple_to_userset",
        "union",
    ),
}


def _authorization_native(value: Any, native_type: type[Any]) -> Any:
    if value is None or isinstance(value, native_type):
        return value
    if isinstance(value, authorization_pb2.Subject | authorization_pb2.Resource):
        data = json_format.MessageToDict(value, preserving_proto_field_name=True)
        return _authorization_from_dict(data, native_type)
    if hasattr(value, "DESCRIPTOR"):
        data = json_format.MessageToDict(value, preserving_proto_field_name=True)
        return _authorization_from_dict(data, native_type)
    return _authorization_from_dict(value, native_type)


def _authorization_from_dict(value: Any, native_type: type[Any]) -> Any:
    if value is None or isinstance(value, native_type):
        return value
    mapping = _dataclass_mapping(value)
    if mapping is None:
        if not isinstance(value, Mapping):
            raise TypeError(
                f"authorization: expected {native_type.__name__}, mapping, "
                f"or protobuf message, got {type(value).__name__}"
            )
        mapping = dict(value)
    kwargs: dict[str, Any] = {}
    for field in _dataclasses.fields(native_type):
        if field.name not in mapping:
            continue
        raw = mapping[field.name]
        if raw is None:
            kwargs[field.name] = None
            continue
        if field.name == "created_at" and native_type is AuthorizationModelRef:
            kwargs[field.name] = _datetime_from_proto_json(raw)
            continue
        nested = _NESTED_FIELD_TYPES.get((native_type, field.name))
        if nested is not None:
            kwargs[field.name] = _authorization_from_dict(raw, nested)
            continue
        sequence = _SEQUENCE_FIELD_TYPES.get((native_type, field.name))
        if sequence is not None:
            kwargs[field.name] = [
                _authorization_from_dict(item, sequence) for item in raw
            ]
            continue
        kwargs[field.name] = raw
    return native_type(**kwargs)


def _authorization_message(value: Any, message_type: Any) -> Any:
    if isinstance(value, message_type):
        return value
    message = message_type()
    if value is None:
        return message
    json_format.ParseDict(_authorization_to_proto_dict(value), message)
    return message


def _authorization_to_proto_dict(value: Any) -> Any:
    if value is None:
        return None
    if isinstance(value, _dt.datetime):
        return _datetime_to_proto_json(value)
    if _dataclasses.is_dataclass(value) and not isinstance(value, type):
        oneof_fields = _ONEOF_FIELD_NAMES.get(type(value))
        if oneof_fields is not None:
            return _authorization_oneof_to_proto_dict(value, oneof_fields)
        output: dict[str, Any] = {}
        for field in _dataclasses.fields(value):
            entry = getattr(value, field.name)
            if entry is None:
                continue
            if isinstance(entry, Sequence) and not isinstance(
                entry, str | bytes | bytearray
            ):
                if len(entry) == 0:
                    continue
            elif isinstance(entry, Mapping) and len(entry) == 0:
                continue
            output[field.name] = _authorization_to_proto_dict(entry)
        return output
    if isinstance(value, Mapping):
        return {key: _authorization_to_proto_dict(entry) for key, entry in value.items()}
    if isinstance(value, Sequence) and not isinstance(value, str | bytes | bytearray):
        return [_authorization_to_proto_dict(entry) for entry in value]
    return value


def _authorization_oneof_to_proto_dict(
    value: Any,
    field_names: tuple[str, ...],
) -> dict[str, Any]:
    selected: list[tuple[str, Any]] = []
    for field_name in field_names:
        entry = getattr(value, field_name)
        if entry is None:
            continue
        if isinstance(entry, str) and entry == "":
            continue
        selected.append((field_name, entry))
    if len(selected) > 1:
        names = ", ".join(name for name, _ in selected)
        raise ValueError(
            f"authorization: {type(value).__name__} sets multiple oneof fields: {names}"
        )
    if not selected:
        return {}
    field_name, entry = selected[0]
    return {field_name: _authorization_to_proto_dict(entry)}


def _authorization_from_message(message: Any, native_type: type[Any] | None = None) -> Any:
    if native_type is None:
        native_type = _MESSAGE_NATIVE_TYPES[type(message)]
    data = json_format.MessageToDict(message, preserving_proto_field_name=True)
    return _authorization_from_dict(data, native_type)


def _datetime_from_proto_json(value: Any) -> _dt.datetime | None:
    if value is None or isinstance(value, _dt.datetime):
        return value
    if isinstance(value, str):
        return _dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
    raise TypeError(f"authorization: expected datetime or timestamp string, got {type(value).__name__}")


def _datetime_to_proto_json(value: _dt.datetime) -> str:
    if value.tzinfo is None:
        value = value.replace(tzinfo=_dt.timezone.utc)
    return value.astimezone(_dt.timezone.utc).isoformat().replace("+00:00", "Z")


class AuthorizationProtocol(Protocol):
    """Fakeable client contract for host authorization calls."""

    def evaluate(self, request: AccessEvaluationRequest) -> AccessDecision:
        """Evaluate one authorization request."""

    def evaluate_many(
        self, request: AccessEvaluationsRequest
    ) -> AccessEvaluationsResponse:
        """Evaluate multiple authorization requests."""

    def search_resources(
        self, request: ResourceSearchRequest
    ) -> ResourceSearchResponse:
        """Search resources visible to a subject for an action."""

    def search_subjects(self, request: SubjectSearchRequest) -> SubjectSearchResponse:
        """Search subjects related to a resource and action."""

    def effective_search_resources(
        self, request: ResourceSearchRequest
    ) -> ResourceSearchResponse:
        """Search effective resources through inherited relationships."""

    def effective_search_subjects(
        self, request: EffectiveSubjectSearchRequest
    ) -> EffectiveSubjectSearchResponse:
        """Search effective subjects or subject sets."""

    def search_actions(self, request: ActionSearchRequest) -> ActionSearchResponse:
        """Search actions available between a subject and resource."""

    def expand(self, request: ExpandRequest) -> ExpandResponse:
        """Expand one resource relation."""

    def read_relationships(
        self, request: ReadRelationshipsRequest
    ) -> ReadRelationshipsResponse:
        """Read authorization relationships."""

    def write_relationships(self, request: WriteRelationshipsRequest) -> None:
        """Write and delete authorization relationships."""

    def get_metadata(self) -> AuthorizationMetadata:
        """Return host authorization metadata."""

    def get_active_model(self) -> GetActiveModelResponse:
        """Return the active authorization model."""

    def list_models(self, request: ListModelsRequest) -> ListModelsResponse:
        """List authorization model references."""

    def write_model(self, request: WriteModelRequest) -> AuthorizationModelRef:
        """Write an authorization model."""


class Authorization:
    """Transport client for the host authorization provider."""

    def __new__(
        cls,
        socket_target: str | None = None,
        relay_token: str | None = None,
        *,
        _shared: bool = False,
    ) -> Authorization:
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
        self._channel = _authorization_channel(target, token=token)
        self._stub = authorization_pb2_grpc.AuthorizationProviderStub(self._channel)
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

    def evaluate(self, request: Any) -> Any:
        """Evaluate one authorization request."""

        return _authorization_from_message(self._stub.Evaluate(
            _authorization_message(
                request,
                authorization_pb2.AccessEvaluationRequest,
            )
        ))

    def evaluate_many(self, request: Any) -> Any:
        """Evaluate multiple authorization requests."""

        return _authorization_from_message(self._stub.EvaluateMany(
            _authorization_message(
                request,
                authorization_pb2.AccessEvaluationsRequest,
            )
        ))

    def search_resources(self, request: Any) -> Any:
        """Search resources visible to a subject for an action."""

        return _authorization_from_message(self._stub.SearchResources(
            _authorization_message(
                request,
                authorization_pb2.ResourceSearchRequest,
            )
        ))

    def search_subjects(self, request: Any) -> Any:
        """Search subjects related to a resource and action."""

        return _authorization_from_message(self._stub.SearchSubjects(
            _authorization_message(
                request,
                authorization_pb2.SubjectSearchRequest,
            )
        ))

    def effective_search_resources(self, request: Any) -> Any:
        """Search effective resources visible through inherited relationships."""

        return _authorization_from_message(self._stub.EffectiveSearchResources(
            _authorization_message(
                request,
                authorization_pb2.ResourceSearchRequest,
            )
        ))

    def effective_search_subjects(self, request: Any) -> Any:
        """Search effective subjects or subject sets for a resource and action."""

        return _authorization_from_message(self._stub.EffectiveSearchSubjects(
            _authorization_message(
                request,
                authorization_pb2.EffectiveSubjectSearchRequest,
            )
        ))

    def search_actions(self, request: Any) -> Any:
        """Search actions available between a subject and resource."""

        return _authorization_from_message(self._stub.SearchActions(
            _authorization_message(
                request,
                authorization_pb2.ActionSearchRequest,
            )
        ))

    def expand(self, request: Any) -> Any:
        """Expand one resource relation into contributing relationship targets."""

        return _authorization_from_message(self._stub.Expand(
            _authorization_message(
                request,
                authorization_pb2.ExpandRequest,
            )
        ))

    def read_relationships(self, request: Any) -> Any:
        """Read authorization relationships matching a request."""

        return _authorization_from_message(self._stub.ReadRelationships(
            _authorization_message(
                request,
                authorization_pb2.ReadRelationshipsRequest,
            )
        ))

    def write_relationships(self, request: Any) -> None:
        """Write authorization relationships."""

        self._stub.WriteRelationships(
            _authorization_message(
                request,
                authorization_pb2.WriteRelationshipsRequest,
            )
        )

    def get_metadata(self) -> Any:
        """Return host authorization provider metadata."""

        return _authorization_from_message(self._stub.GetMetadata(empty_pb2.Empty()))

    def get_active_model(self) -> Any:
        """Return the active authorization model."""

        return _authorization_from_message(self._stub.GetActiveModel(empty_pb2.Empty()))

    def list_models(self, request: Any) -> Any:
        """List authorization model references."""

        return _authorization_from_message(self._stub.ListModels(
            _authorization_message(
                request,
                authorization_pb2.ListModelsRequest,
            )
        ))

    def write_model(self, request: Any) -> Any:
        """Write an authorization model."""

        return _authorization_from_message(self._stub.WriteModel(
            _authorization_message(
                request,
                authorization_pb2.WriteModelRequest,
            )
        ))

    def __enter__(self) -> Authorization:
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()


def _shared_authorization_client() -> Authorization:
    """Return a cached client for the host authorization provider."""

    target = _resolve_authorization_socket_target()
    token = os.environ.get(ENV_HOST_SERVICE_TOKEN, "").strip()
    shared = _shared_authorization_transport
    with _shared_authorization_lock:
        client = shared.get("client")
        if (
            client is not None
            and shared.get("target") == target
            and shared.get("token") == token
        ):
            return client

        client = Authorization(target, token, _shared=True)
        stale = shared.get("client")
        shared["target"] = target
        shared["token"] = token
        shared["client"] = client
        if stale is not None:
            stale._close_channel()
        return client

def _resolve_authorization_socket_target(
    socket_target: str | None = None,
) -> str:
    target = (
        socket_target
        if socket_target is not None
        else os.environ.get(ENV_HOST_SERVICE_SOCKET, "")
    ).strip()
    if not target:
        raise RuntimeError(f"authorization: {ENV_HOST_SERVICE_SOCKET} is not set")
    return target


def _authorization_channel(raw_target: str, *, token: str = "") -> grpc.Channel:
    target = raw_target.strip()
    if not target:
        raise RuntimeError("authorization: transport target is required")
    if target.startswith("tcp://"):
        address = target[len("tcp://") :].strip()
        if not address:
            raise RuntimeError(
                f"authorization: tcp target {raw_target!r} is missing host:port"
            )
        return _with_authorization_relay_token(
            insecure_internal_channel(internal_channel_target("tcp", address)),
            token,
        )
    if target.startswith("tls://"):
        address = target[len("tls://") :].strip()
        if not address:
            raise RuntimeError(
                f"authorization: tls target {raw_target!r} is missing host:port"
            )
        return _with_authorization_relay_token(
            secure_internal_channel(internal_channel_target("tls", address)),
            token,
        )
    if target.startswith("unix://"):
        socket_path = target[len("unix://") :].strip()
        if not socket_path:
            raise RuntimeError(
                f"authorization: unix target {raw_target!r} is missing a socket path"
            )
        return _with_authorization_relay_token(
            insecure_internal_channel(internal_channel_target("unix", socket_path)),
            token,
        )
    if "://" in target:
        parsed = _urlparse.urlparse(target)
        raise RuntimeError(
            f"authorization: unsupported target scheme {parsed.scheme!r}"
        )
    return _with_authorization_relay_token(
        insecure_internal_channel(internal_channel_target("unix", target)),
        token,
    )


def _with_authorization_relay_token(
    channel: grpc.Channel,
    token: str,
) -> grpc.Channel:
    token = token.strip()
    if not token:
        return channel
    return grpc.intercept_channel(channel, _RelayTokenInterceptor(token))


class _ClientCallDetails(grpc.ClientCallDetails):
    def __init__(
        self,
        method: str,
        timeout: float | None,
        metadata: Any,
        credentials: Any,
        wait_for_ready: bool | None,
        compression: Any,
    ) -> None:
        self.method = method
        self.timeout = timeout
        self.metadata = metadata
        self.credentials = credentials
        self.wait_for_ready = wait_for_ready
        self.compression = compression


class _RelayTokenInterceptor(grpc.UnaryUnaryClientInterceptor):
    def __init__(self, token: str) -> None:
        self._token = token

    def intercept_unary_unary(
        self,
        continuation: Any,
        client_call_details: grpc.ClientCallDetails,
        request: Any,
    ) -> Any:
        fields = cast(_ClientCallDetailsFields, client_call_details)
        metadata = list(fields.metadata or [])
        metadata.append((_AUTHORIZATION_RELAY_TOKEN_HEADER, self._token))
        updated_details = _ClientCallDetails(
            method=fields.method,
            timeout=fields.timeout,
            metadata=metadata,
            credentials=fields.credentials,
            wait_for_ready=fields.wait_for_ready,
            compression=fields.compression,
        )
        return continuation(updated_details, request)


class _ClientCallDetailsFields(Protocol):
    method: str
    timeout: float | None
    metadata: Any
    credentials: Any
    wait_for_ready: bool | None
    compression: Any
