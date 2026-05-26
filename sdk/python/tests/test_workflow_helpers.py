import datetime as dt
import unittest
from collections.abc import Mapping

from google.protobuf import json_format, struct_pb2

import gestalt


class WorkflowHelperTests(unittest.TestCase):
    def test_native_inputs_build_workflow_messages(self) -> None:
        created_at = dt.datetime(2026, 5, 8, 12, 0, tzinfo=dt.timezone.utc)

        target = gestalt.bound_workflow_target(
            steps=[
                gestalt.WorkflowStep(
                    id="run",
                    app=gestalt.WorkflowStepAppCall(
                        name="app",
                        operation="run",
                        input=gestalt.WorkflowValue(
                            literal={"ok": False, "count": 0}
                        ),
                    ),
                )
            ]
        )
        signal = gestalt.workflow_signal(
            name="ready",
            payload={"ok": True, "count": 1},
            created_at=created_at,
            sequence=0,
        )
        run = gestalt.bound_workflow_run(
            id="run-1",
            status=gestalt.WORKFLOW_RUN_STATUS_PENDING,
            target=target,
            created_at=created_at,
            trigger=gestalt.WorkflowRunTrigger(manual=True),
        )

        step = target.steps[0]
        self.assertEqual(step.app.name, "app")
        self.assertEqual(step.app.input.literal.struct_value.fields["count"].number_value, 0)
        self.assertFalse(step.app.input.literal.struct_value.fields["ok"].bool_value)
        self.assertEqual(signal.payload.fields["ok"].bool_value, True)
        self.assertEqual(signal.sequence, 0)
        self.assertEqual(run.created_at.ToDatetime(tzinfo=dt.timezone.utc), created_at)

    def test_copy_helpers_do_not_alias_nested_payloads(self) -> None:
        target = gestalt.bound_workflow_target(
            steps=[
                gestalt.WorkflowStep(
                    id="run",
                    app=gestalt.WorkflowStepAppCall(
                        name="app",
                        operation="run",
                        input=gestalt.WorkflowValue(
                            object={
                                "nested": gestalt.WorkflowValue(
                                    object={"value": gestalt.WorkflowValue(literal="original")}
                                )
                            }
                        ),
                    ),
                )
            ]
        )
        copied = gestalt.bound_workflow_target_from_target(target)

        target.steps[0].app.input.object.fields["nested"].object.fields[
            "value"
        ].literal.string_value = "changed"

        self.assertEqual(
            copied.steps[0]
            .app.input.object.fields["nested"]
            .object.fields["value"]
            .literal.string_value,
            "original",
        )

    def test_workflow_value_round_trips_sources_and_empty_collections(self) -> None:
        value = gestalt.workflow_value(
            object={
                "empty_object": gestalt.WorkflowValue(object={}),
                "empty_array": gestalt.WorkflowValue(array=[]),
                "null_literal": gestalt.WorkflowValue(literal=None),
                "thread": gestalt.WorkflowValue(signal_payload="event.thread_ts"),
                "result": gestalt.WorkflowValue(
                    step_output=gestalt.WorkflowStepOutputSource(
                        step_id="diagnosis",
                        path="agent.structuredOutput.actionableForPr",
                    )
                ),
            }
        )

        copied = gestalt.workflow_value_input_from_value(value)

        self.assertEqual(copied.object["empty_object"].object, {})
        self.assertEqual(copied.object["empty_array"].array, [])
        self.assertIsNone(copied.object["null_literal"].literal)
        self.assertEqual(copied.object["thread"].signal_payload, "event.thread_ts")
        self.assertEqual(copied.object["result"].step_output.step_id, "diagnosis")

    def test_steps_target_round_trip(self) -> None:
        self.assertIsNotNone(gestalt.workflow_step)
        self.assertIsNotNone(gestalt.workflow_step_when)

        target = gestalt.bound_workflow_target(
            steps=[
                gestalt.WorkflowStep(
                    id="diagnosis",
                    inputs={
                        "thread": gestalt.WorkflowValue(signal_payload="event.thread_ts")
                    },
                    agent=gestalt.WorkflowStepAgentTurn(
                        provider="claude",
                        model="gpt-5.5",
                        prompt=gestalt.WorkflowText(template="Diagnose the alert."),
                        messages=[
                            gestalt.WorkflowAgentMessage(
                                role="system",
                                text="Use concise replies.",
                            )
                        ],
                        tools=[
                            gestalt.AgentToolRef(
                                app="datadog",
                                operation="queryLogs",
                            )
                        ],
                        response_schema={"type": "object"},
                        model_options={"temperature": 0},
                    ),
                    timeout_seconds=45,
                    metadata={"kind": "diagnosis"},
                ),
                gestalt.WorkflowStep(
                    id="pr_fix",
                    agent=gestalt.WorkflowStepAgentTurn(
                        provider="claude",
                        prompt="Open a PR.",
                        tools=[
                            gestalt.AgentToolRef(
                                app="github",
                                operation="createPullRequest",
                            )
                        ],
                    ),
                    when=gestalt.WorkflowStepWhen(
                        value=gestalt.WorkflowValue(
                            step_output=gestalt.WorkflowStepOutputSource(
                                step_id="diagnosis",
                                path="agent.structuredOutput.actionableForPr",
                            )
                        ),
                        equals=True,
                    ),
                ),
            ],
        )

        self.assertEqual(target.steps[0].agent.tools[0].app, "datadog")
        self.assertEqual(target.steps[1].when.equals.bool_value, True)
        copied = gestalt.bound_workflow_target_input_from_target(target)
        self.assertIsInstance(copied.steps[0].agent.messages[0], gestalt.WorkflowAgentMessage)
        self.assertEqual(copied.steps[0].agent.response_schema["type"], "object")
        self.assertEqual(
            copied.steps[1].when.value.step_output.path,
            "agent.structuredOutput.actionableForPr",
        )

    def test_workflow_evaluates_templates_and_paths(self) -> None:
        req = gestalt.WorkflowExecutionRequest(
            provider_name="indexeddb",
            run_id="run-1",
            input={"customer": {"id": "cust_1"}},
            signals=[
                gestalt.WorkflowSignal(
                    id="sig-1",
                    payload={"thread": {"ts": "123.456"}},
                )
            ],
        )
        ctx = gestalt.WorkflowEvalContext(
            request=req,
            inputs={"thread": "123.456"},
            allow_inputs=True,
        )

        rendered = gestalt.render_workflow_template(
            ctx,
            "customer=${runInput.customer.id}; thread=${signalPayload.thread.ts}; input=${inputs.thread}; literal=$${x}",
        )

        self.assertEqual(
            rendered,
            "customer=cust_1; thread=123.456; input=123.456; literal=${x}",
        )
        value, ok = gestalt.evaluate_workflow_value(
            ctx, gestalt.WorkflowValue(run_input="customer.id")
        )
        self.assertTrue(ok)
        self.assertEqual(value, "cust_1")
        quoted, ok = gestalt.path_value({"quote'key": {"value": 42}}, "['quote\\'key'].value")
        self.assertTrue(ok)
        self.assertEqual(quoted, 42)

    def test_workflow_types_expose_direct_agent_path(self) -> None:
        target = gestalt.BoundWorkflowTarget(
            steps=[
                gestalt.WorkflowStep(
                    id="agent",
                    agent=gestalt.WorkflowStepAgentTurn(
                        provider="openai",
                        model="gpt-5.5",
                        model_options={"temperature": 0},
                    ),
                )
            ]
        )
        request = gestalt.WorkflowSignalOrStartRun(
            provider_name="slack",
            target=target,
        )

        assert request.target is not None
        step = request.target.steps[0]
        assert step.agent is not None
        provider: str = step.agent.provider
        model_options: Mapping[str, object] | None = step.agent.model_options

        self.assertEqual(provider, "openai")
        self.assertEqual(model_options, {"temperature": 0})

    def test_workflow_run_context_matches_runtime_shape(self) -> None:
        created_at = dt.datetime(2026, 5, 8, 12, 0, tzinfo=dt.timezone.utc)
        req = gestalt.WorkflowExecutionRequest(
            provider_name="indexeddb",
            run_id="run-1",
            target=gestalt.BoundWorkflowTarget(
                steps=[
                    gestalt.WorkflowStep(
                        id="notify",
                        app=gestalt.WorkflowStepAppCall(
                            name="slack",
                            operation="chat.postMessage",
                            credential_mode="user",
                        ),
                    )
                ]
            ),
            trigger=gestalt.WorkflowRunTrigger(manual=True),
            created_by=gestalt.WorkflowActor(subject_id="user-1", subject_kind="user"),
            signals=[
                gestalt.WorkflowSignal(
                    id="sig-1",
                    name="ready",
                    payload={
                        "delivery_id": "delivery-1",
                        "payload": {"large": True},
                        "extra": "kept",
                    },
                    created_at=created_at,
                )
            ],
        )

        ctx = gestalt.workflow_run_context(req)

        self.assertEqual(
            ctx["target"],
            {
                "kind": "steps",
                "steps": [
                    {
                        "id": "notify",
                        "kind": "app",
                        "app": "slack",
                        "operation": "chat.postMessage",
                        "credentialMode": "user",
                    }
                ],
            },
        )
        self.assertEqual(ctx["trigger"], {"kind": "manual"})
        self.assertEqual(ctx["createdBy"], {"subjectId": "user-1", "subjectKind": "user"})
        self.assertEqual(
            ctx["signals"],
            [
                {
                    "id": "sig-1",
                    "name": "ready",
                    "payload": {
                        "delivery_id": "delivery-1",
                        "fields": {"extra": "kept"},
                        "payloadOmitted": True,
                    },
                    "createdAt": "2026-05-08T12:00:00Z",
                }
            ],
        )

    def test_parse_workflow_run_context_from_request(self) -> None:
        req = gestalt.Request(
            workflow={
                "provider": "github",
                "runId": "run-1",
                "target": {"kind": "steps", "steps": [{"id": "review"}]},
                "trigger": {
                    "kind": "schedule",
                    "scheduleId": "sched-1",
                    "scheduledFor": "2026-05-08T12:00:00Z",
                },
                "input": {"repository": "valon/app"},
                "metadata": {"definitionId": "def-1"},
                "createdBy": {"subjectId": "user-1", "subjectKind": "user"},
                "signals": [
                    "ignored",
                    {"id": "sig-1", "name": "queued", "payload": {"state": "queued"}},
                    {
                        "id": "sig-2",
                        "name": "github",
                        "payload": {
                            "github_event": "pull_request",
                            "delivery_id": "delivery-1",
                            "payloadOmitted": True,
                        },
                        "metadata": {"source": "webhook"},
                        "createdBy": {"subjectId": "bot-1", "displayName": "GitHub"},
                        "createdAt": "2026-05-08T12:01:00Z",
                        "idempotencyKey": "idem-1",
                        "sequence": 2,
                    },
                ],
            }
        )

        ctx = req.workflow_run_context()

        self.assertEqual(ctx.provider, "github")
        self.assertEqual(ctx.run_id, "run-1")
        self.assertEqual(ctx.target, {"kind": "steps", "steps": [{"id": "review"}]})
        self.assertEqual(ctx.trigger.kind, "schedule")
        self.assertEqual(ctx.trigger.schedule_id, "sched-1")
        self.assertEqual(ctx.trigger.scheduled_for, "2026-05-08T12:00:00Z")
        self.assertEqual(ctx.input, {"repository": "valon/app"})
        self.assertEqual(ctx.metadata, {"definitionId": "def-1"})
        assert ctx.created_by is not None
        self.assertEqual(ctx.created_by.subject_id, "user-1")
        self.assertEqual(len(ctx.signals), 2)
        latest_signal = ctx.latest_signal
        assert latest_signal is not None
        self.assertEqual(latest_signal.id, "sig-2")
        self.assertEqual(latest_signal.payload["github_event"], "pull_request")
        self.assertEqual(latest_signal.metadata, {"source": "webhook"})
        assert latest_signal.created_by is not None
        self.assertEqual(latest_signal.created_by.display_name, "GitHub")
        self.assertEqual(latest_signal.sequence, 2)

    def test_parse_workflow_run_context_preserves_struct_sequence(self) -> None:
        workflow = getattr(struct_pb2, "Struct")()
        json_format.ParseDict({"signals": [{"sequence": 2}]}, workflow)
        workflow_dict = json_format.MessageToDict(
            workflow,
            preserving_proto_field_name=True,
        )

        ctx = gestalt.parse_workflow_run_context(workflow_dict)

        latest_signal = ctx.latest_signal
        assert latest_signal is not None
        self.assertEqual(latest_signal.sequence, 2)

    def test_parse_workflow_run_context_tolerates_malformed_values(self) -> None:
        ctx = gestalt.parse_workflow_run_context(
            {
                "provider": 123,
                "runId": None,
                "target": [],
                "trigger": {
                    "kind": "event",
                    "triggerId": "trigger-1",
                    "event": {
                        "type": "github.pull_request",
                        "specVersion": "1.0",
                    },
                },
                "input": "bad",
                "metadata": ["bad"],
                "createdBy": {},
                "signals": [
                    {"sequence": True, "payload": "bad", "metadata": {"ok": True}},
                    None,
                ],
            }
        )

        self.assertEqual(ctx.provider, "")
        self.assertEqual(ctx.run_id, "")
        self.assertIsNone(ctx.target)
        self.assertEqual(ctx.trigger.kind, "event")
        self.assertEqual(ctx.trigger.trigger_id, "trigger-1")
        self.assertEqual(
            ctx.trigger.event,
            {"type": "github.pull_request", "specVersion": "1.0"},
        )
        self.assertEqual(ctx.input, {})
        self.assertEqual(ctx.metadata, {})
        self.assertIsNone(ctx.created_by)
        self.assertEqual(len(ctx.signals), 1)
        self.assertEqual(ctx.signals[0].payload, {})
        self.assertEqual(ctx.signals[0].metadata, {"ok": True})
        self.assertIsNone(ctx.signals[0].sequence)

    def test_workflow_run_context_exports_public_helpers(self) -> None:
        self.assertIn("WorkflowRunContext", gestalt.__all__)
        self.assertIn("parse_workflow_run_context", gestalt.__all__)


if __name__ == "__main__":
    unittest.main()
