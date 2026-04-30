//! OpenTelemetry helpers for provider-authored GenAI instrumentation.
//!
//! Gestalt injects standard `OTEL_*` environment variables into provider
//! processes from the host telemetry configuration. This module records GenAI
//! semantic-convention spans and metrics through the process-global
//! OpenTelemetry API configured by the provider runtime.

use std::any::type_name;
use std::error::Error;
use std::time::Instant;

use opentelemetry::global;
use opentelemetry::metrics::Histogram;
use opentelemetry::trace::{Span, SpanKind, Status, Tracer};
use opentelemetry::{Array, KeyValue, StringValue, Value};
use std::sync::OnceLock;

/// OpenTelemetry instrumentation scope used by the Gestalt SDK.
pub const TELEMETRY_INSTRUMENTATION_NAME: &str = "gestalt.provider";
/// Default GenAI provider name used for Gestalt-owned agent and tool work.
pub const GENAI_PROVIDER_NAME: &str = "gestalt";

pub const GENAI_OPERATION_CHAT: &str = "chat";
pub const GENAI_OPERATION_EXECUTE_TOOL: &str = "execute_tool";
pub const GENAI_OPERATION_INVOKE_AGENT: &str = "invoke_agent";

pub const GENAI_TOOL_TYPE_DATASTORE: &str = "datastore";
pub const GENAI_TOOL_TYPE_EXTENSION: &str = "extension";

const OPERATION_DURATION_METRIC: &str = "gen_ai.client.operation.duration";
const TOKEN_USAGE_METRIC: &str = "gen_ai.client.token.usage";
const OPERATION_DURATION_BUCKETS: &[f64] = &[
    0.01, 0.02, 0.04, 0.08, 0.16, 0.32, 0.64, 1.28, 2.56, 5.12, 10.24, 20.48, 40.96, 81.92,
];
const TOKEN_USAGE_BUCKETS: &[f64] = &[
    1.0, 4.0, 16.0, 64.0, 256.0, 1024.0, 4096.0, 16384.0, 65536.0, 262144.0, 1048576.0, 4194304.0,
    16777216.0, 67108864.0,
];

static OPERATION_DURATION: OnceLock<Histogram<f64>> = OnceLock::new();
static TOKEN_USAGE: OnceLock<Histogram<u64>> = OnceLock::new();

/// Options for recording an upstream model SDK call.
#[derive(Clone, Debug, Default)]
pub struct ModelOperationOptions {
    pub provider_name: String,
    pub request_model: String,
    pub request_options: RequestOptions,
    pub request_attributes: Vec<KeyValue>,
}

impl ModelOperationOptions {
    pub fn new(provider_name: impl Into<String>, request_model: impl Into<String>) -> Self {
        Self {
            provider_name: provider_name.into(),
            request_model: request_model.into(),
            ..Self::default()
        }
    }

    pub fn with_request_options(mut self, request_options: RequestOptions) -> Self {
        self.request_options = request_options;
        self
    }

    pub fn with_request_attribute(mut self, attribute: KeyValue) -> Self {
        self.request_attributes.push(attribute);
        self
    }
}

/// Common GenAI request options that are useful as span attributes.
#[derive(Clone, Debug, Default)]
pub struct RequestOptions {
    pub choice_count: Option<i64>,
    pub frequency_penalty: Option<f64>,
    pub max_tokens: Option<i64>,
    pub presence_penalty: Option<f64>,
    pub seed: Option<i64>,
    pub temperature: Option<f64>,
    pub top_k: Option<i64>,
    pub top_p: Option<f64>,
}

/// Options for recording provider-owned agent turn execution.
#[derive(Clone, Debug, Default)]
pub struct AgentInvocationOptions {
    pub agent_name: String,
    pub session_id: String,
    pub turn_id: String,
    pub model: String,
}

impl AgentInvocationOptions {
    pub fn new(
        agent_name: impl Into<String>,
        session_id: impl Into<String>,
        turn_id: impl Into<String>,
        model: impl Into<String>,
    ) -> Self {
        Self {
            agent_name: agent_name.into(),
            session_id: session_id.into(),
            turn_id: turn_id.into(),
            model: model.into(),
        }
    }
}

/// Options for recording provider-owned tool execution.
#[derive(Clone, Debug, Default)]
pub struct ToolExecutionOptions {
    pub tool_name: String,
    pub tool_call_id: String,
    pub tool_type: String,
}

impl ToolExecutionOptions {
    pub fn new(tool_name: impl Into<String>) -> Self {
        Self {
            tool_name: tool_name.into(),
            tool_type: GENAI_TOOL_TYPE_EXTENSION.to_string(),
            ..Self::default()
        }
    }

    pub fn with_tool_call_id(mut self, tool_call_id: impl Into<String>) -> Self {
        self.tool_call_id = tool_call_id.into();
        self
    }

    pub fn with_tool_type(mut self, tool_type: impl Into<String>) -> Self {
        self.tool_type = tool_type.into();
        self
    }
}

/// GenAI token usage recorded on spans and token usage metrics.
#[derive(Clone, Debug, Default)]
pub struct TokenUsage {
    pub input_tokens: Option<u64>,
    pub output_tokens: Option<u64>,
    pub cache_creation_input_tokens: Option<u64>,
    pub cache_read_input_tokens: Option<u64>,
    pub reasoning_output_tokens: Option<u64>,
}

/// Records a GenAI span plus operation duration and token usage metrics.
#[derive(Debug)]
pub struct GenAIOperation {
    span: global::BoxedSpan,
    started_at: Instant,
    metric_attributes: Vec<KeyValue>,
    error_type: Option<String>,
    ended: bool,
}

impl GenAIOperation {
    /// Ends the span and records operation duration.
    pub fn end(&mut self) {
        if self.ended {
            return;
        }
        self.ended = true;

        let mut attributes = self.metric_attributes.clone();
        if let Some(error_type) = self.error_type.clone() {
            append_or_replace(
                &mut attributes,
                KeyValue::new("error.type", error_type.to_string()),
            );
        }
        operation_duration().record(self.started_at.elapsed().as_secs_f64(), &attributes);
        self.span.end();
    }

    /// Marks the operation span and duration metric as failed.
    pub fn mark_error(&mut self, error_type: impl Into<String>, description: impl Into<String>) {
        let error_type = clean_string(error_type.into()).unwrap_or_else(|| "_OTHER".to_string());
        self.error_type = Some(error_type.clone());
        append_or_replace(
            &mut self.metric_attributes,
            KeyValue::new("error.type", error_type.clone()),
        );
        self.span
            .set_attribute(KeyValue::new("error.type", error_type.clone()));
        self.span.set_status(Status::error(description.into()));
    }

    /// Records an error object and marks the operation as failed.
    pub fn record_error<E>(&mut self, err: &E)
    where
        E: Error + 'static,
    {
        self.mark_error(type_name::<E>(), err.to_string());
        self.span.record_error(err);
    }

    /// Sets a span attribute. `gen_ai.response.model` is also added to metric attributes.
    pub fn set_attribute(&mut self, attribute: KeyValue) {
        if attribute.key.as_str() == "gen_ai.response.model" {
            append_or_replace(&mut self.metric_attributes, attribute.clone());
        }
        self.span.set_attribute(attribute);
    }

    /// Attaches common GenAI response metadata to the span.
    pub fn set_response_metadata(
        &mut self,
        response_id: Option<&str>,
        response_model: Option<&str>,
        finish_reasons: &[&str],
    ) {
        if let Some(response_id) = clean_string(response_id.unwrap_or_default()) {
            self.set_attribute(KeyValue::new("gen_ai.response.id", response_id));
        }
        if let Some(response_model) = clean_string(response_model.unwrap_or_default()) {
            self.set_attribute(KeyValue::new("gen_ai.response.model", response_model));
        }
        let finish_reasons = finish_reasons
            .iter()
            .filter_map(|reason| clean_string(*reason).map(StringValue::from))
            .collect::<Vec<_>>();
        if !finish_reasons.is_empty() {
            self.set_attribute(KeyValue::new(
                "gen_ai.response.finish_reasons",
                Value::Array(Array::String(finish_reasons)),
            ));
        }
    }

    /// Records GenAI token usage on the span and token usage metric.
    pub fn record_usage(&mut self, usage: TokenUsage) {
        self.set_u64_attribute("gen_ai.usage.input_tokens", usage.input_tokens);
        self.set_u64_attribute("gen_ai.usage.output_tokens", usage.output_tokens);
        self.set_u64_attribute(
            "gen_ai.usage.cache_creation.input_tokens",
            usage.cache_creation_input_tokens,
        );
        self.set_u64_attribute(
            "gen_ai.usage.cache_read.input_tokens",
            usage.cache_read_input_tokens,
        );
        self.set_u64_attribute(
            "gen_ai.usage.reasoning.output_tokens",
            usage.reasoning_output_tokens,
        );

        self.record_token_usage(usage.input_tokens, "input");
        self.record_token_usage(usage.output_tokens, "output");
    }

    fn set_u64_attribute(&mut self, key: &'static str, value: Option<u64>) {
        let Some(value) = value else {
            return;
        };
        if value <= i64::MAX as u64 {
            self.set_attribute(KeyValue::new(key, value as i64));
        }
    }

    fn record_token_usage(&self, tokens: Option<u64>, token_type: &'static str) {
        let Some(tokens) = tokens else {
            return;
        };
        let mut attributes = self.metric_attributes.clone();
        append_or_replace(
            &mut attributes,
            KeyValue::new("gen_ai.token.type", token_type),
        );
        token_usage().record(tokens, &attributes);
    }
}

impl Drop for GenAIOperation {
    fn drop(&mut self) {
        self.end();
    }
}

/// Starts a GenAI client span for an upstream model SDK call.
pub fn model_operation(options: ModelOperationOptions) -> GenAIOperation {
    let provider_name = clean_string(options.provider_name).unwrap_or_else(|| "_OTHER".to_string());
    let request_model = clean_string(options.request_model).unwrap_or_default();
    let metric_attributes = vec![
        KeyValue::new("gen_ai.operation.name", GENAI_OPERATION_CHAT),
        KeyValue::new("gen_ai.provider.name", provider_name),
        KeyValue::new("gen_ai.request.model", request_model.clone()),
    ];
    let mut span_attributes = metric_attributes.clone();
    span_attributes.extend(request_option_attributes(options.request_options));
    span_attributes.extend(options.request_attributes);

    start_operation(
        span_name(GENAI_OPERATION_CHAT, &request_model),
        SpanKind::Client,
        span_attributes,
        metric_attributes,
    )
}

/// Starts a GenAI internal span for provider-owned agent turn execution.
pub fn agent_invocation(options: AgentInvocationOptions) -> GenAIOperation {
    let agent_name = clean_string(options.agent_name).unwrap_or_else(|| "provider".to_string());
    let model = clean_string(options.model).unwrap_or_default();
    let span_attributes = vec![
        KeyValue::new("gen_ai.operation.name", GENAI_OPERATION_INVOKE_AGENT),
        KeyValue::new("gen_ai.provider.name", GENAI_PROVIDER_NAME),
        KeyValue::new("gen_ai.agent.name", agent_name.clone()),
        KeyValue::new(
            "gen_ai.conversation.id",
            clean_string(options.session_id).unwrap_or_default(),
        ),
        KeyValue::new("gen_ai.request.model", model.clone()),
        KeyValue::new(
            "gestalt.agent.turn_id",
            clean_string(options.turn_id).unwrap_or_default(),
        ),
    ];
    let metric_attributes = vec![
        KeyValue::new("gen_ai.operation.name", GENAI_OPERATION_INVOKE_AGENT),
        KeyValue::new("gen_ai.provider.name", GENAI_PROVIDER_NAME),
        KeyValue::new("gen_ai.agent.name", agent_name.clone()),
        KeyValue::new("gen_ai.request.model", model),
    ];

    start_operation(
        span_name(GENAI_OPERATION_INVOKE_AGENT, &agent_name),
        SpanKind::Internal,
        span_attributes,
        metric_attributes,
    )
}

/// Starts a GenAI internal span for provider-owned tool execution.
pub fn tool_execution(options: ToolExecutionOptions) -> GenAIOperation {
    let tool_name = clean_string(options.tool_name).unwrap_or_else(|| "_OTHER".to_string());
    let tool_type =
        clean_string(options.tool_type).unwrap_or_else(|| GENAI_TOOL_TYPE_EXTENSION.to_string());
    let span_attributes = vec![
        KeyValue::new("gen_ai.operation.name", GENAI_OPERATION_EXECUTE_TOOL),
        KeyValue::new("gen_ai.provider.name", GENAI_PROVIDER_NAME),
        KeyValue::new("gen_ai.tool.name", tool_name.clone()),
        KeyValue::new(
            "gen_ai.tool.call.id",
            clean_string(options.tool_call_id).unwrap_or_default(),
        ),
        KeyValue::new("gen_ai.tool.type", tool_type.clone()),
    ];
    let metric_attributes = vec![
        KeyValue::new("gen_ai.operation.name", GENAI_OPERATION_EXECUTE_TOOL),
        KeyValue::new("gen_ai.provider.name", GENAI_PROVIDER_NAME),
        KeyValue::new("gen_ai.tool.name", tool_name.clone()),
        KeyValue::new("gen_ai.tool.type", tool_type),
    ];

    start_operation(
        span_name(GENAI_OPERATION_EXECUTE_TOOL, &tool_name),
        SpanKind::Internal,
        span_attributes,
        metric_attributes,
    )
}

fn start_operation(
    name: String,
    kind: SpanKind,
    span_attributes: Vec<KeyValue>,
    metric_attributes: Vec<KeyValue>,
) -> GenAIOperation {
    let tracer = global::tracer(TELEMETRY_INSTRUMENTATION_NAME);
    let span = tracer
        .span_builder(name)
        .with_kind(kind)
        .with_attributes(span_attributes)
        .start(&tracer);

    GenAIOperation {
        span,
        started_at: Instant::now(),
        metric_attributes,
        error_type: None,
        ended: false,
    }
}

fn operation_duration() -> &'static Histogram<f64> {
    OPERATION_DURATION.get_or_init(|| {
        global::meter(TELEMETRY_INSTRUMENTATION_NAME)
            .f64_histogram(OPERATION_DURATION_METRIC)
            .with_unit("s")
            .with_description("GenAI operation duration.")
            .with_boundaries(OPERATION_DURATION_BUCKETS.to_vec())
            .build()
    })
}

fn token_usage() -> &'static Histogram<u64> {
    TOKEN_USAGE.get_or_init(|| {
        global::meter(TELEMETRY_INSTRUMENTATION_NAME)
            .u64_histogram(TOKEN_USAGE_METRIC)
            .with_unit("{token}")
            .with_description("Number of input and output tokens used.")
            .with_boundaries(TOKEN_USAGE_BUCKETS.to_vec())
            .build()
    })
}

fn request_option_attributes(options: RequestOptions) -> Vec<KeyValue> {
    let mut attributes = Vec::new();
    push_i64(
        &mut attributes,
        "gen_ai.request.choice.count",
        options.choice_count,
    );
    push_f64(
        &mut attributes,
        "gen_ai.request.frequency_penalty",
        options.frequency_penalty,
    );
    push_i64(
        &mut attributes,
        "gen_ai.request.max_tokens",
        options.max_tokens,
    );
    push_f64(
        &mut attributes,
        "gen_ai.request.presence_penalty",
        options.presence_penalty,
    );
    push_i64(&mut attributes, "gen_ai.request.seed", options.seed);
    push_f64(
        &mut attributes,
        "gen_ai.request.temperature",
        options.temperature,
    );
    push_i64(&mut attributes, "gen_ai.request.top_k", options.top_k);
    push_f64(&mut attributes, "gen_ai.request.top_p", options.top_p);
    attributes
}

fn push_i64(attributes: &mut Vec<KeyValue>, key: &'static str, value: Option<i64>) {
    if let Some(value) = value {
        attributes.push(KeyValue::new(key, value));
    }
}

fn push_f64(attributes: &mut Vec<KeyValue>, key: &'static str, value: Option<f64>) {
    if let Some(value) = value.filter(|value| value.is_finite()) {
        attributes.push(KeyValue::new(key, value));
    }
}

fn append_or_replace(attributes: &mut Vec<KeyValue>, attribute: KeyValue) {
    if let Some(existing) = attributes
        .iter_mut()
        .find(|existing| existing.key.as_str() == attribute.key.as_str())
    {
        *existing = attribute;
    } else {
        attributes.push(attribute);
    }
}

fn span_name(operation: &'static str, subject: &str) -> String {
    let subject = subject.trim();
    if subject.is_empty() {
        operation.to_string()
    } else {
        format!("{operation} {subject}")
    }
}

fn clean_string(value: impl Into<String>) -> Option<String> {
    let value = value.into().trim().to_string();
    if value.is_empty() { None } else { Some(value) }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fmt;

    #[derive(Debug)]
    struct CustomTelemetryError;

    impl fmt::Display for CustomTelemetryError {
        fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
            write!(f, "custom telemetry error")
        }
    }

    impl Error for CustomTelemetryError {}

    #[test]
    fn model_metric_attributes_exclude_request_options() {
        let operation = model_operation(
            ModelOperationOptions::new("openai", "gpt-4.1").with_request_options(RequestOptions {
                seed: Some(123),
                temperature: Some(0.2),
                ..RequestOptions::default()
            }),
        );

        assert!(
            !operation
                .metric_attributes
                .iter()
                .any(|attr| attr.key.as_str() == "gen_ai.request.seed")
        );
    }

    #[test]
    fn operations_record_without_configured_sdk() {
        let mut operation = model_operation(ModelOperationOptions::new("openai", "gpt-4.1"));
        operation.set_response_metadata(Some("resp-123"), Some("gpt-4.1"), &["stop"]);
        operation.record_usage(TokenUsage {
            input_tokens: Some(12),
            output_tokens: Some(34),
            ..TokenUsage::default()
        });
        operation.end();

        let mut agent = agent_invocation(AgentInvocationOptions::new(
            "simple",
            "session-123",
            "turn-123",
            "claude-opus-4-1",
        ));
        agent.mark_error("agent_error", "agent failed");
        agent.end();

        let mut tool = tool_execution(
            ToolExecutionOptions::new("github.search").with_tool_call_id("call-123"),
        );
        tool.mark_error("tool_error", "tool failed");
        tool.end();
    }

    #[test]
    fn record_error_uses_concrete_error_type() {
        let mut operation = model_operation(ModelOperationOptions::new("openai", "gpt-4.1"));
        let err = CustomTelemetryError;

        operation.record_error(&err);

        assert_eq!(
            operation.error_type.as_deref(),
            Some("gestalt::telemetry::tests::CustomTelemetryError")
        );
    }
}
