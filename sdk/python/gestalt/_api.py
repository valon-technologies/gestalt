"""Core request, response, and model helpers for authored operations."""

from __future__ import annotations

import builtins
import dataclasses
import json
import threading
from dataclasses import MISSING
from http import HTTPStatus
from typing import TYPE_CHECKING, Any, Final, Generic, TypeVar, cast

from ._protocol import JsonObject, JsonValue

if TYPE_CHECKING:
    from typing_extensions import dataclass_transform

    from gestalt.public.client import GestaltClient
else:
    try:
        from typing import dataclass_transform
    except ImportError:
        try:
            from typing_extensions import dataclass_transform
        except ImportError:

            def dataclass_transform(*args: Any, **kwargs: Any):
                def decorator(cls: type[Any]) -> type[Any]:
                    return cls

                return decorator


if TYPE_CHECKING:
    from ._agent import AgentToolRef
    from ._workflow import Workflow, WorkflowRunContext
    from .agent import Agent
    from .app import App, RequestContext
    from .authorization import Authorization

FIELD_DESCRIPTION_KEY: Final[str] = "description"
FIELD_REQUIRED_KEY: Final[str] = "required"


def parse_subject_id(subject_id: str) -> tuple[str, str] | None:
    """Split a canonical subject ID such as user:ada into kind and id."""
    trimmed = subject_id.strip()
    if ":" not in trimmed:
        return None
    kind, _, id_part = trimmed.partition(":")
    kind = kind.strip()
    id_part = id_part.strip()
    if not kind or not id_part:
        return None
    return kind, id_part


T = TypeVar("T")
ResponseHeaderValue = str | list[str] | tuple[str, ...]
ResponseHeaders = dict[str, ResponseHeaderValue]


@dataclasses.dataclass(slots=True)
class SubjectPermission:
    """One app permission attached to a subject."""

    app: str = ""
    operations: list[str] = dataclasses.field(default_factory=list)
    all_operations: bool = False


@dataclasses.dataclass(slots=True)
class Subject:
    """Identity information attached to an incoming provider request."""

    id: str = ""
    email: str = ""
    display_name: str = ""
    scopes: list[str] = dataclasses.field(default_factory=list)
    permissions: list[SubjectPermission] = dataclasses.field(default_factory=list)


@dataclasses.dataclass(slots=True)
class Credential:
    """Credential metadata resolved by the Gestalt host for the request."""

    mode: str = ""
    subject_id: str = ""
    connection: str = ""
    instance: str = ""


@dataclasses.dataclass(slots=True)
class Access:
    """Authorization context resolved for the request."""

    policy: str = ""
    role: str = ""


@dataclasses.dataclass(slots=True)
class Host:
    """Public host metadata for the request."""

    public_base_url: str = ""


@dataclasses.dataclass(slots=True)
class Request:
    """Host-provided request context for an operation invocation."""

    token: str = ""
    connection_params: dict[str, str] = dataclasses.field(default_factory=dict)
    subject: Subject = dataclasses.field(default_factory=Subject)
    credential: Credential = dataclasses.field(default_factory=Credential)
    access: Access = dataclasses.field(default_factory=Access)
    # Workflow callback metadata uses a JSON-style lowerCamelCase object such
    # as runId, target.steps, trigger.activationId, and trigger.event.specVersion.
    workflow: JsonObject = dataclasses.field(default_factory=dict)
    tool_refs: list[AgentToolRef] = dataclasses.field(default_factory=list)
    tool_refs_set: bool = False
    idempotency_key: str = ""
    host: Host = dataclasses.field(default_factory=Host)
    agent_subject: Subject = dataclasses.field(default_factory=Subject)
    context: Any | None = None

    def connection_param(self, name: str) -> str | None:
        """Return a connection parameter by name if the host supplied it."""

        return self.connection_params.get(name)

    def app(self, *, timeout: float | None = None) -> "App":
        """Return a generated :class:`gestalt.app.App` client bound to this
        request's ambient context."""

        from .app import App

        return App.connect(context=self._native_context(), timeout=timeout)

    def gestalt(self) -> "GestaltClient":
        """Return the public Gestalt client bound to this request's relay context."""

        from gestalt.public.bound import gestalt_from_request

        return gestalt_from_request(self)

    def agent(self, *, timeout: float | None = None) -> "Agent":
        """Return a generated :class:`gestalt.agent.Agent` client bound to this
        request's ambient context."""

        from .agent import Agent

        return Agent.connect(context=self._native_context(), timeout=timeout)

    def workflows(self, *, timeout: float | None = None) -> "Workflow":
        from ._workflow import Workflow

        return Workflow(self, timeout=timeout)

    def authorization(self) -> "Authorization":
        """Return the shared generated :class:`gestalt.authorization.Authorization`
        client for this provider process."""

        return _shared_authorization_client()

    def _native_context(self) -> "RequestContext | None":
        return native_request_context(self.context)

    def workflow_run_context(self) -> "WorkflowRunContext":
        from ._workflow import parse_workflow_run_context

        return parse_workflow_run_context(self.workflow)


def native_request_context(context: Any | None) -> "RequestContext | None":
    """Convert a provider-protocol wire request context into the native
    :class:`RequestContext` accepted by the generated clients."""

    if context is None:
        return None
    from ._codec.app import from_wire_request_context

    return from_wire_request_context(context)


@dataclasses.dataclass(slots=True)
class Response(Generic[T]):
    """Structured operation response with an explicit HTTP status."""

    status: int | None
    body: T
    headers: ResponseHeaders | None = None

    @property
    def ok(self) -> bool:
        """Return whether the response status is in the HTTP 2xx range."""

        status = int(self.status or 0)
        return 200 <= status < 300

    def bytes(self) -> builtins.bytes:
        """Return the response body as bytes."""

        if isinstance(self.body, builtins.bytes):
            return builtins.bytes(self.body)
        if isinstance(self.body, bytearray):
            return builtins.bytes(self.body)
        if isinstance(self.body, str):
            return self.body.encode("utf-8")
        raise TypeError("response body is not bytes or text")

    def text(self) -> str:
        """Decode the response body as UTF-8 text."""

        if isinstance(self.body, str):
            return self.body
        return self.bytes().decode("utf-8", errors="replace")

    def decode_json(self) -> JsonValue:
        """Decode the raw response body without app envelope handling."""

        body = self.bytes()
        if body.strip() == b"":
            return {}
        return cast(JsonValue, json.loads(body))

    def raise_for_status(self) -> Response[T]:
        """Raise ``InvokeError`` when the response has a non-2xx status."""

        if self.ok:
            return self
        from .invoke_support import InvokeError

        try:
            body = self.decode_json()
        except ValueError:
            body = None
        raise InvokeError(
            f"app invoke failed with status {int(self.status or 0)}",
            status=int(self.status or 0),
            body=body,
            raw_body=self.bytes(),
        )


def OK(body: T, headers: ResponseHeaders | None = None) -> Response[T]:
    """Wrap ``body`` in a success response with status ``200 OK``."""

    return Response(status=HTTPStatus.OK, body=body, headers=headers)


class Error(Exception):
    """Application error raised by a provider operation."""

    def __init__(self, status: int | HTTPStatus, message: str = "") -> None:
        self.status = int(status)
        if message:
            self.message = message
        else:
            try:
                self.message = HTTPStatus(self.status).phrase
            except ValueError:
                self.message = ""
        super().__init__(self.message)


def field(
    *,
    description: str = "",
    default: Any = MISSING,
    default_factory: Any = MISSING,
    required: bool | None = None,
) -> Any:
    """Declare a model field with catalog metadata.

    Args:
        description: Human-readable parameter description exported to the
            generated catalog.
        default: Explicit default value for the field.
        default_factory: Callable used to create the default value.
        required: Override the inferred required flag in the generated catalog.
    """

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


_shared_authorization_state: dict[str, Any] = {}
_shared_authorization_lock = threading.Lock()


def _shared_authorization_client() -> "Authorization":
    """Return a process-wide authorization client, rebuilt when the host
    service transport environment changes."""

    import os

    from ._grpc_transport import ENV_HOST_SERVICE_SOCKET, ENV_HOST_SERVICE_TOKEN
    from .authorization import Authorization

    target = os.environ.get(ENV_HOST_SERVICE_SOCKET, "").strip()
    if not target:
        raise RuntimeError(f"authorization: {ENV_HOST_SERVICE_SOCKET} is not set")
    token = os.environ.get(ENV_HOST_SERVICE_TOKEN, "").strip()
    with _shared_authorization_lock:
        client = _shared_authorization_state.get("client")
        if (
            client is not None
            and _shared_authorization_state.get("target") == target
            and _shared_authorization_state.get("token") == token
        ):
            return client
        client = Authorization.connect()
        # Process-lifetime singleton: callers must never close its channel, so a
        # stray close()/__exit__ cannot leave the cache pointing at a dead client.
        client._owns_channel = False
        _shared_authorization_state["target"] = target
        _shared_authorization_state["token"] = token
        _shared_authorization_state["client"] = client
        return client


@dataclass_transform(field_specifiers=(field,))
class Model:
    """Base class for operation input and output models.

    Subclasses are automatically converted into dataclasses, so provider authors
    can declare request and response types with normal annotations:

    .. code-block:: python

        class SearchInput(Model):
            query: str = field(description="Search term")
    """

    def __init_subclass__(cls, **kwargs: Any) -> None:
        super().__init_subclass__(**kwargs)
        if "__dataclass_fields__" not in cls.__dict__:
            dataclasses.dataclass(cls)
