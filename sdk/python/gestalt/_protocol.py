"""Public protobuf helper functions for provider authoring."""

from __future__ import annotations

import datetime as _dt
from collections.abc import Mapping
from typing import Any

from google.protobuf import json_format as _json_format
from google.protobuf import message as _message
from google.protobuf import struct_pb2 as _struct_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2

_UTC = _dt.timezone.utc
struct_pb2: Any = _struct_pb2
timestamp_pb2: Any = _timestamp_pb2


def struct_from_dict(value: Mapping[str, Any] | None = None) -> Any:
    """Convert a JSON-like mapping into a protobuf ``Struct``."""

    message = struct_pb2.Struct()
    if value is None:
        return message
    if not isinstance(value, Mapping):
        raise TypeError("struct_from_dict expects a mapping or None")
    _json_format.ParseDict(dict(value), message)
    return message


def struct_to_dict(value: Any | None) -> dict[str, Any]:
    """Convert a protobuf ``Struct`` into a JSON-like dictionary."""

    if value is None:
        return {}
    return dict(_json_format.MessageToDict(value, preserving_proto_field_name=True))


def value_from_json(value: Any) -> Any:
    """Convert a JSON-like Python value into a protobuf ``Value``."""

    message = struct_pb2.Value()
    _json_format.ParseDict(value, message)
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
