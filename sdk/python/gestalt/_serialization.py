import dataclasses
import json
import pathlib
from typing import Any


def json_body(value: Any) -> str:
    return json.dumps(_json_value(value), separators=(",", ":"))


def _json_value(value: Any) -> Any:
    if dataclasses.is_dataclass(value):
        return {
            field_definition.name: _json_value(getattr(value, field_definition.name))
            for field_definition in dataclasses.fields(value)
        }
    if isinstance(value, pathlib.Path):
        return str(value)
    if isinstance(value, dict):
        return {_json_value(key): _json_value(item) for key, item in value.items()}
    if isinstance(value, (list, tuple, set)):
        return [_json_value(item) for item in value]
    return value
