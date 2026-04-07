import asyncio
import dataclasses
import inspect
import json
import pathlib
import traceback
import types
from dataclasses import dataclass
from http import HTTPStatus
from typing import Any, Union, get_args, get_origin, get_type_hints

from ._api import Request, Response


@dataclass(frozen=True, slots=True)
class OperationDefinition:
    id: str
    method: str
    title: str
    description: str
    tags: list[str]
    read_only: bool
    visible: bool | None
    handler: Any
    input_type: Any
    takes_request: bool


def inspect_handler(func: Any) -> tuple[Any, bool]:
    signature = inspect.signature(func)
    parameters = list(signature.parameters.values())
    type_hints = get_type_hints(func)

    if not parameters:
        return None, False
    if len(parameters) > 2:
        raise TypeError("operation handlers may declare at most two parameters")

    if len(parameters) == 1:
        annotation = type_hints.get(parameters[0].name, parameters[0].annotation)
        if annotation is Request:
            return None, True
        return _normalize_input_type(annotation), False

    first_parameter, second_parameter = parameters
    second_annotation = type_hints.get(second_parameter.name, second_parameter.annotation)
    if second_annotation not in (inspect.Signature.empty, Request):
        raise TypeError("second handler parameter must be annotated as gestalt.Request")

    first_annotation = type_hints.get(first_parameter.name, first_parameter.annotation)
    return _normalize_input_type(first_annotation), True


def execute_operation(
    operation: OperationDefinition | None,
    *,
    params: dict[str, Any],
    request: Request,
) -> tuple[int, str]:
    if operation is None:
        return _error_result(HTTPStatus.NOT_FOUND, "unknown operation")

    args: list[Any] = []
    if operation.input_type is not None:
        try:
            args.append(decode_input(operation.input_type, params))
        except Exception as error:
            return _error_result(HTTPStatus.BAD_REQUEST, str(error))
    if operation.takes_request:
        args.append(request)

    try:
        result = maybe_await(operation.handler(*args))
        if isinstance(result, Response):
            status = HTTPStatus.OK if result.status is None else result.status
            body = result.body
        else:
            status = HTTPStatus.OK
            body = result

        return status, json_body(body)
    except Exception as error:
        traceback.print_exception(error)
        return _error_result(HTTPStatus.INTERNAL_SERVER_ERROR, str(error))


def decode_input(input_type: Any, params: dict[str, Any]) -> Any:
    actual_type = strip_optional(input_type)
    if actual_type in (str, int, float, bool) and isinstance(params, dict):
        if len(params) != 1:
            raise TypeError("primitive operation inputs must be passed as a scalar or single-field object")
        params = next(iter(params.values()))

    return _decode_value(input_type, params)


def maybe_await(value: Any) -> Any:
    if inspect.isawaitable(value):
        return asyncio.run(value)
    return value


def json_body(value: Any) -> str:
    return json.dumps(_json_value(value), separators=(",", ":"))


def _error_result(status: HTTPStatus, message: str) -> tuple[int, str]:
    return status, json_body({"error": message})


def _normalize_input_type(annotation: Any) -> Any:
    if annotation in (inspect.Signature.empty, None, type(None)):
        return None
    return annotation


def _decode_value(annotation: Any, value: Any) -> Any:
    if annotation in (inspect.Signature.empty, Any):
        return value
    if value is None:
        return None

    actual_type = strip_optional(annotation)
    origin = get_origin(actual_type)

    if dataclasses.is_dataclass(actual_type):
        return _decode_dataclass(actual_type, value)

    if origin in (list, set):
        item_type = get_args(actual_type)[0] if get_args(actual_type) else Any
        if not isinstance(value, list):
            return value
        items = [_decode_value(item_type, item) for item in value]
        return set(items) if origin is set else items

    if origin is tuple:
        if not isinstance(value, (list, tuple)):
            return value
        item_types = get_args(actual_type)
        if len(item_types) == 2 and item_types[1] is Ellipsis:
            return tuple(_decode_value(item_types[0], item) for item in value)
        return tuple(
            _decode_value(item_types[index] if index < len(item_types) else Any, item)
            for index, item in enumerate(value)
        )

    if origin is dict:
        key_type, value_type = (get_args(actual_type) + (Any, Any))[:2]
        if not isinstance(value, dict):
            return value
        return {
            _decode_value(key_type, key): _decode_value(value_type, item)
            for key, item in value.items()
        }

    if actual_type is bool:
        return _decode_bool(value)
    if actual_type is int:
        return _decode_int(value)
    if actual_type is float:
        return _decode_float(value)
    if actual_type is str:
        return _decode_str(value)

    return value


def _decode_dataclass(model_type: Any, value: Any) -> Any:
    if isinstance(value, model_type):
        return value
    if not isinstance(value, dict):
        return value

    type_hints = get_type_hints(model_type)
    kwargs: dict[str, Any] = {}
    for field_definition in dataclasses.fields(model_type):
        if field_definition.name not in value:
            continue
        annotation = type_hints.get(field_definition.name, field_definition.type)
        kwargs[field_definition.name] = _decode_value(annotation, value[field_definition.name])
    return model_type(**kwargs)


def _decode_bool(value: Any) -> bool:
    if isinstance(value, bool):
        return value
    if isinstance(value, str):
        lowered = value.strip().lower()
        if lowered in ("1", "true", "yes", "on"):
            return True
        if lowered in ("0", "false", "no", "off", ""):
            return False
    return bool(value)


def _decode_int(value: Any) -> int:
    if isinstance(value, bool):
        return int(value)
    if isinstance(value, int):
        return value
    if isinstance(value, float):
        if value.is_integer():
            return int(value)
        raise TypeError(f"expected integer-compatible value, got {value!r}")
    return int(value)


def _decode_float(value: Any) -> float:
    if isinstance(value, bool):
        return float(int(value))
    if isinstance(value, (int, float)):
        return float(value)
    return float(value)


def _decode_str(value: Any) -> str:
    if isinstance(value, str):
        return value
    return str(value)


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


def strip_optional(annotation: Any) -> Any:
    origin = get_origin(annotation)
    if origin not in (Union, types.UnionType):
        return annotation

    args = [arg for arg in get_args(annotation) if arg is not type(None)]
    if len(args) == 1:
        return args[0]
    return annotation


def is_optional_type(annotation: Any) -> bool:
    origin = get_origin(annotation)
    return origin in (Union, types.UnionType) and type(None) in get_args(annotation)
