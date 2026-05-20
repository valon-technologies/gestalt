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
                    id="sync",
                    plugin=gestalt.WorkflowStepPluginCall(
                        name="plugin",
                        operation="run",
                        input=Payload(ok=False, count=0),
                    ),
                )
            ]
        )
        activation = gestalt.workflow_activation(
            id="manual",
            mode=gestalt.WORKFLOW_ACTIVATION_MODE_START,
            input=gestalt.WorkflowValue(literal={"source": "test"}),
            manual=True,
        )
        signal = gestalt.workflow_signal(
            name="ready",
            payload=Payload(ok=True, count=1),
            created_at=created_at,
            sequence=0,
        )
        run = gestalt.workflow_run(
            id="run-1",
            definition_id="deployment-1",
            status=gestalt.WORKFLOW_RUN_STATUS_PENDING,
            created_at=created_at,
            trigger=gestalt.WorkflowRunTrigger(
                definition_id="deployment-1",
                activation_id="manual",
                manual=True,
            ),
        )

        self.assertEqual(target.steps[0].plugin.name, "plugin")
        self.assertEqual(target.steps[0].plugin.input.literal.struct_value.fields["count"].number_value, 0)
        self.assertFalse(target.steps[0].plugin.input.literal.struct_value.fields["ok"].bool_value)
        self.assertEqual(activation.id, "manual")
        self.assertEqual(activation.WhichOneof("kind"), "manual")
        self.assertEqual(signal.payload.fields["ok"].bool_value, True)
        self.assertEqual(signal.sequence, 0)
        self.assertEqual(run.trigger.definition_id, "deployment-1")
        self.assertEqual(run.created_at.ToDatetime(tzinfo=dt.timezone.utc), created_at)

    def test_copy_helpers_do_not_alias_nested_payloads(self) -> None:
        target = gestalt.bound_workflow_target(
            steps=[
                gestalt.WorkflowStep(
                    id="sync",
                    plugin=gestalt.WorkflowStepPluginCall(
                        name="plugin",
                        operation="run",
                        input={"nested": {"value": "original"}},
                    ),
                )
            ]
        )
        copied = gestalt.bound_workflow_target(target)

        target.steps[0].plugin.input.literal.struct_value.fields[
            "nested"
        ].struct_value.fields["value"].string_value = "changed"

        self.assertEqual(
            copied.steps[0]
            .plugin.input.literal.struct_value.fields["nested"]
            .struct_value.fields["value"]
            .string_value,
            "original",
        )

    def test_agent_turn_step_builds_proto(self) -> None:
        step = gestalt.workflow_step(
            id="diagnosis",
            agent=gestalt.WorkflowStepAgentTurn(
                provider="claude",
                model="claude-3-5-sonnet",
                prompt="Diagnose the alert.",
                messages=[
                    gestalt.WorkflowAgentMessage(
                        role="system",
                        text="Use concise replies.",
                    )
                ],
                tools=[
                    gestalt.AgentToolRef(
                        plugin="github",
                        operation="search/code",
                    )
                ],
                response_schema={"type": "object"},
                model_options={"temperature": 0},
            ),
            timeout_seconds=45,
            metadata={"kind": "diagnosis"},
        )

        self.assertEqual(step.agent.provider, "claude")
        self.assertEqual(step.agent.messages[0].text.template, "Use concise replies.")
        self.assertEqual(step.agent.tools[0].plugin, "github")
        self.assertEqual(step.agent.response_schema.fields["type"].string_value, "object")
        self.assertEqual(step.metadata.fields["kind"].string_value, "diagnosis")

    def test_step_conditions_and_delivery_build_proto(self) -> None:
        step = gestalt.workflow_step(
            id="notify",
            plugin=gestalt.WorkflowStepPluginCall(
                name="slack",
                operation="reply",
                input=gestalt.WorkflowValue(
                    object=gestalt.WorkflowObject(
                        fields={
                            "text": gestalt.WorkflowValue(
                                step_output=gestalt.WorkflowStepOutputSource(
                                    step_id="diagnosis",
                                    path="text",
                                )
                            )
                        }
                    )
                ),
            ),
            when=gestalt.WorkflowStepWhen(
                value=gestalt.WorkflowValue(
                    step_output=gestalt.WorkflowStepOutputSource(
                        step_id="diagnosis",
                        path="structured_output.actionable_for_pr",
                    )
                ),
                equals=True,
            ),
            output_delivery=gestalt.WorkflowStepDelivery(
                plugin=gestalt.WorkflowStepPluginCall(
                    name="audit",
                    operation="record",
                )
            ),
        )

        self.assertEqual(step.when.value.step_output.step_id, "diagnosis")
        self.assertEqual(step.when.equals.bool_value, True)
        self.assertEqual(step.plugin.input.object.fields["text"].step_output.path, "text")
        self.assertEqual(step.output_delivery.plugin.name, "audit")

    def test_deployment_spec_carries_activations_and_run_as(self) -> None:
        spec = gestalt.workflow_definition_spec(
            id="deployment-1",
            generation=2,
            target=gestalt.BoundWorkflowTarget(
                steps=[
                    gestalt.WorkflowStep(
                        id="sync",
                        plugin=gestalt.WorkflowStepPluginCall(
                            name="plugin",
                            operation="run",
                        ),
                    )
                ]
            ),
            activations=[
                gestalt.WorkflowActivation(
                    id="nightly",
                    mode=gestalt.WORKFLOW_ACTIVATION_MODE_SIGNAL_OR_START,
                    schedule=gestalt.WorkflowScheduleActivation(
                        cron="0 0 * * *",
                        timezone="UTC",
                    ),
                )
            ],
            run_as=gestalt.WorkflowRunAsSubject(
                subject_id="service_account:workflow",
                credential_subject_id="service_account:credential",
            ),
            permissions=[
                gestalt.WorkflowAccessPermission(
                    plugin="github",
                    operations=["search/code"],
                    actions=["issues.read"],
                )
            ],
            labels={"team": "eng"},
        )

        self.assertEqual(spec.id, "deployment-1")
        self.assertEqual(spec.activations[0].schedule.cron, "0 0 * * *")
        self.assertEqual(spec.run_as.credential_subject_id, "service_account:credential")
        self.assertEqual(spec.permissions[0].operations[0], "search/code")
        self.assertEqual(spec.labels["team"], "eng")

    def test_invoke_workflow_action_request_builds_proto(self) -> None:
        request = gestalt.invoke_workflow_action_request(
            selector=gestalt.WorkflowHostActionSelector(
                run_id="run-1",
                definition_id="deployment-1",
                step_id="sync",
                action_id="sync.plugin",
            ),
            plugin=gestalt.WorkflowPluginActionPayload(
                input={"operation": "sync"},
            ),
            signals=[
                gestalt.WorkflowSignal(
                    name="ready",
                    payload={"ok": True},
                )
            ],
        )

        self.assertEqual(request.selector.action_id, "sync.plugin")
        self.assertEqual(request.plugin.input.fields["operation"].string_value, "sync")
        self.assertTrue(request.signals[0].payload.fields["ok"].bool_value)

    def test_old_target_exports_are_removed(self) -> None:
        self.assertFalse(hasattr(gestalt, "BoundWorkflowPluginTarget"))
        self.assertFalse(hasattr(gestalt, "BoundWorkflowAgentTarget"))
        self.assertFalse(hasattr(gestalt, "WorkflowAgentStep"))


if __name__ == "__main__":
    unittest.main()
