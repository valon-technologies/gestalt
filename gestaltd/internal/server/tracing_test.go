package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestTracing_HTTPAndBrokerSpans(t *testing.T) { //nolint:paralleltest // mutates global otel.TracerProvider
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	stub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "tracer-prov",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		ops: []core.Operation{{Name: "ping", Method: http.MethodPost}},
	}

	providers := testutil.NewProviderRegistry(t, stub)
	ds := testutil.NewStubServices(t)
	broker := invocation.NewBroker(providers, ds.Users, ds.ExternalCredentials)

	srv, err := server.New(server.Config{
		Auth: &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "session-token" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: "trace@example.com"}, nil
			},
		},
		Services:    ds,
		Providers:   providers,
		Invoker:     broker,
		StateSecret: []byte("0123456789abcdef0123456789abcdef"),
	})
	if err != nil {
		t.Fatalf("creating server: %v", err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tracer-prov/ping", bytes.NewBufferString(`{}`))
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "session-token"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	_ = tp.ForceFlush(context.Background())
	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one span, got none")
	}

	httpSpan := findSpanPrefix(spans, "gestaltd")
	if httpSpan == nil {
		var names []string
		for _, s := range spans {
			names = append(names, s.Name)
		}
		t.Fatalf("expected HTTP span with prefix 'gestaltd', got spans: %v", names)
	}

	brokerSpan := findSpan(spans, "broker.invoke")
	if brokerSpan == nil {
		t.Fatal("expected broker span named 'broker.invoke'")
	}
	assertSpanHasAttr(t, httpSpan, "gestaltd.provider.name", "tracer-prov")
	assertSpanHasAttr(t, httpSpan, "gestaltd.operation.name", "ping")
	assertSpanHasAttr(t, httpSpan, "gestaltd.connection.mode", "none")
	assertSpanHasAttr(t, httpSpan, "gestaltd.invocation.surface", "http")
	assertSpanLacksAttr(t, httpSpan, "gestalt.provider")
	assertSpanLacksAttr(t, httpSpan, "gestalt.operation")

	dbUser, err := ds.Users.FindOrCreateUser(context.Background(), "trace@example.com")
	if err != nil {
		t.Fatalf("resolve trace user: %v", err)
	}
	assertSpanHasAttr(t, brokerSpan, "gestalt.provider", "tracer-prov")
	assertSpanHasAttr(t, brokerSpan, "gestalt.operation", "ping")
	assertSpanHasAttr(t, brokerSpan, "gestalt.subject_id", principal.UserSubjectID(dbUser.ID))
	assertSpanHasAttr(t, brokerSpan, "gestalt.connection_mode", "none")
	assertSpanLacksAttr(t, brokerSpan, "gestalt.user_id")

	if brokerSpan.Parent.TraceID() != httpSpan.SpanContext.TraceID() {
		t.Errorf("broker span should share trace ID with HTTP span: broker=%s http=%s",
			brokerSpan.Parent.TraceID(), httpSpan.SpanContext.TraceID())
	}
}

func TestTracing_BrokerSpanRecordsErrors(t *testing.T) { //nolint:paralleltest // mutates global otel.TracerProvider
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	stub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "err-prov",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return nil, fmt.Errorf("provider exploded")
			},
		},
		ops: []core.Operation{{Name: "boom", Method: http.MethodPost}},
	}

	providers := testutil.NewProviderRegistry(t, stub)
	ds := testutil.NewStubServices(t)
	broker := invocation.NewBroker(providers, ds.Users, ds.ExternalCredentials)

	srv, err := server.New(server.Config{
		Auth:        &coretesting.StubAuthProvider{N: "none"},
		Services:    ds,
		Providers:   providers,
		Invoker:     broker,
		StateSecret: []byte("0123456789abcdef0123456789abcdef"),
	})
	if err != nil {
		t.Fatalf("creating server: %v", err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/err-prov/boom", bytes.NewBufferString(`{}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.ReadAll(resp.Body)

	_ = tp.ForceFlush(context.Background())
	spans := exporter.GetSpans()
	brokerSpan := findSpan(spans, "broker.invoke")
	if brokerSpan == nil {
		t.Fatal("expected broker span")
	}

	if len(brokerSpan.Events) == 0 {
		t.Fatal("expected error event on broker span")
	}

	foundError := false
	for _, ev := range brokerSpan.Events {
		if ev.Name == "exception" {
			foundError = true
			break
		}
	}
	if !foundError {
		t.Error("expected an exception event on the broker span")
	}
}

func TestTracing_AgentTurnTraceTree(t *testing.T) { //nolint:paralleltest // mutates global otel.TracerProvider
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	agentProvider := observability.InstrumentAgentProvider("managed", newMemoryAgentProvider())
	services := testutil.NewStubServices(t)
	providers := testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
		N:        "docs",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
			ID:       "search",
			Title:    "Search",
			ReadOnly: true,
		}}},
	})
	broker := invocation.NewBroker(providers, services.Users, services.ExternalCredentials)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "agent-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: "agent-trace@example.com", DisplayName: "Trace"}, nil
			},
		}
		cfg.Services = services
		cfg.Providers = providers
		cfg.Invoker = broker
		cfg.AgentManager = agentmanager.New(agentmanager.Config{
			Agent:     &stubAgentControl{defaultProviderName: "managed", provider: agentProvider},
			Providers: providers,
			RunGrants: newServerTestAgentRunGrants(t),
			Invoker:   broker,
		})
	})
	testutil.CloseOnCleanup(t, ts)

	sessionReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions", bytes.NewBufferString(`{"provider":"managed","model":"claude-sonnet"}`))
	sessionReq.AddCookie(&http.Cookie{Name: "session_token", Value: "agent-session"})
	sessionResp, err := http.DefaultClient.Do(sessionReq)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer func() { _ = sessionResp.Body.Close() }()
	if sessionResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(sessionResp.Body)
		t.Fatalf("create session status = %d body=%s", sessionResp.StatusCode, string(body))
	}
	var session map[string]any
	if err := json.NewDecoder(sessionResp.Body).Decode(&session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	sessionID, _ := session["id"].(string)
	if sessionID == "" {
		t.Fatalf("session response missing id: %#v", session)
	}

	turnBody := `{"messages":[{"role":"user","text":"search docs"}],"toolRefs":[{"plugin":"docs","operation":"search"}]}`
	turnReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions/"+sessionID+"/turns", bytes.NewBufferString(turnBody))
	turnReq.AddCookie(&http.Cookie{Name: "session_token", Value: "agent-session"})
	turnResp, err := http.DefaultClient.Do(turnReq)
	if err != nil {
		t.Fatalf("create turn: %v", err)
	}
	defer func() { _ = turnResp.Body.Close() }()
	if turnResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(turnResp.Body)
		t.Fatalf("create turn status = %d body=%s", turnResp.StatusCode, string(body))
	}

	_ = tp.ForceFlush(context.Background())
	spans := exporter.GetSpans()
	agentSpan := findSpanWithAttr(spans, "agent.operation", "gestalt.agent.operation", "create_turn")
	if agentSpan == nil {
		t.Fatalf("expected create_turn agent.operation span, got spans: %v", spanNames(spans))
	}
	providerSpan := findSpanWithAttr(spans, "agent.provider.operation", "gestalt.agent.operation", "create_turn")
	if providerSpan == nil {
		t.Fatal("expected create_turn agent.provider.operation span")
	}
	catalogSpan := findSpan(spans, "catalog.operation.resolve")
	if catalogSpan == nil {
		t.Fatal("expected catalog.operation.resolve span")
	}
	for _, child := range []*tracetest.SpanStub{providerSpan, catalogSpan} {
		if child.SpanContext.TraceID() != agentSpan.SpanContext.TraceID() {
			t.Fatalf("span %q trace id = %s, want agent trace id %s", child.Name, child.SpanContext.TraceID(), agentSpan.SpanContext.TraceID())
		}
	}
	assertSpanHasAttr(t, agentSpan, "gestalt.agent.provider", "managed")
	assertSpanHasAttr(t, providerSpan, "gestalt.agent.provider", "managed")
	assertSpanHasAttr(t, catalogSpan, "gestalt.provider", "docs")
	assertSpanHasAttr(t, catalogSpan, "gestalt.operation", "search")
	assertSpanHasAttr(t, catalogSpan, "gestalt.catalog.source", "static")
}

func findSpan(spans tracetest.SpanStubs, name string) *tracetest.SpanStub {
	for i := range spans {
		if spans[i].Name == name {
			return &spans[i]
		}
	}
	return nil
}

func findSpanWithAttr(spans tracetest.SpanStubs, name string, key string, value string) *tracetest.SpanStub {
	for i := range spans {
		if spans[i].Name != name {
			continue
		}
		for _, attr := range spans[i].Attributes {
			if string(attr.Key) == key && attr.Value.AsString() == value {
				return &spans[i]
			}
		}
	}
	return nil
}

func spanNames(spans tracetest.SpanStubs) []string {
	names := make([]string, 0, len(spans))
	for _, span := range spans {
		names = append(names, span.Name)
	}
	return names
}

func findSpanPrefix(spans tracetest.SpanStubs, prefix string) *tracetest.SpanStub {
	for i := range spans {
		if strings.HasPrefix(spans[i].Name, prefix) {
			return &spans[i]
		}
	}
	return nil
}

func assertSpanHasAttr(t *testing.T, span *tracetest.SpanStub, key, expected string) {
	t.Helper()
	for _, attr := range span.Attributes {
		if string(attr.Key) == key {
			if attr.Value.AsString() != expected {
				t.Errorf("span %q: attr %q = %q, want %q", span.Name, key, attr.Value.AsString(), expected)
			}
			return
		}
	}
	t.Errorf("span %q: missing attribute %q", span.Name, key)
}

func assertSpanLacksAttr(t *testing.T, span *tracetest.SpanStub, key string) {
	t.Helper()
	for _, attr := range span.Attributes {
		if string(attr.Key) == key {
			t.Errorf("span %q: unexpected attribute %q=%q", span.Name, key, attr.Value.AsString())
			return
		}
	}
}
