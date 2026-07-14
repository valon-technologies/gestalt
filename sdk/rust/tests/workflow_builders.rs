#[allow(dead_code)]
mod helpers;

use serde::Serialize;
use serde_json::json;

use gestalt::{
    BoundWorkflowTarget, WorkflowAgentMessage, WorkflowEvalContext, WorkflowExecutionRequest,
    WorkflowSignal, WorkflowStep, WorkflowStepAction, WorkflowStepAgentTurn, WorkflowStepAppCall,
    WorkflowStepWhen, WorkflowText, evaluate_workflow_value, new_bound_workflow_target,
    new_bound_workflow_target_from_target, render_workflow_template, workflow_value_input,
    workflow_value_literal, workflow_value_signal, workflow_value_step_input,
    workflow_value_step_output,
};

#[derive(Serialize)]
struct Payload {
    ok: bool,
    count: i32,
}

#[test]
fn workflow_steps_do_not_require_timeout() -> gestalt::Result<()> {
    let target = new_bound_workflow_target(BoundWorkflowTarget {
        steps: vec![WorkflowStep {
            id: "run".to_string(),
            action: Some(WorkflowStepAction::App(WorkflowStepAppCall {
                name: "app".to_string(),
                operation: "run".to_string(),
                input: Some(workflow_value_literal(Payload {
                    ok: false,
                    count: 0,
                })?),
                ..Default::default()
            })),
            ..Default::default()
        }],
    })?;

    assert_eq!(target.steps[0].timeout_seconds, 0);
    let app = app_step(&target, 0)?;
    assert_eq!(app.name, "app");
    assert_eq!(
        evaluate_workflow_value(
            &WorkflowEvalContext::default(),
            app.input.as_ref().expect("input")
        )?
        .value,
        Some(json!({"ok": false, "count": 0.0}))
    );
    Ok(())
}

#[test]
fn workflow_copy_helpers_do_not_alias_nested_payloads() -> gestalt::Result<()> {
    let mut target = new_bound_workflow_target(BoundWorkflowTarget {
        steps: vec![WorkflowStep {
            id: "run".to_string(),
            action: Some(WorkflowStepAction::App(WorkflowStepAppCall {
                name: "app".to_string(),
                operation: "run".to_string(),
                input: Some(workflow_value_literal(json!({
                    "nested": {"value": "original"}
                }))?),
                ..Default::default()
            })),
            ..Default::default()
        }],
    })?;
    let copied = new_bound_workflow_target_from_target(&target)?;

    let app = app_step_mut(&mut target, 0)?;
    app.input = Some(workflow_value_literal(json!({
        "nested": {"value": "changed"}
    }))?);

    let copied_app = app_step(&copied, 0)?;
    assert_eq!(
        evaluate_workflow_value(
            &WorkflowEvalContext::default(),
            copied_app.input.as_ref().expect("input")
        )?
        .value,
        Some(json!({"nested": {"value": "original"}}))
    );
    Ok(())
}

#[test]
fn workflow_values_and_templates_use_current_roots() -> gestalt::Result<()> {
    let ctx = WorkflowEvalContext {
        request: WorkflowExecutionRequest {
            provider: "indexeddb".to_string(),
            run_id: "run-1".to_string(),
            input: Some(json!({"customer": {"id": "cust_1"}})),
            signals: vec![WorkflowSignal {
                id: "sig-1".to_string(),
                payload: Some(helpers::struct_from_json(json!({
                    "thread": {"ts": "123.456"}
                }))),
                ..Default::default()
            }],
            ..Default::default()
        },
        outputs: [(
            "diagnose".to_string(),
            json!({"summary": {"action": "notify"}}),
        )]
        .into(),
        inputs: [("notify".to_string(), json!({"channel": "C123"}))].into(),
        allow_inputs: true,
    };

    assert_eq!(
        evaluate_workflow_value(&ctx, &workflow_value_input("customer.id"))?.value,
        Some(json!("cust_1"))
    );
    assert_eq!(
        evaluate_workflow_value(&ctx, &workflow_value_signal("thread.ts"))?.value,
        Some(json!("123.456"))
    );
    assert_eq!(
        evaluate_workflow_value(
            &ctx,
            &workflow_value_step_output("diagnose", "summary.action")
        )?
        .value,
        Some(json!("notify"))
    );
    assert_eq!(
        evaluate_workflow_value(&ctx, &workflow_value_step_input("notify", "channel"))?.value,
        Some(json!("C123"))
    );
    assert_eq!(
        render_workflow_template(
            &ctx,
            "customer=${{ input.customer.id }} action=${{ steps.diagnose.outputs.summary.action }}",
        )?,
        "customer=cust_1 action=notify"
    );
    Ok(())
}

#[test]
fn workflow_steps_round_trip_current_step_shapes() -> gestalt::Result<()> {
    let target = new_bound_workflow_target(BoundWorkflowTarget {
        steps: vec![
            WorkflowStep {
                id: "diagnose".to_string(),
                action: Some(WorkflowStepAction::Agent(WorkflowStepAgentTurn {
                    provider: "agent".to_string(),
                    model: "gpt-5.5".to_string(),
                    prompt: Some(WorkflowText {
                        template: "Diagnose the alert.".to_string(),
                    }),
                    messages: vec![WorkflowAgentMessage {
                        role: "system".to_string(),
                        text: Some(WorkflowText {
                            template: "Use concise replies.".to_string(),
                        }),
                        ..Default::default()
                    }],
                    ..Default::default()
                })),
                metadata: Some(helpers::struct_from_json(json!({"kind": "diagnosis"}))),
                ..Default::default()
            },
            WorkflowStep {
                id: "notify".to_string(),
                action: Some(WorkflowStepAction::App(WorkflowStepAppCall {
                    name: "slack".to_string(),
                    operation: "messages.post".to_string(),
                    input: Some(workflow_value_step_output("diagnose", "summary")),
                    ..Default::default()
                })),
                when: Some(WorkflowStepWhen {
                    value: Some(workflow_value_step_output("diagnose", "actionable")),
                    equals: Some(helpers::json_to_prost(&json!(true))),
                }),
                timeout_seconds: 45,
                ..Default::default()
            },
        ],
    })?;
    let copied = new_bound_workflow_target_from_target(&target)?;

    let diagnosis = agent_step(&copied, 0)?;
    assert_eq!(diagnosis.provider, "agent");
    assert_eq!(target.steps.len(), 2);
    assert_eq!(
        copied.steps[1]
            .when
            .as_ref()
            .and_then(|when| when.value.as_ref())
            .and_then(|value| value.kind.as_ref())
            .and_then(|kind| match kind {
                gestalt::workflow_provider::workflow_value::Kind::StepOutput(source) => {
                    Some(source.path.as_str())
                }
                _ => None,
            }),
        Some("actionable")
    );
    Ok(())
}

fn app_step(
    target: &gestalt::BoundWorkflowTarget,
    index: usize,
) -> gestalt::Result<&gestalt::WorkflowStepAppCall> {
    match target
        .steps
        .get(index)
        .and_then(|step| step.action.as_ref())
    {
        Some(gestalt::WorkflowStepAction::App(app)) => Ok(app),
        _ => Err(gestalt::Error::bad_request("expected app step")),
    }
}

fn agent_step(
    target: &gestalt::BoundWorkflowTarget,
    index: usize,
) -> gestalt::Result<&gestalt::WorkflowStepAgentTurn> {
    match target
        .steps
        .get(index)
        .and_then(|step| step.action.as_ref())
    {
        Some(gestalt::WorkflowStepAction::Agent(agent)) => Ok(agent),
        _ => Err(gestalt::Error::bad_request("expected agent step")),
    }
}

fn app_step_mut(
    target: &mut gestalt::BoundWorkflowTarget,
    index: usize,
) -> gestalt::Result<&mut gestalt::WorkflowStepAppCall> {
    match target
        .steps
        .get_mut(index)
        .and_then(|step| step.action.as_mut())
    {
        Some(gestalt::WorkflowStepAction::App(app)) => Ok(app),
        _ => Err(gestalt::Error::bad_request("expected app step")),
    }
}
