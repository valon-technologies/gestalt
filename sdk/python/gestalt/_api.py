"""Core request, response, and model helpers for authored operations."""

from __future__ import annotations

import dataclasses
from dataclasses import MISSING
from http import HTTPStatus
from typing import TYPE_CHECKING, Any, Final, Generic, TypeVar

from ._protocol import JsonObject

if TYPE_CHECKING:
    from typing_extensions import dataclass_transform
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
    from ._agent import Agent, AgentToolRef
    from ._app_access import AppProtocol
    from ._authorization import Authorization
    from ._workflow import Workflow, WorkflowRunContext

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
class Subject:
    """Identity information attached to an incoming provider request."""

    id: str = ""
    credential_subject_id: str = ""
    email: str = ""



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
    invocation_token: str = ""
    # Workflow callback metadata uses a JSON-style lowerCamelCase object such
    # as runId, target.steps, trigger.scheduleId, and trigger.event.specVersion.
    workflow: JsonObject = dataclasses.field(default_factory=dict)
    tool_refs: list[AgentToolRef] = dataclasses.field(default_factory=list)
    tool_refs_set: bool = False
    idempotency_key: str = ""
    host: Host = dataclasses.field(default_factory=Host)
    agent_subject: Subject = dataclasses.field(default_factory=Subject)

    def connection_param(self, name: str) -> str | None:
        """Return a connection parameter by name if the host supplied it."""

        return self.connection_params.get(name)

    def app(self) -> "AppProtocol":
        from ._app_access import _AppClient

        return _AppClient(self.invocation_token, workflow=self.workflow)

    def agent(self) -> "Agent":
        from ._agent import Agent

        return Agent(self.invocation_token, workflow=self.workflow)

    def workflows(self) -> "Workflow":
        from ._workflow import Workflow

        return Workflow(
            self.invocation_token,
            idempotency_key=self.idempotency_key,
        )

    def workflow_run_context(self) -> "WorkflowRunContext":
        from ._workflow import parse_workflow_run_context

        return parse_workflow_run_context(self.workflow)

    def authorization(self) -> "Authorization":
        from ._authorization import _shared_authorization_client

        return _shared_authorization_client()


@dataclasses.dataclass(slots=True)
class Response(Generic[T]):
    """Structured operation response with an explicit HTTP status."""

    status: int | None
    body: T
    headers: ResponseHeaders | None = None


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
