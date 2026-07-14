import json
import unittest
from pathlib import Path

import gestalt
from gestalt.workflow_authoring import (
    build_workflow_from_lowering_case,
    canonical_workflow_definition_spec,
    define_workflow,
    event,
    load_workflow_lowering_contract,
    resolve_workflow_definition_spec,
    schedule,
)


class WorkflowAuthoringTests(unittest.TestCase):
    def test_define_workflow_requires_run_as(self) -> None:
        with self.assertRaisesRegex(ValueError, "run_as"):
            define_workflow(workflow_id="demo", run_as="")

    def test_typed_workflow_builder_matches_extract_row_example(self) -> None:
        spec = (
            define_workflow(
                workflow_id="extractRow",
                run_as="service_account:deal-hub-extraction",
            )
            .on(
                event(
                    "deal_hub.analyses.extract.requested",
                    lambda activation_event: {
                        "analysisId": activation_event.data.analysisId,
                    },
                )
            )
            .step(
                "extract",
                {
                    "app": {
                        "name": "dealHub",
                        "operation": "analyses.extractRowWorkflow",
                        "input": lambda scope: {
                            "analysisId": scope.input.analysisId,
                        },
                    }
                },
            )
            .to_spec()
        )

        contract = load_workflow_lowering_contract()
        expected = next(
            case["expectedSpec"] for case in contract["cases"] if case["name"] == "extract_row"
        )
        self.assertEqual(canonical_workflow_definition_spec(spec), expected)

    def test_resolve_workflow_definition_spec_accepts_builders(self) -> None:
        builder = define_workflow(
            workflow_id="extractRow",
            run_as="service_account:deal-hub-extraction",
        ).on(schedule("0 2 * * *", lambda scope: {"reason": scope.reason}))

        from_builder = resolve_workflow_definition_spec(builder)
        from_spec = resolve_workflow_definition_spec(from_builder)
        self.assertEqual(from_builder.activations[0].schedule.cron, "0 2 * * *")
        self.assertEqual(from_spec.id, "extractRow")


def load_cases() -> list[dict]:
    return load_workflow_lowering_contract()["cases"]


class WorkflowAuthoringGoldenTests(unittest.TestCase):
    pass


for case in load_cases():
    def _make_test(case_data: dict):
        def test(self: WorkflowAuthoringGoldenTests) -> None:
            spec = build_workflow_from_lowering_case(case_data).to_spec()
            self.assertEqual(
                canonical_workflow_definition_spec(spec),
                case_data["expectedSpec"],
            )

        test.__name__ = f"test_golden_fixture_{case_data['name']}"
        return test

    setattr(WorkflowAuthoringGoldenTests, f"test_golden_fixture_{case['name']}", _make_test(case))


if __name__ == "__main__":
    unittest.main()
