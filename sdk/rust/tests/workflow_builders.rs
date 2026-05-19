use std::collections::BTreeMap;
use std::time::{Duration, UNIX_EPOCH};

use serde::Serialize;
use serde_json::json;

use gestalt::{
    AgentToolRef, BoundWorkflowRun, BoundWorkflowTarget, WorkflowAgentMessage, WorkflowRunStatus,
    WorkflowRunTrigger, WorkflowSignal, WorkflowStep, WorkflowStepAction, WorkflowStepAgentTurn,
    WorkflowStepDelivery, WorkflowStepOutputSource, WorkflowStepPluginCall, WorkflowStepWhen,
    WorkflowText, WorkflowValue, new_bound_workflow_run, new_bound_workflow_target,
    new_bound_workflow_target_from_target, new_workflow_signal,
};

#[derive(Serialize)]
struct Payload {
    ok: bool,
    count: i32,
}

#[test]
fn workflow_builders_accept_serde_values_and_system_time() -> gestalt::Result<()> {
    let created_at = UNIX_EPOCH + Duration::from_secs(1_778_241_600);
    let plugin = WorkflowStepPluginCall {
        name: "plugin".to_string(),
        operation: "run".to_string(),
        ..Default::default()
    }
    .with_input(Payload {
        ok: false,
        count: 0,
    })?;
    let target = new_bound_workflow_target(BoundWorkflowTarget {
        steps: vec![WorkflowStep {
            id: "run".to_string(),
            action: WorkflowStepAction::Plugin(plugin),
            ..Default::default()
        }],
    })?;
    let signal = new_workflow_signal(
        WorkflowSignal {
            name: "ready".to_string(),
            created_at: Some(created_at),
            sequence: 0,
            ..Default::default()
        }
        .with_payload(Payload { ok: true, count: 1 })?,
    )?;
    let run = new_bound_workflow_run(BoundWorkflowRun {
        id: "run-1".to_string(),
        status: WorkflowRunStatus::Pending,
        target: Some(new_bound_workflow_target_from_target(&target)?),
        trigger: Some(WorkflowRunTrigger::Manual),
        created_at: Some(created_at),
        ..Default::default()
    })?;

    let plugin = plugin_step(&target, 0)?;
    assert_eq!(plugin.name, "plugin");
    assert_eq!(
        plugin
            .input
            .as_ref()
            .and_then(literal_value)
            .and_then(|input| input.get("count")),
        Some(&json!(0))
    );
    assert_eq!(signal.sequence, 0);
    assert_eq!(run.created_at, Some(created_at));
    Ok(())
}

#[test]
fn workflow_copy_helpers_do_not_alias_nested_payloads() -> gestalt::Result<()> {
    let plugin = WorkflowStepPluginCall {
        name: "plugin".to_string(),
        operation: "run".to_string(),
        ..Default::default()
    }
    .with_input(serde_json::json!({"nested": {"value": "original"}}))?;
    let mut target = new_bound_workflow_target(BoundWorkflowTarget {
        steps: vec![WorkflowStep {
            id: "run".to_string(),
            action: WorkflowStepAction::Plugin(plugin),
            ..Default::default()
        }],
    })?;
    let copied = new_bound_workflow_target_from_target(&target)?;

    let plugin = plugin_step_mut(&mut target, 0)?;
    literal_value_mut(plugin.input.as_mut().expect("input"))?["nested"]["value"] = json!("changed");

    let copied_plugin = plugin_step(&copied, 0)?;
    assert_eq!(
        copied_plugin
            .input
            .as_ref()
            .and_then(literal_value)
            .and_then(|input| input.get("nested"))
            .and_then(|nested| nested.get("value")),
        Some(&json!("original"))
    );
    Ok(())
}

#[test]
fn workflow_steps_round_trip_through_copy_helpers() -> gestalt::Result<()> {
    let target = new_bound_workflow_target(BoundWorkflowTarget {
        steps: vec![
            WorkflowStep {
                id: "diagnosis".to_string(),
                action: WorkflowStepAction::Agent(WorkflowStepAgentTurn {
                    provider: "agent".to_string(),
                    model: "claude".to_string(),
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
                    tools: vec![AgentToolRef {
                        plugin: "datadog".to_string(),
                        operation: "queryLogs".to_string(),
                        ..Default::default()
                    }],
                    response_schema: Some(json!({"type": "object"})),
                    model_options: Some(json!({"temperature": 0})),
                    ..Default::default()
                }),
                timeout_seconds: 45,
                output_delivery: Some(WorkflowStepDelivery {
                    plugin: Some(WorkflowStepPluginCall {
                        name: "slack".to_string(),
                        operation: "reply".to_string(),
                        input: Some(WorkflowValue::Object(BTreeMap::from([(
                            "text".to_string(),
                            WorkflowValue::StepOutput(WorkflowStepOutputSource {
                                step_id: "diagnosis".to_string(),
                                path: "agent.text".to_string(),
                            }),
                        )]))),
                        ..Default::default()
                    }),
                }),
                metadata: Some(json!({"kind": "diagnosis"})),
                ..Default::default()
            },
            WorkflowStep {
                id: "pr_fix".to_string(),
                action: WorkflowStepAction::Agent(WorkflowStepAgentTurn {
                    provider: "agent".to_string(),
                    model: "claude".to_string(),
                    prompt: Some(WorkflowText {
                        template: "Open a PR.".to_string(),
                    }),
                    tools: vec![AgentToolRef {
                        plugin: "github".to_string(),
                        operation: "createPullRequest".to_string(),
                        ..Default::default()
                    }],
                    ..Default::default()
                }),
                when: Some(WorkflowStepWhen {
                    value: Some(WorkflowValue::StepOutput(WorkflowStepOutputSource {
                        step_id: "diagnosis".to_string(),
                        path: "agent.structuredOutput.actionable_for_pr".to_string(),
                    })),
                    equals: Some(json!(true)),
                }),
                ..Default::default()
            },
        ],
    })?;
    let copied = new_bound_workflow_target_from_target(&target)?;

    let diagnosis = agent_step(&target, 0)?;
    assert_eq!(target.steps.len(), 2);
    assert_eq!(diagnosis.tools[0].plugin, "datadog");
    assert_eq!(
        target.steps[1]
            .when
            .as_ref()
            .and_then(|when| when.equals.as_ref()),
        Some(&json!(true))
    );

    let copied_diagnosis = agent_step(&copied, 0)?;
    assert_eq!(copied_diagnosis.tools[0].operation, "queryLogs");
    assert_eq!(
        copied.steps[0]
            .output_delivery
            .as_ref()
            .and_then(|delivery| delivery.plugin.as_ref())
            .map(|plugin| plugin.name.as_str()),
        Some("slack")
    );
    assert_eq!(
        copied.steps[1]
            .when
            .as_ref()
            .and_then(|when| match when.value.as_ref() {
                Some(WorkflowValue::StepOutput(source)) => Some(source.path.as_str()),
                _ => None,
            }),
        Some("agent.structuredOutput.actionable_for_pr")
    );
    Ok(())
}

fn plugin_step(
    target: &gestalt::BoundWorkflowTarget,
    index: usize,
) -> gestalt::Result<&gestalt::WorkflowStepPluginCall> {
    match target.steps.get(index).map(|step| &step.action) {
        Some(gestalt::WorkflowStepAction::Plugin(plugin)) => Ok(plugin),
        _ => Err(gestalt::Error::bad_request("expected plugin step")),
    }
}

fn agent_step(
    target: &gestalt::BoundWorkflowTarget,
    index: usize,
) -> gestalt::Result<&gestalt::WorkflowStepAgentTurn> {
    match target.steps.get(index).map(|step| &step.action) {
        Some(gestalt::WorkflowStepAction::Agent(agent)) => Ok(agent),
        _ => Err(gestalt::Error::bad_request("expected agent step")),
    }
}

fn plugin_step_mut(
    target: &mut gestalt::BoundWorkflowTarget,
    index: usize,
) -> gestalt::Result<&mut gestalt::WorkflowStepPluginCall> {
    match target.steps.get_mut(index).map(|step| &mut step.action) {
        Some(gestalt::WorkflowStepAction::Plugin(plugin)) => Ok(plugin),
        _ => Err(gestalt::Error::bad_request("expected plugin step")),
    }
}

fn literal_value(input: &WorkflowValue) -> Option<&serde_json::Value> {
    match input {
        WorkflowValue::Literal(value) => Some(value),
        _ => None,
    }
}

fn literal_value_mut(input: &mut WorkflowValue) -> gestalt::Result<&mut serde_json::Value> {
    match input {
        WorkflowValue::Literal(value) => Ok(value),
        _ => Err(gestalt::Error::bad_request("expected literal value")),
    }
}
