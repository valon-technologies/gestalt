use std::time::{Duration, UNIX_EPOCH};

use serde::Serialize;
use serde_json::json;

use gestalt::{
    BoundWorkflowPluginTarget, BoundWorkflowRun, BoundWorkflowTarget, WorkflowRunStatus,
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
    let plugin = BoundWorkflowPluginTarget {
        plugin_name: "plugin".to_string(),
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

    let plugin = plugin_target(&target)?;
    assert_eq!(plugin.plugin_name, "plugin");
    assert_eq!(
        plugin.input.as_ref().and_then(|input| input.get("count")),
        Some(&json!(0))
    );
    assert_eq!(signal.sequence, 0);
    assert_eq!(run.created_at, Some(created_at));
    Ok(())
}

#[test]
fn workflow_copy_helpers_do_not_alias_nested_payloads() -> gestalt::Result<()> {
    let plugin = BoundWorkflowPluginTarget {
        plugin_name: "plugin".to_string(),
        operation: "run".to_string(),
        ..Default::default()
    }
    .with_input(serde_json::json!({"nested": {"value": "original"}}))?;
    let mut target = new_bound_workflow_target(BoundWorkflowTarget::Plugin(plugin))?;
    let copied = new_bound_workflow_target_from_target(&target)?;

    let plugin = plugin_target_mut(&mut target)?;
    plugin.input.as_mut().expect("input")["nested"]["value"] = json!("changed");

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

fn plugin_target(
    target: &gestalt::BoundWorkflowTarget,
) -> gestalt::Result<&gestalt::BoundWorkflowPluginTarget> {
    match target {
        gestalt::BoundWorkflowTarget::Plugin(plugin) => Ok(plugin),
        _ => Err(gestalt::Error::bad_request("expected plugin target")),
    }
}

fn plugin_target_mut(
    target: &mut gestalt::BoundWorkflowTarget,
) -> gestalt::Result<&mut gestalt::BoundWorkflowPluginTarget> {
    match target {
        gestalt::BoundWorkflowTarget::Plugin(plugin) => Ok(plugin),
        _ => Err(gestalt::Error::bad_request("expected plugin target")),
    }
}
