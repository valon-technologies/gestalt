package server_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
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
	ds := coretesting.NewStubServices(t)
	broker := invocation.NewBroker(providers, ds.Users, ds.Tokens)

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

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tracer-prov/ping", bytes.NewBufferString(`{}`))
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
	assertSpanHasAttr(t, brokerSpan, "gestalt.provider", "tracer-prov")
	assertSpanHasAttr(t, brokerSpan, "gestalt.operation", "ping")
	assertSpanHasAttr(t, brokerSpan, "gestalt.connection_mode", "none")

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
	ds := coretesting.NewStubServices(t)
	broker := invocation.NewBroker(providers, ds.Users, ds.Tokens)

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

func findSpan(spans tracetest.SpanStubs, name string) *tracetest.SpanStub {
	for i := range spans {
		if spans[i].Name == name {
			return &spans[i]
		}
	}
	return nil
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
