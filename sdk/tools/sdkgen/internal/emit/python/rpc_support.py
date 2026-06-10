"""Shared runtime support for generated Gestalt SDK clients: the canonical
error model, native representations for well-known types, and the call
helpers every generated client uses."""

from __future__ import annotations

import datetime
from dataclasses import dataclass
from typing import Any, Callable, Iterable, Iterator, TypeVar

import grpc
from google.protobuf import duration_pb2 as _duration_pb2
from google.protobuf import struct_pb2 as _struct_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.rpc import status_pb2 as _status_pb2

duration_pb2: Any = _duration_pb2
status_pb2: Any = _status_pb2
struct_pb2: Any = _struct_pb2
timestamp_pb2: Any = _timestamp_pb2

_UTC = datetime.timezone.utc

_Native = TypeVar("_Native")
_Result = TypeVar("_Result")
_Wire = TypeVar("_Wire")

JsonValue = bool | int | float | str | list["JsonValue"] | dict[str, "JsonValue"] | None
"""The native representation of google.protobuf.Value."""


@dataclass(frozen=True, slots=True)
class RpcStatus:
    """The native representation of google.rpc.Status carried in response
    payloads, mirroring the canonical error model."""

    code: int = 0
    message: str = ""


class GestaltErrorCode:
    """Canonical SDK error codes, drawn from the standard gRPC status codes."""

    CANCELED = 1
    UNKNOWN = 2
    INVALID_ARGUMENT = 3
    DEADLINE_EXCEEDED = 4
    NOT_FOUND = 5
    ALREADY_EXISTS = 6
    PERMISSION_DENIED = 7
    RESOURCE_EXHAUSTED = 8
    FAILED_PRECONDITION = 9
    ABORTED = 10
    OUT_OF_RANGE = 11
    UNIMPLEMENTED = 12
    INTERNAL = 13
    UNAVAILABLE = 14
    DATA_LOSS = 15
    UNAUTHENTICATED = 16


class GestaltError(Exception):
    """Canonical SDK error: one code, a message, and the underlying cause.
    Transport error types never appear in the public SDK surface."""

    def __init__(self, code: int, message: str) -> None:
        super().__init__(message)
        self.code = code
        self.message = message


def to_gestalt_error(error: BaseException) -> GestaltError:
    """Convert any raised error to the canonical GestaltError."""

    if isinstance(error, GestaltError):
        return error
    if isinstance(error, grpc.Call):
        code = error.code()
        numeric = code.value[0] if code is not None else GestaltErrorCode.UNKNOWN
        return GestaltError(int(numeric), str(error.details() or ""))
    return GestaltError(GestaltErrorCode.UNKNOWN, str(error))


def call_unary(call: Callable[[], _Result]) -> _Result:
    """Invoke one RPC, converting transport errors to GestaltError."""

    try:
        return call()
    except grpc.RpcError as error:
        raise to_gestalt_error(error) from error


def map_recv(
    stream: Iterable[_Wire], convert: Callable[[_Wire], _Native]
) -> Iterator[_Native]:
    """Yield converted response frames, converting transport errors to
    GestaltError."""

    try:
        for frame in stream:
            yield convert(frame)
    except grpc.RpcError as error:
        raise to_gestalt_error(error) from error


def map_send(
    stream: Iterable[_Native], convert: Callable[[_Native], _Wire]
) -> Iterator[_Wire]:
    """Yield converted request frames."""

    for frame in stream:
        yield convert(frame)


def to_wire_timestamp(value: datetime.datetime) -> Any:
    """Convert a datetime (naive values are assumed UTC) to a wire Timestamp."""

    if value.tzinfo is None:
        value = value.replace(tzinfo=_UTC)
    else:
        value = value.astimezone(_UTC)
    out = timestamp_pb2.Timestamp()
    out.FromDatetime(value)
    return out


def from_wire_timestamp(value: Any) -> datetime.datetime:
    """Convert a wire Timestamp to a UTC datetime."""

    return value.ToDatetime(tzinfo=_UTC)


def to_wire_duration(value: datetime.timedelta) -> Any:
    """Convert a timedelta to a wire Duration."""

    out = duration_pb2.Duration()
    out.FromTimedelta(value)
    return out


def from_wire_duration(value: Any) -> datetime.timedelta:
    """Convert a wire Duration to a timedelta."""

    return value.ToTimedelta()


def to_wire_struct(value: dict[str, JsonValue]) -> Any:
    """Convert a JSON object to a wire Struct."""

    out = struct_pb2.Struct()
    for key, item in value.items():
        out.fields[key].CopyFrom(to_wire_value(item))
    return out


def from_wire_struct(value: Any) -> dict[str, JsonValue]:
    """Convert a wire Struct to a JSON object."""

    return {key: from_wire_value(item) for key, item in value.fields.items()}


def to_wire_value(value: JsonValue) -> Any:
    """Convert a JSON value to a wire Value."""

    out = struct_pb2.Value()
    if value is None:
        out.null_value = struct_pb2.NULL_VALUE
    elif isinstance(value, bool):
        out.bool_value = value
    elif isinstance(value, (int, float)):
        out.number_value = float(value)
    elif isinstance(value, str):
        out.string_value = value
    elif isinstance(value, list):
        out.list_value.values.extend(to_wire_value(item) for item in value)
    else:
        for key, item in value.items():
            out.struct_value.fields[key].CopyFrom(to_wire_value(item))
    return out


def from_wire_value(value: Any) -> JsonValue:
    """Convert a wire Value to a JSON value."""

    case = value.WhichOneof("kind")
    if case == "null_value":
        return None
    if case == "number_value":
        return float(value.number_value)
    if case == "string_value":
        return str(value.string_value)
    if case == "bool_value":
        return bool(value.bool_value)
    if case == "struct_value":
        return from_wire_struct(value.struct_value)
    if case == "list_value":
        return [from_wire_value(item) for item in value.list_value.values]
    return None


def to_wire_status(value: RpcStatus) -> Any:
    """Convert a native RpcStatus to a wire google.rpc.Status."""

    return status_pb2.Status(code=value.code, message=value.message)


def from_wire_status(value: Any) -> RpcStatus:
    """Convert a wire google.rpc.Status to a native RpcStatus."""

    return RpcStatus(code=value.code, message=value.message)
