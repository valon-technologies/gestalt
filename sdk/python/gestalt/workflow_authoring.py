"""Typed workflow authoring builder with proxy-based reference capture."""

from __future__ import annotations

import json
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable, Mapping, MutableMapping

from gestalt._gen.v1 import workflow_pb2 as pb
from gestalt._protocol import value_to_json, which_oneof
from gestalt._workflow import (
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
    def steps(self) -> Mapping[str, _StepOutputs]:
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
                text = message.get("text")
                if callable(text):
                    text = text(scope)
                messages.append(
                    workflow_agent_message(role=message.get("role", ""), text=workflow_text(text))
                )
            step_kwargs["agent"] = workflow_step_agent_turn(
                provider=agent.get("provider", ""),
                model=agent.get("model", ""),
                session_key=agent.get("session_key", ""),
                prompt=None if prompt is None else workflow_text(prompt),
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


def resolve_workflow_definition_spec(spec: WorkflowBuilder | Mapping[str, Any] | Any) -> Any:
    if isinstance(spec, WorkflowBuilder):
        return spec.to_spec()
    return workflow_definition_spec(spec)


async def apply_workflow_definition(
    workflow: Any,
    *,
    provider: str,
    idempotency_key: str,
    spec: WorkflowBuilder | Mapping[str, Any] | Any | None = None,
) -> Any:
    resolved = None if spec is None else resolve_workflow_definition_spec(spec)
    return await workflow.apply_definition(
        provider=provider,
        idempotency_key=idempotency_key,
        spec=resolved,
    )


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


def _capture_value(value: Any, default_kind: str) -> dict[str, Any]:
    if isinstance(value, _WorkflowRef):
        if value.kind == "stepOutput":
            return {"step_output": {"step_id": value.step_id, "path": value.path}}
        if value.kind == "stepInput":
            return {"step_input": {"step_id": value.step_id, "path": value.path}}
        if value.kind == "input":
            return {"input": value.path}
        if value.kind == "signal":
            return {"signal": value.path}
    if isinstance(value, _WorkflowRefProxy):
        ref = value._ref
        if ref.kind == "input":
            return {"input": ref.path}
        if ref.kind == "signal":
            return {"signal": ref.path}
        if ref.kind == "stepOutput":
            return {"step_output": {"step_id": ref.step_id, "path": ref.path}}
        if ref.kind == "stepInput":
            return {"step_input": {"step_id": ref.step_id, "path": ref.path}}
    if isinstance(value, list):
        return {"array": [_capture_value(item, default_kind) for item in value]}
    if isinstance(value, Mapping):
        return {"object": _capture_object(value, default_kind)}
    return {"literal": value}


def lower_workflow_value_node(node: Mapping[str, Any]) -> dict[str, Any]:
    kind = str(node.get("kind", ""))
    if kind == "input":
        return {"input": str(node.get("path", ""))}
    if kind == "signal":
        return {"signal": str(node.get("path", ""))}
    if kind == "stepOutput":
        return {
            "step_output": {
                "step_id": str(node.get("stepId", "")),
                "path": str(node.get("path", "")),
            }
        }
    if kind == "stepInput":
        return {
            "step_input": {
                "step_id": str(node.get("stepId", "")),
                "path": str(node.get("path", "")),
            }
        }
    if kind == "literal":
        return {"literal": node.get("value")}
    if kind == "template":
        return {"template": str(node.get("template", ""))}
    if kind == "object":
        fields = node.get("fields", {})
        if not isinstance(fields, Mapping):
            raise ValueError("workflow object node requires fields")
        return {
            "object": {
                key: lower_workflow_value_node(value)
                for key, value in fields.items()
            }
        }
    if kind == "array":
        values = node.get("values", [])
        if not isinstance(values, list):
            raise ValueError("workflow array node requires values")
        return {"array": [lower_workflow_value_node(item) for item in values]}
    raise ValueError(f"unsupported workflow value kind: {kind}")


def build_workflow_from_lowering_case(case_data: Mapping[str, Any]) -> WorkflowBuilder:
    init = case_data["init"]
    builder = define_workflow(
        workflow_id=str(init["id"]),
        run_as=str(init["runAs"]),
        paused=bool(init.get("paused", False)),
    )

    for activation in case_data.get("activations", []):
        if activation.get("event") is not None:
            event_config = activation["event"]
            input_node = activation.get("input")
            builder.on(
                event(
                    str(event_config.get("type", "")),
                    None
                    if input_node is None
                    else (lambda _scope, node=input_node: _lower_contract_input_to_mapped_object(node)),
                    id=str(activation.get("id", "")),
                    source=str(event_config.get("source", "")),
                    subject=str(event_config.get("subject", "")),
                    paused=bool(activation.get("paused", False)),
                )
            )
            continue
        schedule_config = activation["schedule"]
        input_node = activation.get("input")
        builder.on(
            schedule(
                str(schedule_config.get("cron", "")),
                None
                if input_node is None
                else (lambda _scope, node=input_node: _lower_contract_input_to_mapped_object(node)),
                id=str(activation.get("id", "")),
                timezone=str(schedule_config.get("timezone", "")),
                paused=bool(activation.get("paused", False)),
            )
        )

    for step in case_data.get("steps", []):
        config: dict[str, Any] = {}
        if step.get("inputs") is not None:
            config["inputs"] = lambda _scope, node=step["inputs"]: _lower_contract_input_to_mapped_object(
                node
            )
        if step.get("app") is not None:
            app = step["app"]
            config["app"] = {
                "name": app.get("name", ""),
                "operation": app.get("operation", ""),
                "input": None
                if app.get("input") is None
                else (lambda _scope, node=app["input"]: _lower_contract_input_to_mapped_object(node)),
                "connection": app.get("connection", ""),
                "instance": app.get("instance", ""),
                "credential_mode": app.get("credentialMode", ""),
            }
        if step.get("agent") is not None:
            agent = step["agent"]
            config["agent"] = {
                "provider": agent.get("provider", ""),
                "model": agent.get("model", ""),
                "session_key": agent.get("sessionKey", ""),
                "prompt": None
                if agent.get("prompt") is None
                else (
                    lambda _scope, node=agent["prompt"]: str(node.get("template", ""))
                    if node.get("kind") == "template"
                    else (_ for _ in ()).throw(
                        ValueError("agent prompt must be a template node in lowering contract")
                    )
                ),
                "messages": [
                    {
                        "role": message.get("role", ""),
                        "text": message["text"].get("value")
                        if message["text"].get("kind") == "literal"
                        else message["text"].get("template", ""),
                    }
                    for message in agent.get("messages", [])
                ],
                "tools": agent.get("tools"),
                "output": agent.get("output"),
                "model_options": agent.get("modelOptions"),
            }
        if step.get("when") is not None:
            when = step["when"]
            config["when"] = {
                "value": _lower_contract_value_to_runtime(when["value"]),
                "equals": when.get("equals"),
            }
        if "timeoutSeconds" in step:
            config["timeout_seconds"] = step["timeoutSeconds"]
        if "metadata" in step:
            config["metadata"] = step["metadata"]
        builder.step(str(step.get("id", "")), config)

    return builder


def _lower_contract_input_to_mapped_object(node: Mapping[str, Any]) -> dict[str, Any]:
    if node.get("kind") != "object":
        raise ValueError("lowering contract input must be an object node")
    fields = node.get("fields")
    if not isinstance(fields, Mapping):
        raise ValueError("lowering contract object node requires fields")
    return {key: _lower_contract_value_to_runtime(value) for key, value in fields.items()}


def _lower_contract_value_to_runtime(node: Mapping[str, Any]) -> Any:
    kind = str(node.get("kind", ""))
    if kind == "input":
        return _WorkflowRefProxy("input", str(node.get("path", "")))
    if kind == "signal":
        return _WorkflowRefProxy("signal", str(node.get("path", "")))
    if kind == "stepOutput":
        return _WorkflowRef("stepOutput", str(node.get("path", "")), str(node.get("stepId", "")))
    if kind == "stepInput":
        return _WorkflowRef("stepInput", str(node.get("path", "")), str(node.get("stepId", "")))
    if kind == "literal":
        return node.get("value")
    if kind == "template":
        return _TemplateMarker(str(node.get("template", "")))
    if kind == "object":
        fields = node.get("fields")
        if not isinstance(fields, Mapping):
            raise ValueError("workflow object node requires fields")
        return {key: _lower_contract_value_to_runtime(value) for key, value in fields.items()}
    if kind == "array":
        values = node.get("values")
        if not isinstance(values, list):
            raise ValueError("workflow array node requires values")
        return [_lower_contract_value_to_runtime(item) for item in values]
    raise ValueError(f"unsupported workflow value kind: {kind}")


def load_workflow_lowering_contract() -> dict[str, Any]:
    path = Path(__file__).resolve().parents[2] / "fixtures" / "workflow-authoring" / "lowering-contract.json"
    return json.loads(path.read_text(encoding="utf-8"))


def canonical_workflow_definition_spec(spec: Any) -> dict[str, Any]:
    normalized = workflow_definition_spec(spec)
    out: dict[str, Any] = {
        "id": normalized.id,
        "runAs": normalized.run_as,
        "paused": normalized.paused,
        "activations": [
            _canonical_activation(activation) for activation in normalized.activations
        ],
    }
    if normalized.target is not None:
        out["target"] = {
            "steps": [_canonical_step(step) for step in normalized.target.steps],
        }
    return out


def _canonical_activation(activation: Any) -> dict[str, Any]:
    out: dict[str, Any] = {"id": activation.id, "paused": activation.paused}
    if which_oneof(activation, "trigger") == "schedule":
        out["schedule"] = {
            "cron": activation.schedule.cron,
            "timezone": activation.schedule.timezone,
        }
    if which_oneof(activation, "trigger") == "event":
        out["event"] = {
            "match": {
                "type": activation.event.match.type,
                "source": activation.event.match.source,
                "subject": activation.event.match.subject,
            }
        }
    if activation.input is not None and activation.input.ByteSize() > 0:
        out["input"] = _canonical_workflow_value(activation.input)
    return out


def _canonical_step(step: Any) -> dict[str, Any]:
    out: dict[str, Any] = {"id": step.id}
    if step.inputs:
        out["inputs"] = {key: _canonical_workflow_value(value) for key, value in step.inputs.items()}
    if which_oneof(step, "action") == "app":
        app: dict[str, Any] = {"name": step.app.name, "operation": step.app.operation}
        if step.app.input is not None and step.app.input.ByteSize() > 0:
            app["input"] = _canonical_workflow_value(step.app.input)
        out["app"] = app
    if which_oneof(step, "action") == "agent":
        agent: dict[str, Any] = {
            "provider": step.agent.provider,
            "model": step.agent.model,
        }
        if step.agent.prompt.template:
            agent["prompt"] = {"template": step.agent.prompt.template}
        if step.agent.messages:
            agent["messages"] = [
                {
                    "role": message.role,
                    "text": {"template": message.text.template},
                }
                for message in step.agent.messages
            ]
        if step.agent.tools:
            agent["tools"] = [
                {"app": tool.app, "operation": tool.operation} for tool in step.agent.tools
            ]
        if step.agent.output is not None and step.agent.output.structured.schema:
            agent["output"] = {
                "structured": {
                    "schema": json.loads(
                        json.dumps(
                            {
                                key: value_to_json(item)
                                for key, item in step.agent.output.structured.schema.fields.items()
                            }
                        )
                    )
                }
            }
    if step.agent.model_options.fields:
        agent["modelOptions"] = json.loads(
            json.dumps(
                {
                    key: value_to_json(item)
                    for key, item in step.agent.model_options.fields.items()
                }
            )
        )
        out["agent"] = agent
    if step.when.value is not None and step.when.value.ByteSize() > 0:
        out["when"] = {
            "value": _canonical_workflow_value(step.when.value),
            "equals": value_to_json(step.when.equals),
        }
    if step.timeout_seconds:
        out["timeoutSeconds"] = step.timeout_seconds
    if step.metadata.fields:
        out["metadata"] = {
            key: value_to_json(item) for key, item in step.metadata.fields.items()
        }
    return out


def _canonical_workflow_value(value: Any) -> dict[str, Any]:
    kind = which_oneof(value, "kind")
    if kind == "literal":
        return {"literal": value_to_json(value.literal)}
    if kind == "template":
        return {"template": value.template.template}
    if kind == "input":
        return {"input": value.input.path}
    if kind == "signal":
        return {"signal": value.signal.path}
    if kind == "step_output":
        return {
            "stepOutput": {
                "stepId": value.step_output.step_id,
                "path": value.step_output.path,
            }
        }
    if kind == "step_input":
        return {
            "stepInput": {
                "stepId": value.step_input.step_id,
                "path": value.step_input.path,
            }
        }
    if kind == "object":
        return {
            "object": {
                key: _canonical_workflow_value(nested)
                for key, nested in value.object.fields.items()
            }
        }
    if kind == "array":
        return {"array": [_canonical_workflow_value(item) for item in value.array.values]}
    return {}
