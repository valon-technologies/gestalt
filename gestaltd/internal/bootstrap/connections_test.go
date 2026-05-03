package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestBuildConnectionRuntimePlatformManualDirectAuthMapping(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Plugins: map[string]*config.ProviderEntry{
			"gong": {
				ResolvedManifest: &providermanifestv1.Manifest{
					Spec: &providermanifestv1.Spec{
						Connections: map[string]*providermanifestv1.ManifestConnectionDef{
							"default": {
								Mode: providermanifestv1.ConnectionModeUser,
								Auth: &providermanifestv1.ProviderAuth{
									Type: providermanifestv1.AuthTypeManual,
									Credentials: []providermanifestv1.CredentialField{
										{Name: "access_key_id"},
										{Name: "secret_key"},
									},
									AuthMapping: &providermanifestv1.AuthMapping{
										Basic: &providermanifestv1.BasicAuthMapping{
											Username: providermanifestv1.AuthValue{
												ValueFrom: &providermanifestv1.AuthValueFrom{
													CredentialFieldRef: &providermanifestv1.CredentialFieldRef{Name: "access_key_id"},
												},
											},
											Password: providermanifestv1.AuthValue{
												ValueFrom: &providermanifestv1.AuthValueFrom{
													CredentialFieldRef: &providermanifestv1.CredentialFieldRef{Name: "secret_key"},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				Connections: map[string]*config.ConnectionDef{
					"default": {
						Mode: providermanifestv1.ConnectionModePlatform,
						Auth: config.ConnectionAuthDef{
							Type:        providermanifestv1.AuthTypeManual,
							Credentials: []config.CredentialFieldDef{},
							AuthMapping: &config.AuthMappingDef{
								Basic: &config.BasicAuthMappingDef{
									Username: config.AuthValueDef{Value: "access-key-id"},
									Password: config.AuthValueDef{Value: "access-key-secret"},
								},
							},
						},
					},
				},
			},
		},
	}

	runtime, err := BuildConnectionRuntime(cfg)
	if err != nil {
		t.Fatalf("BuildConnectionRuntime() error = %v", err)
	}
	info, ok := runtime.Resolve("gong", "default")
	if !ok {
		t.Fatal("runtime.Resolve(gong, default) not found")
	}
	if info.Mode != core.ConnectionModePlatform {
		t.Fatalf("Mode = %q, want %q", info.Mode, core.ConnectionModePlatform)
	}
	if info.Token != "{}" {
		t.Fatalf("Token = %q, want placeholder JSON token", info.Token)
	}
}

func TestBuildConnectionRuntimePlatformManualCredentialRefsRequireToken(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Plugins: map[string]*config.ProviderEntry{
			"sample": {
				Connections: map[string]*config.ConnectionDef{
					"default": {
						Mode: providermanifestv1.ConnectionModePlatform,
						Auth: config.ConnectionAuthDef{
							Type: providermanifestv1.AuthTypeManual,
							AuthMapping: &config.AuthMappingDef{
								Headers: map[string]config.AuthValueDef{
									"X-API-Key": {
										ValueFrom: &config.AuthValueFromDef{
											CredentialFieldRef: &config.CredentialFieldRefDef{Name: "api_key"},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	_, err := BuildConnectionRuntime(cfg)
	if err == nil {
		t.Fatal("BuildConnectionRuntime() error = nil, want credential ref error")
	}
	if !strings.Contains(err.Error(), "manual auth with credential refs requires auth.token") {
		t.Fatalf("BuildConnectionRuntime() error = %v, want credential ref token error", err)
	}
}

func TestBuildConnectionRuntimeRejectsProviderNamespaceCollision(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Plugins: map[string]*config.ProviderEntry{
			"shared": {},
		},
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"shared": {},
			},
		},
	}

	_, err := BuildConnectionRuntime(cfg)
	if err == nil {
		t.Fatal("BuildConnectionRuntime() error = nil, want namespace collision error")
	}
	if !strings.Contains(err.Error(), "conflicts with another provider connection namespace") {
		t.Fatalf("BuildConnectionRuntime() error = %v, want namespace collision error", err)
	}
}

func TestClientCredentialsTokenSourceHeaderAuth(t *testing.T) {
	t.Parallel()

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		clientID, clientSecret, ok := r.BasicAuth()
		if !ok {
			t.Fatal("BasicAuth missing")
		}
		if clientID != url.QueryEscape("client id:/") || clientSecret != url.QueryEscape("client secret+/") {
			t.Fatalf("BasicAuth = %q/%q, want URL-escaped client credentials", clientID, clientSecret)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error = %v", err)
		}
		if got := r.Form.Get("client_id"); got != "" {
			t.Fatalf("client_id form field = %q, want empty when clientAuth=header", got)
		}
		if got := r.Form.Get("client_secret"); got != "" {
			t.Fatalf("client_secret form field = %q, want empty when clientAuth=header", got)
		}
		if got := r.Form.Get("grant_type"); got != "client_credentials" {
			t.Fatalf("grant_type = %q, want client_credentials", got)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"access_token": "header-token",
			"expires_in":   3600,
		}); err != nil {
			t.Fatalf("Encode() error = %v", err)
		}
	}))
	defer tokenServer.Close()

	source, err := newClientCredentialsTokenSource(config.ConnectionAuthDef{
		TokenURL:     tokenServer.URL,
		ClientID:     "client id:/",
		ClientSecret: "client secret+/",
		ClientAuth:   "header",
	})
	if err != nil {
		t.Fatalf("newClientCredentialsTokenSource() error = %v", err)
	}
	credential, err := source.ResolveConnectionCredential(context.Background())
	if err != nil {
		t.Fatalf("ResolveConnectionCredential() error = %v", err)
	}
	if credential.Token != "header-token" {
		t.Fatalf("Token = %q, want header-token", credential.Token)
	}
	if credential.ExpiresAt == nil {
		t.Fatal("ExpiresAt = nil, want expiry from token endpoint")
	}
}

func TestClientCredentialsTokenSourceCanceledCallerDoesNotCancelSharedFetch(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	secondRequest := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	var requests atomic.Int32
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch requests.Add(1) {
		case 1:
			close(started)
		case 2:
			close(secondRequest)
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"access_token": "shared-token",
			"expires_in":   3600,
		}); err != nil {
			t.Fatalf("Encode() error = %v", err)
		}
	}))
	defer func() {
		releaseOnce.Do(func() { close(release) })
		tokenServer.Close()
	}()

	source, err := newClientCredentialsTokenSource(config.ConnectionAuthDef{
		TokenURL:     tokenServer.URL,
		ClientID:     "client-id",
		ClientSecret: "client-secret",
	})
	if err != nil {
		t.Fatalf("newClientCredentialsTokenSource() error = %v", err)
	}

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstErr := make(chan error, 1)
	go func() {
		_, err := source.ResolveConnectionCredential(firstCtx)
		firstErr <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first token request")
	}
	cancelFirst()
	select {
	case err := <-firstErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("first ResolveConnectionCredential() error = %v, want context canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for canceled caller")
	}

	type result struct {
		credentialToken string
		err             error
	}
	secondResult := make(chan result, 1)
	go func() {
		credential, err := source.ResolveConnectionCredential(context.Background())
		secondResult <- result{credentialToken: credential.Token, err: err}
	}()

	select {
	case <-secondRequest:
		t.Fatal("second caller started a new token request instead of sharing the in-flight fetch")
	case <-time.After(100 * time.Millisecond):
	}
	releaseOnce.Do(func() { close(release) })

	select {
	case result := <-secondResult:
		if result.err != nil {
			t.Fatalf("second ResolveConnectionCredential() error = %v", result.err)
		}
		if result.credentialToken != "shared-token" {
			t.Fatalf("second token = %q, want shared-token", result.credentialToken)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second caller")
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("token requests = %d, want 1 shared request", got)
	}
}
