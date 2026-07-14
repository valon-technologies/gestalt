from __future__ import annotations

import json
from pathlib import Path
from typing import Any, Mapping

from gestalt._protocol import value_to_json, which_oneof
from gestalt._workflow import workflow_definition_spec
from gestalt.workflow_authoring import (
    WorkflowBuilder,
    _capture_object,
    _TemplateMarker,
    _WorkflowRef,
    _WorkflowRefProxy,
    define_workflow,
    event,
    schedule,
)


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
