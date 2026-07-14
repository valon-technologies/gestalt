"""Fluent workflow definition builder with proxy-based reference capture."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Callable, Mapping, Protocol

from gestalt._gen.v1 import workflow_pb2 as pb
from gestalt._workflow import (
    bound_workflow_target,
    workflow_activation,
    workflow_agent_message,
    workflow_definition_spec,
    workflow_event_match,
    workflow_step,
    workflow_step_agent_turn,
    workflow_step_app_call,
    workflow_text,
    workflow_value,
)


class _TextExpression:
    __slots__ = ("parts",)

    def __init__(self, parts: tuple[str | _WorkflowRef | _WorkflowRefProxy, ...]) -> None:
        self.parts = parts


class _TemplateMarker:
    __slots__ = ("template",)

    def __init__(self, template: str) -> None:
        self.template = template


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
        self._steps: dict[str, _StepOutputs] = {}

    def step_output(self, step_id: str, path: str) -> _WorkflowRef:
        return _WorkflowRef("stepOutput", path, step_id)

    def step_input(self, step_id: str, path: str) -> _WorkflowRef:
        return _WorkflowRef("stepInput", path, step_id)

    @property
    def steps(self) -> _StepOutputsMap:
        return _StepOutputsMap(self)


class _StepOutputs:
    def __init__(self, step_id: str) -> None:
        self.output = _WorkflowRefProxy("stepOutput", "", step_id)
        self.input = _WorkflowRefProxy("stepInput", "", step_id)


class _StepOutputsMap:
    def __init__(self, scope: _StepScope) -> None:
        self._scope = scope

    def __getitem__(self, step_id: str) -> _StepOutputs:
        return _StepOutputs(step_id)


class _EventScope:
    def __init__(self) -> None:
        self.data = _WorkflowRefProxy("signal", "data")


@dataclass(frozen=True)
class EventActivationConfig:
    type: str
    map_input: Callable[[_EventScope], Mapping[str, Any]] | None = None
    id: str = ""
    source: str = ""
    subject: str = ""
    paused: bool = False


@dataclass(frozen=True)
class ScheduleActivationConfig:
    cron: str
    map_input: Callable[[_WorkflowRefProxy], Mapping[str, Any]] | None = None
    id: str = ""
    timezone: str = ""
    paused: bool = False


class WorkflowBuilder:
    def __init__(self, *, workflow_id: str, run_as: str, paused: bool = False) -> None:
        if not run_as.strip():
            raise ValueError("define_workflow requires run_as")
        if not workflow_id.strip():
            raise ValueError("define_workflow requires id")
        self._id = workflow_id
        self._run_as = run_as
        self._paused = paused
        self._activations: list[Any] = []
        self._steps: list[Any] = []

    def on(self, activation: EventActivationConfig | ScheduleActivationConfig) -> WorkflowBuilder:
        if isinstance(activation, EventActivationConfig):
            activation_id = activation.id.strip() or activation.type
            mapped = (
                activation.map_input(_EventScope()) if activation.map_input is not None else None
            )
            self._activations.append(
                workflow_activation(
                    id=activation_id,
                    paused=activation.paused,
                    event={"match": workflow_event_match(
                        type=activation.type,
                        source=activation.source,
                        subject=activation.subject,
                    )},
                    input=None
                    if mapped is None
                    else _capture_object_value(mapped, "signal"),
                )
            )
            return self

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
        input=None
        if mapped is None
        else _capture_object_value(mapped, "input"),
            )
        )
        return self

    def step(self, step_id: str, config: Mapping[str, Any]) -> WorkflowBuilder:
        scope = _StepScope()
        step_kwargs: dict[str, Any] = {"id": step_id}

        inputs = config.get("inputs")
        if inputs is not None:
            mapped = inputs(scope) if callable(inputs) else inputs
            step_kwargs["inputs"] = _capture_object(mapped, "input")

        app = config.get("app")
        if app is not None:
            app_input = app.get("input")
            mapped_input = app_input(scope) if callable(app_input) else app_input
            step_kwargs["app"] = workflow_step_app_call(
                name=app.get("name", ""),
                operation=app.get("operation", ""),
                input=None
                if mapped_input is None
                else _capture_object_value(mapped_input, "input"),
                connection=app.get("connection", ""),
                instance=app.get("instance", ""),
                credential_mode=app.get("credential_mode", ""),
            )

        agent = config.get("agent")
        if agent is not None:
            prompt = agent.get("prompt")
            if callable(prompt):
                prompt = prompt(scope)
            messages = []
            for message in agent.get("messages", []):
                message_text = message.get("text")
                if callable(message_text):
                    message_text = message_text(scope)
                messages.append(
                    workflow_agent_message(
                        role=message.get("role", ""),
                        text=workflow_text(_resolve_workflow_prompt(message_text)),
                    )
                )
            step_kwargs["agent"] = workflow_step_agent_turn(
                provider=agent.get("provider", ""),
                model=agent.get("model", ""),
                session_key=agent.get("session_key", ""),
                prompt=None if prompt is None else workflow_text(_resolve_workflow_prompt(prompt)),
                messages=messages,
                tools=agent.get("tools"),
                output=agent.get("output"),
                model_options=agent.get("model_options"),
            )

        when = config.get("when")
        if when is not None:
            value = when.get("value")
            proto_value = _capture_proto(value, "input")
            condition = pb.WorkflowStepWhen(value=proto_value)
            if "equals" in when:
                from gestalt._workflow import _value

                condition.equals.CopyFrom(_value(when.get("equals")))
            step_kwargs["when"] = condition

        if "timeout_seconds" in config:
            step_kwargs["timeout_seconds"] = config["timeout_seconds"]
        if "metadata" in config:
            step_kwargs["metadata"] = config["metadata"]

        self._steps.append(workflow_step(**step_kwargs))
        return self

    def to_spec(self) -> Any:
        target = None
        if self._steps:
            target = bound_workflow_target(steps=self._steps)
        return workflow_definition_spec(
            id=self._id,
            run_as=self._run_as,
            paused=self._paused,
            activations=self._activations,
            target=target,
        )


def define_workflow(*, workflow_id: str, run_as: str, paused: bool = False) -> WorkflowBuilder:
    return WorkflowBuilder(workflow_id=workflow_id, run_as=run_as, paused=paused)


def event(
    type_name: str,
    map_input: Callable[[_EventScope], Mapping[str, Any]] | None = None,
    *,
    id: str = "",
    source: str = "",
    subject: str = "",
    paused: bool = False,
) -> EventActivationConfig:
    return EventActivationConfig(
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
) -> ScheduleActivationConfig:
    return ScheduleActivationConfig(
        cron=cron,
        map_input=map_input,
        id=id,
        timezone=timezone,
        paused=paused,
    )


def text(*parts: str | _WorkflowRef | _WorkflowRefProxy) -> _TextExpression:
    """Compose workflow prompt/message text from literals and captured references."""
    return _TextExpression(parts)


class _WorkflowDefinitionSpecProvider(Protocol):
    def to_spec(self) -> Any: ...


def resolve_workflow_definition_spec(spec: WorkflowBuilder | _WorkflowDefinitionSpecProvider | Mapping[str, Any] | Any) -> Any:
    if isinstance(spec, WorkflowBuilder):
        return spec.to_spec()
    to_spec = getattr(spec, "to_spec", None)
    if callable(to_spec):
        return workflow_definition_spec(to_spec())
    return workflow_definition_spec(spec)


def _capture_object(
    value: Mapping[str, Any], default_kind: str
) -> dict[str, Any]:
    return {
        key: _capture_proto(item, default_kind)
        for key, item in value.items()
    }


def _capture_object_value(value: Mapping[str, Any], default_kind: str) -> Any:
    return pb.WorkflowValue(
        object=pb.WorkflowObject(fields=_capture_object(value, default_kind))
    )


def _capture_proto(value: Any, default_kind: str) -> Any:
    if isinstance(value, _WorkflowRef):
        if value.kind == "stepOutput":
            return pb.WorkflowValue(
                step_output=pb.WorkflowStepOutputSource(
                    step_id=value.step_id, path=value.path
                )
            )
        if value.kind == "stepInput":
            return pb.WorkflowValue(
                step_input=pb.WorkflowStepInputSource(
                    step_id=value.step_id, path=value.path
                )
            )
        if value.kind == "input":
            return pb.WorkflowValue(input=pb.WorkflowPathSource(path=value.path))
        if value.kind == "signal":
            return pb.WorkflowValue(signal=pb.WorkflowPathSource(path=value.path))
    if isinstance(value, _WorkflowRefProxy):
        ref = value._ref
        if ref.kind == "stepOutput":
            return pb.WorkflowValue(
                step_output=pb.WorkflowStepOutputSource(
                    step_id=ref.step_id, path=ref.path
                )
            )
        if ref.kind == "stepInput":
            return pb.WorkflowValue(
                step_input=pb.WorkflowStepInputSource(
                    step_id=ref.step_id, path=ref.path
                )
            )
        if ref.kind == "input":
            return pb.WorkflowValue(input=pb.WorkflowPathSource(path=ref.path))
        if ref.kind == "signal":
            return pb.WorkflowValue(signal=pb.WorkflowPathSource(path=ref.path))
    if isinstance(value, _TemplateMarker):
        return workflow_value(template=value.template)
    if isinstance(value, list):
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


def _resolve_workflow_prompt(value: Any) -> str:
    if isinstance(value, str):
        return value
    if isinstance(value, _TextExpression):
        return _render_text_expression(value)
    raise ValueError("workflow prompt must be a string or text() expression")


def _render_text_expression(expression: _TextExpression) -> str:
    rendered = ""
    for part in expression.parts:
        if isinstance(part, str):
            rendered += part
            continue
        rendered += _ref_to_template_placeholder(part)
    return rendered


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
