use std::time::{Duration, UNIX_EPOCH};

use gestalt::proto::v1::bound_workflow_target;
use prost_types::value::Kind;
use serde::Serialize;

use gestalt::{
    BoundWorkflowPluginTargetInput, BoundWorkflowRunInput, BoundWorkflowTargetInput,
    WorkflowRunStatus, WorkflowRunTriggerInput, WorkflowSignalInput, new_bound_workflow_run,
    new_bound_workflow_target, new_bound_workflow_target_from_target, new_workflow_signal,
};

#[derive(Serialize)]
struct Payload {
    ok: bool,
    count: i32,
}

#[test]
fn workflow_builders_accept_serde_values_and_system_time() -> gestalt::Result<()> {
    let created_at = UNIX_EPOCH + Duration::from_secs(1_778_241_600);
    let plugin = BoundWorkflowPluginTargetInput {
        plugin_name: "plugin".to_string(),
        operation: "run".to_string(),
        ..Default::default()
    }
    .with_input(Payload {
        ok: false,
        count: 0,
    })?;
    let target = new_bound_workflow_target(BoundWorkflowTargetInput::Plugin(plugin))?;
    let signal = new_workflow_signal(
        WorkflowSignalInput {
            name: "ready".to_string(),
            created_at: Some(created_at),
            sequence: 0,
            ..Default::default()
        }
        .with_payload(Payload { ok: true, count: 1 })?,
    )?;
    let run = new_bound_workflow_run(BoundWorkflowRunInput {
        id: "run-1".to_string(),
        status: WorkflowRunStatus::Pending,
        target: Some(BoundWorkflowTargetInput::Plugin(
            gestalt::bound_workflow_plugin_target_input_from_target(plugin_target(&target)?)?,
        )),
        trigger: Some(WorkflowRunTriggerInput::Manual),
        created_at: Some(created_at),
        ..Default::default()
    })?;

    let plugin = plugin_target(&target)?;
    assert_eq!(plugin.plugin_name, "plugin");
    assert_eq!(
        plugin
            .input
            .as_ref()
            .and_then(|input| input.fields.get("count"))
            .and_then(|value| value.kind.as_ref()),
        Some(&Kind::NumberValue(0.0))
    );
    assert_eq!(signal.sequence, 0);
    assert_eq!(
        run.created_at.as_ref().map(|value| value.seconds),
        Some(1_778_241_600)
    );
    Ok(())
}

#[test]
fn workflow_copy_helpers_do_not_alias_nested_payloads() -> gestalt::Result<()> {
    let plugin = BoundWorkflowPluginTargetInput {
        plugin_name: "plugin".to_string(),
        operation: "run".to_string(),
        ..Default::default()
    }
    .with_input(serde_json::json!({"nested": {"value": "original"}}))?;
    let mut target = new_bound_workflow_target(BoundWorkflowTargetInput::Plugin(plugin))?;
    let copied = new_bound_workflow_target_from_target(&target)?;

    let plugin = plugin_target_mut(&mut target)?;
    let nested = plugin
        .input
        .as_mut()
        .and_then(|input| input.fields.get_mut("nested"))
        .and_then(|value| value.kind.as_mut())
        .and_then(|kind| match kind {
            Kind::StructValue(value) => Some(value),
            _ => None,
        })
        .expect("nested struct");
    nested.fields.insert(
        "value".to_string(),
        prost_types::Value {
            kind: Some(Kind::StringValue("changed".to_string())),
        },
    );

    let copied_plugin = plugin_target(&copied)?;
    assert_eq!(
        copied_plugin
            .input
            .as_ref()
            .and_then(|input| input.fields.get("nested"))
            .and_then(|value| value.kind.as_ref())
            .and_then(|kind| match kind {
                Kind::StructValue(value) => value.fields.get("value"),
                _ => None,
            })
            .and_then(|value| value.kind.as_ref()),
        Some(&Kind::StringValue("original".to_string()))
    );
    Ok(())
}

fn plugin_target(
    target: &gestalt::BoundWorkflowTarget,
) -> gestalt::Result<&gestalt::BoundWorkflowPluginTarget> {
    match target.kind.as_ref() {
        Some(bound_workflow_target::Kind::Plugin(plugin)) => Ok(plugin),
        _ => Err(gestalt::Error::bad_request("expected plugin target")),
    }
}

fn plugin_target_mut(
    target: &mut gestalt::BoundWorkflowTarget,
) -> gestalt::Result<&mut gestalt::BoundWorkflowPluginTarget> {
    match target.kind.as_mut() {
        Some(bound_workflow_target::Kind::Plugin(plugin)) => Ok(plugin),
        _ => Err(gestalt::Error::bad_request("expected plugin target")),
    }
}
