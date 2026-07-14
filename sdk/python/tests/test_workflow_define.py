import pytest

from gestalt import (
    AgentOutput,
    AgentTextOutput,
    AgentTurn,
    WorkflowStepAgentConfig,
    WorkflowStepAppConfig,
    define_workflow,
    event,
    schedule,
    text,
)


def test_workflow_builder_lowers_references_and_text() -> None:
    definition = (
        define_workflow(
            id="example",
            run_as="service_account:workflow-runner",
        )
        .on(schedule("0 * * * *", lambda value: {"account_id": value.account_id}))
        .on(event("deal.updated", lambda value: {"deal_id": value.data.deal_id}))
        .step(
            "extract",
            app=WorkflowStepAppConfig(
                name="dealHub",
                operation="extractRow",
                input=lambda scope: {"account_id": scope.input.account_id},
            ),
        )
        .step(
            "notify",
            agent=WorkflowStepAgentConfig(
                provider="openai",
                output=AgentOutput(text=AgentTextOutput()),
                prompt=lambda scope: text(
                    "Extracted ",
                    scope.steps["extract"].outputs.row_id,
                    " for ",
                    scope.steps["extract"].inputs.account_id,
                ),
            ),
        )
    )

    spec = definition.to_spec()
    assert spec.id == "example"
    assert spec.run_as == "service_account:workflow-runner"
    assert len(spec.activations) == 2
    assert spec.target.steps[0].app.name == "dealHub"
    assert spec.target.steps[1].agent.prompt.template == (
        "Extracted ${{ steps.extract.outputs.row_id }} for "
        "${{ steps.extract.inputs.account_id }}"
    )


def test_agent_turn_export_remains_the_provider_model() -> None:
    codec_shaped_turn = AgentTurn(
        id="turn-1",
        model="provider-model",
        status=1,
        messages=[],
        created_at=None,
    )

    assert isinstance(codec_shaped_turn, AgentTurn)
    assert AgentTurn.__module__ == "gestalt._agent"


def test_apply_definition_accepts_builder_as_top_level_request() -> None:
    from gestalt._workflow import _workflow_apply_definition_request

    definition = define_workflow(
        id="example",
        run_as="service_account:workflow-runner",
    ).step(
        "extract",
        app=WorkflowStepAppConfig(
            name="dealHub",
            operation="extractRow",
            input=lambda scope: {"account_id": scope.input.account_id},
        ),
    )

    request = _workflow_apply_definition_request(
        definition,
        provider="local",
        idempotency_key="workflow-definition-key",
    )

    assert request.provider == "local"
    assert request.idempotency_key == "workflow-definition-key"
    assert request.spec.id == "example"


def test_workflow_builder_rejects_both_app_and_agent() -> None:
    definition = define_workflow(
        id="invalid-step",
        run_as="service_account:workflow-runner",
    )

    with pytest.raises(
        ValueError,
        match="workflow step cannot configure both app and agent actions",
    ):
        definition.step(
            "broken",
            app=WorkflowStepAppConfig(
                name="dealHub",
                operation="extractRow",
            ),
            agent=WorkflowStepAgentConfig(
                provider="openai",
                prompt="summarize",
            ),
        )
