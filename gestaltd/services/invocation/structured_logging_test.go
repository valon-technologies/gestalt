package invocation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	otelcodes "go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestBrokerMalformedMetadataJSON_StructuredLog(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	prov := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "myservice",
			ConnMode: core.ConnectionModeUser,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		ops: []core.Operation{{Name: "do_thing", Method: http.MethodGet}},
	}

	svc := testutil.NewStubServices(t)
	ctx := context.Background()
	u, err := svc.Users.FindOrCreateUser(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}
	if err := svc.ExternalCredentials.PutCredential(ctx, &core.ExternalCredential{
		ID: "tok1", SubjectID: principal.UserSubjectID(u.ID), Integration: "myservice",
		Connection: "", Instance: "default", AccessToken: "test-token",
		MetadataJSON: "not-valid-json{",
	}); err != nil {
		t.Fatalf("PutCredential: %v", err)
	}

	broker := invocation.NewBroker(testutil.NewProviderRegistry(t, prov), svc.Users, svc.ExternalCredentials, invocation.WithLogger(logger))
	p := &principal.Principal{
		Identity: &core.UserIdentity{Email: "test@example.com"},
	}

	result, err := broker.Invoke(ctx, p, "myservice", "", "do_thing", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("expected 200, got %d", result.Status)
	}

	output := buf.String()
	if output == "" {
		t.Fatal("expected structured log output for malformed MetadataJSON, got empty")
	}

	var foundWarning bool
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("log line is not valid JSON: %q: %v", line, err)
		}

		if record["msg"] == "malformed metadata JSON" {
			foundWarning = true
			if record["level"] != "WARN" {
				t.Errorf("expected level=WARN, got level=%v", record["level"])
			}
			if record["provider"] != "myservice" {
				t.Errorf("expected provider=myservice, got provider=%v", record["provider"])
			}
			if _, ok := record["error"]; !ok {
				t.Error("malformed metadata JSON log missing 'error' field")
			}
		}
	}

	if !foundWarning {
		t.Errorf("did not find 'malformed metadata JSON' warning in output:\n%s", output)
	}
}

func TestBrokerPlugin5xxResultObservability(t *testing.T) {
	t.Parallel()

	longBody := `{"error":"` + strings.Repeat("x", 5000) + `"}`
	tests := []struct {
		name          string
		operation     string
		transport     string
		ctx           context.Context
		wantWarning   bool
		wantSpanError bool
	}{
		{
			name:      "plugin transport",
			operation: "assistant.reconcileStuckRequests",
			transport: catalog.TransportApp,
			ctx: invocation.WithHTTPBinding(
				invocation.WithInvocationSurface(context.Background(), invocation.InvocationSurfaceHTTPBinding),
				"slack_events",
			),
			wantWarning:   true,
			wantSpanError: true,
		},
		{
			name:      "rest transport",
			operation: "chat.postMessage",
			transport: catalog.TransportREST,
			ctx:       context.Background(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			logger := newTestLogger(&buf)
			exporter, tracerProvider := newTestTracerProvider(t)
			prov := &coretesting.StubIntegration{
				N:        "slack",
				ConnMode: core.ConnectionModeNone,
				CatalogVal: &catalog.Catalog{
					Name: "slack",
					Operations: []catalog.CatalogOperation{{
						ID:        tt.operation,
						Method:    http.MethodPost,
						Transport: tt.transport,
					}},
				},
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
					return &core.OperationResult{Status: http.StatusInternalServerError, Body: longBody}, nil
				},
			}

			svc := testutil.NewStubServices(t)
			broker := invocation.NewBroker(
				testutil.NewProviderRegistry(t, prov),
				svc.Users,
				svc.ExternalCredentials,
				invocation.WithLogger(logger),
				invocation.WithTracerProvider(tracerProvider),
			)
			result, err := broker.Invoke(
				tt.ctx,
				&principal.Principal{SubjectID: "service_account:workflow-config"},
				"slack",
				"",
				tt.operation,
				nil,
			)
			if err != nil {
				t.Fatalf("Invoke: %v", err)
			}
			if result.Status != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d", result.Status, http.StatusInternalServerError)
			}

			record, found := findStructuredLogRecord(t, buf.String(), "provider operation returned 5xx result")
			if found != tt.wantWarning {
				t.Fatalf("warning found = %v, want %v; output:\n%s", found, tt.wantWarning, buf.String())
			}
			if tt.wantWarning {
				assertStructuredLogField(t, record, "provider", "slack")
				assertStructuredLogField(t, record, "operation", tt.operation)
				assertStructuredLogField(t, record, "transport", catalog.TransportApp)
				assertStructuredLogField(t, record, "surface", string(invocation.InvocationSurfaceHTTPBinding))
				assertStructuredLogField(t, record, "http_binding", "slack_events")
				assertStructuredLogField(t, record, "subject_id", "service_account:workflow-config")
				assertStructuredLogField(t, record, "subject_kind", "service_account")
				assertStructuredLogField(t, record, "result_status_class", "5xx")
				if got := record["result_status"]; got != float64(http.StatusInternalServerError) {
					t.Fatalf("result_status = %v, want %d", got, http.StatusInternalServerError)
				}
				body, ok := record["result_body"].(string)
				if !ok {
					t.Fatalf("result_body = %T, want string", record["result_body"])
				}
				if len(body) != 4096 {
					t.Fatalf("result_body length = %d, want 4096", len(body))
				}
				if !strings.HasPrefix(longBody, body) {
					t.Fatal("result_body is not a prefix of the operation result body")
				}
				for _, field := range []string{"result_body_bytes", "result_body_truncated", "result_body_sha256"} {
					if _, ok := record[field]; ok {
						t.Fatalf("unexpected %q field in structured log", field)
					}
				}
			}

			span := findTestSpan(t, exporter.GetSpans(), "broker.invoke")
			if tt.wantSpanError && span.Status.Code != otelcodes.Error {
				t.Fatalf("broker.invoke span status = %v, want %v", span.Status.Code, otelcodes.Error)
			}
			if !tt.wantSpanError && span.Status.Code == otelcodes.Error {
				t.Fatalf("broker.invoke span status = %v, want non-error", span.Status.Code)
			}
			for _, event := range span.Events {
				if event.Name == "exception" {
					t.Fatal("broker.invoke span recorded an exception event for a nil-error result")
				}
			}
		})
	}
}

func newTestLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func newTestTracerProvider(t *testing.T) (*tracetest.InMemoryExporter, *sdktrace.TracerProvider) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})
	return exporter, tp
}

func findStructuredLogRecord(t *testing.T, output, msg string) (map[string]any, bool) {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("log line is not valid JSON: %q: %v", line, err)
		}
		if record["msg"] == msg {
			return record, true
		}
	}
	return nil, false
}

func assertStructuredLogField(t *testing.T, record map[string]any, field string, want string) {
	t.Helper()
	if got := record[field]; got != want {
		t.Fatalf("%s = %v, want %s", field, got, want)
	}
}

func findTestSpan(t *testing.T, spans tracetest.SpanStubs, name string) *tracetest.SpanStub {
	t.Helper()
	for i := range spans {
		if spans[i].Name == name {
			return &spans[i]
		}
	}
	t.Fatalf("span %q not found; got %d spans", name, len(spans))
	return nil
}
