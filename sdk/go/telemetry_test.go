package gestalt

import (
	"context"
	"errors"
	"testing"
)

func TestGenAITelemetryOperations(t *testing.T) {
	t.Parallel()

	ctx, op := ModelOperation(context.Background(), ModelOperationOptions{
		ProviderName: "openai",
		RequestModel: "gpt-4.1",
		RequestOptions: map[string]any{
			"max_tokens":  128,
			"temperature": 0.2,
		},
		RequestAttributes: map[string]any{
			"gen_ai.request.service_tier": "default",
		},
	})
	assertMissingMetricAttr(t, op, "gen_ai.request.temperature")
	assertMissingMetricAttr(t, op, "gen_ai.request.service_tier")
	op.SetResponseMetadata("resp-123", "gpt-4.1", "stop")
	op.RecordUsage(TokenUsage{InputTokens: TokenCount(12), OutputTokens: TokenCount(34)})
	op.RecordUsage(TokenUsage{InputTokens: TokenCount(0), OutputTokens: TokenCount(0)})
	op.End(nil)

	_, agentOp := AgentInvocation(ctx, AgentInvocationOptions{
		AgentName: "simple",
		SessionID: "session-123",
		TurnID:    "turn-123",
		Model:     "claude-opus-4-1",
	})
	agentOp.End(errors.New("agent failed"))

	_, toolOp := ToolExecution(ctx, ToolExecutionOptions{
		ToolName:   "github.search",
		ToolCallID: "call-123",
		ToolType:   GenAIToolTypeExtension,
	})
	toolOp.MarkError("tool_error", "tool failed", nil)
	toolOp.End(nil)
}

func assertMissingMetricAttr(t *testing.T, op *GenAIOperation, key string) {
	t.Helper()
	for _, attr := range op.metricAttrs {
		if string(attr.Key) == key {
			t.Fatalf("metric attrs unexpectedly included %q", key)
		}
	}
}
