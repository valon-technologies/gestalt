import asyncio
import dataclasses
import inspect
import json
import pathlib
import re
import types
from dataclasses import MISSING
from typing import Any, Generic, TypeVar, Union, dataclass_transform, get_args, get_origin, get_type_hints

import yaml

ENV_WRITE_CATALOG = "GESTALT_PLUGIN_WRITE_CATALOG"

T = TypeVar("T")


@dataclasses.dataclass(slots=True)
class Request:
    token: str = ""
    connection_params: dict[str, str] = dataclasses.field(default_factory=dict)

    def connection_param(self, name: str) -> str:
        return self.connection_params.get(name, "")


@dataclasses.dataclass(slots=True)
class Response(Generic[T]):
    status: int
    body: T


def OK(body: T) -> Response[T]:
    return Response(status=200, body=body)


def field(
    *,
    description: str = "",
    default: Any = MISSING,
    default_factory: Any = MISSING,
    required: bool | None = None,
) -> Any:
    metadata: dict[str, Any] = {}
    if description:
        metadata["description"] = description
    if required is not None:
        metadata["required"] = required

    kwargs: dict[str, Any] = {"metadata": metadata}
    if default is not MISSING:
        kwargs["default"] = default
    if default_factory is not MISSING:
        kwargs["default_factory"] = default_factory
    return dataclasses.field(**kwargs)


@dataclass_transform(field_specifiers=(field,))
class Model:
    def __init_subclass__(cls, **kwargs: Any) -> None:
        super().__init_subclass__(**kwargs)
        if "__dataclass_fields__" not in cls.__dict__:
            dataclasses.dataclass(cls)


@dataclasses.dataclass(slots=True)
class _Operation:
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


class Plugin:
    def __init__(self, name: str) -> None:
        self.name = _slug_name(name)
        self._operations: dict[str, _Operation] = {}
        self._configure_handler: Any = None

    @classmethod
    def from_manifest(cls, path: str | pathlib.Path) -> "Plugin":
        manifest_path = pathlib.Path(path)
        if not manifest_path.is_absolute() and not manifest_path.exists():
            caller = inspect.stack()[1].filename
            manifest_path = pathlib.Path(caller).resolve().parent / manifest_path
        name = _derive_name_from_manifest(manifest_path)
        return cls(name)

    def configure(self, func: Any) -> Any:
        self._configure_handler = func
        return func

    def operation(
        self,
        *,
        id: str,
        method: str = "POST",
        title: str = "",
        description: str = "",
        tags: list[str] | None = None,
        read_only: bool = False,
        visible: bool | None = None,
    ) -> Any:
        op_id = id.strip()
        if not op_id:
            raise ValueError("operation id is required")

        def decorator(func: Any) -> Any:
            if op_id in self._operations:
                raise ValueError(f"duplicate operation id {op_id!r}")

            input_type, takes_request = _inspect_handler(func)
            self._operations[op_id] = _Operation(
                id=op_id,
                method=(method or "POST").upper(),
                title=title.strip(),
                description=description.strip(),
                tags=list(tags or []),
                read_only=read_only,
                visible=visible,
                handler=func,
                input_type=input_type,
                takes_request=takes_request,
            )
            return func

        return decorator

    def configure_provider(self, name: str, config: dict[str, Any]) -> None:
        if self._configure_handler is None:
            return
        _maybe_await(self._configure_handler(name, config))

    def execute(self, operation: str, params: dict[str, Any], request: Request) -> tuple[int, str]:
        op = self._operations.get(operation)
        if op is None:
            return 404, json.dumps({"error": "unknown operation"})

        args: list[Any] = []
        if op.input_type is not None:
            args.append(_decode_input(op.input_type, params))
        if op.takes_request:
            args.append(request)

        result = _maybe_await(op.handler(*args))
        if isinstance(result, Response):
            status = result.status if result.status is not None else 200
            body = result.body
        else:
            status = 200
            body = result
        return status, _json_body(body)

    def catalog_dict(self) -> dict[str, Any]:
        return {
            "name": self.name,
            "operations": [_catalog_operation(op) for op in self._operations.values()],
        }

    def write_catalog(self, path: str | pathlib.Path) -> None:
        catalog_path = pathlib.Path(path)
        catalog_path.parent.mkdir(parents=True, exist_ok=True)
        catalog_path.write_text(
            yaml.dump(
                self.catalog_dict(),
                Dumper=_CatalogDumper,
                sort_keys=False,
                default_flow_style=False,
                allow_unicode=True,
            ),
            encoding="utf-8",
        )

    def serve(self) -> None:
        from . import _runtime

        _runtime.serve(self)


def _inspect_handler(func: Any) -> tuple[Any, bool]:
    signature = inspect.signature(func)
    params = list(signature.parameters.values())
    type_hints = get_type_hints(func)

    if len(params) == 0:
        return None, False
    if len(params) > 2:
        raise TypeError("operation handlers may declare at most two parameters")

    if len(params) == 1:
        annotation = type_hints.get(params[0].name, params[0].annotation)
        if annotation is Request:
            return None, True
        return _normalize_input_type(annotation), False

    first, second = params
    second_annotation = type_hints.get(second.name, second.annotation)
    if second_annotation not in (inspect.Signature.empty, Request):
        raise TypeError("second handler parameter must be annotated as gestalt.Request")
    first_annotation = type_hints.get(first.name, first.annotation)
    return _normalize_input_type(first_annotation), True


def _normalize_input_type(annotation: Any) -> Any:
    if annotation in (inspect.Signature.empty, None, type(None)):
        return None
    return annotation


def _decode_input(input_type: Any, params: dict[str, Any]) -> Any:
    actual = _strip_optional(input_type)
    if actual in (str, int, float, bool) and isinstance(params, dict):
        if len(params) != 1:
            raise TypeError("primitive operation inputs must be passed as a scalar or single-field object")
        params = next(iter(params.values()))
    return _decode_value(input_type, params)


def _decode_value(annotation: Any, value: Any) -> Any:
    if annotation in (inspect.Signature.empty, Any):
        return value
    if value is None:
        return None

    actual = _strip_optional(annotation)
    origin = get_origin(actual)

    if dataclasses.is_dataclass(actual):
        return _decode_dataclass(actual, value)

    if origin in (list, set):
        item_type = get_args(actual)[0] if get_args(actual) else Any
        if not isinstance(value, list):
            return value
        items = [_decode_value(item_type, item) for item in value]
        return set(items) if origin is set else items

    if origin is tuple:
        if not isinstance(value, (list, tuple)):
            return value
        item_types = get_args(actual)
        if len(item_types) == 2 and item_types[1] is Ellipsis:
            return tuple(_decode_value(item_types[0], item) for item in value)
        return tuple(
            _decode_value(item_types[i] if i < len(item_types) else Any, item)
            for i, item in enumerate(value)
        )

    if origin is dict:
        key_type, value_type = (get_args(actual) + (Any, Any))[:2]
        if not isinstance(value, dict):
            return value
        return {
            _decode_value(key_type, key): _decode_value(value_type, item)
            for key, item in value.items()
        }

    if actual is bool:
        return _decode_bool(value)
    if actual is int:
        return _decode_int(value)
    if actual is float:
        return _decode_float(value)
    if actual is str:
        return _decode_str(value)

    return value


def _decode_dataclass(model_type: Any, value: Any) -> Any:
    if isinstance(value, model_type):
        return value
    if not isinstance(value, dict):
        return value

    type_hints = get_type_hints(model_type)
    kwargs: dict[str, Any] = {}
    for field_def in dataclasses.fields(model_type):
        if field_def.name not in value:
            continue
        annotation = type_hints.get(field_def.name, field_def.type)
        kwargs[field_def.name] = _decode_value(annotation, value[field_def.name])
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


def _maybe_await(value: Any) -> Any:
    if inspect.isawaitable(value):
        return asyncio.run(value)
    return value


def _catalog_operation(op: _Operation) -> dict[str, Any]:
    data: dict[str, Any] = {
        "id": op.id,
        "method": op.method,
    }
    if op.title:
        data["title"] = op.title
    if op.description:
        data["description"] = op.description
    if op.tags:
        data["tags"] = op.tags
    if op.read_only:
        data["read_only"] = True
    if op.visible is not None:
        data["visible"] = op.visible
    params = _catalog_parameters(op.input_type)
    if params:
        data["parameters"] = params
    return data


def _catalog_parameters(input_type: Any) -> list[dict[str, Any]]:
    if input_type is None:
        return []

    input_type = _strip_optional(input_type)
    origin = get_origin(input_type)
    if origin is not None:
        input_type = origin

    if not dataclasses.is_dataclass(input_type):
        return []

    type_hints = get_type_hints(input_type)
    params: list[dict[str, Any]] = []
    for field_def in dataclasses.fields(input_type):
        annotation = type_hints.get(field_def.name, field_def.type)
        param: dict[str, Any] = {
            "name": field_def.name,
            "type": _catalog_type(annotation),
        }
        description = str(field_def.metadata.get("description", "")).strip()
        if description:
            param["description"] = description
        required = field_def.metadata.get("required")
        if required is None:
            required = field_def.default is MISSING and field_def.default_factory is MISSING and not _is_optional_type(annotation)
        if required:
            param["required"] = True
        if field_def.default is not MISSING:
            param["default"] = field_def.default
        params.append(param)
    return params


def _catalog_type(annotation: Any) -> str:
    actual = _strip_optional(annotation)
    origin = get_origin(actual)
    if origin in (list, tuple, set):
        return "array"
    if origin is dict:
        return "object"

    if actual in (str,):
        return "string"
    if actual in (bool,):
        return "boolean"
    if actual in (int,):
        return "integer"
    if actual in (float,):
        return "number"
    if dataclasses.is_dataclass(actual):
        return "object"
    if actual in (dict, list, tuple, set):
        return "object" if actual is dict else "array"
    return "object"


def _strip_optional(annotation: Any) -> Any:
    origin = get_origin(annotation)
    if origin not in (Union, types.UnionType):
        return annotation
    args = [arg for arg in get_args(annotation) if arg is not type(None)]
    if len(args) == 1:
        return args[0]
    return annotation


def _is_optional_type(annotation: Any) -> bool:
    origin = get_origin(annotation)
    return origin in (Union, types.UnionType) and type(None) in get_args(annotation)


def _json_body(value: Any) -> str:
    return json.dumps(_json_value(value), separators=(",", ":"))


def _json_value(value: Any) -> Any:
    if dataclasses.is_dataclass(value):
        return {field.name: _json_value(getattr(value, field.name)) for field in dataclasses.fields(value)}
    if isinstance(value, pathlib.Path):
        return str(value)
    if isinstance(value, dict):
        return {_json_value(key): _json_value(item) for key, item in value.items()}
    if isinstance(value, (list, tuple, set)):
        return [_json_value(item) for item in value]
    return value


def _derive_name_from_manifest(path: pathlib.Path) -> str:
    manifest_path = path
    if manifest_path.is_dir():
        manifest_path = manifest_path / "plugin.yaml"

    try:
        text = manifest_path.read_text(encoding="utf-8")
    except OSError:
        return _slug_name(manifest_path.parent.name or "plugin")

    if manifest_path.suffix.lower() == ".json":
        try:
            data = json.loads(text)
        except json.JSONDecodeError:
            return _slug_name(manifest_path.parent.name or "plugin")
        return _manifest_name_from_mapping(data, fallback=manifest_path.parent.name or "plugin")

    for key in ("source", "display_name"):
        match = re.search(rf"(?m)^%s:\s*(.+?)\s*$" % re.escape(key), text)
        if match:
            value = match.group(1).strip().strip("\"'")
            if key == "source":
                return _slug_name(value.rsplit("/", 1)[-1])
            return _slug_name(value)
    return _slug_name(manifest_path.parent.name or "plugin")


def _manifest_name_from_mapping(data: dict[str, Any], fallback: str) -> str:
    source = data.get("source")
    if isinstance(source, str) and source.strip():
        return _slug_name(source.rsplit("/", 1)[-1])
    display_name = data.get("display_name")
    if isinstance(display_name, str) and display_name.strip():
        return _slug_name(display_name)
    return _slug_name(fallback)


def _slug_name(value: str) -> str:
    cleaned = re.sub(r"[^A-Za-z0-9._-]+", "-", value.strip()).strip("-")
    return cleaned or "plugin"


class _CatalogDumper(yaml.SafeDumper):
    def increase_indent(self, flow: bool = False, indentless: bool = False) -> Any:
        return super().increase_indent(flow, False)
