from __future__ import annotations

import os
from collections.abc import Mapping, Sequence
from dataclasses import dataclass, field
from typing import Any, TypeAlias

import grpc

from ._gen.v1 import model_pb2 as _pb
from ._gen.v1 import model_pb2_grpc as _pb_grpc
from ._grpc_transport import host_service_channel
from ._protocol import (
    JsonObjectInput,
    has_field,
    struct_from_dict,
    struct_to_dict,
)
from ._protocol import (
    coerce_model as _coerce,
)
from ._protocol import (
    copy_message as _copy,
)
from ._protocol import (
    input_data as _data,
)

pb: Any = _pb
pb_grpc: Any = _pb_grpc

ENV_MODEL_MANAGER_SOCKET = "GESTALT_MODEL_MANAGER_SOCKET"
ENV_MODEL_MANAGER_SOCKET_TOKEN = f"{ENV_MODEL_MANAGER_SOCKET}_TOKEN"

MODEL_MESSAGE_PART_TYPE_UNSPECIFIED = pb.MODEL_MESSAGE_PART_TYPE_UNSPECIFIED
MODEL_MESSAGE_PART_TYPE_TEXT = pb.MODEL_MESSAGE_PART_TYPE_TEXT

JsonObject: TypeAlias = dict[str, Any]


@dataclass(slots=True)
class ModelMessagePart:
    type: int | str = MODEL_MESSAGE_PART_TYPE_UNSPECIFIED
    text: str = ""


@dataclass(slots=True)
class ModelMessage:
    role: str = ""
    text: str = ""
    parts: Sequence[ModelMessagePart | Mapping[str, Any]] = field(default_factory=list)
    metadata: JsonObjectInput | None = None


@dataclass(slots=True)
class ModelSubjectContext:
    subject_id: str = ""
    subject_kind: str = ""
    credential_subject_id: str = ""
    display_name: str = ""
    auth_source: str = ""


@dataclass(slots=True)
class ModelUsage:
    input_tokens: int = 0
    output_tokens: int = 0
    total_tokens: int = 0


@dataclass(slots=True)
class ModelProviderCapabilities:
    text_output: bool = True
    structured_output: bool = False
    usage: bool = False
    parallel_requests: bool = False


@dataclass(slots=True)
class GetModelProviderCapabilitiesRequest:
    pass


@dataclass(slots=True)
class GenerateModelRequest:
    provider_name: str = ""
    model: str = ""
    messages: Sequence[ModelMessage | Mapping[str, Any]] = field(default_factory=list)
    response_schema: JsonObjectInput | None = None
    model_options: JsonObjectInput | None = None
    metadata: JsonObjectInput | None = None
    subject: ModelSubjectContext | Mapping[str, Any] | None = None
    caller_plugin_name: str = ""


@dataclass(slots=True)
class GenerateModelResponse:
    message: ModelMessage | Mapping[str, Any] | None = None
    output_text: str = ""
    structured_output: JsonObjectInput | None = None
    finish_reason: str = ""
    usage: ModelUsage | Mapping[str, Any] | None = None
    provider_metadata: JsonObjectInput | None = None


@dataclass(slots=True)
class ModelManagerGenerate:
    provider_name: str = ""
    model: str = ""
    messages: Sequence[ModelMessage | Mapping[str, Any]] = field(default_factory=list)
    response_schema: JsonObjectInput | None = None
    model_options: JsonObjectInput | None = None
    metadata: JsonObjectInput | None = None


def generate_model_request_from_proto(value: Any) -> GenerateModelRequest:
    return GenerateModelRequest(
        provider_name=value.provider_name,
        model=value.model,
        messages=[model_message_from_proto(message) for message in value.messages],
        response_schema=struct_to_dict(value.response_schema)
        if has_field(value, "response_schema")
        else None,
        model_options=struct_to_dict(value.model_options)
        if has_field(value, "model_options")
        else None,
        metadata=struct_to_dict(value.metadata) if has_field(value, "metadata") else None,
        subject=model_subject_context_from_proto(value.subject)
        if has_field(value, "subject")
        else None,
        caller_plugin_name=value.caller_plugin_name,
    )


def generate_model_response_to_proto(
    value: GenerateModelResponse | Mapping[str, Any],
) -> Any:
    response = _coerce(value, GenerateModelResponse, "GenerateModelResponse")
    out = pb.GenerateModelResponse(
        output_text=response.output_text,
        finish_reason=response.finish_reason,
    )
    _copy_message(out, "message", model_message_to_proto(response.message))
    _copy_message(out, "usage", model_usage_to_proto(response.usage))
    _copy_struct(out, "structured_output", response.structured_output)
    _copy_struct(out, "provider_metadata", response.provider_metadata)
    return out


def generate_model_response_from_proto(value: Any) -> GenerateModelResponse:
    return GenerateModelResponse(
        message=model_message_from_proto(value.message)
        if has_field(value, "message")
        else None,
        output_text=value.output_text,
        structured_output=struct_to_dict(value.structured_output)
        if has_field(value, "structured_output")
        else None,
        finish_reason=value.finish_reason,
        usage=model_usage_from_proto(value.usage) if has_field(value, "usage") else None,
        provider_metadata=struct_to_dict(value.provider_metadata)
        if has_field(value, "provider_metadata")
        else None,
    )


def model_provider_capabilities_to_proto(
    value: ModelProviderCapabilities | Mapping[str, Any],
) -> Any:
    capabilities = _coerce(
        value, ModelProviderCapabilities, "ModelProviderCapabilities"
    )
    return pb.ModelProviderCapabilities(
        text_output=capabilities.text_output,
        structured_output=capabilities.structured_output,
        usage=capabilities.usage,
        parallel_requests=capabilities.parallel_requests,
    )


def model_message_from_proto(value: Any) -> ModelMessage:
    return ModelMessage(
        role=value.role,
        text=value.text,
        parts=[model_message_part_from_proto(part) for part in value.parts],
        metadata=struct_to_dict(value.metadata)
        if has_field(value, "metadata")
        else None,
    )


def model_message_to_proto(value: ModelMessage | Mapping[str, Any] | None) -> Any | None:
    if value is None:
        return None
    message = _coerce(value, ModelMessage, "ModelMessage")
    out = pb.ModelMessage(
        role=message.role,
        text=message.text,
        parts=[model_message_part_to_proto(part) for part in message.parts],
    )
    _copy_struct(out, "metadata", message.metadata)
    return out


def model_message_part_from_proto(value: Any) -> ModelMessagePart:
    return ModelMessagePart(type=value.type, text=value.text)


def model_message_part_to_proto(value: ModelMessagePart | Mapping[str, Any]) -> Any:
    part = _coerce(value, ModelMessagePart, "ModelMessagePart")
    part_type = _model_message_part_type(part.type)
    if part_type == pb.MODEL_MESSAGE_PART_TYPE_UNSPECIFIED and part.text:
        part_type = pb.MODEL_MESSAGE_PART_TYPE_TEXT
    return pb.ModelMessagePart(type=part_type, text=part.text)


def model_subject_context_from_proto(value: Any) -> ModelSubjectContext:
    return ModelSubjectContext(
        subject_id=value.subject_id,
        subject_kind=value.subject_kind,
        credential_subject_id=value.credential_subject_id,
        display_name=value.display_name,
        auth_source=value.auth_source,
    )


def model_subject_context_to_proto(
    value: ModelSubjectContext | Mapping[str, Any] | None,
) -> Any | None:
    if value is None:
        return None
    subject = _coerce(value, ModelSubjectContext, "ModelSubjectContext")
    return pb.ModelSubjectContext(
        subject_id=subject.subject_id,
        subject_kind=subject.subject_kind,
        credential_subject_id=subject.credential_subject_id,
        display_name=subject.display_name,
        auth_source=subject.auth_source,
    )


def model_usage_from_proto(value: Any) -> ModelUsage:
    return ModelUsage(
        input_tokens=value.input_tokens,
        output_tokens=value.output_tokens,
        total_tokens=value.total_tokens,
    )


def model_usage_to_proto(value: ModelUsage | Mapping[str, Any] | None) -> Any | None:
    if value is None:
        return None
    usage = _coerce(value, ModelUsage, "ModelUsage")
    return pb.ModelUsage(
        input_tokens=usage.input_tokens,
        output_tokens=usage.output_tokens,
        total_tokens=usage.total_tokens,
    )


def _model_manager_generate_request(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.ModelManagerGenerateRequest):
        return _copy(value)
    data = _data(value, kwargs)
    request = pb.ModelManagerGenerateRequest(
        provider_name=data.get("provider_name", ""),
        model=data.get("model", ""),
        messages=[model_message_to_proto(message) for message in data.get("messages", [])],
    )
    _copy_struct(request, "response_schema", data.get("response_schema"))
    _copy_struct(request, "model_options", data.get("model_options"))
    _copy_struct(request, "metadata", data.get("metadata"))
    return request


class ModelManager:
    """Client for stateless model inference through the host model manager."""

    def __init__(self, request_or_token: Any) -> None:
        invocation_token = (
            request_or_token
            if isinstance(request_or_token, str)
            else getattr(request_or_token, "invocation_token", "")
        )
        trimmed_token = str(invocation_token).strip()
        if not trimmed_token:
            raise RuntimeError("model manager: invocation token is not available")

        target = os.environ.get(ENV_MODEL_MANAGER_SOCKET, "")
        if not target:
            raise RuntimeError(f"model manager: {ENV_MODEL_MANAGER_SOCKET} is not set")
        relay_token = os.environ.get(ENV_MODEL_MANAGER_SOCKET_TOKEN, "")

        self._channel = host_service_channel("model manager", target, token=relay_token)
        self._stub = pb_grpc.ModelManagerHostStub(self._channel)
        self._invocation_token = trimmed_token

    def close(self) -> None:
        """Close the underlying gRPC channel."""

        self._channel.close()

    def generate(
        self, request: Any | None = None, **kwargs: Any
    ) -> GenerateModelResponse:
        """Generate one stateless model response."""

        proto_request = _model_manager_generate_request(request, **kwargs)
        proto_request.invocation_token = self._invocation_token
        return generate_model_response_from_proto(
            _grpc_call(self._stub.Generate, proto_request)
        )

    def __enter__(self) -> "ModelManager":
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()


def _copy_message(target: Any, field: str, value: Any | None) -> None:
    if value is not None:
        getattr(target, field).CopyFrom(value)


def _copy_struct(target: Any, field: str, value: JsonObjectInput | None) -> None:
    if value is not None:
        getattr(target, field).CopyFrom(struct_from_dict(value))


def _grpc_call(method: Any, request: Any) -> Any:
    try:
        return method(request)
    except grpc.RpcError:
        raise


def _model_message_part_type(value: Any) -> int:
    if isinstance(value, str):
        text = value.strip()
        if not text:
            return pb.MODEL_MESSAGE_PART_TYPE_UNSPECIFIED
        if text.removeprefix("-").isdigit():
            return int(text)
        normalized = text.upper()
        if normalized == "TEXT":
            return pb.MODEL_MESSAGE_PART_TYPE_TEXT
        enum_value = getattr(pb, normalized, None)
        if isinstance(enum_value, int):
            return enum_value
        enum_value = getattr(pb, f"MODEL_MESSAGE_PART_TYPE_{normalized}", None)
        if isinstance(enum_value, int):
            return enum_value
    return int(value or 0)
