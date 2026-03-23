package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	coretesting "github.com/valon-technologies/gestalt/core/testing"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"github.com/valon-technologies/gestalt/internal/pluginproc"
	"github.com/valon-technologies/gestalt/internal/principal"
	secretsenv "github.com/valon-technologies/gestalt/plugins/secrets/env"
	"gopkg.in/yaml.v3"
)

const helperEnv = "GESTALT_PLUGIN_HELPER"

func TestPluginSubprocessProviderExecuteAndFilter(t *testing.T) {
	binary := os.Args[0]
	t.Setenv("GESTALT_TEST_BINARY", binary)

	cfg := mustWritePluginConfig(t, `
auth:
  provider: stub-auth
datastore:
  provider: stub-ds
server:
  dev_mode: true
integrations:
  demo:
    plugin:
      command:
        - ${GESTALT_TEST_BINARY}
        - -test.run=TestPluginHelperProcess$
      env:
        `+helperEnv+`: "1"
        GESTALT_PLUGIN_MODE: "basic"
      config:
        label: demo-config
      allowed_operations:
        echo: Echo the payload
      startup_timeout: 5s
      request_timeout: 5s
`)

	result := mustBootstrapPluginEnv(t, cfg)

	prov, err := result.Providers.Get("demo")
	if err != nil {
		t.Fatalf("Providers.Get: %v", err)
	}
	if got := prov.ListOperations(); len(got) != 1 || got[0].Name != "echo" {
		t.Fatalf("ListOperations: got %+v, want [echo]", got)
	}
	if _, ok := prov.(core.ManualProvider); ok {
		t.Fatal("expected basic plugin to not advertise manual auth")
	}

	p := &principal.Principal{UserID: "user-123"}
	out, err := result.Invoker.Invoke(context.Background(), p, "demo", "echo", map[string]any{"hello": "world"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if out.Status != 200 {
		t.Fatalf("Invoke status: got %d, want 200", out.Status)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(out.Body), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body["token"] != "stored-access-token" {
		t.Fatalf("response token: got %v, want stored-access-token", body["token"])
	}
	if body["operation"] != "echo" {
		t.Fatalf("response operation: got %v, want echo", body["operation"])
	}

	params, ok := body["params"].(map[string]any)
	if !ok || params["hello"] != "world" {
		t.Fatalf("response params: got %v, want hello=world", body["params"])
	}

	pluginCfg, ok := body["plugin_config"].(map[string]any)
	if !ok || pluginCfg["label"] != "demo-config" {
		t.Fatalf("response plugin_config: got %v, want label=demo-config", body["plugin_config"])
	}

	if _, err := result.Invoker.Invoke(context.Background(), p, "demo", "hidden", map[string]any{}); !errors.Is(err, invocation.ErrOperationNotFound) {
		t.Fatalf("Invoke(hidden): got %v, want ErrOperationNotFound", err)
	}
}

func TestPluginSubprocessProviderManualAuth(t *testing.T) {
	binary := os.Args[0]
	t.Setenv("GESTALT_TEST_BINARY", binary)

	cfg := mustWritePluginConfig(t, `
auth:
  provider: stub-auth
datastore:
  provider: stub-ds
server:
  dev_mode: true
integrations:
  manual-demo:
    plugin:
      command:
        - ${GESTALT_TEST_BINARY}
        - -test.run=TestPluginHelperProcess$
      env:
        `+helperEnv+`: "1"
        GESTALT_PLUGIN_MODE: "manual"
      startup_timeout: 5s
      request_timeout: 5s
`)

	result := mustBootstrapPluginEnv(t, cfg)

	prov, err := result.Providers.Get("manual-demo")
	if err != nil {
		t.Fatalf("Providers.Get: %v", err)
	}
	mp, ok := prov.(core.ManualProvider)
	if !ok {
		t.Fatal("expected manual plugin to advertise ManualProvider")
	}
	if !mp.SupportsManualAuth() {
		t.Fatal("expected SupportsManualAuth to be true")
	}
	if got := prov.ListOperations(); len(got) != 1 || got[0].Name != "ping" {
		t.Fatalf("ListOperations: got %+v, want [ping]", got)
	}
}

func TestPluginSubprocessProviderOAuth(t *testing.T) {
	binary := os.Args[0]
	t.Setenv("GESTALT_TEST_BINARY", binary)

	cfg := mustWritePluginConfig(t, `
auth:
  provider: stub-auth
datastore:
  provider: stub-ds
server:
  dev_mode: true
integrations:
  oauth-demo:
    plugin:
      command:
        - ${GESTALT_TEST_BINARY}
        - -test.run=TestPluginHelperProcess$
      env:
        `+helperEnv+`: "1"
        GESTALT_PLUGIN_MODE: "oauth"
      startup_timeout: 5s
      request_timeout: 5s
`)

	result := mustBootstrapPluginEnv(t, cfg)

	prov, err := result.Providers.Get("oauth-demo")
	if err != nil {
		t.Fatalf("Providers.Get: %v", err)
	}
	oauthProv, ok := prov.(core.OAuthProvider)
	if !ok {
		t.Fatal("expected oauth plugin to advertise OAuthProvider")
	}

	authURL := oauthProv.AuthorizationURL("state-123", []string{"scope:a"})
	if authURL != "https://example.com/oauth/start" {
		t.Fatalf("AuthorizationURL: got %q, want https://example.com/oauth/start", authURL)
	}

	type oauthStarter interface {
		StartOAuth(state string, scopes []string) (authURL string, verifier string, err error)
	}
	starter, ok := prov.(oauthStarter)
	if !ok {
		t.Fatal("expected oauth plugin to expose StartOAuth")
	}
	authURL, verifier, startErr := starter.StartOAuth("state-123", []string{"scope:a"})
	if startErr != nil {
		t.Fatalf("StartOAuth: %v", startErr)
	}
	if authURL != "https://example.com/oauth/start" || verifier != "verifier-123" {
		t.Fatalf("StartOAuth: got (%q, %q), want (https://example.com/oauth/start, verifier-123)", authURL, verifier)
	}

	type oauthVerifierExchanger interface {
		ExchangeCodeWithVerifier(ctx context.Context, code, verifier string) (*core.TokenResponse, error)
	}
	exchanger, ok := prov.(oauthVerifierExchanger)
	if !ok {
		t.Fatal("expected oauth plugin to expose ExchangeCodeWithVerifier")
	}
	tokenResp, err := exchanger.ExchangeCodeWithVerifier(context.Background(), "code-123", "verifier-123")
	if err != nil {
		t.Fatalf("ExchangeCodeWithVerifier: %v", err)
	}
	if tokenResp.AccessToken != "oauth-access-token" || tokenResp.RefreshToken != "oauth-refresh-token" {
		t.Fatalf("ExchangeCodeWithVerifier: got %+v", tokenResp)
	}

	refreshResp, err := oauthProv.RefreshToken(context.Background(), "oauth-refresh-token")
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if refreshResp.AccessToken != "oauth-access-token-refreshed" {
		t.Fatalf("RefreshToken: got %+v, want refreshed access token", refreshResp)
	}
}

func TestPluginHelperProcess(t *testing.T) { //nolint:paralleltest // subprocess helper, not a real test
	if os.Getenv(helperEnv) != "1" {
		return
	}

	state := &helperState{}
	codec := newHelperCodec(os.Stdin, os.Stdout)

	for {
		req, err := codec.read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				os.Exit(0)
			}
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		switch req.Method {
		case "initialize":
			var params pluginproc.InitializeParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				writeHelperError(codec, req.ID, -32700, err.Error())
				continue
			}
			if params.Integration.Config != nil {
				state.pluginConfig = params.Integration.Config
			}
			writeHelperResult(codec, req.ID, pluginproc.InitializeResult{
				ProtocolVersion: pluginproc.ProtocolVersion,
				PluginInfo: pluginproc.PluginInfo{
					Name:    "test-plugin",
					Version: "1.0.0",
				},
				Provider: state.manifest(),
				Capabilities: pluginproc.PluginCapabilities{
					ManualAuth: state.mode() == "manual",
					OAuth:      state.mode() == "oauth",
				},
			})
		case "provider.execute":
			var params pluginproc.ExecuteParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				writeHelperError(codec, req.ID, -32700, err.Error())
				continue
			}
			payload := map[string]any{
				"operation":     params.Operation,
				"token":         params.Token,
				"params":        params.Params,
				"plugin_config": state.pluginConfig,
			}
			body, err := json.Marshal(payload)
			if err != nil {
				writeHelperError(codec, req.ID, -32603, err.Error())
				continue
			}
			writeHelperResult(codec, req.ID, core.OperationResult{
				Status: 200,
				Body:   string(body),
			})
		case "auth.start":
			writeHelperResult(codec, req.ID, pluginproc.AuthStartResult{
				AuthURL:  "https://example.com/oauth/start",
				Verifier: "verifier-123",
			})
		case "auth.exchange_code":
			writeHelperResult(codec, req.ID, core.TokenResponse{
				AccessToken:  "oauth-access-token",
				RefreshToken: "oauth-refresh-token",
				ExpiresIn:    3600,
				TokenType:    "Bearer",
			})
		case "auth.refresh_token":
			writeHelperResult(codec, req.ID, core.TokenResponse{
				AccessToken:  "oauth-access-token-refreshed",
				RefreshToken: "oauth-refresh-token",
				ExpiresIn:    3600,
				TokenType:    "Bearer",
			})
		case "shutdown":
			writeHelperResult(codec, req.ID, map[string]any{"ok": true})
			os.Exit(0)
		default:
			writeHelperError(codec, req.ID, -32601, "unknown method")
		}
	}
}

func mustBootstrapPluginEnv(t *testing.T, cfgPath string) *bootstrap.Result {
	t.Helper()

	factories := bootstrap.NewFactoryRegistry()
	factories.Auth["stub-auth"] = func(node yaml.Node, _ bootstrap.Deps) (core.AuthProvider, error) {
		return &coretesting.StubAuthProvider{N: "stub-auth"}, nil
	}
	factories.Secrets["env"] = secretsenv.Factory
	factories.Datastores["stub-ds"] = func(node yaml.Node, _ bootstrap.Deps) (core.Datastore, error) {
		return &coretesting.StubDatastore{
			TokenFn: func(ctx context.Context, userID, integration, instance string) (*core.IntegrationToken, error) {
				if userID != "user-123" {
					t.Fatalf("TokenFn userID: got %q, want user-123", userID)
				}
				if integration != "demo" && integration != "manual-demo" {
					t.Fatalf("TokenFn integration: got %q", integration)
				}
				if instance != "default" {
					t.Fatalf("TokenFn instance: got %q, want default", instance)
				}
				return &core.IntegrationToken{AccessToken: "stored-access-token"}, nil
			},
		}, nil
	}
	factories.DefaultProvider = defaultProviderFactory(nil)

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("bootstrap.Bootstrap: %v", err)
	}
	<-result.ProvidersReady
	return result
}

func mustWritePluginConfig(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

type helperState struct {
	pluginMode   string
	pluginConfig any
}

func (s *helperState) mode() string {
	if s.pluginMode != "" {
		return s.pluginMode
	}
	s.pluginMode = os.Getenv("GESTALT_PLUGIN_MODE")
	return s.pluginMode
}

func (s *helperState) manifest() pluginproc.ProviderManifest {
	switch s.mode() {
	case "manual":
		return pluginproc.ProviderManifest{
			DisplayName:    "Manual Demo",
			Description:    "Subprocess plugin with manual auth",
			ConnectionMode: string(core.ConnectionModeUser),
			Operations: []core.Operation{
				{Name: "ping", Description: "Ping the plugin", Method: "POST"},
			},
			Auth: &pluginproc.ProviderAuthConfig{Type: pluginproc.AuthTypeManual},
		}
	case "oauth":
		return pluginproc.ProviderManifest{
			DisplayName:    "OAuth Demo",
			Description:    "Subprocess plugin with oauth",
			ConnectionMode: string(core.ConnectionModeUser),
			Operations: []core.Operation{
				{Name: "ping", Description: "Ping the plugin", Method: "POST"},
			},
			Auth: &pluginproc.ProviderAuthConfig{Type: pluginproc.AuthTypeOAuth2},
		}
	default:
		return pluginproc.ProviderManifest{
			DisplayName:    "Demo Plugin",
			Description:    "Subprocess plugin",
			ConnectionMode: string(core.ConnectionModeUser),
			Operations: []core.Operation{
				{Name: "echo", Description: "Echo the payload", Method: "POST"},
				{Name: "hidden", Description: "Should be filtered", Method: "POST"},
			},
		}
	}
}

type helperCodec struct {
	r *bufio.Reader
	w *bufio.Writer
}

type helperMessage struct {
	ID     *int64          `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
}

func newHelperCodec(r io.Reader, w io.Writer) *helperCodec {
	return &helperCodec{r: bufio.NewReader(r), w: bufio.NewWriter(w)}
}

func (c *helperCodec) read() (*helperMessage, error) {
	length := -1
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			if _, err := fmt.Sscanf(line, "Content-Length: %d", &length); err != nil {
				return nil, err
			}
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("missing Content-Length")
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(c.r, payload); err != nil {
		return nil, err
	}
	var msg helperMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func (c *helperCodec) write(id *int64, result any, rpcErr *helperRPCError) error {
	msg := map[string]any{"jsonrpc": "2.0"}
	if id != nil {
		msg["id"] = *id
	}
	if rpcErr != nil {
		msg["error"] = rpcErr
	} else {
		msg["result"] = result
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
		return err
	}
	if _, err := c.w.Write(payload); err != nil {
		return err
	}
	return c.w.Flush()
}

type helperRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func writeHelperResult(c *helperCodec, id *int64, result any) {
	if err := c.write(id, result, nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func writeHelperError(c *helperCodec, id *int64, code int, message string) {
	if err := c.write(id, nil, &helperRPCError{Code: code, Message: message}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
