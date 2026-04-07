import dataclasses
from dataclasses import MISSING
from http import HTTPStatus
from typing import Any, Final, Generic, TypeVar

from typing_extensions import dataclass_transform

FIELD_DESCRIPTION_KEY: Final[str] = "description"
FIELD_REQUIRED_KEY: Final[str] = "required"

T = TypeVar("T")


@dataclasses.dataclass(slots=True)
class Request:
    token: str = ""
    connection_params: dict[str, str] = dataclasses.field(default_factory=dict)

    def connection_param(self, name: str) -> str:
        return self.connection_params.get(name, "")


@dataclasses.dataclass(slots=True)
class Response(Generic[T]):
    status: int | None
    body: T


def _respond(status: int, body: T) -> Response[T]:
    return Response(status=status, body=body)


def OK(body: T) -> Response[T]:
    return _respond(HTTPStatus.OK, body)


def BadRequest(body: T) -> Response[T]:
    return _respond(HTTPStatus.BAD_REQUEST, body)


def Unauthorized(body: T) -> Response[T]:
    return _respond(HTTPStatus.UNAUTHORIZED, body)


def Forbidden(body: T) -> Response[T]:
    return _respond(HTTPStatus.FORBIDDEN, body)


def NotFound(body: T) -> Response[T]:
    return _respond(HTTPStatus.NOT_FOUND, body)


def Conflict(body: T) -> Response[T]:
    return _respond(HTTPStatus.CONFLICT, body)


def InternalServerError(body: T) -> Response[T]:
    return _respond(HTTPStatus.INTERNAL_SERVER_ERROR, body)


def BadGateway(body: T) -> Response[T]:
    return _respond(HTTPStatus.BAD_GATEWAY, body)


def ServiceUnavailable(body: T) -> Response[T]:
    return _respond(HTTPStatus.SERVICE_UNAVAILABLE, body)


def field(
    *,
    description: str = "",
    default: Any = MISSING,
    default_factory: Any = MISSING,
    required: bool | None = None,
) -> Any:
    metadata: dict[str, Any] = {}
    if description:
        metadata[FIELD_DESCRIPTION_KEY] = description
    if required is not None:
        metadata[FIELD_REQUIRED_KEY] = required

    kwargs: dict[str, Any] = {"metadata": metadata}
    if default is not MISSING:
        kwargs["default"] = default
    if default_factory is not MISSING:
        kwargs["default_factory"] = default_factory
    return dataclasses.field(**kwargs)


@dataclass_transform(field_specifiers=(field,))
class Model:
    """Base class for operation input/output types. Subclasses are automatically dataclasses."""

    def __init_subclass__(cls, **kwargs: Any) -> None:
        super().__init_subclass__(**kwargs)
        if "__dataclass_fields__" not in cls.__dict__:
            dataclasses.dataclass(cls)
