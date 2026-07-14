"""Private fluent workflow-definition authoring helpers."""

from __future__ import annotations

from collections.abc import Callable, Mapping, Sequence
from dataclasses import dataclass, field
from typing import Any, cast

from ._gen.v1 import workflow_pb2 as pb
from ._protocol import dataclass_mapping as _dataclass_mapping
from ._workflow import (
    WorkflowValue as _NativeWorkflowValue,
)
from ._workflow import (
    bound_workflow_target,
    workflow_activation,
    workflow_agent_message,
    workflow_definition_spec,
    workflow_event_match,
    workflow_step,
    workflow_step_agent_turn,
    workflow_step_app_call,
    workflow_step_when,
    workflow_text,
    workflow_value,
)

__all__ = (
    "WorkflowStepAppConfig",
    "WorkflowStepAgentConfig",
    "WorkflowBuilder",
    "define_workflow",
    "event",
    "schedule",
    "text",
)

class _Unset:
    pass


_UNSET = _Unset()


class _TextExpression:
    __slots__ = ("parts",)

    def __init__(
        self, parts: tuple[str | _WorkflowRef | _WorkflowRefProxy, ...]
    ) -> None:
        self.parts = parts


class _WorkflowRef:
    __slots__ = ("kind", "path", "step_id")

    def __init__(self, kind: str, path: str = "", step_id: str = "") -> None:
        self.kind = kind
        self.path = path
        self.step_id = step_id


class _WorkflowRefProxy:
    def __init__(self, kind: str, path: str = "", step_id: str = "") -> None:
        self._ref = _WorkflowRef(kind, path, step_id)

    def __getattr__(self, name: str) -> _WorkflowRefProxy:
        next_path = f"{self._ref.path}.{name}" if self._ref.path else name
        return _WorkflowRefProxy(self._ref.kind, next_path, self._ref.step_id)

    def __getitem__(self, name: str) -> _WorkflowRefProxy:
        return self.__getattr__(str(name))


class _StepScope:
    def __init__(self) -> None:
        self.input = _WorkflowRefProxy("input")
        self.signal = _WorkflowRefProxy("signal")

    def step_output(self, step_id: str, path: str) -> _WorkflowRef:
        return _WorkflowRef("stepOutput", path, step_id)

    def step_input(self, step_id: str, path: str) -> _WorkflowRef:
        return _WorkflowRef("stepInput", path, step_id)

    @property
    def steps(self) -> _StepOutputsMap:
        return _StepOutputsMap()


class _StepOutputs:
    def __init__(self, step_id: str) -> None:
        self.outputs = _WorkflowRefProxy("stepOutput", "", step_id)
        # Keep the singular spelling as a compatibility alias for early users.
        self.output = self.outputs
        self.inputs = _WorkflowRefProxy("stepInput", "", step_id)
        # Keep the singular spelling as a compatibility alias for early users.
        self.input = self.inputs


class _StepOutputsMap:
    def __getitem__(self, step_id: str) -> _StepOutputs:
        return _StepOutputs(step_id)


class _EventScope:
    def __init__(self) -> None:
        self.data = _WorkflowRefProxy("signal", "data")


@dataclass(frozen=True, slots=True)
class WorkflowStepAppConfig:
    """Typed configuration for an app action in a workflow step."""

    name: str = ""
    operation: str = ""
    input: Mapping[str, Any] | Callable[[_StepScope], Mapping[str, Any]] | None = None
    connection: str = ""
    instance: str = ""
    credential_mode: str = ""


@dataclass(frozen=True, slots=True)
class WorkflowStepAgentConfig:
    """Typed configuration for an agent turn in a workflow step."""

    provider: str = ""
    model: str = ""
    session_key: str = ""
    prompt: (
        str | _TextExpression | Callable[[_StepScope], str | _TextExpression] | None
    ) = None
    messages: Sequence[Any] = field(default_factory=list)
    tools: Sequence[Any] = field(default_factory=list)
    output: Any = None
    model_options: Mapping[str, Any] | None = None


@dataclass(frozen=True, slots=True)
class _EventActivationConfig:
    type: str
    map_input: Callable[[_EventScope], Mapping[str, Any]] | None = None
    id: str = ""
    source: str = ""
    subject: str = ""
    paused: bool = False


@dataclass(frozen=True, slots=True)
class _ScheduleActivationConfig:
    cron: str
    map_input: Callable[[_WorkflowRefProxy], Mapping[str, Any]] | None = None
    id: str = ""
    timezone: str = ""
    paused: bool = False


class WorkflowBuilder:
    def __init__(self, *, id: str, run_as: str, paused: bool = False) -> None:
        workflow_id = id.strip()
        workflow_run_as = run_as.strip()
        if not workflow_run_as:
            raise ValueError("define_workflow requires run_as")
        if not workflow_id:
            raise ValueError("define_workflow requires id")
        self._id = workflow_id
        self._run_as = workflow_run_as
        self._paused = paused
        self._activations: list[Any] = []
        self._steps: list[Any] = []

    def on(
        self, activation: _EventActivationConfig | _ScheduleActivationConfig
    ) -> WorkflowBuilder:
        if isinstance(activation, _EventActivationConfig):
            activation_id = activation.id.strip() or activation.type
            mapped = (
                activation.map_input(_EventScope())
                if activation.map_input is not None
                else None
            )
            self._activations.append(
                workflow_activation(
                    id=activation_id,
                    paused=activation.paused,
                    event={
                        "match": workflow_event_match(
                            type=activation.type,
                            source=activation.source,
                            subject=activation.subject,
                        )
                    },
                    input=(
                        None
                        if mapped is None
                        else _capture_object_value(mapped, "signal")
                    ),
                )
            )
            return self

        if not isinstance(activation, _ScheduleActivationConfig):
            raise TypeError("workflow activation must be event() or schedule()")
        activation_id = activation.id.strip() or activation.cron
        mapped = (
            activation.map_input(_WorkflowRefProxy("input"))
            if activation.map_input is not None
            else None
        )
        self._activations.append(
            workflow_activation(
                id=activation_id,
                paused=activation.paused,
                schedule={"cron": activation.cron, "timezone": activation.timezone},
                input=(
                    None if mapped is None else _capture_object_value(mapped, "input")
                ),
            )
        )
        return self

    def step(
        self,
        step_id: str,
        config: Mapping[str, Any] | None = None,
        *,
        inputs: Any = _UNSET,
        app: WorkflowStepAppConfig | Mapping[str, Any] | None | _Unset = _UNSET,
        agent: WorkflowStepAgentConfig | Mapping[str, Any] | None | _Unset = _UNSET,
        when: Mapping[str, Any] | Any | None = _UNSET,
        timeout_seconds: int | None | object = _UNSET,
        metadata: Mapping[str, Any] | None | object = _UNSET,
    ) -> WorkflowBuilder:
        """Append a step, preferably using typed keyword arguments.

        A mapping in the second positional argument remains accepted for
        compatibility with the initial builder implementation.
        """

        if config is not None:
            if any(
                value is not _UNSET
                for value in (inputs, app, agent, when, timeout_seconds, metadata)
            ):
                raise TypeError("step config cannot be combined with keyword fields")
            step_data = dict(config)
        else:
            step_data = {
                key: value
                for key, value in (
                    ("inputs", inputs),
                    ("app", app),
                    ("agent", agent),
                    ("when", when),
                    ("timeout_seconds", timeout_seconds),
                    ("metadata", metadata),
                )
                if value is not _UNSET
            }

        scope = _StepScope()
        step_kwargs: dict[str, Any] = {"id": step_id}

        inputs_value = step_data.get("inputs")
        if inputs_value is not None:
            mapped = _resolve_mapped_object(inputs_value, scope)
            step_kwargs["inputs"] = _capture_object(mapped, "input")

        app_value = step_data.get("app")
        agent_value = step_data.get("agent")
        if app_value is not None and agent_value is not None:
            raise ValueError(
                "workflow step cannot configure both app and agent actions"
            )

        if app_value is not None:
            app_data = _message_data(app_value, "WorkflowStepAppConfig")
            mapped_input = app_data.get("input")
            if mapped_input is not None:
                mapped_input = _resolve_mapped_object(mapped_input, scope)
            step_kwargs["app"] = workflow_step_app_call(
                name=app_data.get("name", ""),
                operation=app_data.get("operation", ""),
                input=(
                    None
                    if mapped_input is None
                    else _capture_object_value(mapped_input, "input")
                ),
                connection=app_data.get("connection", ""),
                instance=app_data.get("instance", ""),
                credential_mode=app_data.get("credential_mode", ""),
            )

        if agent_value is not None:
            agent_data = _message_data(agent_value, "WorkflowStepAgentConfig")
            prompt = agent_data.get("prompt")
            if callable(prompt):
                prompt = prompt(scope)
            messages = []
            for message in agent_data.get("messages") or []:
                message_data = _message_data(message, "agent message")
                message_text = message_data.get("text")
                if callable(message_text):
                    message_text = message_text(scope)
                messages.append(
                    workflow_agent_message(
                        role=message_data.get("role", ""),
                        text=workflow_text(_resolve_workflow_prompt(message_text)),
                    )
                )
            step_kwargs["agent"] = workflow_step_agent_turn(
                provider=agent_data.get("provider", ""),
                model=agent_data.get("model", ""),
                session_key=agent_data.get("session_key", ""),
                prompt=(
                    None
                    if prompt is None
                    else workflow_text(_resolve_workflow_prompt(prompt))
                ),
                messages=messages,
                tools=agent_data.get("tools"),
                output=agent_data.get("output"),
                model_options=agent_data.get("model_options"),
            )

        when_value = step_data.get("when")
        if when_value is not None:
            if isinstance(when_value, pb.WorkflowStepWhen):
                step_kwargs["when"] = workflow_step_when(when_value)
            else:
                when_data = _message_data(when_value, "WorkflowStepWhen")
                when_kwargs: dict[str, Any] = {}
                if "value" in when_data:
                    when_kwargs["value"] = _capture_proto(when_data["value"], "input")
                if "equals" in when_data:
                    when_kwargs["equals"] = when_data["equals"]
                step_kwargs["when"] = workflow_step_when(**when_kwargs)

        timeout_value = step_data.get("timeout_seconds")
        if timeout_value is not None:
            step_kwargs["timeout_seconds"] = timeout_value
        metadata_value = step_data.get("metadata")
        if metadata_value is not None:
            step_kwargs["metadata"] = metadata_value

        self._steps.append(workflow_step(**step_kwargs))
        return self

    def to_spec(self) -> Any:
        target = bound_workflow_target(steps=self._steps) if self._steps else None
        return workflow_definition_spec(
            id=self._id,
            run_as=self._run_as,
            paused=self._paused,
            activations=self._activations,
            target=target,
        )


def define_workflow(*, id: str, run_as: str, paused: bool = False) -> WorkflowBuilder:
    return WorkflowBuilder(id=id, run_as=run_as, paused=paused)


def event(
    type_name: str,
    map_input: Callable[[_EventScope], Mapping[str, Any]] | None = None,
    *,
    id: str = "",
    source: str = "",
    subject: str = "",
    paused: bool = False,
) -> _EventActivationConfig:
    return _EventActivationConfig(
        type=type_name,
        map_input=map_input,
        id=id,
        source=source,
        subject=subject,
        paused=paused,
    )


def schedule(
    cron: str,
    map_input: Callable[[_WorkflowRefProxy], Mapping[str, Any]] | None = None,
    *,
    id: str = "",
    timezone: str = "",
    paused: bool = False,
) -> _ScheduleActivationConfig:
    return _ScheduleActivationConfig(
        cron=cron,
        map_input=map_input,
        id=id,
        timezone=timezone,
        paused=paused,
    )


def text(*parts: str | _WorkflowRef | _WorkflowRefProxy) -> _TextExpression:
    """Compose workflow prompt/message text from literals and references."""

    return _TextExpression(parts)


def _message_data(value: Any, field_name: str) -> dict[str, Any]:
    if isinstance(value, pb.WorkflowAgentMessage):
        return {
            "role": value.role,
            "text": value.text.template if value.HasField("text") else None,
        }
    mapping = _dataclass_mapping(value)
    if mapping is not None:
        return dict(mapping)
    if isinstance(value, Mapping):
        return dict(value)
    raise TypeError(f"{field_name} must be a dataclass or mapping")


def _capture_object(value: Mapping[str, Any], default_kind: str) -> dict[str, Any]:
    if not isinstance(value, Mapping):
        raise TypeError("workflow mapped input must be a mapping")
    return {key: _capture_proto(item, default_kind) for key, item in value.items()}


def _resolve_mapped_object(value: Any, scope: _StepScope) -> Mapping[str, Any]:
    if callable(value):
        value = cast(Callable[[_StepScope], Mapping[str, Any]], value)(scope)
    if not isinstance(value, Mapping):
        raise TypeError("workflow mapped input must be a mapping or callback")
    return value


def _capture_object_value(value: Mapping[str, Any], default_kind: str) -> Any:
    return pb.WorkflowValue(
        object=pb.WorkflowObject(fields=_capture_object(value, default_kind))
    )


def _capture_proto(value: Any, default_kind: str) -> Any:
    if isinstance(value, _WorkflowRef):
        return _workflow_ref_value(value)
    if isinstance(value, _WorkflowRefProxy):
        return _workflow_ref_value(value._ref)
    if isinstance(value, (pb.WorkflowValue, _NativeWorkflowValue)):
        return workflow_value(value)
    if isinstance(value, list | tuple):
        return pb.WorkflowValue(
            array=pb.WorkflowArray(
                values=[_capture_proto(item, default_kind) for item in value]
            )
        )
    if isinstance(value, Mapping):
        return pb.WorkflowValue(
            object=pb.WorkflowObject(
                fields={
                    key: _capture_proto(item, default_kind)
                    for key, item in value.items()
                }
            )
        )
    return workflow_value(literal=value)


def _workflow_ref_value(ref: _WorkflowRef) -> Any:
    if ref.kind == "stepOutput":
        return pb.WorkflowValue(
            step_output=pb.WorkflowStepOutputSource(
                step_id=ref.step_id,
                path=ref.path,
            )
        )
    if ref.kind == "stepInput":
        return pb.WorkflowValue(
            step_input=pb.WorkflowStepInputSource(
                step_id=ref.step_id,
                path=ref.path,
            )
        )
    if ref.kind == "input":
        return pb.WorkflowValue(input=pb.WorkflowPathSource(path=ref.path))
    if ref.kind == "signal":
        return pb.WorkflowValue(signal=pb.WorkflowPathSource(path=ref.path))
    raise ValueError(f"unsupported workflow ref kind: {ref.kind}")


def _resolve_workflow_prompt(value: Any) -> str:
    if isinstance(value, str):
        return value
    if isinstance(value, _TextExpression):
        rendered = ""
        for part in value.parts:
            rendered += (
                part if isinstance(part, str) else _ref_to_template_placeholder(part)
            )
        return rendered
    raise ValueError("workflow prompt must be a string or text() expression")


def _ref_to_template_placeholder(ref: _WorkflowRef | _WorkflowRefProxy) -> str:
    if isinstance(ref, _WorkflowRefProxy):
        ref = ref._ref
    if ref.kind == "input":
        return f"${{{{ input.{ref.path} }}}}"
    if ref.kind == "signal":
        return f"${{{{ signal.{ref.path} }}}}"
    if ref.kind == "stepOutput":
        return f"${{{{ steps.{ref.step_id}.outputs.{ref.path} }}}}"
    if ref.kind == "stepInput":
        return f"${{{{ steps.{ref.step_id}.inputs.{ref.path} }}}}"
    raise ValueError(f"unsupported workflow ref kind: {ref.kind}")
