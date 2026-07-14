//! Typed workflow authoring builder with explicit path helpers.
#![allow(missing_docs)]

use std::collections::BTreeMap;
use std::fs;
use std::path::PathBuf;

use serde::Deserialize;
use serde_json::{Map, Value, json};

use crate::agent::{AgentOutput, AgentOutputKind, AgentStructuredOutput};
use crate::app::AgentToolRef;
use crate::workflow::{
    BoundWorkflowTarget, WorkflowActivation, WorkflowActivationTrigger, WorkflowAgentMessage,
    WorkflowArray, WorkflowDefinitionSpec, WorkflowEventActivation, WorkflowEventMatch,
    WorkflowObject, WorkflowPathSource, WorkflowScheduleActivation, WorkflowStep,
    WorkflowStepAction, WorkflowStepAgentTurn, WorkflowStepAppCall, WorkflowStepInputSource,
    WorkflowStepOutputSource, WorkflowStepWhen, WorkflowText, WorkflowValue, WorkflowValueKind,
};

fn workflow_value_literal(value: Value) -> crate::Result<WorkflowValue> {
    Ok(WorkflowValue {
        kind: Some(WorkflowValueKind::Literal(value)),
    })
}

fn workflow_value_template(template: impl Into<String>) -> WorkflowValue {
    WorkflowValue {
        kind: Some(WorkflowValueKind::Template(WorkflowText {
            template: template.into(),
        })),
    }
}

fn workflow_value_input(path: impl Into<String>) -> WorkflowValue {
    WorkflowValue {
        kind: Some(WorkflowValueKind::Input(WorkflowPathSource {
            path: path.into(),
        })),
    }
}

fn workflow_value_signal(path: impl Into<String>) -> WorkflowValue {
    WorkflowValue {
        kind: Some(WorkflowValueKind::Signal(WorkflowPathSource {
            path: path.into(),
        })),
    }
}

fn workflow_value_step_output(step_id: impl Into<String>, path: impl Into<String>) -> WorkflowValue {
    WorkflowValue {
        kind: Some(WorkflowValueKind::StepOutput(WorkflowStepOutputSource {
            step_id: step_id.into(),
            path: path.into(),
        })),
    }
}

fn workflow_value_step_input(step_id: impl Into<String>, path: impl Into<String>) -> WorkflowValue {
    WorkflowValue {
        kind: Some(WorkflowValueKind::StepInput(WorkflowStepInputSource {
            step_id: step_id.into(),
            path: path.into(),
        })),
    }
}

fn workflow_value_object(fields: BTreeMap<String, WorkflowValue>) -> WorkflowValue {
    WorkflowValue {
        kind: Some(WorkflowValueKind::Object(WorkflowObject { fields })),
    }
}

fn workflow_value_array(values: Vec<WorkflowValue>) -> WorkflowValue {
    WorkflowValue {
        kind: Some(WorkflowValueKind::Array(WorkflowArray { values })),
    }
}

/// References a run-input path.
pub fn workflow_ref_input(path: impl Into<String>) -> WorkflowValue {
    workflow_value_input(path)
}

/// References an activation signal path.
pub fn workflow_ref_signal(path: impl Into<String>) -> WorkflowValue {
    workflow_value_signal(path)
}

/// References a prior step output path.
pub fn workflow_ref_step_output(step_id: impl Into<String>, path: impl Into<String>) -> WorkflowValue {
    workflow_value_step_output(step_id, path)
}

/// References a prior step input path.
pub fn workflow_ref_step_input(step_id: impl Into<String>, path: impl Into<String>) -> WorkflowValue {
    workflow_value_step_input(step_id, path)
}

/// Builds a literal workflow value.
pub fn workflow_ref_literal(value: Value) -> crate::Result<WorkflowValue> {
    workflow_value_literal(value)
}

/// Builds a template workflow value.
pub fn workflow_ref_template(template: impl Into<String>) -> WorkflowValue {
    workflow_value_template(template)
}

/// Builds an object workflow value.
pub fn workflow_ref_object(fields: BTreeMap<String, WorkflowValue>) -> WorkflowValue {
    workflow_value_object(fields)
}

/// Builds an array workflow value.
pub fn workflow_ref_array(values: Vec<WorkflowValue>) -> WorkflowValue {
    workflow_value_array(values)
}

/// Options for `define_workflow`.
#[derive(Clone, Debug)]
pub struct DefineWorkflowOptions {
    pub id: String,
    pub run_as: String,
    pub paused: bool,
}

/// Options for event activations.
#[derive(Clone, Debug, Default)]
pub struct WorkflowEventActivationOptions {
    pub id: String,
    pub source: String,
    pub subject: String,
    pub paused: bool,
}

/// Options for schedule activations.
#[derive(Clone, Debug, Default)]
pub struct WorkflowScheduleActivationOptions {
    pub id: String,
    pub timezone: String,
    pub paused: bool,
}

/// Event activation configuration for `WorkflowBuilder::on`.
#[derive(Clone)]
pub struct WorkflowEventActivationConfig {
    pub type_name: String,
    pub map_input: Option<fn() -> BTreeMap<String, WorkflowValue>>,
    pub options: WorkflowEventActivationOptions,
}

/// Schedule activation configuration for `WorkflowBuilder::on`.
#[derive(Clone)]
pub struct WorkflowScheduleActivationConfig {
    pub cron: String,
    pub map_input: Option<fn() -> BTreeMap<String, WorkflowValue>>,
    pub options: WorkflowScheduleActivationOptions,
}

/// Explicit scope helpers for step authoring.
#[derive(Clone, Copy, Debug, Default)]
pub struct WorkflowStepScope;

impl WorkflowStepScope {
    pub fn input(path: impl Into<String>) -> WorkflowValue {
        workflow_ref_input(path)
    }

    pub fn signal(path: impl Into<String>) -> WorkflowValue {
        workflow_ref_signal(path)
    }

    pub fn step_output(step_id: impl Into<String>, path: impl Into<String>) -> WorkflowValue {
        workflow_ref_step_output(step_id, path)
    }

    pub fn step_input(step_id: impl Into<String>, path: impl Into<String>) -> WorkflowValue {
        workflow_ref_step_input(step_id, path)
    }
}

/// Explicit scope helpers for event activation mapping.
#[derive(Clone, Copy, Debug, Default)]
pub struct WorkflowEventScope;

impl WorkflowEventScope {
    pub fn data(path: impl AsRef<str>) -> WorkflowValue {
        workflow_ref_signal(join_path("data", path.as_ref()))
    }
}

/// Explicit scope helpers for schedule activation mapping.
#[derive(Clone, Copy, Debug, Default)]
pub struct WorkflowActivationScope;

impl WorkflowActivationScope {
    pub fn input(path: impl Into<String>) -> WorkflowValue {
        workflow_ref_input(path)
    }
}

/// App step configuration.
#[derive(Clone)]
pub struct WorkflowStepAppConfig {
    pub name: String,
    pub operation: String,
    pub input: Option<fn(WorkflowStepScope) -> BTreeMap<String, WorkflowValue>>,
    pub input_map: Option<BTreeMap<String, WorkflowValue>>,
    pub connection: String,
    pub instance: String,
    pub credential_mode: String,
}

/// Agent message configuration.
#[derive(Clone)]
pub struct WorkflowStepAgentMessageConfig {
    pub role: String,
    pub text: String,
}

/// Agent step configuration.
#[derive(Clone)]
pub struct WorkflowStepAgentConfig {
    pub provider: String,
    pub model: String,
    pub session_key: String,
    pub prompt: String,
    pub messages: Vec<WorkflowStepAgentMessageConfig>,
    pub tools: Vec<AgentToolRef>,
    pub output: Option<AgentOutput>,
    pub model_options: Option<serde_json::Map<String, Value>>,
}

/// Step guard configuration.
#[derive(Clone)]
pub struct WorkflowStepWhenConfig {
    pub value: WorkflowValue,
    pub equals: Option<Value>,
}

/// Step configuration.
#[derive(Clone, Default)]
pub struct WorkflowStepConfig {
    pub inputs: Option<fn(WorkflowStepScope) -> BTreeMap<String, WorkflowValue>>,
    pub inputs_map: Option<BTreeMap<String, WorkflowValue>>,
    pub app: Option<WorkflowStepAppConfig>,
    pub agent: Option<WorkflowStepAgentConfig>,
    pub when: Option<WorkflowStepWhenConfig>,
    pub timeout_seconds: i32,
    pub metadata: Option<serde_json::Map<String, Value>>,
}

/// Activation configuration accepted by `WorkflowBuilder::on`.
pub enum WorkflowActivationConfig {
    /// Event activation.
    Event(WorkflowEventActivationConfig),
    /// Schedule activation.
    Schedule(WorkflowScheduleActivationConfig),
}

/// Incrementally authors a workflow definition spec.
#[derive(Clone, Debug)]
pub struct WorkflowBuilder {
    id: String,
    run_as: String,
    paused: bool,
    activations: Vec<WorkflowActivation>,
    steps: Vec<WorkflowStep>,
}

pub fn event(
    type_name: impl Into<String>,
    map_input: Option<fn() -> BTreeMap<String, WorkflowValue>>,
    options: WorkflowEventActivationOptions,
) -> WorkflowEventActivationConfig {
    WorkflowEventActivationConfig {
        type_name: type_name.into(),
        map_input,
        options,
    }
}

pub fn schedule(
    cron: impl Into<String>,
    map_input: Option<fn() -> BTreeMap<String, WorkflowValue>>,
    options: WorkflowScheduleActivationOptions,
) -> WorkflowScheduleActivationConfig {
    WorkflowScheduleActivationConfig {
        cron: cron.into(),
        map_input,
        options,
    }
}

pub fn define_workflow(options: DefineWorkflowOptions) -> crate::Result<WorkflowBuilder> {
    if options.run_as.trim().is_empty() {
        return Err(crate::Error::bad_request("define_workflow requires run_as"));
    }
    if options.id.trim().is_empty() {
        return Err(crate::Error::bad_request("define_workflow requires id"));
    }
    Ok(WorkflowBuilder {
        id: options.id,
        run_as: options.run_as,
        paused: options.paused,
        activations: Vec::new(),
        steps: Vec::new(),
    })
}

impl WorkflowBuilder {
    pub fn on(mut self, activation: WorkflowActivationConfig) -> Self {
        match activation {
            WorkflowActivationConfig::Event(activation) => {
                let activation_id = if activation.options.id.trim().is_empty() {
                    activation.type_name.clone()
                } else {
                    activation.options.id.clone()
                };
                let input = activation
                    .map_input
                    .map(|map_input| workflow_value_object(map_input()));
                self.activations.push(WorkflowActivation {
                    id: activation_id,
                    paused: activation.options.paused,
                    trigger: Some(WorkflowActivationTrigger::Event(WorkflowEventActivation {
                        r#match: Some(WorkflowEventMatch {
                            r#type: activation.type_name,
                            source: activation.options.source,
                            subject: activation.options.subject,
                        }),
                    })),
                    input,
                });
            }
            WorkflowActivationConfig::Schedule(activation) => {
                let activation_id = if activation.options.id.trim().is_empty() {
                    activation.cron.clone()
                } else {
                    activation.options.id.clone()
                };
                let input = activation
                    .map_input
                    .map(|map_input| workflow_value_object(map_input()));
                self.activations.push(WorkflowActivation {
                    id: activation_id,
                    paused: activation.options.paused,
                    trigger: Some(WorkflowActivationTrigger::Schedule(
                        WorkflowScheduleActivation {
                            cron: activation.cron,
                            timezone: activation.options.timezone,
                        },
                    )),
                    input,
                });
            }
        }
        self
    }

    pub fn step(mut self, step_id: impl Into<String>, config: WorkflowStepConfig) -> Self {
        let scope = WorkflowStepScope;
        let mut step = WorkflowStep {
            id: step_id.into(),
            ..Default::default()
        };
        if let Some(inputs) = config.inputs {
            step.inputs = inputs(scope);
        } else if let Some(inputs) = config.inputs_map {
            step.inputs = inputs;
        }
        if let Some(app) = config.app {
            let input = if let Some(map_input) = app.input {
                Some(workflow_value_object(map_input(scope)))
            } else {
                app.input_map.map(workflow_value_object)
            };
            step.action = Some(WorkflowStepAction::App(WorkflowStepAppCall {
                name: app.name,
                operation: app.operation,
                input,
                connection: app.connection,
                instance: app.instance,
                credential_mode: app.credential_mode,
            }));
        }
        if let Some(agent) = config.agent {
            step.action = Some(WorkflowStepAction::Agent(WorkflowStepAgentTurn {
                provider: agent.provider,
                model: agent.model,
                session_key: agent.session_key,
                prompt: Some(WorkflowText {
                    template: agent.prompt,
                }),
                messages: agent
                    .messages
                    .into_iter()
                    .map(|message| WorkflowAgentMessage {
                        role: message.role,
                        text: Some(WorkflowText {
                            template: message.text,
                        }),
                        ..Default::default()
                    })
                    .collect(),
                tools: agent.tools,
                output: agent.output,
                model_options: agent.model_options,
            }));
        }
        if let Some(when) = config.when {
            step.when = Some(WorkflowStepWhen {
                value: Some(when.value),
                equals: when.equals,
            });
        }
        step.timeout_seconds = config.timeout_seconds;
        step.metadata = config.metadata;
        self.steps.push(step);
        self
    }

    pub fn to_spec(self) -> WorkflowDefinitionSpec {
        WorkflowDefinitionSpec {
            id: self.id,
            run_as: self.run_as,
            paused: self.paused,
            activations: self.activations,
            target: if self.steps.is_empty() {
                None
            } else {
                Some(BoundWorkflowTarget { steps: self.steps })
            },
        }
    }
}

pub fn resolve_workflow_definition_spec(
    input: WorkflowDefinitionSpec,
) -> WorkflowDefinitionSpec {
    input
}

pub fn resolve_workflow_definition_spec_from_builder(
    builder: WorkflowBuilder,
) -> WorkflowDefinitionSpec {
    builder.to_spec()
}

/// Applies a workflow definition from either a builder or a raw spec.
pub async fn apply_workflow_definition(
    workflow: &mut crate::workflow::Workflow,
    provider: String,
    idempotency_key: String,
    spec: Option<WorkflowDefinitionSpecOrBuilder>,
) -> Result<crate::workflow::WorkflowDefinition, crate::rpc_support::GestaltError> {
    let resolved = spec.map(|value| value.into_spec());
    workflow
        .apply_definition(provider, idempotency_key, resolved)
        .await
}

/// Accepted workflow definition input for apply helpers.
pub enum WorkflowDefinitionSpecOrBuilder {
    /// A raw workflow definition spec.
    Spec(WorkflowDefinitionSpec),
    /// A typed workflow builder.
    Builder(WorkflowBuilder),
}

impl WorkflowDefinitionSpecOrBuilder {
    fn into_spec(self) -> WorkflowDefinitionSpec {
        match self {
            Self::Spec(spec) => spec,
            Self::Builder(builder) => builder.to_spec(),
        }
    }
}

#[derive(Debug, Deserialize)]
struct WorkflowLoweringContract {
    cases: Vec<WorkflowLoweringCase>,
}

#[derive(Debug, Deserialize)]
pub struct WorkflowLoweringCase {
    pub name: String,
    init: WorkflowLoweringInit,
    #[serde(default)]
    activations: Vec<WorkflowLoweringActivation>,
    #[serde(default)]
    steps: Vec<WorkflowLoweringStep>,
    #[serde(rename = "expectedSpec")]
    pub expected_spec: Value,
}

#[derive(Debug, Deserialize)]
struct WorkflowLoweringInit {
    id: String,
    #[serde(rename = "runAs")]
    run_as: String,
    #[serde(default)]
    paused: bool,
}

#[derive(Debug, Deserialize)]
struct WorkflowLoweringActivation {
    id: String,
    event: Option<WorkflowLoweringEvent>,
    schedule: Option<WorkflowLoweringSchedule>,
    input: Option<Value>,
    #[serde(default)]
    paused: bool,
}

#[derive(Debug, Deserialize)]
struct WorkflowLoweringEvent {
    #[serde(rename = "type")]
    type_name: String,
    #[serde(default)]
    source: String,
    #[serde(default)]
    subject: String,
}

#[derive(Debug, Deserialize)]
struct WorkflowLoweringSchedule {
    cron: String,
    #[serde(default)]
    timezone: String,
}

#[derive(Debug, Deserialize)]
struct WorkflowLoweringStep {
    id: String,
    inputs: Option<Value>,
    app: Option<WorkflowLoweringAppStep>,
    agent: Option<WorkflowLoweringAgentStep>,
    when: Option<WorkflowLoweringWhen>,
    #[serde(rename = "timeoutSeconds", default)]
    timeout_seconds: i32,
    metadata: Option<serde_json::Map<String, Value>>,
}

#[derive(Debug, Deserialize)]
struct WorkflowLoweringAppStep {
    name: String,
    operation: String,
    input: Option<Value>,
    #[serde(default)]
    connection: String,
    #[serde(default)]
    instance: String,
    #[serde(rename = "credentialMode", default)]
    credential_mode: String,
}

#[derive(Debug, Deserialize)]
struct WorkflowLoweringAgentTool {
    app: String,
    operation: String,
}

#[derive(Debug, Deserialize)]
struct WorkflowLoweringAgentStep {
    provider: String,
    #[serde(default)]
    model: String,
    #[serde(rename = "sessionKey", default)]
    session_key: String,
    prompt: Option<Value>,
    #[serde(default)]
    messages: Vec<WorkflowLoweringAgentMessage>,
    #[serde(default)]
    tools: Vec<WorkflowLoweringAgentTool>,
    output: Option<Value>,
    #[serde(rename = "modelOptions")]
    model_options: Option<serde_json::Map<String, Value>>,
}

#[derive(Debug, Deserialize)]
struct WorkflowLoweringAgentMessage {
    role: String,
    text: Value,
}

#[derive(Debug, Deserialize)]
struct WorkflowLoweringWhen {
    value: Value,
    equals: Option<Value>,
}

pub fn load_workflow_lowering_contract() -> crate::Result<Vec<WorkflowLoweringCase>> {
    let path = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("../fixtures/workflow-authoring/lowering-contract.json");
    let data = fs::read_to_string(&path)
        .map_err(|err| crate::Error::internal(format!("read workflow lowering contract: {err}")))?;
    let contract: WorkflowLoweringContract = serde_json::from_str(&data)
        .map_err(|err| crate::Error::internal(format!("parse workflow lowering contract: {err}")))?;
    Ok(contract.cases)
}

pub fn build_workflow_from_lowering_case(
    case_data: &WorkflowLoweringCase,
) -> crate::Result<WorkflowBuilder> {
    let mut builder = define_workflow(DefineWorkflowOptions {
        id: case_data.init.id.clone(),
        run_as: case_data.init.run_as.clone(),
        paused: case_data.init.paused,
    })?;

    for activation in &case_data.activations {
        if let Some(event) = &activation.event {
            let input = activation
                .input
                .as_ref()
                .map(lower_contract_object)
                .transpose()?
                .map(workflow_value_object);
            builder.activations.push(WorkflowActivation {
                id: activation.id.clone(),
                paused: activation.paused,
                trigger: Some(WorkflowActivationTrigger::Event(WorkflowEventActivation {
                    r#match: Some(WorkflowEventMatch {
                        r#type: event.type_name.clone(),
                        source: event.source.clone(),
                        subject: event.subject.clone(),
                    }),
                })),
                input,
            });
            continue;
        }
        if let Some(schedule) = &activation.schedule {
            let input = activation
                .input
                .as_ref()
                .map(lower_contract_object)
                .transpose()?
                .map(workflow_value_object);
            builder.activations.push(WorkflowActivation {
                id: activation.id.clone(),
                paused: activation.paused,
                trigger: Some(WorkflowActivationTrigger::Schedule(WorkflowScheduleActivation {
                    cron: schedule.cron.clone(),
                    timezone: schedule.timezone.clone(),
                })),
                input,
            });
        }
    }

    for step in &case_data.steps {
        let mut config = WorkflowStepConfig {
            timeout_seconds: step.timeout_seconds,
            metadata: step.metadata.clone(),
            ..Default::default()
        };
        if let Some(inputs) = &step.inputs {
            let fields = lower_contract_object(inputs)?;
            config.inputs_map = Some(fields);
        }
        if let Some(app) = &step.app {
            let input = app
                .input
                .as_ref()
                .map(lower_contract_object)
                .transpose()?;
            config.app = Some(WorkflowStepAppConfig {
                name: app.name.clone(),
                operation: app.operation.clone(),
                input: None,
                input_map: input,
                connection: app.connection.clone(),
                instance: app.instance.clone(),
                credential_mode: app.credential_mode.clone(),
            });
        }
        if let Some(agent) = &step.agent {
            let messages = agent
                .messages
                .iter()
                .map(|message| {
                    Ok(WorkflowStepAgentMessageConfig {
                        role: message.role.clone(),
                        text: lower_contract_text(&message.text)?,
                    })
                })
                .collect::<crate::Result<Vec<_>>>()?;
            config.agent = Some(WorkflowStepAgentConfig {
                provider: agent.provider.clone(),
                model: agent.model.clone(),
                session_key: agent.session_key.clone(),
                prompt: lower_contract_template(agent.prompt.as_ref())?,
                messages,
                tools: agent
                    .tools
                    .iter()
                    .map(|tool| AgentToolRef {
                        app: tool.app.clone(),
                        operation: tool.operation.clone(),
                        ..Default::default()
                    })
                    .collect(),
                output: agent.output.as_ref().map(parse_agent_output),
                model_options: agent.model_options.clone(),
            });
        }
        if let Some(when) = &step.when {
            config.when = Some(WorkflowStepWhenConfig {
                value: lower_contract_value(&when.value)?,
                equals: when.equals.clone(),
            });
        }
        builder = builder.step(step.id.clone(), config);
    }

    Ok(builder)
}

fn parse_agent_output(value: &Value) -> AgentOutput {
    let schema = value
        .get("structured")
        .and_then(|structured| structured.get("schema"))
        .and_then(|schema| schema.as_object().cloned());
    if let Some(schema) = schema {
        return AgentOutput {
            kind: Some(AgentOutputKind::Structured(AgentStructuredOutput {
                schema: Some(schema),
            })),
        };
    }
    AgentOutput::default()
}

fn lower_contract_object(node: &Value) -> crate::Result<BTreeMap<String, WorkflowValue>> {
    let kind = node.get("kind").and_then(Value::as_str).unwrap_or_default();
    if kind != "object" {
        return Err(crate::Error::bad_request(
            "lowering contract input must be an object node",
        ));
    }
    let fields = node
        .get("fields")
        .and_then(Value::as_object)
        .ok_or_else(|| crate::Error::bad_request("workflow object node requires fields"))?;
    let mut out = BTreeMap::new();
    for (key, nested) in fields {
        out.insert(key.clone(), lower_contract_value(nested)?);
    }
    Ok(out)
}

fn lower_contract_value(node: &Value) -> crate::Result<WorkflowValue> {
    let kind = node.get("kind").and_then(Value::as_str).unwrap_or_default();
    match kind {
        "input" => Ok(workflow_ref_input(
            node.get("path").and_then(Value::as_str).unwrap_or_default(),
        )),
        "signal" => Ok(workflow_ref_signal(
            node.get("path").and_then(Value::as_str).unwrap_or_default(),
        )),
        "stepOutput" => Ok(workflow_ref_step_output(
            node.get("stepId").and_then(Value::as_str).unwrap_or_default(),
            node.get("path").and_then(Value::as_str).unwrap_or_default(),
        )),
        "stepInput" => Ok(workflow_ref_step_input(
            node.get("stepId").and_then(Value::as_str).unwrap_or_default(),
            node.get("path").and_then(Value::as_str).unwrap_or_default(),
        )),
        "literal" => workflow_ref_literal(node.get("value").cloned().unwrap_or(Value::Null)),
        "template" => Ok(workflow_ref_template(
            node.get("template").and_then(Value::as_str).unwrap_or_default(),
        )),
        "object" => {
            let fields = lower_contract_object(node)?;
            Ok(workflow_ref_object(fields))
        }
        "array" => {
            let values = node
                .get("values")
                .and_then(Value::as_array)
                .ok_or_else(|| crate::Error::bad_request("workflow array node requires values"))?;
            let mut out = Vec::new();
            for item in values {
                out.push(lower_contract_value(item)?);
            }
            Ok(workflow_ref_array(out))
        }
        _ => Err(crate::Error::bad_request(format!(
            "unsupported workflow value kind: {kind}"
        ))),
    }
}

fn lower_contract_template(node: Option<&Value>) -> crate::Result<String> {
    let node = node.ok_or_else(|| crate::Error::bad_request("agent prompt is required"))?;
    if node.get("kind").and_then(Value::as_str) != Some("template") {
        return Err(crate::Error::bad_request(
            "agent prompt must be a template node in lowering contract",
        ));
    }
    Ok(node
        .get("template")
        .and_then(Value::as_str)
        .unwrap_or_default()
        .to_string())
}

fn lower_contract_text(node: &Value) -> crate::Result<String> {
    match node.get("kind").and_then(Value::as_str) {
        Some("literal") => Ok(node
            .get("value")
            .and_then(Value::as_str)
            .unwrap_or_default()
            .to_string()),
        Some("template") => Ok(node
            .get("template")
            .and_then(Value::as_str)
            .unwrap_or_default()
            .to_string()),
        _ => Err(crate::Error::bad_request(
            "agent message text must be literal or template",
        )),
    }
}

fn join_path(prefix: &str, path: impl AsRef<str>) -> String {
    let prefix = prefix.trim();
    let path = path.as_ref().trim();
    match (prefix.is_empty(), path.is_empty()) {
        (true, _) => path.to_string(),
        (_, true) => prefix.to_string(),
        _ => format!("{prefix}.{path}"),
    }
}

pub fn canonical_workflow_definition_spec(spec: &WorkflowDefinitionSpec) -> Value {
    json!({
        "id": spec.id,
        "runAs": spec.run_as,
        "paused": spec.paused,
        "activations": spec.activations.iter().map(canonical_activation).collect::<Vec<_>>(),
        "target": spec.target.as_ref().map(|target| json!({
            "steps": target.steps.iter().map(canonical_step).collect::<Vec<_>>(),
        })),
    })
}

fn canonical_activation(activation: &WorkflowActivation) -> Value {
    let mut out = json!({
        "id": activation.id,
        "paused": activation.paused,
    });
    if let Some(WorkflowActivationTrigger::Schedule(schedule)) = &activation.trigger {
        out["schedule"] = json!({
            "cron": schedule.cron,
            "timezone": schedule.timezone,
        });
    }
    if let Some(WorkflowActivationTrigger::Event(event)) = &activation.trigger {
        let match_value = event.r#match.as_ref();
        out["event"] = json!({
            "match": {
                "type": match_value.map(|value| value.r#type.as_str()).unwrap_or_default(),
                "source": match_value.map(|value| value.source.as_str()).unwrap_or_default(),
                "subject": match_value.map(|value| value.subject.as_str()).unwrap_or_default(),
            }
        });
    }
    if let Some(input) = &activation.input {
        out["input"] = canonical_workflow_value(input);
    }
    out
}

fn canonical_step(step: &WorkflowStep) -> Value {
    let mut out = json!({ "id": step.id });
    if !step.inputs.is_empty() {
        out["inputs"] = Value::Object(
            step.inputs
                .iter()
                .map(|(key, value)| (key.clone(), canonical_workflow_value(value)))
                .collect(),
        );
    }
    if let Some(WorkflowStepAction::App(app)) = &step.action {
        let mut app_value = json!({
            "name": app.name,
            "operation": app.operation,
        });
        if let Some(input) = &app.input {
            app_value["input"] = canonical_workflow_value(input);
        }
        out["app"] = app_value;
    }
    if let Some(WorkflowStepAction::Agent(agent)) = &step.action {
        let mut agent_value = json!({
            "provider": agent.provider,
            "model": agent.model,
        });
        if let Some(prompt) = &agent.prompt {
            agent_value["prompt"] = json!({ "template": prompt.template });
        }
        if !agent.messages.is_empty() {
            agent_value["messages"] = Value::Array(
                agent
                    .messages
                    .iter()
                    .map(|message| {
                        json!({
                            "role": message.role,
                            "text": {
                                "template": message.text.as_ref().map(|text| text.template.clone()).unwrap_or_default(),
                            }
                        })
                    })
                    .collect(),
            );
        }
        if !agent.tools.is_empty() {
            agent_value["tools"] = Value::Array(
                agent
                    .tools
                    .iter()
                    .map(|tool| json!({ "app": tool.app, "operation": tool.operation }))
                    .collect(),
            );
        }
        if let Some(output) = &agent.output {
        if let Some(AgentOutputKind::Structured(structured)) = output.kind.as_ref() {
            if let Some(schema) = &structured.schema {
                agent_value["output"] = json!({ "structured": { "schema": schema } });
            }
        }
        }
        if let Some(model_options) = &agent.model_options {
            agent_value["modelOptions"] = Value::Object(model_options.clone());
        }
        out["agent"] = agent_value;
    }
    if let Some(when) = &step.when {
        out["when"] = json!({
            "value": when.value.as_ref().map(canonical_workflow_value),
            "equals": when.equals,
        });
    }
    if step.timeout_seconds != 0 {
        out["timeoutSeconds"] = json!(step.timeout_seconds);
    }
    if let Some(metadata) = &step.metadata {
        out["metadata"] = Value::Object(metadata.clone());
    }
    out
}

fn canonical_workflow_value(value: &WorkflowValue) -> Value {
    match value.kind.as_ref() {
        Some(WorkflowValueKind::Literal(literal)) => json!({ "literal": literal }),
        Some(WorkflowValueKind::Template(template)) => json!({ "template": template.template }),
        Some(WorkflowValueKind::Input(source)) => json!({ "input": source.path }),
        Some(WorkflowValueKind::Signal(source)) => json!({ "signal": source.path }),
        Some(WorkflowValueKind::StepOutput(source)) => json!({
            "stepOutput": { "stepId": source.step_id, "path": source.path }
        }),
        Some(WorkflowValueKind::StepInput(source)) => json!({
            "stepInput": { "stepId": source.step_id, "path": source.path }
        }),
        Some(WorkflowValueKind::Object(object)) => json!({
            "object": object.fields.iter().map(|(key, value)| (key.clone(), canonical_workflow_value(value))).collect::<Map<_, _>>(),
        }),
        Some(WorkflowValueKind::Array(array)) => json!({
            "array": array.values.iter().map(canonical_workflow_value).collect::<Vec<_>>(),
        }),
        None => Value::Object(Map::new()),
    }
}
