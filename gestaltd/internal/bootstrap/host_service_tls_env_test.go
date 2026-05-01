package bootstrap

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"gopkg.in/yaml.v3"
)

func TestPrepareCoreLoadsHostServiceTLSCAFileForHostedRuntimeEnv(t *testing.T) {
	certSrv := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(certSrv.Close)
	caPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certSrv.Certificate().Raw}))
	caFile := filepath.Join(t.TempDir(), "host-service-ca.pem")
	if err := os.WriteFile(caFile, []byte(caPEM), 0o644); err != nil {
		t.Fatalf("WriteFile(caFile): %v", err)
	}
	t.Setenv(gestalt.EnvHostServiceTLSCAFile, caFile)
	t.Setenv(gestalt.EnvHostServiceTLSCAPEM, "")

	factories := NewFactoryRegistry()
	factories.ExternalCredentials = func(context.Context, string, yaml.Node, []runtimehost.HostService, Deps) (core.ExternalCredentialProvider, error) {
		return coretesting.NewStubExternalCredentialProvider(), nil
	}
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) {
		return &coretesting.StubIndexedDB{}, nil
	}
	factories.Secrets["stub"] = func(yaml.Node) (core.SecretManager, error) {
		return &coretesting.StubSecretManager{Secrets: map[string]string{}}, nil
	}
	factories.Telemetry["noop"] = func(yaml.Node) (core.TelemetryProvider, error) {
		return noopTelemetryProvider{}, nil
	}

	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			Secrets: map[string]*config.ProviderEntry{
				"default": {Source: config.ProviderSource{Builtin: "stub"}},
			},
			Telemetry: map[string]*config.ProviderEntry{
				"default": {Source: config.ProviderSource{Builtin: "noop"}},
			},
			IndexedDB: map[string]*config.ProviderEntry{
				"main": {Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.1/provider-release.yaml")},
			},
		},
		Server: config.ServerConfig{
			BaseURL:       "https://gestalt.example.test",
			EncryptionKey: "test-encryption-key",
			Providers:     config.ServerProvidersConfig{IndexedDB: "main"},
		},
	}

	prepared, err := prepareCore(context.Background(), cfg, factories, true)
	if err != nil {
		t.Fatalf("prepareCore: %v", err)
	}
	t.Cleanup(func() { _ = prepared.Close(context.Background()) })

	want := strings.TrimSpace(caPEM)
	if got := prepared.Deps.HostServiceTLSCAPEM; got != want {
		t.Fatalf("HostServiceTLSCAPEM = %q, want file contents", got)
	}
	if got := prepared.Deps.HostServiceTLSCAFile; got != "" {
		t.Fatalf("HostServiceTLSCAFile = %q, want omitted after reading file", got)
	}

	env := withHostServiceTLSCAEnv(nil, prepared.Deps)
	if got := env[gestalt.EnvHostServiceTLSCAPEM]; got != want {
		t.Fatalf("host service CA PEM env = %q, want file contents", got)
	}
	if got := env[gestalt.EnvHostServiceTLSCAFile]; got != "" {
		t.Fatalf("host service CA file env = %q, want omitted", got)
	}
}
