package bootstrap_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

type restartCloseFailureProvider struct {
	coretesting.StubIntegration
	err error
}

func (p *restartCloseFailureProvider) Close() error { return p.err }

func TestAppProviderRestarterRestartable(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Server: config.ServerConfig{Remote: "https://remote.test"},
		Apps: map[string]*config.ProviderEntry{
			"remote":         {},
			"local-override": {Local: true},
			"dev-active":     {DevActive: true},
		},
	}
	restarter := bootstrap.NewAppProviderRestarter(bootstrap.AppProviderRestarterConfig{Config: cfg})

	for _, tc := range []struct {
		app  string
		want bool
	}{
		{app: "remote", want: false},
		{app: "local-override", want: true},
		{app: "dev-active", want: false},
	} {
		got, err := restarter.Restartable(tc.app)
		if err != nil {
			t.Fatalf("Restartable(%q): %v", tc.app, err)
		}
		if got != tc.want {
			t.Errorf("Restartable(%q) = %v, want %v", tc.app, got, tc.want)
		}
	}

	cfg.Server.Remote = ""
	got, err := restarter.Restartable("remote")
	if err != nil {
		t.Fatalf("Restartable without server.remote: %v", err)
	}
	if !got {
		t.Error("packaged app should be restartable when server.remote is unset")
	}
}

func TestAppProviderRestarterStopAppNoOpsWhenProviderMissing(t *testing.T) {
	t.Parallel()

	srv := newRestartTestOpenAPIServer(t)
	cfg := restartTestConfig(srv.URL)

	result, err := bootstrap.Bootstrap(context.Background(), cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	t.Cleanup(func() { _ = result.Close(context.Background()) })

	select {
	case <-result.ProvidersReady:
	case <-time.After(5 * time.Second):
		t.Fatal("providers did not become ready")
	}

	result.Providers.Remove("restart-app")
	if err := result.AppRestarter.StopApp(context.Background(), "restart-app"); err != nil {
		t.Fatalf("StopApp: %v", err)
	}
}

func TestAppProviderRestarterStartAppRegistersMissingProvider(t *testing.T) {
	t.Parallel()

	srv := newRestartTestOpenAPIServer(t)
	cfg := restartTestConfig(srv.URL)

	result, err := bootstrap.Bootstrap(context.Background(), cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	t.Cleanup(func() { _ = result.Close(context.Background()) })

	select {
	case <-result.ProvidersReady:
	case <-time.After(5 * time.Second):
		t.Fatal("providers did not become ready")
	}

	result.Providers.Remove("restart-app")
	if err := result.AppRestarter.StartApp(context.Background(), "restart-app"); err != nil {
		t.Fatalf("StartApp: %v", err)
	}
	if _, err := result.Providers.Get("restart-app"); err != nil {
		t.Fatalf("Get after StartApp: %v", err)
	}
	if err := result.AppRestarter.StartApp(context.Background(), "restart-app"); err != nil {
		t.Fatalf("second StartApp: %v", err)
	}
}

func TestAppProviderRestarterStopRemovesProviderAndStartRestoresIt(t *testing.T) {
	t.Parallel()

	srv := newRestartTestOpenAPIServer(t)
	result, err := bootstrap.Bootstrap(context.Background(), restartTestConfig(srv.URL), validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	t.Cleanup(func() { _ = result.Close(context.Background()) })

	select {
	case <-result.ProvidersReady:
	case <-time.After(5 * time.Second):
		t.Fatal("providers did not become ready")
	}

	if err := result.AppRestarter.StopApp(context.Background(), "restart-app"); err != nil {
		t.Fatalf("StopApp: %v", err)
	}
	if _, err := result.Providers.Get("restart-app"); err == nil {
		t.Fatal("provider remains registered after StopApp")
	}
	if err := result.AppRestarter.StartApp(context.Background(), "restart-app"); err != nil {
		t.Fatalf("StartApp: %v", err)
	}
	if _, err := result.Providers.Get("restart-app"); err != nil {
		t.Fatalf("Get after StartApp: %v", err)
	}
}

func TestAppProviderRestarterStopQuarantinesProviderWhenCloseFails(t *testing.T) {
	t.Parallel()

	srv := newRestartTestOpenAPIServer(t)
	result, err := bootstrap.Bootstrap(context.Background(), restartTestConfig(srv.URL), validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	t.Cleanup(func() { _ = result.Close(context.Background()) })

	select {
	case <-result.ProvidersReady:
	case <-time.After(5 * time.Second):
		t.Fatal("providers did not become ready")
	}

	closeErr := errors.New("close failed")
	provider := &restartCloseFailureProvider{err: closeErr}
	if err := result.Providers.Replace("restart-app", provider); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if err := result.AppRestarter.StopApp(context.Background(), "restart-app"); !errors.Is(err, closeErr) {
		t.Fatalf("StopApp error = %v, want %v", err, closeErr)
	}
	if _, err := result.Providers.Get("restart-app"); err == nil {
		t.Fatal("partially closed provider was republished after failed StopApp")
	}
	if err := result.AppRestarter.StopApp(context.Background(), "restart-app"); !errors.Is(err, closeErr) {
		t.Fatalf("second StopApp error = %v, want retained close failure", err)
	}
}

func newRestartTestOpenAPIServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"openapi":"3.0.0",
			"info":{"title":"restart-app","version":"1.0.0"},
			"paths":{"/status":{"get":{"operationId":"status","responses":{"200":{"description":"ok"}}}}}
		}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func restartTestConfig(openAPIURL string) *config.Config {
	cfg := validConfig()
	cfg.Apps = map[string]*config.ProviderEntry{
		"restart-app": {
			ResolvedManifest: &providermanifestv1.Manifest{
				Spec: &providermanifestv1.Spec{
					DefaultConnection: config.AppConnectionName,
					Connections: map[string]*providermanifestv1.ManifestConnectionDef{
						config.AppConnectionName: {Mode: providermanifestv1.ConnectionModeNone},
					},
					Surfaces: &providermanifestv1.ProviderSurfaces{
						OpenAPI: &providermanifestv1.OpenAPISurface{Document: openAPIURL, BaseURL: openAPIURL},
					},
				},
			},
		},
	}
	return cfg
}
