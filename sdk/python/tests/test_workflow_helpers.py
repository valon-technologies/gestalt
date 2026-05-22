import dataclasses
import datetime as dt
import unittest

import gestalt


@dataclasses.dataclass
class Payload:
    ok: bool
    count: int


class WorkflowHelperTests(unittest.TestCase):
    def test_native_inputs_build_workflow_messages(self) -> None:
        created_at = dt.datetime(2026, 5, 8, 12, 0, tzinfo=dt.timezone.utc)

        target = gestalt.bound_workflow_target(
            steps=[
                gestalt.WorkflowStep(
                    id="run",
                    plugin=gestalt.WorkflowStepPluginCall(
                        name="plugin",
                        operation="run",
                        input=gestalt.WorkflowValue(literal=Payload(ok=False, count=0)),
                    ),
                )
            ]
        )
        signal = gestalt.workflow_signal(
            name="ready",
            payload=Payload(ok=True, count=1),
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
        self.assertEqual(step.plugin.name, "plugin")
        self.assertEqual(step.plugin.input.literal.struct_value.fields["count"].number_value, 0)
        self.assertFalse(step.plugin.input.literal.struct_value.fields["ok"].bool_value)
        self.assertEqual(signal.payload.fields["ok"].bool_value, True)
        self.assertEqual(signal.sequence, 0)
        self.assertEqual(run.created_at.ToDatetime(tzinfo=dt.timezone.utc), created_at)

    def test_copy_helpers_do_not_alias_nested_payloads(self) -> None:
        target = gestalt.bound_workflow_target(
            steps=[
                gestalt.WorkflowStep(
                    id="run",
                    plugin=gestalt.WorkflowStepPluginCall(
                        name="plugin",
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

        target.steps[0].plugin.input.object.fields["nested"].object.fields[
            "value"
        ].literal.string_value = "changed"

        self.assertEqual(
            copied.steps[0]
            .plugin.input.object.fields["nested"]
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
                                plugin="datadog",
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
                                plugin="github",
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

        self.assertEqual(target.steps[0].agent.tools[0].plugin, "datadog")
        self.assertEqual(target.steps[1].when.equals.bool_value, True)
        copied = gestalt.bound_workflow_target_input_from_target(target)
        self.assertIsInstance(copied.steps[0].agent.messages[0], gestalt.WorkflowAgentMessage)
        self.assertEqual(copied.steps[0].agent.response_schema["type"], "object")
        self.assertEqual(
            copied.steps[1].when.value.step_output.path,
            "agent.structuredOutput.actionableForPr",
        )

    def test_old_target_exports_are_removed(self) -> None:
        self.assertFalse(hasattr(gestalt, "BoundWorkflowPluginTarget"))
        self.assertFalse(hasattr(gestalt, "BoundWorkflowAgentTarget"))
        self.assertFalse(hasattr(gestalt, "WorkflowAgentStep"))


if __name__ == "__main__":
    unittest.main()
