package bootstrap_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/scim"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"gopkg.in/yaml.v3"
)

func TestSCIMSecretResolutionBootstrapAndPublicServer(t *testing.T) {
	t.Parallel()

	const bearerToken = "resolved-rippling-scim-token"
	cfg := validConfig()
	cfg.Server.BaseURL = "https://gestalt.example"
	cfg.Server.SCIM = config.ServerSCIMConfig{Clients: map[string]config.SCIMClientConfig{
		"rippling": {
			Credentials: []config.SCIMCredentialConfig{{
				ID:          "current",
				BearerToken: config.EncodeSecretRefTransport(config.SecretRef{Provider: "default", Name: "rippling-scim-token"}),
			}},
		},
	}}
	factories := validFactories()
	factories.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
		return &coretesting.StubSecretManager{Secrets: map[string]string{"rippling-scim-token": bearerToken}}, nil
	}
	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	t.Cleanup(func() { _ = result.Close(context.Background()) })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := result.Start(ctx); err != nil {
		t.Fatalf("Result.Start: %v", err)
	}

	publicServer, err := server.New(server.Config{
		Services:           result.Services,
		Providers:          result.Providers,
		Invoker:            result.Invoker,
		AppInvocation:      result.AppInvocation,
		SCIMHandler:        result.SCIMHandler,
		PublicBaseURL:      cfg.Server.BaseURL,
		StateSecret:        []byte("0123456789abcdef0123456789abcdef"),
		PublicHostServices: result.PublicHostServices,
		RouteProfile:       server.RouteProfilePublic,
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	request := func(method, path, token string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var encoded []byte
		if body != nil {
			encoded, err = json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal request: %v", err)
			}
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(encoded))
		req.Header.Set("Authorization", "Bearer "+token)
		if body != nil {
			req.Header.Set("Content-Type", "application/scim+json")
		}
		response := httptest.NewRecorder()
		publicServer.ServeHTTP(response, req)
		return response
	}

	if response := request(http.MethodGet, "/scim/v2/Schemas", "wrong", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d", response.Code)
	}
	if response := request(http.MethodGet, "/scim/v2/Schemas", bearerToken, nil); response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/scim+json" {
		t.Fatalf("resolved token schema response = %d %q", response.Code, response.Header().Get("Content-Type"))
	}
	payload := map[string]any{"schemas": []string{scim.UserSchemaURN}, "userName": "alice@valon.com", "active": true}
	if response := request(http.MethodPost, "/scim/v2/Users", bearerToken, payload); response.Code != http.StatusCreated {
		t.Fatalf("public SCIM create status = %d", response.Code)
	}
}
