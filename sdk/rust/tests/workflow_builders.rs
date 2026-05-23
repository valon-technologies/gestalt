use std::time::{Duration, UNIX_EPOCH};

use serde::Serialize;
use serde_json::json;

use gestalt::{
    AgentMessage, AgentToolRef, BoundWorkflowAgentTarget, BoundWorkflowAppTarget,
    BoundWorkflowRun, BoundWorkflowTarget, WorkflowAgentStep, WorkflowAgentStepWhen,
    WorkflowOutputBinding, WorkflowOutputDelivery, WorkflowOutputValueSource, WorkflowRunStatus,
    WorkflowRunTrigger, WorkflowSignal, new_bound_workflow_run, new_bound_workflow_target,
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
    let app = BoundWorkflowAppTarget {
        app_name: "app".to_string(),
        operation: "run".to_string(),
        ..Default::default()
    }
    .with_input(Payload {
        ok: false,
        count: 0,
    })?;
    let target = new_bound_workflow_target(BoundWorkflowTarget::Plugin(plugin))?;
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
        target: Some(BoundWorkflowTarget::Plugin(
            gestalt::bound_workflow_plugin_target_input_from_target(plugin_target(&target)?)?,
        )),
        trigger: Some(WorkflowRunTrigger::Manual),
        created_at: Some(created_at),
        ..Default::default()
    })?;

    let app = plugin_target(&target)?;
    assert_eq!(app.app_name, "app");
    assert_eq!(
        app.input.as_ref().and_then(|input| input.get("count")),
        Some(&json!(0))
    );
    assert_eq!(signal.sequence, 0);
    assert_eq!(run.created_at, Some(created_at));
    Ok(())
}

#[test]
fn workflow_copy_helpers_do_not_alias_nested_payloads() -> gestalt::Result<()> {
    let app = BoundWorkflowAppTarget {
        app_name: "app".to_string(),
        operation: "run".to_string(),
        ..Default::default()
    }
    .with_input(serde_json::json!({"nested": {"value": "original"}}))?;
    let mut target = new_bound_workflow_target(BoundWorkflowTarget::Plugin(plugin))?;
    let copied = new_bound_workflow_target_from_target(&target)?;

    let app = plugin_target_mut(&mut target)?;
    app.input.as_mut().expect("input")["nested"]["value"] = json!("changed");

    let copied_plugin = plugin_target(&copied)?;
    assert_eq!(
        copied_plugin
            .input
            .as_ref()
            .and_then(|input| input.get("nested"))
            .and_then(|nested| nested.get("value")),
        Some(&json!("original"))
    );
    Ok(())
}

#[test]
fn agent_workflow_steps_round_trip_through_copy_helpers() -> gestalt::Result<()> {
    let target = new_bound_workflow_target(BoundWorkflowTarget::Agent(BoundWorkflowAgentTarget {
        provider_name: "agent".to_string(),
        model: "claude".to_string(),
        steps: vec![
            WorkflowAgentStep {
                id: "diagnosis".to_string(),
                prompt: "Diagnose the alert.".to_string(),
                messages: vec![AgentMessage {
                    role: "system".to_string(),
                    text: "Use concise replies.".to_string(),
                    ..Default::default()
                }],
                tool_refs: vec![AgentToolRef {
                    plugin: "datadog".to_string(),
                    operation: "queryLogs".to_string(),
                    ..Default::default()
                }],
                response_schema: Some(json!({"type": "object"})),
                model_options: Some(json!({"temperature": 0})),
                timeout_seconds: 45,
                output_delivery: Some(WorkflowOutputDelivery {
                    target: Some(BoundWorkflowAppTarget {
                        app_name: "slack".to_string(),
                        operation: "reply".to_string(),
                        ..Default::default()
                    }),
                    input_bindings: vec![WorkflowOutputBinding {
                        input_field: "text".to_string(),
                        value: Some(WorkflowOutputValueSource::AgentOutput("text".to_string())),
                    }],
                    ..Default::default()
                }),
                metadata: Some(json!({"kind": "diagnosis"})),
                ..Default::default()
            },
            WorkflowAgentStep {
                id: "pr_fix".to_string(),
                prompt: "Open a PR.".to_string(),
                tool_refs: vec![AgentToolRef {
                    plugin: "github".to_string(),
                    operation: "createPullRequest".to_string(),
                    ..Default::default()
                }],
                when: Some(WorkflowAgentStepWhen {
                    step_id: "diagnosis".to_string(),
                    output_path: "structured_output.actionable_for_pr".to_string(),
                    equals: Some(json!(true)),
                }),
                ..Default::default()
            },
        ],
        ..Default::default()
    }))?;
    let copied = new_bound_workflow_target_from_target(&target)?;

    let agent = agent_target(&target)?;
    assert_eq!(agent.steps.len(), 2);
    assert_eq!(agent.steps[0].tool_refs[0].plugin, "datadog");
    assert_eq!(
        agent.steps[1]
            .when
            .as_ref()
            .and_then(|when| when.equals.as_ref()),
        Some(&json!(true))
    );

    let copied_agent = agent_target(&copied)?;
    assert_eq!(
        copied_agent.steps[0]
            .output_delivery
            .as_ref()
            .and_then(|delivery| delivery.target.as_ref())
            .map(|target| target.app_name.as_str()),
        Some("slack")
    );
    assert_eq!(
        copied_agent.steps[1]
            .when
            .as_ref()
            .map(|when| when.output_path.as_str()),
        Some("structured_output.actionable_for_pr")
    );
    Ok(())
}

fn plugin_target(
    target: &gestalt::BoundWorkflowTarget,
) -> gestalt::Result<&gestalt::BoundWorkflowAppTarget> {
    match target {
        gestalt::BoundWorkflowTarget::Plugin(plugin) => Ok(plugin),
        _ => Err(gestalt::Error::bad_request("expected app target")),
    }
}

fn agent_target(
    target: &gestalt::BoundWorkflowTarget,
) -> gestalt::Result<&gestalt::BoundWorkflowAgentTarget> {
    match target {
        gestalt::BoundWorkflowTarget::Agent(agent) => Ok(agent),
        _ => Err(gestalt::Error::bad_request("expected agent target")),
    }
}

fn plugin_target_mut(
    target: &mut gestalt::BoundWorkflowTarget,
) -> gestalt::Result<&mut gestalt::BoundWorkflowAppTarget> {
    match target {
        gestalt::BoundWorkflowTarget::Plugin(plugin) => Ok(plugin),
        _ => Err(gestalt::Error::bad_request("expected app target")),
    }
}
