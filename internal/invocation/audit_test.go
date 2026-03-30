package invocation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"go.opentelemetry.io/otel/trace"
)

func TestSlogAuditSink_AllowedEntry(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	sink := invocation.NewSlogAuditSink(&buf)

	eventTime := time.Date(2026, 3, 15, 12, 30, 0, 0, time.UTC)
	entry := core.AuditEntry{
		Timestamp: eventTime,
		RequestID: "req-abc-123",
		Source:    "binding:test-hook",
		UserID:    "user-42",
		Provider:  "alpha",
		Operation: "fetch",
		Depth:     1,
		Allowed:   true,
	}

	sink.Log(context.Background(), entry)

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("failed to parse JSON log output: %v", err)
	}

	if record["log.type"] != "audit" {
		t.Errorf("expected log.type=audit, got %v", record["log.type"])
	}
	if record["level"] != "INFO" {
		t.Errorf("expected level=INFO for allowed entry, got %v", record["level"])
	}
	parsedEventTime, err := time.Parse(time.RFC3339Nano, record["event_time"].(string))
	if err != nil {
		t.Fatalf("failed to parse event_time: %v", err)
	}
	if !parsedEventTime.Equal(eventTime) {
		t.Errorf("expected event_time=%v, got %v", eventTime, parsedEventTime)
	}
	if record["request_id"] != "req-abc-123" {
		t.Errorf("expected request_id=req-abc-123, got %v", record["request_id"])
	}
	if record["source"] != "binding:test-hook" {
		t.Errorf("expected source=binding:test-hook, got %v", record["source"])
	}
	if record["user_id"] != "user-42" {
		t.Errorf("expected user_id=user-42, got %v", record["user_id"])
	}
	if record["provider"] != "alpha" {
		t.Errorf("expected provider=alpha, got %v", record["provider"])
	}
	if record["operation"] != "fetch" {
		t.Errorf("expected operation=fetch, got %v", record["operation"])
	}
	if record["depth"] != float64(1) {
		t.Errorf("expected depth=1, got %v", record["depth"])
	}
	if record["allowed"] != true {
		t.Errorf("expected allowed=true, got %v", record["allowed"])
	}
	if _, hasError := record["error"]; hasError {
		t.Errorf("expected no error field for allowed entry, got %v", record["error"])
	}
}

func TestSlogAuditSink_DeniedEntry(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	sink := invocation.NewSlogAuditSink(&buf)

	entry := core.AuditEntry{
		Timestamp: time.Now(),
		RequestID: "req-deny-456",
		Source:    "binding:test-hook",
		UserID:    "user-99",
		Provider:  "beta",
		Operation: "write",
		Depth:     2,
		Allowed:   false,
		Error:     "provider \"beta\" is not available in this scope",
	}

	sink.Log(context.Background(), entry)

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("failed to parse JSON log output: %v", err)
	}

	if record["log.type"] != "audit" {
		t.Errorf("expected log.type=audit, got %v", record["log.type"])
	}
	if record["level"] != "WARN" {
		t.Errorf("expected level=WARN for denied entry, got %v", record["level"])
	}
	if record["allowed"] != false {
		t.Errorf("expected allowed=false, got %v", record["allowed"])
	}
	if record["error"] != entry.Error {
		t.Errorf("expected error=%q, got %v", entry.Error, record["error"])
	}
}

func TestSlogAuditSink_WithSpanContext(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	sink := invocation.NewSlogAuditSink(&buf)

	traceID, _ := trace.TraceIDFromHex("0af7651916cd43dd8448eb211c80319c")
	spanID, _ := trace.SpanIDFromHex("b7ad6b7169203331")
	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanCtx)

	entry := core.AuditEntry{
		Timestamp: time.Now(),
		RequestID: "req-trace-789",
		Source:    "binding:traced",
		UserID:    "user-1",
		Provider:  "gamma",
		Operation: "read",
		Depth:     0,
		Allowed:   true,
	}

	sink.Log(ctx, entry)

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("failed to parse JSON log output: %v", err)
	}

	if record["log.type"] != "audit" {
		t.Errorf("expected log.type=audit, got %v", record["log.type"])
	}
	if record["request_id"] != "req-trace-789" {
		t.Errorf("expected request_id=req-trace-789, got %v", record["request_id"])
	}
	if record["trace_id"] != "0af7651916cd43dd8448eb211c80319c" {
		t.Errorf("expected trace_id=0af7651916cd43dd8448eb211c80319c, got %v", record["trace_id"])
	}
	if record["span_id"] != "b7ad6b7169203331" {
		t.Errorf("expected span_id=b7ad6b7169203331, got %v", record["span_id"])
	}
}

func TestSlogAuditSink_NoTraceContext(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	sink := invocation.NewSlogAuditSink(&buf)

	entry := core.AuditEntry{
		Timestamp: time.Now(),
		RequestID: "req-no-trace",
		Source:    "binding:test-hook",
		UserID:    "user-1",
		Provider:  "delta",
		Operation: "read",
		Depth:     0,
		Allowed:   true,
	}

	sink.Log(context.Background(), entry)

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("failed to parse JSON log output: %v", err)
	}

	if _, has := record["trace_id"]; has {
		t.Errorf("expected no trace_id without span context, got %v", record["trace_id"])
	}
	if _, has := record["span_id"]; has {
		t.Errorf("expected no span_id without span context, got %v", record["span_id"])
	}
}

func TestSlogAuditSink_GuaranteedDelivery(t *testing.T) {
	t.Parallel()

	// Verify that the audit sink emits both INFO-level (allowed) and WARN-level
	// (denied) entries. A typical application logger configured at WARN would
	// suppress the allowed entries, but the audit sink's internal logger is set
	// to LevelDebug so nothing is filtered.
	warnOnly := slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if warnOnly.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("expected warn-level logger to filter INFO")
	}

	var buf bytes.Buffer
	sink := invocation.NewSlogAuditSink(&buf)

	entries := []core.AuditEntry{
		{
			Timestamp: time.Now(),
			RequestID: "req-allowed",
			Source:    "binding:test-hook",
			UserID:    "user-1",
			Provider:  "epsilon",
			Operation: "read",
			Allowed:   true,
		},
		{
			Timestamp: time.Now(),
			RequestID: "req-denied",
			Source:    "binding:test-hook",
			UserID:    "user-2",
			Provider:  "zeta",
			Operation: "write",
			Allowed:   false,
			Error:     "access denied",
		},
	}

	for _, entry := range entries {
		sink.Log(context.Background(), entry)
	}

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("expected 2 audit log lines, got %d", len(lines))
	}

	var first map[string]any
	if err := json.Unmarshal(lines[0], &first); err != nil {
		t.Fatalf("failed to parse first log line: %v", err)
	}
	if first["request_id"] != "req-allowed" {
		t.Errorf("expected first entry request_id=req-allowed, got %v", first["request_id"])
	}
	if first["level"] != "INFO" {
		t.Errorf("expected first entry level=INFO, got %v", first["level"])
	}

	var second map[string]any
	if err := json.Unmarshal(lines[1], &second); err != nil {
		t.Fatalf("failed to parse second log line: %v", err)
	}
	if second["request_id"] != "req-denied" {
		t.Errorf("expected second entry request_id=req-denied, got %v", second["request_id"])
	}
}
