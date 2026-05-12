"""Public protobuf helper functions for provider authoring."""

from __future__ import annotations

import dataclasses as _dataclasses
import datetime as _dt
import math as _math
from collections.abc import Mapping, Sequence
from typing import Any, TypeAlias

from google.protobuf import json_format as _json_format
from google.protobuf import message as _message
from google.protobuf import struct_pb2 as _struct_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2

_UTC = _dt.timezone.utc
struct_pb2: Any = _struct_pb2
timestamp_pb2: Any = _timestamp_pb2

Struct: Any = getattr(_struct_pb2, "Struct")
Value: Any = getattr(_struct_pb2, "Value")
Timestamp: Any = getattr(_timestamp_pb2, "Timestamp")
JsonPrimitive: TypeAlias = None | bool | int | float | str
JsonValue: TypeAlias = JsonPrimitive | list["JsonValue"] | dict[str, "JsonValue"]
JsonObject: TypeAlias = dict[str, JsonValue]
JsonInput: TypeAlias = Any
JsonObjectInput: TypeAlias = Any


def struct_from_dict(value: JsonObjectInput | None = None) -> Any:
    """Convert a JSON-like object into a protobuf ``Struct``.

    ``value`` may be a mapping or a dataclass instance whose fields are
    JSON-compatible. Unsupported values raise ``TypeError`` instead of being
    stringified or silently dropped.
    """

    message = struct_pb2.Struct()
    if value is None:
        return message
    normalized = json_from_native(value, path="struct")
    if not isinstance(normalized, dict):
        raise TypeError(
            f"struct_from_dict expects an object, got {type(value).__name__}"
        )
    return _struct_from_normalized_object(normalized)


def struct_to_dict(value: Any | None) -> dict[str, Any]:
    """Convert a protobuf ``Struct`` into a JSON-like dictionary."""

    if value is None:
        return {}
    return dict(_json_format.MessageToDict(value, preserving_proto_field_name=True))


def value_from_json(value: JsonInput) -> Any:
    """Convert a JSON-like Python value into a protobuf ``Value``."""

    message = struct_pb2.Value()
    _json_format.ParseDict(json_from_native(value), message)
    return message


def value_to_json(value: Any | None) -> Any:
    """Convert a protobuf ``Value`` into a JSON-like Python value."""

    if value is None:
        return None
    return _json_format.MessageToDict(value, preserving_proto_field_name=True)


def message_to_dict(
    message: Any,
    *,
    preserving_proto_field_name: bool = True,
) -> Any:
    """Convert a protobuf message into its protobuf JSON dictionary form."""

    return _json_format.MessageToDict(
        message,
        preserving_proto_field_name=preserving_proto_field_name,
    )


def message_from_dict(value: Any, message: Any) -> Any:
    """Parse protobuf JSON data into ``message`` and return the same message."""

    _json_format.ParseDict(value, message)
    return message


def dataclass_mapping(value: Any) -> dict[str, Any] | None:
    """Return a shallow mapping of dataclass field names to values."""

    if _dataclasses.is_dataclass(value) and not isinstance(value, type):
        return {
            field.name: getattr(value, field.name)
            for field in _dataclasses.fields(value)
        }
    return None


def input_data(value: Any | None, kwargs: dict[str, Any]) -> dict[str, Any]:
    """Normalize a dataclass or mapping input and overlay keyword arguments."""

    if value is None:
        return dict(kwargs)
    mapping = dataclass_mapping(value)
    if mapping is None:
        if not isinstance(value, Mapping):
            raise TypeError(
                f"expected a mapping or dataclass, got {type(value).__name__}"
            )
        mapping = dict(value)
    mapping.update(kwargs)
    return mapping


def coerce_model(value: Any, cls: type[Any], field_name: str) -> Any:
    """Coerce a dataclass instance or mapping into ``cls``."""

    if isinstance(value, cls):
        return value
    mapping = dataclass_mapping(value)
    if mapping is not None:
        return cls(**dict(mapping))
    if isinstance(value, Mapping):
        return cls(**dict(value))
    raise TypeError(f"{field_name} must be {cls.__name__} or a mapping")


def copy_message(value: Any) -> Any:
    """Return a protobuf message copy preserving the concrete message type."""

    message = type(value)()
    message.CopyFrom(value)
    return message


def json_from_native(value: JsonInput, *, path: str = "value") -> JsonValue:
    """Return a JSON-compatible Python value from native SDK input.

    Mappings, sequences, and dataclass instances are recursively normalized.
    Datetimes and other rich objects are rejected for generic JSON payloads;
    use timestamp-specific helpers for timestamp fields.
    """

    return _json_from_native(value, path=path, seen=set())


def _struct_from_normalized_object(value: JsonObject) -> Any:
    message = struct_pb2.Struct()
    _json_format.ParseDict(value, message)
    return message


def timestamp_from_datetime(
    value: _dt.datetime | None,
) -> Any | None:
    """Convert a ``datetime`` to a protobuf ``Timestamp``."""

    if value is None:
        return None
    normalized = value if value.tzinfo is not None else value.replace(tzinfo=_UTC)
    timestamp = timestamp_pb2.Timestamp()
    timestamp.FromDatetime(normalized.astimezone(_UTC))
    return timestamp


def datetime_from_timestamp(
    value: Any | None,
) -> _dt.datetime | None:
    """Convert a protobuf ``Timestamp`` to a timezone-aware UTC ``datetime``."""

    if value is None:
        return None
    return value.ToDatetime(tzinfo=_UTC)


def has_field(message: Any, field: str) -> bool:
    """Return whether a protobuf message field with presence is set."""

    if not isinstance(message, _message.Message):
        return False
    try:
        return bool(message.HasField(field))
    except ValueError:
        return False


def which_oneof(message: Any, group: str) -> str | None:
    """Return the selected field name for a protobuf oneof group."""

    if not isinstance(message, _message.Message):
        return None
    try:
        return message.WhichOneof(group)
    except ValueError:
        return None


def _json_from_native(value: Any, *, path: str, seen: set[int]) -> JsonValue:
    if value is None or isinstance(value, bool | str):
        return value
    if isinstance(value, int):
        return value
    if isinstance(value, float):
        if not _math.isfinite(value):
            raise TypeError(f"{path} must be a finite number")
        return value
    if isinstance(value, _dt.datetime | _dt.date | _dt.time):
        raise TypeError(
            f"{path} must be JSON-compatible; use timestamp helpers for datetime values"
        )
    if isinstance(value, bytes | bytearray | memoryview):
        raise TypeError(f"{path} must be JSON-compatible, got bytes")
    if _dataclasses.is_dataclass(value):
        if isinstance(value, type):
            raise TypeError(
                f"{path} must be a dataclass instance, not a dataclass type"
            )
        return _json_object_from_items(
            (
                (field.name, getattr(value, field.name))
                for field in _dataclasses.fields(value)
            ),
            path=path,
            seen=seen,
            container=value,
        )
    if isinstance(value, Mapping):
        return _json_object_from_items(
            value.items(), path=path, seen=seen, container=value
        )
    if isinstance(value, Sequence) and not isinstance(value, str | bytes | bytearray):
        obj_id = id(value)
        if obj_id in seen:
            raise TypeError(f"{path} contains a cycle")
        seen.add(obj_id)
        try:
            return [
                _json_from_native(item, path=f"{path}[{index}]", seen=seen)
                for index, item in enumerate(value)
            ]
        finally:
            seen.remove(obj_id)
    raise TypeError(f"{path} must be JSON-compatible, got {type(value).__name__}")


def _json_object_from_items(
    items: Any,
    *,
    path: str,
    seen: set[int],
    container: Any,
) -> JsonObject:
    obj_id = id(container)
    if obj_id in seen:
        raise TypeError(f"{path} contains a cycle")
    seen.add(obj_id)
    try:
        output: JsonObject = {}
        for key, item in items:
            if not isinstance(key, str):
                raise TypeError(
                    f"{path} keys must be strings, got {type(key).__name__}"
                )
            output[key] = _json_from_native(item, path=f"{path}.{key}", seen=seen)
        return output
    finally:
        seen.remove(obj_id)
