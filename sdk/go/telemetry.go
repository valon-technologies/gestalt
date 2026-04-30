package gestalt

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	// TelemetryInstrumentationName is the OpenTelemetry instrumentation scope used by the Gestalt SDK.
	TelemetryInstrumentationName = "gestalt.provider"
	// GenAIProviderName is the default GenAI provider name used for Gestalt-owned agent and tool work.
	GenAIProviderName = "gestalt"

	GenAIOperationChat        = "chat"
	GenAIOperationExecuteTool = "execute_tool"
	GenAIOperationInvokeAgent = "invoke_agent"

	GenAIToolTypeDatastore = "datastore"
	GenAIToolTypeExtension = "extension"

	genAIOperationDurationMetric = "gen_ai.client.operation.duration"
	genAITokenUsageMetric        = "gen_ai.client.token.usage"
)

var genAIOperationDurationBuckets = []float64{0.01, 0.02, 0.04, 0.08, 0.16, 0.32, 0.64, 1.28, 2.56, 5.12, 10.24, 20.48, 40.96, 81.92}
var genAITokenUsageBuckets = []float64{1, 4, 16, 64, 256, 1024, 4096, 16384, 65536, 262144, 1048576, 4194304, 16777216, 67108864}

var (
	genAIMeterOnce        sync.Once
	genAIOperationLatency metric.Float64Histogram
	genAITokenUsage       metric.Int64Histogram
)

// ModelOperationOptions describes an upstream model call that should be recorded with GenAI semantic conventions.
type ModelOperationOptions struct {
	ProviderName      string
	RequestModel      string
	RequestOptions    map[string]any
	RequestAttributes map[string]any
	RequestAttrs      []attribute.KeyValue
}

// AgentInvocationOptions describes provider-owned agent turn execution.
type AgentInvocationOptions struct {
	AgentName string
	SessionID string
	TurnID    string
	Model     string
}

// ToolExecutionOptions describes provider-owned tool execution.
type ToolExecutionOptions struct {
	ToolName   string
	ToolCallID string
	ToolType   string
}

// TokenUsage is the GenAI token usage recorded on spans and token usage metrics.
type TokenUsage struct {
	InputTokens              *int64
	OutputTokens             *int64
	CacheCreationInputTokens *int64
	CacheReadInputTokens     *int64
	ReasoningOutputTokens    *int64
}

// GenAIOperation records a GenAI span plus operation duration and token usage metrics.
type GenAIOperation struct {
	ctx         context.Context
	span        trace.Span
	startedAt   time.Time
	metricAttrs []attribute.KeyValue
	errorType   string
	ended       bool
	mu          sync.Mutex
}

// TokenCount returns a pointer to a token count for TokenUsage fields.
func TokenCount(value int64) *int64 {
	return &value
}

// ModelOperation starts a GenAI client span for an upstream model SDK call.
func ModelOperation(ctx context.Context, opts ModelOperationOptions) (context.Context, *GenAIOperation) {
	providerName := cleanTelemetryString(opts.ProviderName)
	if providerName == "" {
		providerName = "_OTHER"
	}
	requestModel := cleanTelemetryString(opts.RequestModel)

	attrs := []attribute.KeyValue{
		attribute.String("gen_ai.operation.name", GenAIOperationChat),
		attribute.String("gen_ai.provider.name", providerName),
		attribute.String("gen_ai.request.model", requestModel),
	}
	spanAttrs := append([]attribute.KeyValue(nil), attrs...)
	spanAttrs = append(spanAttrs, requestOptionAttrs(opts.RequestOptions)...)
	spanAttrs = append(spanAttrs, attrsFromMap(opts.RequestAttributes)...)
	spanAttrs = append(spanAttrs, opts.RequestAttrs...)

	return startGenAIOperation(ctx, spanName(GenAIOperationChat, requestModel), trace.SpanKindClient, spanAttrs, attrs)
}

// AgentInvocation starts a GenAI internal span for a provider-owned agent turn.
func AgentInvocation(ctx context.Context, opts AgentInvocationOptions) (context.Context, *GenAIOperation) {
	agentName := cleanTelemetryString(opts.AgentName)
	if agentName == "" {
		agentName = "provider"
	}
	model := cleanTelemetryString(opts.Model)
	spanAttrs := []attribute.KeyValue{
		attribute.String("gen_ai.operation.name", GenAIOperationInvokeAgent),
		attribute.String("gen_ai.provider.name", GenAIProviderName),
		attribute.String("gen_ai.agent.name", agentName),
		attribute.String("gen_ai.conversation.id", cleanTelemetryString(opts.SessionID)),
		attribute.String("gen_ai.request.model", model),
		attribute.String("gestalt.agent.turn_id", cleanTelemetryString(opts.TurnID)),
	}
	metricAttrs := []attribute.KeyValue{
		attribute.String("gen_ai.operation.name", GenAIOperationInvokeAgent),
		attribute.String("gen_ai.provider.name", GenAIProviderName),
		attribute.String("gen_ai.agent.name", agentName),
		attribute.String("gen_ai.request.model", model),
	}
	return startGenAIOperation(ctx, spanName(GenAIOperationInvokeAgent, agentName), trace.SpanKindInternal, spanAttrs, metricAttrs)
}

// ToolExecution starts a GenAI internal span for provider-owned tool execution.
func ToolExecution(ctx context.Context, opts ToolExecutionOptions) (context.Context, *GenAIOperation) {
	toolName := cleanTelemetryString(opts.ToolName)
	if toolName == "" {
		toolName = "_OTHER"
	}
	toolType := cleanTelemetryString(opts.ToolType)
	if toolType == "" {
		toolType = GenAIToolTypeExtension
	}
	spanAttrs := []attribute.KeyValue{
		attribute.String("gen_ai.operation.name", GenAIOperationExecuteTool),
		attribute.String("gen_ai.provider.name", GenAIProviderName),
		attribute.String("gen_ai.tool.name", toolName),
		attribute.String("gen_ai.tool.call.id", cleanTelemetryString(opts.ToolCallID)),
		attribute.String("gen_ai.tool.type", toolType),
	}
	metricAttrs := []attribute.KeyValue{
		attribute.String("gen_ai.operation.name", GenAIOperationExecuteTool),
		attribute.String("gen_ai.provider.name", GenAIProviderName),
		attribute.String("gen_ai.tool.name", toolName),
		attribute.String("gen_ai.tool.type", toolType),
	}
	return startGenAIOperation(ctx, spanName(GenAIOperationExecuteTool, toolName), trace.SpanKindInternal, spanAttrs, metricAttrs)
}

// End records the operation duration and ends the span. Passing err marks the operation as failed.
func (op *GenAIOperation) End(err error) {
	if op == nil {
		return
	}
	if err != nil {
		op.MarkError(errorType(err), err.Error(), err)
	}

	op.mu.Lock()
	if op.ended {
		op.mu.Unlock()
		return
	}
	op.ended = true
	attrs := append([]attribute.KeyValue(nil), op.metricAttrs...)
	if op.errorType != "" {
		attrs = appendOrReplaceAttr(attrs, attribute.String("error.type", op.errorType))
	}
	startedAt := op.startedAt
	op.mu.Unlock()

	genAIOperationDuration().Record(op.ctx, time.Since(startedAt).Seconds(), metric.WithAttributes(attrs...))
	op.span.End()
}

// MarkError marks the span and operation duration metric as failed.
func (op *GenAIOperation) MarkError(errorTypeValue, description string, err error) {
	if op == nil {
		return
	}
	cleaned := cleanTelemetryString(errorTypeValue)
	if cleaned == "" {
		cleaned = "_OTHER"
	}

	op.mu.Lock()
	op.errorType = cleaned
	op.metricAttrs = appendOrReplaceAttr(op.metricAttrs, attribute.String("error.type", cleaned))
	op.mu.Unlock()

	op.span.SetAttributes(attribute.String("error.type", cleaned))
	if err != nil {
		op.span.RecordError(err)
	}
	if description == "" {
		description = cleaned
	}
	op.span.SetStatus(codes.Error, description)
}

// SetAttribute sets a span attribute when the value is supported by OpenTelemetry.
func (op *GenAIOperation) SetAttribute(key string, value any) {
	if op == nil {
		return
	}
	attr, ok := attrFromAny(key, value)
	if !ok {
		return
	}
	op.span.SetAttributes(attr)
	if attr.Key == "gen_ai.response.model" {
		op.mu.Lock()
		op.metricAttrs = appendOrReplaceAttr(op.metricAttrs, attr)
		op.mu.Unlock()
	}
}

// SetResponseMetadata attaches common GenAI response metadata to the span.
func (op *GenAIOperation) SetResponseMetadata(responseID, responseModel string, finishReasons ...string) {
	op.SetAttribute("gen_ai.response.id", responseID)
	op.SetAttribute("gen_ai.response.model", responseModel)
	if len(finishReasons) > 0 {
		op.SetAttribute("gen_ai.response.finish_reasons", finishReasons)
	}
}

// RecordUsage records GenAI token usage on the span and token usage metric.
func (op *GenAIOperation) RecordUsage(usage TokenUsage) {
	if op == nil {
		return
	}
	op.setTokenAttribute("gen_ai.usage.input_tokens", usage.InputTokens)
	op.setTokenAttribute("gen_ai.usage.output_tokens", usage.OutputTokens)
	op.setTokenAttribute("gen_ai.usage.cache_creation.input_tokens", usage.CacheCreationInputTokens)
	op.setTokenAttribute("gen_ai.usage.cache_read.input_tokens", usage.CacheReadInputTokens)
	op.setTokenAttribute("gen_ai.usage.reasoning.output_tokens", usage.ReasoningOutputTokens)

	op.recordTokenUsage(usage.InputTokens, "input")
	op.recordTokenUsage(usage.OutputTokens, "output")
}

func (op *GenAIOperation) setTokenAttribute(key string, tokens *int64) {
	if tokens == nil || *tokens < 0 {
		return
	}
	op.SetAttribute(key, *tokens)
}

func (op *GenAIOperation) recordTokenUsage(tokens *int64, tokenType string) {
	if tokens == nil || *tokens < 0 {
		return
	}
	op.mu.Lock()
	attrs := append([]attribute.KeyValue(nil), op.metricAttrs...)
	op.mu.Unlock()

	attrs = appendOrReplaceAttr(attrs, attribute.String("gen_ai.token.type", tokenType))
	genAITokenUsageHistogram().Record(op.ctx, *tokens, metric.WithAttributes(attrs...))
}

func startGenAIOperation(
	ctx context.Context,
	name string,
	kind trace.SpanKind,
	spanAttrs []attribute.KeyValue,
	metricAttrs []attribute.KeyValue,
) (context.Context, *GenAIOperation) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, span := otel.Tracer(TelemetryInstrumentationName).Start(
		ctx,
		name,
		trace.WithSpanKind(kind),
		trace.WithAttributes(spanAttrs...),
	)
	return ctx, &GenAIOperation{
		ctx:         ctx,
		span:        span,
		startedAt:   time.Now(),
		metricAttrs: append([]attribute.KeyValue(nil), metricAttrs...),
	}
}

func genAIOperationDuration() metric.Float64Histogram {
	genAIMeterOnce.Do(func() {
		meter := otel.Meter(TelemetryInstrumentationName)
		genAIOperationLatency, _ = meter.Float64Histogram(
			genAIOperationDurationMetric,
			metric.WithUnit("s"),
			metric.WithDescription("GenAI operation duration."),
			metric.WithExplicitBucketBoundaries(genAIOperationDurationBuckets...),
		)
		genAITokenUsage, _ = meter.Int64Histogram(
			genAITokenUsageMetric,
			metric.WithUnit("{token}"),
			metric.WithDescription("Number of input and output tokens used."),
			metric.WithExplicitBucketBoundaries(genAITokenUsageBuckets...),
		)
	})
	return genAIOperationLatency
}

func genAITokenUsageHistogram() metric.Int64Histogram {
	genAIOperationDuration()
	return genAITokenUsage
}

func requestOptionAttrs(options map[string]any) []attribute.KeyValue {
	if len(options) == 0 {
		return nil
	}
	mapping := map[string]string{
		"choice_count":          "gen_ai.request.choice.count",
		"frequency_penalty":     "gen_ai.request.frequency_penalty",
		"max_completion_tokens": "gen_ai.request.max_tokens",
		"max_output_tokens":     "gen_ai.request.max_tokens",
		"max_tokens":            "gen_ai.request.max_tokens",
		"n":                     "gen_ai.request.choice.count",
		"presence_penalty":      "gen_ai.request.presence_penalty",
		"seed":                  "gen_ai.request.seed",
		"temperature":           "gen_ai.request.temperature",
		"top_k":                 "gen_ai.request.top_k",
		"top_p":                 "gen_ai.request.top_p",
	}
	attrs := make([]attribute.KeyValue, 0, len(options))
	for option, attrName := range mapping {
		if value, ok := options[option]; ok {
			if attr, ok := attrFromAny(attrName, value); ok {
				attrs = append(attrs, attr)
			}
		}
	}
	return attrs
}

func attrsFromMap(values map[string]any) []attribute.KeyValue {
	if len(values) == 0 {
		return nil
	}
	attrs := make([]attribute.KeyValue, 0, len(values))
	for key, value := range values {
		if attr, ok := attrFromAny(key, value); ok {
			attrs = append(attrs, attr)
		}
	}
	return attrs
}

func attrFromAny(key string, value any) (attribute.KeyValue, bool) {
	key = cleanTelemetryString(key)
	if key == "" || value == nil {
		return attribute.KeyValue{}, false
	}
	switch v := value.(type) {
	case string:
		v = cleanTelemetryString(v)
		if v == "" {
			return attribute.KeyValue{}, false
		}
		return attribute.String(key, v), true
	case []string:
		if len(v) == 0 {
			return attribute.KeyValue{}, false
		}
		return attribute.StringSlice(key, v), true
	case bool:
		return attribute.Bool(key, v), true
	case int:
		return attribute.Int(key, v), true
	case int8, int16, int32, int64:
		return attribute.Int64(key, reflect.ValueOf(v).Int()), true
	case uint, uint8, uint16, uint32, uint64:
		unsigned := reflect.ValueOf(v).Uint()
		if unsigned > uint64(^uint64(0)>>1) {
			return attribute.KeyValue{}, false
		}
		return attribute.Int64(key, int64(unsigned)), true
	case float32:
		return attribute.Float64(key, float64(v)), true
	case float64:
		return attribute.Float64(key, v), true
	default:
		return attribute.String(key, fmt.Sprint(v)), true
	}
}

func appendOrReplaceAttr(attrs []attribute.KeyValue, attr attribute.KeyValue) []attribute.KeyValue {
	for i := range attrs {
		if attrs[i].Key == attr.Key {
			attrs[i] = attr
			return attrs
		}
	}
	return append(attrs, attr)
}

func spanName(operation, subject string) string {
	subject = cleanTelemetryString(subject)
	if subject == "" {
		return operation
	}
	return operation + " " + subject
}

func cleanTelemetryString(value string) string {
	return strings.TrimSpace(value)
}

func errorType(err error) string {
	if err == nil {
		return ""
	}
	name := reflect.TypeOf(err).String()
	return strings.TrimPrefix(name, "*")
}
