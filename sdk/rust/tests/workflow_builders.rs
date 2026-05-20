use std::time::{Duration, UNIX_EPOCH};

use prost_types::{Value, value};
use serde::Serialize;
use serde_json::json;

use gestalt::{
    AgentToolRef, BoundWorkflowTarget, WorkflowActivation, WorkflowActivationMode,
    WorkflowAgentMessage, WorkflowEvent, WorkflowEventActivation, WorkflowEventMatch,
    WorkflowScheduleActivation, WorkflowSignal, WorkflowStep, WorkflowStepAgentTurn,
    WorkflowStepPluginCall, WorkflowStepWhen, workflow_activation, workflow_json_from_struct,
    workflow_step, workflow_text, workflow_timestamp_from_system_time, workflow_value,
    workflow_value_literal, workflow_value_run_input,
    workflow_value_signal_payload, workflow_value_step_output,
};

#[derive(Serialize)]
struct Payload {
    ok: bool,
    count: i32,
}

#[test]
fn workflow_steps_accept_values_and_system_time() -> gestalt::Result<()> {
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
    let step = WorkflowStep {
        id: "refresh".to_string(),
        inputs: [(
            "customer_id".to_string(),
            workflow_value_run_input("customer_id"),
        )]
        .into(),
        action: Some(workflow_step::Action::Plugin(plugin)),
        ..Default::default()
    }
    .with_metadata(json!({ "kind": "plugin" }))?;
    let target = BoundWorkflowTarget::from_steps([step]);
    let signal = WorkflowSignal {
        name: "ready".to_string(),
        created_at: Some(workflow_timestamp_from_system_time(created_at)),
        sequence: 0,
        ..Default::default()
    }
    .with_payload(Payload { ok: true, count: 1 })?;

    assert_eq!(target.steps.len(), 1);
    assert_eq!(target.steps[0].id, "refresh");
    assert_eq!(signal.sequence, 0);
    assert_eq!(
        workflow_json_from_struct(signal.payload.as_ref().expect("payload")).get("count"),
        Some(&json!(1.0))
    );
    Ok(())
}

#[test]
fn workflow_copy_helpers_do_not_alias_nested_payloads() -> gestalt::Result<()> {
    let target = BoundWorkflowTarget::from_steps([WorkflowStep {
        id: "source".to_string(),
        action: Some(workflow_step::Action::Plugin(
            WorkflowStepPluginCall {
                name: "plugin".to_string(),
                operation: "run".to_string(),
                ..Default::default()
            }
            .with_input(json!({"nested": {"value": "original"}}))?,
        )),
        ..Default::default()
    }]);
    let mut copied = gestalt::new_bound_workflow_target_from_target(&target);

    let plugin = match copied.steps[0].action.as_mut().expect("action") {
        workflow_step::Action::Plugin(plugin) => plugin,
        _ => panic!("expected plugin action"),
    };
    plugin.input = Some(workflow_value_literal(
        json!({"nested": {"value": "changed"}}),
    )?);

    let original_plugin = match target.steps[0].action.as_ref().expect("action") {
        workflow_step::Action::Plugin(plugin) => plugin,
        _ => panic!("expected plugin action"),
    };
    let original_input = match original_plugin
        .input
        .as_ref()
        .and_then(|value| value.kind.as_ref())
    {
        Some(workflow_value::Kind::Literal(value)) => value,
        _ => panic!("expected literal input"),
    };
    assert_eq!(
        support_value_json(original_input)
            .pointer("/nested/value")
            .cloned(),
        Some(json!("original"))
    );
    Ok(())
}

#[test]
fn workflow_activations_and_agent_steps_cover_new_surface() -> gestalt::Result<()> {
    let activation = WorkflowActivation {
        id: "evt".to_string(),
        mode: WorkflowActivationMode::SignalOrStart as i32,
        input: Some(workflow_value_run_input("data")),
        run_key: Some(workflow_value_signal_payload("thread_ts")),
        kind: Some(workflow_activation::Kind::Event(WorkflowEventActivation {
            r#match: Some(WorkflowEventMatch {
                r#type: "slack.message".to_string(),
                source: "slack".to_string(),
                ..Default::default()
            }),
        })),
        ..Default::default()
    };
    let schedule = WorkflowActivation {
        id: "nightly".to_string(),
        mode: WorkflowActivationMode::Start as i32,
        kind: Some(workflow_activation::Kind::Schedule(
            WorkflowScheduleActivation {
                cron: "0 5 * * *".to_string(),
                timezone: "UTC".to_string(),
            },
        )),
        ..Default::default()
    };
    let agent_step = WorkflowStep {
        id: "summarize".to_string(),
        when: Some(WorkflowStepWhen {
            value: Some(workflow_value_step_output("classify", "actionable")),
            equals: Some(Value {
                kind: Some(value::Kind::BoolValue(true)),
            }),
        }),
        action: Some(workflow_step::Action::Agent(
            WorkflowStepAgentTurn {
                provider: "openai".to_string(),
                model: "gpt-5.1".to_string(),
                prompt: Some(workflow_text("Summarize the customer issue.")),
                messages: vec![WorkflowAgentMessage::text("system", "Be concise.")],
                ..Default::default()
            }
            .with_tools([AgentToolRef {
                plugin: "github".to_string(),
                operation: "createPullRequest".to_string(),
                ..Default::default()
            }])
            .with_model_options(json!({ "temperature": 0 }))?,
        )),
        ..Default::default()
    };

    assert_eq!(activation.id, "evt");
    assert_eq!(schedule.id, "nightly");
    let agent = match agent_step.action.as_ref().expect("agent action") {
        workflow_step::Action::Agent(agent) => agent,
        _ => panic!("expected agent action"),
    };
    assert_eq!(agent.tools[0].plugin, "github");
    assert_eq!(agent.messages[0].role, "system");
    Ok(())
}

#[test]
fn workflow_event_helpers_accept_data_and_extensions() -> gestalt::Result<()> {
    let event = WorkflowEvent {
        r#type: "customer.updated".to_string(),
        source: "crm".to_string(),
        ..Default::default()
    }
    .with_data(json!({ "customer_id": "cust_123" }))?
    .with_extension("trace_id", "trace-1")?;

    assert_eq!(
        workflow_json_from_struct(event.data.as_ref().expect("data")).get("customer_id"),
        Some(&json!("cust_123"))
    );
    assert!(event.extensions.contains_key("trace_id"));
    Ok(())
}

fn support_value_json(value: &Value) -> serde_json::Value {
    match value.kind.as_ref() {
        Some(value::Kind::NullValue(_)) | None => serde_json::Value::Null,
        Some(value::Kind::NumberValue(value)) => json!(value),
        Some(value::Kind::StringValue(value)) => json!(value),
        Some(value::Kind::BoolValue(value)) => json!(value),
        Some(value::Kind::StructValue(value)) => workflow_json_from_struct(value),
        Some(value::Kind::ListValue(value)) => {
            serde_json::Value::Array(value.values.iter().map(support_value_json).collect())
        }
    }
}
