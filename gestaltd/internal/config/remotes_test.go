package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemotesConfig(t *testing.T) {
	t.Parallel()

	cfg, err := Load(mustWriteConfigFile(t, `
server:
  remote: https://valon.tools/
  remoteToken: test-token
`))
	if err != nil {
		t.Fatalf("Load legacy remote: %v", err)
	}
	remote := cfg.Server.Remotes["default"]
	if remote == nil || remote.URL != "https://valon.tools" || remote.Token != "test-token" || !remote.Default {
		t.Fatalf("legacy canonicalization = %#v", remote)
	}
	if cfg.Server.Remote != "" || cfg.Server.RemoteToken != "" {
		t.Fatalf("legacy fields remain: remote=%q token=%q", cfg.Server.Remote, cfg.Server.RemoteToken)
	}

	_, err = Load(mustWriteConfigFile(t, `
server:
  remote: https://legacy.test
  remotes:
    default:
      url: https://canonical.test
      token: token
      default: true
`))
	if err == nil || !strings.Contains(err.Error(), "server.remote conflicts") {
		t.Fatalf("conflict: err = %v", err)
	}

	cfg, err = Load(mustWriteConfigFile(t, `
server:
  remote: https://prod.test
  remotes:
    default:
      url: https://prod.test/
      token: token
      default: true
`))
	if err != nil {
		t.Fatalf("equivalent legacy and remotes url: %v", err)
	}
	if got := cfg.Server.Remotes["default"].URL; got != "https://prod.test" {
		t.Fatalf("canonical url = %q, want https://prod.test", got)
	}

	dir := t.TempDir()
	basePath := filepath.Join(dir, "base.yaml")
	overridePath := filepath.Join(dir, "override.yaml")
	if err := os.WriteFile(basePath, []byte(`
apiVersion: gestaltd.config/v8
server:
  remotes:
    prod:
      url: https://valon.tools
      token: prod-token
      default: true
    teammate:
      url: http://peer.test:8080
      token: peer-token
apps:
  linear:
    source: {path: ./apps/linear/manifest.yaml}
    remote: prod
`), 0o644); err != nil {
		t.Fatalf("write base: %v", err)
	}
	if err := os.WriteFile(overridePath, []byte(`
apiVersion: gestaltd.config/v8
server:
  remotes:
    teammate: null
    prod:
      default: false
    staging:
      url: https://staging.test
      token: staging-token
      default: true
`), 0o644); err != nil {
		t.Fatalf("write override: %v", err)
	}
	cfg, err = LoadPaths([]string{basePath, overridePath})
	if err != nil {
		t.Fatalf("LoadPaths overlay: %v", err)
	}
	if cfg.Server.Remotes["teammate"] != nil || cfg.Server.Remotes["prod"].Default {
		t.Fatalf("overlay merge failed: %#v", cfg.Server.Remotes)
	}
	if got := cfg.DefaultRemoteName(); got != "staging" {
		t.Fatalf("DefaultRemoteName = %q, want staging", got)
	}
	if got := cfg.Apps["linear"].Remote; got != "prod" {
		t.Fatalf("apps.linear.remote = %q", got)
	}

	for name, yamlBody := range map[string]string{
		"path in url": `server: {remote: https://valon.tools/api, remoteToken: token}`,
		"two defaults": `
server:
  remotes:
    a: {url: https://a.test, token: a, default: true}
    b: {url: https://b.test, token: b, default: true}
`,
		"missing secondary token": `
server:
  remotes:
    prod: {url: https://prod.test, default: true}
    peer: {url: https://peer.test}
`,
		"unknown app remote": `
server:
  remotes:
    prod: {url: https://prod.test, token: a, default: true}
apps:
  linear:
    source: {path: ./apps/linear/manifest.yaml}
    remote: missing
`,
		"local and remote": `
server:
  remotes:
    prod: {url: https://prod.test, token: a, default: true}
apps:
  linear:
    source: {path: ./apps/linear/manifest.yaml}
    local: true
    remote: prod
`,
		"missing remote url": `
server:
  remotes:
    prod: {token: tok, default: true}
`,
		"whitespace remote key": `
server:
  remotes:
    " prod ":
      url: https://prod.test
      token: tok
      default: true
`,
		"remote on identity provider": `
server:
  remotes:
    prod: {url: https://prod.test, token: tok, default: true}
    teammate: {url: https://peer.test, token: peer}
providers:
  identity:
    foo:
      source: {path: ./providers/foo}
      remote: teammate
`,
		"remote on workflow provider": `
server:
  remotes:
    prod: {url: https://prod.test, token: tok, default: true}
providers:
  workflow:
    wf:
      source: {path: ./providers/wf}
      remote: prod
`,
		"remote on agent provider": `
server:
  remotes:
    prod: {url: https://prod.test, token: tok, default: true}
providers:
  agent:
    ag:
      source: {path: ./providers/ag}
      remote: prod
`,
	} {
		t.Run("reject "+name, func(t *testing.T) {
			t.Parallel()
			if _, err := Load(mustWriteConfigFile(t, yamlBody)); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestApplyServeRemoteOverrides(t *testing.T) {
	t.Run("cli overrides and token resolution", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{
			Server: ServerConfig{
				Remotes: map[string]*RemoteConfig{
					"prod": {URL: "https://old.test", Token: "old", Default: true},
				},
			},
		}
		if err := ApplyServeRemoteOverrides(cfg, "https://new.test", "new-token"); err != nil {
			t.Fatalf("ApplyServeRemoteOverrides: %v", err)
		}
		if got := cfg.Server.Remotes["prod"].URL; got != "https://new.test" {
			t.Fatalf("url = %q", got)
		}
		if got := cfg.Server.Remotes["prod"].Token; got != "new-token" {
			t.Fatalf("token = %q", got)
		}
	})

	t.Run("token resolution table", func(t *testing.T) {
		for _, tc := range []struct {
			name        string
			envToken    string
			configJSON  string
			storedToken string
			credentials string
			remotes     map[string]*RemoteConfig
			remoteFlag  string
			tokenFlag   string
			wantToken   string
			wantErr     string
		}{
			{
				name:       "legacy load then env token",
				envToken:   "env-token",
				remoteFlag: "",
				wantToken:  "env-token",
			},
			{
				name:        "stored credentials",
				storedToken: "stored-token",
				wantToken:   "stored-token",
			},
			{
				name:    "missing default token",
				wantErr: "server.remotes.default.token is required for the default remote",
			},
			{
				name:       "config.json creates default",
				configJSON: `{"url":"https://cli.test"}`,
				envToken:   "env-token",
				wantToken:  "env-token",
			},
			{
				name:       "secondary-only ignores config.json",
				configJSON: `{"url":"https://cli.test"}`,
				remotes: map[string]*RemoteConfig{
					"peer": {URL: "https://peer.test", Token: "peer-token"},
				},
			},
			{
				name:     "secondary never uses ambient token",
				envToken: "env-token",
				remotes: map[string]*RemoteConfig{
					"peer": {URL: "https://peer.test"},
				},
				wantErr: "server.remotes.peer.token is required",
			},
			{
				name: "remote-token without default",
				remotes: map[string]*RemoteConfig{
					"peer": {URL: "https://peer.test", Token: "peer-token"},
				},
				tokenFlag: "token",
				wantErr:   "--remote-token requires a default remote",
			},
			{
				name:        "malformed credentials",
				credentials: "{",
				remotes: map[string]*RemoteConfig{
					"default": {URL: "https://prod.test", Default: true},
				},
				wantErr: "credentials.json",
			},
			{
				name:       "remote-token with config.json only",
				configJSON: `{"url":"https://cli.test"}`,
				tokenFlag:  "cli-token",
				wantToken:  "cli-token",
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Setenv("GESTALT_API_KEY", tc.envToken)
				xdg := t.TempDir()
				t.Setenv("XDG_CONFIG_HOME", xdg)
				gestaltDir := filepath.Join(xdg, "gestalt")
				if err := os.MkdirAll(gestaltDir, 0o755); err != nil {
					t.Fatalf("MkdirAll: %v", err)
				}
				if tc.configJSON != "" {
					if err := os.WriteFile(filepath.Join(gestaltDir, "config.json"), []byte(tc.configJSON), 0o600); err != nil {
						t.Fatalf("WriteFile config.json: %v", err)
					}
				}
				if tc.storedToken != "" {
					credentials := fmt.Sprintf(`{"api_token":%q,"api_token_id":"tok_1"}`, tc.storedToken)
					if err := os.WriteFile(filepath.Join(gestaltDir, "credentials.json"), []byte(credentials), 0o600); err != nil {
						t.Fatalf("WriteFile credentials: %v", err)
					}
				}
				if tc.credentials != "" {
					if err := os.WriteFile(filepath.Join(gestaltDir, "credentials.json"), []byte(tc.credentials), 0o600); err != nil {
						t.Fatalf("WriteFile credentials: %v", err)
					}
				}

				var cfg *Config
				switch {
				case tc.remotes != nil:
					cfg = &Config{Server: ServerConfig{Remotes: tc.remotes}}
				case tc.configJSON != "" && tc.tokenFlag != "":
					cfg = &Config{}
				default:
					path := mustWriteConfigFile(t, `
server:
  remote: https://valon.tools
`)
					var err error
					cfg, err = Load(path)
					if err != nil {
						t.Fatalf("Load: %v", err)
					}
				}

				err := ApplyServeRemoteOverrides(cfg, tc.remoteFlag, tc.tokenFlag)
				if tc.wantErr != "" {
					if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
						t.Fatalf("unexpected error: %v", err)
					}
					return
				}
				if err != nil {
					t.Fatalf("ApplyServeRemoteOverrides: %v", err)
				}
				if tc.remotes != nil && cfg.DefaultRemoteName() == "" && tc.configJSON != "" {
					return
				}
				if tc.wantToken != "" {
					name := cfg.DefaultRemoteName()
					if name == "" {
						name = legacyDefaultRemoteName
					}
					if got := cfg.Server.Remotes[name].Token; got != tc.wantToken {
						t.Fatalf("token = %q, want %q", got, tc.wantToken)
					}
				}
			})
		}
	})
}
