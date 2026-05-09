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
            plugin=gestalt.BoundWorkflowPluginTargetInput(
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
            trigger=gestalt.WorkflowRunTriggerInput(manual=True),
        )

        self.assertEqual(target.plugin.plugin_name, "plugin")
        self.assertEqual(target.plugin.input.fields["count"].number_value, 0)
        self.assertFalse(target.plugin.input.fields["ok"].bool_value)
        self.assertEqual(signal.payload.fields["ok"].bool_value, True)
        self.assertEqual(signal.sequence, 0)
        self.assertEqual(gestalt.datetime_from_timestamp(run.created_at), created_at)

    def test_copy_helpers_do_not_alias_nested_payloads(self) -> None:
        target = gestalt.bound_workflow_target(
            plugin=gestalt.BoundWorkflowPluginTargetInput(
                plugin_name="plugin",
                operation="run",
                input={"nested": {"value": "original"}},
            )
        )
        copied = gestalt.bound_workflow_target_from_target(target)

        target.plugin.input.fields["nested"].struct_value.fields["value"].string_value = "changed"

        self.assertEqual(
            copied.plugin.input.fields["nested"].struct_value.fields["value"].string_value,
            "original",
        )


if __name__ == "__main__":
    unittest.main()
