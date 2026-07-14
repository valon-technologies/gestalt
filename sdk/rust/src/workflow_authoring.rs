//! Typed workflow-definition authoring helpers.
#![allow(missing_docs)]

use std::collections::BTreeMap;

use serde_json::{Map, Value};

use crate::agent::AgentOutput;
use crate::app::AgentToolRef;
use crate::workflow::{
    BoundWorkflowTarget, WorkflowActivation, WorkflowActivationTrigger, WorkflowAgentMessage,
    WorkflowDefinitionSpec, WorkflowEventActivation, WorkflowEventMatch,
    WorkflowScheduleActivation, WorkflowStep, WorkflowStepAction, WorkflowStepAgentTurn,
    WorkflowStepAppCall, WorkflowStepWhen, WorkflowText, WorkflowValue, WorkflowValueKind,
};

type WorkflowMap = BTreeMap<String, WorkflowValue>;
type WorkflowMapFn = Box<dyn Fn() -> WorkflowMap + Send + Sync>;

fn value(kind: WorkflowValueKind) -> WorkflowValue {
    WorkflowValue { kind: Some(kind) }
}

/// References a run-input path.
pub fn input(path: impl Into<String>) -> WorkflowValue {
    value(WorkflowValueKind::Input(
        crate::workflow::WorkflowPathSource { path: path.into() },
    ))
}

/// References an activation signal path.
pub fn signal(path: impl Into<String>) -> WorkflowValue {
    value(WorkflowValueKind::Signal(
        crate::workflow::WorkflowPathSource { path: path.into() },
    ))
}

/// References a prior step output path.
pub fn step_output(step_id: impl Into<String>, path: impl Into<String>) -> WorkflowValue {
    value(WorkflowValueKind::StepOutput(
        crate::workflow::WorkflowStepOutputSource {
            step_id: step_id.into(),
            path: path.into(),
        },
    ))
}

/// References a prior step input path.
pub fn step_input(step_id: impl Into<String>, path: impl Into<String>) -> WorkflowValue {
    value(WorkflowValueKind::StepInput(
        crate::workflow::WorkflowStepInputSource {
            step_id: step_id.into(),
            path: path.into(),
        },
    ))
}

/// Builds a literal workflow value.
pub fn literal(literal: Value) -> crate::Result<WorkflowValue> {
    Ok(value(WorkflowValueKind::Literal(literal)))
}

/// Builds a workflow template value.
pub fn template(template: impl Into<String>) -> WorkflowValue {
    value(WorkflowValueKind::Template(WorkflowText {
        template: template.into(),
    }))
}

/// Builds an object workflow value.
pub fn object(fields: WorkflowMap) -> WorkflowValue {
    value(WorkflowValueKind::Object(crate::workflow::WorkflowObject {
        fields,
    }))
}

/// Builds an array workflow value.
pub fn array(values: Vec<WorkflowValue>) -> WorkflowValue {
    value(WorkflowValueKind::Array(crate::workflow::WorkflowArray {
        values,
    }))
}

#[derive(Clone, Debug)]
pub struct WorkflowBuilder {
    id: String,
    run_as: String,
    paused: bool,
    activations: Vec<WorkflowActivation>,
    steps: Vec<WorkflowStep>,
}

/// Starts a fluent workflow definition builder.
pub fn define_workflow(
    id: impl Into<String>,
    run_as: impl Into<String>,
) -> crate::Result<WorkflowBuilder> {
    let id = id.into();
    let run_as = run_as.into();
    if id.trim().is_empty() {
        return Err(crate::Error::bad_request("define_workflow requires id"));
    }
    if run_as.trim().is_empty() {
        return Err(crate::Error::bad_request("define_workflow requires run_as"));
    }
    Ok(WorkflowBuilder {
        id,
        run_as,
        paused: false,
        activations: Vec::new(),
        steps: Vec::new(),
    })
}

impl WorkflowBuilder {
    /// Sets whether the definition starts paused.
    pub fn paused(mut self, paused: bool) -> Self {
        self.paused = paused;
        self
    }

    pub fn on(mut self, activation: WorkflowActivationConfig) -> Self {
        let (id, paused, trigger, input) = match activation.kind {
            WorkflowActivationKind::Event {
                type_name,
                map_input,
            } => (
                type_name.clone(),
                false,
                WorkflowActivationTrigger::Event(WorkflowEventActivation {
                    r#match: Some(WorkflowEventMatch {
                        r#type: type_name,
                        ..Default::default()
                    }),
                }),
                Some(object(map_input())),
            ),
            WorkflowActivationKind::Schedule { cron, map_input } => (
                cron.clone(),
                false,
                WorkflowActivationTrigger::Schedule(WorkflowScheduleActivation {
                    cron,
                    ..Default::default()
                }),
                Some(object(map_input())),
            ),
        };
        self.activations.push(WorkflowActivation {
            id,
            paused,
            trigger: Some(trigger),
            input,
        });
        self
    }

    pub fn step(mut self, step_id: impl Into<String>, config: WorkflowStepConfig) -> Self {
        assert!(
            !(config.app.is_some() && config.agent.is_some()),
            "workflow step cannot configure both app and agent actions"
        );
        let mut step = WorkflowStep {
            id: step_id.into(),
            ..Default::default()
        };
        if let Some(inputs) = config.inputs {
            step.inputs = inputs();
        }
        if let Some(app) = config.app {
            step.action = Some(WorkflowStepAction::App(WorkflowStepAppCall {
                name: app.name,
                operation: app.operation,
                input: app.input.map(|callback| object(callback())),
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
                prompt: Some(agent.prompt),
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
        step.when = config.when.map(|when| WorkflowStepWhen {
            value: Some(when.value),
            equals: when.equals,
        });
        step.timeout_seconds = config.timeout_seconds;
        step.metadata = config.metadata;
        self.steps.push(step);
        self
    }

    /// Consumes the builder and returns the generated workflow definition spec.
    pub fn to_spec(self) -> WorkflowDefinitionSpec {
        WorkflowDefinitionSpec {
            id: self.id,
            run_as: self.run_as,
            paused: self.paused,
            activations: self.activations,
            target: (!self.steps.is_empty()).then_some(BoundWorkflowTarget { steps: self.steps }),
        }
    }
}

pub struct WorkflowActivationConfig {
    kind: WorkflowActivationKind,
}

enum WorkflowActivationKind {
    Event {
        type_name: String,
        map_input: WorkflowMapFn,
    },
    Schedule {
        cron: String,
        map_input: WorkflowMapFn,
    },
}

/// Creates an event activation for [`WorkflowBuilder::on`].
pub fn event<F>(type_name: impl Into<String>, map_input: F) -> WorkflowActivationConfig
where
    F: Fn() -> BTreeMap<String, WorkflowValue> + Send + Sync + 'static,
{
    WorkflowActivationConfig {
        kind: WorkflowActivationKind::Event {
            type_name: type_name.into(),
            map_input: Box::new(map_input),
        },
    }
}

/// Creates a schedule activation for [`WorkflowBuilder::on`].
pub fn schedule<F>(cron: impl Into<String>, map_input: F) -> WorkflowActivationConfig
where
    F: Fn() -> BTreeMap<String, WorkflowValue> + Send + Sync + 'static,
{
    WorkflowActivationConfig {
        kind: WorkflowActivationKind::Schedule {
            cron: cron.into(),
            map_input: Box::new(map_input),
        },
    }
}

pub struct WorkflowStepAppConfig {
    pub name: String,
    pub operation: String,
    input: Option<WorkflowMapFn>,
    pub connection: String,
    pub instance: String,
    pub credential_mode: String,
}

impl WorkflowStepAppConfig {
    /// Creates an app step configuration. Add mapped input with [`Self::with_input`].
    pub fn new(name: impl Into<String>, operation: impl Into<String>) -> Self {
        Self {
            name: name.into(),
            operation: operation.into(),
            input: None,
            connection: String::new(),
            instance: String::new(),
            credential_mode: String::new(),
        }
    }

    /// Sets a zero-argument input mapper; the closure is boxed by the SDK.
    pub fn with_input<F>(mut self, input: F) -> Self
    where
        F: Fn() -> BTreeMap<String, WorkflowValue> + Send + Sync + 'static,
    {
        self.input = Some(Box::new(input));
        self
    }
}

impl Default for WorkflowStepAppConfig {
    fn default() -> Self {
        Self::new("", "")
    }
}

impl WorkflowStepAppConfig {
    pub fn with_connection(mut self, connection: impl Into<String>) -> Self {
        self.connection = connection.into();
        self
    }

    pub fn with_instance(mut self, instance: impl Into<String>) -> Self {
        self.instance = instance.into();
        self
    }

    pub fn with_credential_mode(mut self, credential_mode: impl Into<String>) -> Self {
        self.credential_mode = credential_mode.into();
        self
    }
}

#[derive(Clone)]
pub struct WorkflowStepAgentMessageConfig {
    pub role: String,
    pub text: String,
}

#[derive(Clone, Default)]
pub struct WorkflowStepAgentConfig {
    pub provider: String,
    pub model: String,
    pub session_key: String,
    pub prompt: WorkflowText,
    pub messages: Vec<WorkflowStepAgentMessageConfig>,
    pub tools: Vec<AgentToolRef>,
    pub output: Option<AgentOutput>,
    pub model_options: Option<Map<String, Value>>,
}

impl WorkflowStepAgentConfig {
    pub fn new(provider: impl Into<String>) -> Self {
        Self {
            provider: provider.into(),
            ..Default::default()
        }
    }

    pub fn with_prompt(mut self, prompt: WorkflowText) -> Self {
        self.prompt = prompt;
        self
    }
}

#[derive(Clone)]
pub struct WorkflowStepWhenConfig {
    pub value: WorkflowValue,
    pub equals: Option<Value>,
}

#[derive(Default)]
pub struct WorkflowStepConfig {
    inputs: Option<WorkflowMapFn>,
    pub app: Option<WorkflowStepAppConfig>,
    pub agent: Option<WorkflowStepAgentConfig>,
    pub when: Option<WorkflowStepWhenConfig>,
    pub timeout_seconds: i32,
    pub metadata: Option<Map<String, Value>>,
}

impl WorkflowStepConfig {
    pub fn with_inputs<F>(mut self, inputs: F) -> Self
    where
        F: Fn() -> BTreeMap<String, WorkflowValue> + Send + Sync + 'static,
    {
        self.inputs = Some(Box::new(inputs));
        self
    }
}

fn value_placeholder(value: &WorkflowValue) -> crate::Result<String> {
    let result = match value.kind.as_ref() {
        Some(WorkflowValueKind::Input(source)) => format!("${{{{ input.{} }}}}", source.path),
        Some(WorkflowValueKind::Signal(source)) => format!("${{{{ signal.{} }}}}", source.path),
        Some(WorkflowValueKind::StepOutput(source)) => {
            format!(
                "${{{{ steps.{}.outputs.{} }}}}",
                source.step_id, source.path
            )
        }
        Some(WorkflowValueKind::StepInput(source)) => {
            format!("${{{{ steps.{}.inputs.{} }}}}", source.step_id, source.path)
        }
        _ => {
            return Err(crate::Error::bad_request(
                "text references must be path values",
            ));
        }
    };
    Ok(result)
}

/// Composes workflow prompt text from workflow value references.
pub fn text(parts: &[WorkflowValue]) -> crate::Result<WorkflowText> {
    let mut template = String::new();
    for part in parts {
        template.push_str(&value_placeholder(part)?);
    }
    Ok(WorkflowText { template })
}

pub enum WorkflowDefinitionSpecOrBuilder {
    Spec(WorkflowDefinitionSpec),
    Builder(WorkflowBuilder),
}

impl From<WorkflowBuilder> for WorkflowDefinitionSpecOrBuilder {
    fn from(builder: WorkflowBuilder) -> Self {
        Self::Builder(builder)
    }
}

impl From<WorkflowDefinitionSpec> for WorkflowDefinitionSpecOrBuilder {
    fn from(spec: WorkflowDefinitionSpec) -> Self {
        Self::Spec(spec)
    }
}

impl WorkflowDefinitionSpecOrBuilder {
    fn into_spec(self) -> WorkflowDefinitionSpec {
        match self {
            Self::Spec(spec) => spec,
            Self::Builder(builder) => builder.to_spec(),
        }
    }
}

/// Applies a workflow definition from a raw spec or an authored builder.
pub async fn apply_workflow_definition(
    workflow: &mut crate::workflow::Workflow,
    provider: String,
    idempotency_key: String,
    spec: Option<impl Into<WorkflowDefinitionSpecOrBuilder>>,
) -> Result<crate::workflow::WorkflowDefinition, crate::rpc_support::GestaltError> {
    workflow
        .apply_definition(
            provider,
            idempotency_key,
            spec.map(|value| value.into().into_spec()),
        )
        .await
}
