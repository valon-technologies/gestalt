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
            plugin=gestalt.BoundWorkflowPluginTarget(
                plugin_name="plugin",
                operation="run",
                input=Payload(ok=False, count=0),
            )
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

        self.assertEqual(target.plugin.plugin_name, "plugin")
        self.assertEqual(target.plugin.input.fields["count"].number_value, 0)
        self.assertFalse(target.plugin.input.fields["ok"].bool_value)
        self.assertEqual(signal.payload.fields["ok"].bool_value, True)
        self.assertEqual(signal.sequence, 0)
        self.assertEqual(run.created_at.ToDatetime(tzinfo=dt.timezone.utc), created_at)

    def test_copy_helpers_do_not_alias_nested_payloads(self) -> None:
        target = gestalt.bound_workflow_target(
            plugin=gestalt.BoundWorkflowPluginTarget(
                plugin_name="plugin",
                operation="run",
                input={"nested": {"value": "original"}},
            )
        )
        copied = gestalt.bound_workflow_target_from_target(target)

        target.plugin.input.fields["nested"].struct_value.fields[
            "value"
        ].string_value = "changed"

        self.assertEqual(
            copied.plugin.input.fields["nested"]
            .struct_value.fields["value"]
            .string_value,
            "original",
        )

    def test_native_agent_target_messages_build_proto(self) -> None:
        target = gestalt.bound_workflow_agent_target(
            gestalt.BoundWorkflowAgentTarget(
                provider_name="claude",
                messages=[
                    gestalt.AgentMessage(
                        role="system",
                        text="Watch the alerts channel.",
                    )
                ],
                tool_refs=[
                    gestalt.AgentToolRef(
                        plugin="github",
                        operation="search/code",
                    )
                ],
            )
        )

        self.assertEqual(target.provider_name, "claude")
        self.assertEqual(target.messages[0].role, "system")
        self.assertEqual(target.messages[0].text, "Watch the alerts channel.")
        self.assertEqual(target.tool_refs[0].plugin, "github")
        self.assertEqual(target.tool_refs[0].operation, "search/code")

    def test_agent_tool_ref_carries_run_as(self) -> None:
        target = gestalt.bound_workflow_agent_target(
            tool_refs=[
                gestalt.AgentToolRef(
                    plugin="notion",
                    operation="search",
                    run_as=gestalt.AgentSubjectContext(
                        subject_id="service_account:gestalt-support-notion",
                        subject_kind="service_account",
                        credential_subject_id="service_account:notion-credential",
                        display_name="Gestalt Support Notion",
                        auth_source="notion_service_account",
                    ),
                    run_as_external_identity=gestalt.ExternalIdentity(
                        type="notion_workspace",
                        id="valon-support",
                    ),
                )
            ],
        )

        self.assertEqual(
            target.tool_refs[0].run_as.subject_id,
            "service_account:gestalt-support-notion",
        )
        self.assertEqual(
            target.tool_refs[0].run_as_external_identity.id,
            "valon-support",
        )
        copied = gestalt.bound_workflow_agent_target_input_from_target(target)
        self.assertEqual(
            copied.tool_refs[0].run_as.display_name,
            "Gestalt Support Notion",
        )
        self.assertEqual(
            copied.tool_refs[0].run_as_external_identity.type,
            "notion_workspace",
        )

    def test_agent_target_copy_returns_native_messages(self) -> None:
        target = gestalt.bound_workflow_agent_target(
            messages=[
                gestalt.AgentMessage(
                    role="system",
                    text="Watch the alerts channel.",
                )
            ],
            tool_refs=[
                gestalt.AgentToolRef(
                    plugin="github",
                    operation="search/code",
                )
            ],
        )
        copied = gestalt.bound_workflow_agent_target_input_from_target(target)

        self.assertIsInstance(copied.messages[0], gestalt.AgentMessage)
        self.assertEqual(copied.messages[0].text, "Watch the alerts channel.")
        self.assertIsInstance(copied.tool_refs[0], gestalt.AgentToolRef)
        self.assertEqual(copied.tool_refs[0].plugin, "github")

    def test_agent_step_when_preserves_explicit_null_equals(self) -> None:
        when = gestalt.workflow_agent_step_when(
            step_id="extract",
            output_path="$.value",
            equals=None,
        )

        self.assertTrue(when.HasField("equals"))
        self.assertEqual(when.equals.WhichOneof("kind"), "null_value")

        copied = gestalt.workflow_agent_step_when_input_from_when(when)
        round_tripped = gestalt.workflow_agent_step_when(copied)

        self.assertTrue(round_tripped.HasField("equals"))
        self.assertEqual(round_tripped.equals.WhichOneof("kind"), "null_value")

        absent = gestalt.workflow_agent_step_when(step_id="extract")
        self.assertFalse(absent.HasField("equals"))


if __name__ == "__main__":
    unittest.main()
