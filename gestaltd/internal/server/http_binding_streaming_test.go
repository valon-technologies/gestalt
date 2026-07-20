package server_test

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

// frameReader yields a fixed list of frames then io.EOF. It mirrors the
// slice readers used in broker/app tests; kept local because those are
// unexported and live in other packages.
func frameReader(frames ...*core.InvokeFrame) core.StreamReader {
	i := 0
	return core.StreamReaderFunc(func() (*core.InvokeFrame, error) {
		if i >= len(frames) {
			return nil, io.EOF
		}
		f := frames[i]
		i++
		return f, nil
	})
}

// streamingProvider builds a stub app whose "echo" op is a streaming op.
func streamingProvider(streamFn func(context.Context, string, map[string]any, string) (core.StreamReader, error)) *coretesting.StubIntegration {
	return &coretesting.StubIntegration{
		N:        "streamer",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Name: "streamer",
			Operations: []catalog.CatalogOperation{{
				ID:     "echo",
				Method: "POST",
				Response: &catalog.OperationResponseSpec{
					Stream: &catalog.StreamResponseSpec{MediaType: "text/event-stream"},
				},
			}},
		},
		StreamFn: streamFn,
	}
}

func streamingAppEntry(provider *coretesting.StubIntegration) *config.ProviderEntry {
	return &config.ProviderEntry{
		Source: config.ProviderSource{Builtin: provider.N},
		Connections: map[string]*config.ConnectionDef{
			"default": {Mode: providermanifestv1.ConnectionModeNone, Auth: config.ConnectionAuthDef{Type: providermanifestv1.AuthTypeNone}},
		},
		SecuritySchemes: map[string]*config.HTTPSecurityScheme{
			"none": {Type: providermanifestv1.HTTPSecuritySchemeTypeNone},
		},
		HTTP: map[string]*config.HTTPBinding{
			"echo": {
				Path:      "/hooks/echo",
				Method:    "POST",
				Security:  "none",
				Target:    "echo",
				Streaming: true,
			},
		},
	}
}

func TestHTTPBindingStreaming(t *testing.T) {
	t.Parallel()

	// Streaming binding flushes each data frame incrementally. This also
	// proves the Streaming flag propagates manifest -> MountedHTTPBinding ->
	// handler, since chunks can only arrive via the streaming dispatch branch.
	t.Run("flushes chunks", func(t *testing.T) {
		t.Parallel()

		provider := streamingProvider(func(_ context.Context, op string, params map[string]any, _ string) (core.StreamReader, error) {
			if op != "echo" {
				t.Fatalf("unexpected op %q", op)
			}
			if got, want := params["message"], "hello"; got != want {
				t.Fatalf("params[message] = %v, want %v", got, want)
			}
			return frameReader(
				&core.InvokeFrame{Metadata: &core.InvokeMetadata{Status: http.StatusOK, MediaType: "text/event-stream", Headers: map[string][]string{"X-Stream": {"yes"}}}},
				&core.InvokeFrame{Data: []byte("data: {\"a\":1}\n\n")},
				&core.InvokeFrame{Data: []byte("data: {\"b\":2}\n\n")},
			), nil
		})

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, provider)
			cfg.AppDefs = map[string]*config.ProviderEntry{"streamer": streamingAppEntry(provider)}
		})

		resp, err := http.Post(ts.URL+"/api/v1/streamer/hooks/echo", "application/json", bytes.NewReader([]byte(`{"message":"hello"}`)))
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, string(body))
		}
		if got, want := resp.Header.Get("Content-Type"), "text/event-stream"; got != want {
			t.Fatalf("Content-Type = %q, want %q", got, want)
		}
		if got := resp.Header.Get("X-Stream"); got != "yes" {
			t.Fatalf("X-Stream = %q, want yes", got)
		}

		var chunks []string
		for s := bufio.NewScanner(resp.Body); s.Scan(); {
			chunks = append(chunks, s.Text())
		}
		want := []string{"data: {\"a\":1}", "", "data: {\"b\":2}", ""}
		if !slices.Equal(chunks, want) {
			t.Fatalf("chunks = %v, want %v", chunks, want)
		}
	})

	// Non-streaming bindings still take the unary path and return a buffered result.
	t.Run("unary binding unchanged", func(t *testing.T) {
		t.Parallel()

		unary := &coretesting.StubIntegration{
			N:          "unary",
			ConnMode:   core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Name: "unary", Operations: []catalog.CatalogOperation{{ID: "ping", Method: "POST"}}},
			ExecuteFn: func(_ context.Context, op string, params map[string]any, _ string) (*core.OperationResult, error) {
				if op != "ping" {
					t.Fatalf("unexpected op %q", op)
				}
				if got, want := params["message"], "hello"; got != want {
					t.Fatalf("params[message] = %v, want %v", got, want)
				}
				return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{"ok":true}`), Headers: map[string][]string{"Content-Type": {"application/json"}}}, nil
			},
		}

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, unary)
			cfg.AppDefs = map[string]*config.ProviderEntry{
				"unary": {
					Source:          config.ProviderSource{Builtin: unary.N},
					Connections:     map[string]*config.ConnectionDef{"default": {Mode: providermanifestv1.ConnectionModeNone, Auth: config.ConnectionAuthDef{Type: providermanifestv1.AuthTypeNone}}},
					SecuritySchemes: map[string]*config.HTTPSecurityScheme{"none": {Type: providermanifestv1.HTTPSecuritySchemeTypeNone}},
					HTTP:            map[string]*config.HTTPBinding{"ping": {Path: "/hooks/ping", Method: "POST", Security: "none", Target: "ping"}},
				},
			}
		})

		resp, err := http.Post(ts.URL+"/api/v1/unary/hooks/ping", "application/json", bytes.NewReader([]byte(`{"message":"hello"}`)))
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, string(body))
		}
		if got, want := string(body), `{"ok":true}`; got != want {
			t.Fatalf("body = %q, want %q", got, want)
		}
	})

	// Client disconnect stops the streaming loop (writeStreamingOperationResult
	// honors r.Context().Done()).
	t.Run("client disconnect stops stream", func(t *testing.T) {
		t.Parallel()

		provider := streamingProvider(func(ctx context.Context, _ string, _ map[string]any, _ string) (core.StreamReader, error) {
			return core.StreamReaderFunc(func() (*core.InvokeFrame, error) {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(50 * time.Millisecond):
					return &core.InvokeFrame{Data: []byte("data: chunk\n\n")}, nil
				}
			}), nil
		})

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, provider)
			cfg.AppDefs = map[string]*config.ProviderEntry{"streamer": streamingAppEntry(provider)}
		})

		ctx, cancel := context.WithCancel(context.Background())
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/api/v1/streamer/hooks/echo", nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		_ = resp.StatusCode
		cancel()
		start := time.Now()
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Fatalf("handler did not stop after cancellation: elapsed=%v", elapsed)
		}
	})
}

func TestHTTPBindingStreamingWithGuardedInvoker(t *testing.T) {
	t.Parallel()

	provider := streamingProvider(func(_ context.Context, op string, params map[string]any, _ string) (core.StreamReader, error) {
		if op != "echo" {
			t.Fatalf("unexpected op %q", op)
		}
		return frameReader(
			&core.InvokeFrame{Metadata: &core.InvokeMetadata{Status: http.StatusOK, MediaType: "text/event-stream"}},
			&core.InvokeFrame{Data: []byte("data: ok\n\n")},
		), nil
	})

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, provider)
		cfg.AppDefs = map[string]*config.ProviderEntry{"streamer": streamingAppEntry(provider)}
		// The default test handler creates a raw Broker, so explicitly build a
		// GuardedInvoker around a fresh Broker to reproduce production wiring.
		broker := invocation.NewBroker(cfg.Providers, cfg.Services.Users, cfg.Services.ExternalCredentials)
		cfg.Invoker = invocation.NewGuarded(broker, nil, "http", nil, invocation.WithoutRateLimit())
	})

	resp, err := http.Post(ts.URL+"/api/v1/streamer/hooks/echo", "application/json", bytes.NewReader([]byte(`{"message":"hello"}`)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, string(body))
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte("data: ok")) {
		t.Fatalf("body = %q, want streaming chunk", string(body))
	}
}
