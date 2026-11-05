package bootstrap_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/featureflags"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/services/apps/packageio"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

const (
	bearerMCPAppName    = "vci-brain"
	bearerMCPRegistry   = "berkadia"
	bearerMCPVersion    = "0.0.0-snapshot.gregressiontest"
	bearerMCPToken      = "vci-user-token"
	bearerMCPDisplay    = "VCI Brain"
	bearerMCPDesc       = "Query the VCI Brain knowledge base via its hosted MCP server."
	bearerMCPConnection = "MCP"
	bearerMCPOperation  = "list_brains"
)

// TestBearerMCPAppConnectsAndInvokesWithUserToken drives the user flow for a
// bearer-token MCP app (list apps, connect a token the way `gestalt app
// connect` does, invoke) through a server composed the way the runtime
// composes it. The app's manifest is identical in both cases; only how the
// package reaches gestaltd differs.
//
// Regression for berkadia.valon.tools vci-brain (2026-11-05): the same
// published package served from a local path advertised its MCP connection,
// but installed from an app registry it advertised no connections, so neither
// the CLI nor the UI could store a token and every call reached the upstream
// without one ("initialize: transport error: authorization required").
func TestBearerMCPAppConnectsAndInvokesWithUserToken(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		registry bool
		// yamlRef also declares the connection in deploy config, as the
		// toolshed#6239 stopgap does for berkadia.valon.tools.
		yamlRef bool
	}{
		{name: "path_source"},
		{name: "registry_installed", registry: true},
		{name: "registry_installed_with_yaml_ref", registry: true, yamlRef: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			upstream := newBearerMCPUpstream(t, bearerMCPToken)
			packageDir := writeBearerMCPPackage(t, t.TempDir(), upstream.URL)

			cfg := validConfig()
			artifactsDir := t.TempDir()
			cfg.Server.ArtifactsDir = artifactsDir
			opts := bootstrap.BootstrapOptions{}
			if tc.registry {
				registry, err := config.NewGCSAppRegistry("gs://berkadia-regression-test-registry")
				if err != nil {
					t.Fatalf("NewGCSAppRegistry: %v", err)
				}
				cfg.AppRegistries = map[string]config.AppRegistryConfig{bearerMCPRegistry: registry}
				cfg.Apps[bearerMCPAppName] = &config.ProviderEntry{
					Source: config.ProviderSource{Registry: bearerMCPRegistry},
				}
				// The daemon wires this resolver for registry apps (daemon/factories.go).
				opts.RegistryResolver = &appregistry.MountService{ArtifactsDir: artifactsDir}
			} else {
				cfg.Apps[bearerMCPAppName] = pathSourcedBearerMCPEntry(t, packageDir)
			}
			if tc.yamlRef {
				loaded := loadBearerMCPStopgapConfig(t)
				cfg.Connections = loaded.Connections
				cfg.Apps[bearerMCPAppName].Connections = loaded.Apps[bearerMCPAppName].Connections
			}

			result, err := bootstrap.BootstrapWithOptions(ctx, cfg, validFactories(), opts)
			if err != nil {
				t.Fatalf("Bootstrap: %v", err)
			}
			t.Cleanup(func() { _ = result.Close(context.Background()) })
			select {
			case <-result.ProvidersReady:
			case <-time.After(10 * time.Second):
				t.Fatal("providers did not become ready")
			}

			// The runtime builds connection maps and the server before the
			// registry startup task installs any registry app version.
			ts := newRuntimeComposedServer(t, cfg, result)

			if tc.registry {
				installed := appregistry.MaterializedPath(artifactsDir, bearerMCPAppName, bearerMCPVersion)
				copyDir(t, packageDir, installed)
				if err := result.AppRestarter.StartApp(ctx, bearerMCPAppName, bearerMCPVersion); err != nil {
					t.Fatalf("StartApp: %v", err)
				}
			}

			// 1. The app directory advertises the manifest's metadata and its
			//    manual (bearer) connection, which is what the CLI and UI use
			//    to offer a connect flow.
			app := fetchAppListing(t, ts, bearerMCPAppName)
			if app.DisplayName != bearerMCPDisplay {
				t.Errorf("displayName = %q, want %q", app.DisplayName, bearerMCPDisplay)
			}
			if app.Description != bearerMCPDesc {
				t.Errorf("description = %q, want %q", app.Description, bearerMCPDesc)
			}
			conn, ok := app.connection(bearerMCPConnection)
			if !ok {
				t.Fatalf("connections = %+v, want %q advertised", app.Connections, bearerMCPConnection)
			}
			if !containsString(conn.AuthTypes, "manual") {
				t.Errorf("connection %q authTypes = %v, want manual", bearerMCPConnection, conn.AuthTypes)
			}
			if len(conn.CredentialFields) != 1 || conn.CredentialFields[0].Name != "token" {
				t.Errorf("connection %q credentialFields = %+v, want [token]", bearerMCPConnection, conn.CredentialFields)
			}

			// 2. Store the user's token through the endpoint `gestalt app
			//    connect` and the web connect panel use for manual auth.
			connectManual(t, ts, bearerMCPAppName, bearerMCPConnection, bearerMCPToken)

			// 3. Invocations send the stored token upstream.
			user, err := result.Services.Users.FindOrCreateUser(ctx, "anonymous@gestalt")
			if err != nil {
				t.Fatalf("FindOrCreateUser: %v", err)
			}
			caller := &principal.Principal{
				UserID:    user.ID,
				SubjectID: principal.UserSubjectID(user.ID),
				Kind:      principal.KindUser,
			}
			res, err := result.Invoker.Invoke(ctx, caller, bearerMCPAppName, "", bearerMCPOperation, nil)
			if err != nil {
				t.Fatalf("Invoke(%s): %v (upstream saw Authorization %q)", bearerMCPOperation, err, upstream.authorizations())
			}
			if res.Status != http.StatusOK {
				t.Fatalf("Invoke(%s) status = %d body = %s", bearerMCPOperation, res.Status, res.Body)
			}
			if got := upstream.authorizations(); !containsString(got, "Bearer "+bearerMCPToken) {
				t.Fatalf("upstream Authorization headers = %q, want the user's bearer token", got)
			}

			// Every variant stores the token under the same audience, so
			// tokens stored under the YAML stopgap keep resolving once it is
			// removed in favor of the manifest connection.
			creds, err := result.Services.ExternalCredentials.ListCredentials(ctx, caller.SubjectID, "")
			if err != nil {
				t.Fatalf("ListCredentials: %v", err)
			}
			if len(creds) != 1 || creds[0].Audience != bearerMCPAppName+":"+bearerMCPConnection {
				audiences := make([]string, 0, len(creds))
				for _, cred := range creds {
					audiences = append(audiences, cred.Audience)
				}
				t.Fatalf("stored credential audiences = %q, want [%s:%s]", audiences, bearerMCPAppName, bearerMCPConnection)
			}
		})
	}
}

// loadBearerMCPStopgapConfig loads, through the real config loader, the deploy
// config shape toolshed#6239 uses: a top-level connection named
// "<app>:<connection>" bound to the app by ref.
func loadBearerMCPStopgapConfig(t *testing.T) *config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	yamlConfig := `apiVersion: gestaltd.config/v8
connections:
  "` + bearerMCPAppName + `:` + bearerMCPConnection + `":
    displayName: MCP
    mode: subject
    auth:
      type: manual
      credentials:
        - name: token
          label: VCI Brain API Token
apps:
  ` + bearerMCPAppName + `:
    mcp: true
    source:
      path: ./manifest.yaml
    connections:
      ` + bearerMCPConnection + `:
        ref: "` + bearerMCPAppName + `:` + bearerMCPConnection + `"
`
	if err := os.WriteFile(path, []byte(yamlConfig), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	loaded, err := config.LoadPartialAllowMissingEnvPaths([]string{path})
	if err != nil {
		t.Fatalf("load stopgap config: %v", err)
	}
	return loaded
}

// TestRegistryAppDefinitionsFollowInstalledVersion checks that the routing a
// registry-installed app publishes tracks the running version: a new version
// replaces it, a stop clears it, and readers never race the swap.
func TestRegistryAppDefinitionsFollowInstalledVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const (
		v1, conn1 = "0.0.0-snapshot.gversionone", "MCP"
		v2, conn2 = "0.0.0-snapshot.gversiontwo", "Brain"
	)
	upstream := newBearerMCPUpstream(t, bearerMCPToken)
	artifactsDir := t.TempDir()
	for _, pkg := range []struct{ version, connection string }{{v1, conn1}, {v2, conn2}} {
		dir := appregistry.MaterializedPath(artifactsDir, bearerMCPAppName, pkg.version)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		writeBearerMCPPackageFor(t, dir, pkg.version, pkg.connection, upstream.URL)
	}

	cfg := validConfig()
	cfg.Server.ArtifactsDir = artifactsDir
	registry, err := config.NewGCSAppRegistry("gs://berkadia-regression-test-registry")
	if err != nil {
		t.Fatalf("NewGCSAppRegistry: %v", err)
	}
	cfg.AppRegistries = map[string]config.AppRegistryConfig{bearerMCPRegistry: registry}
	cfg.Apps[bearerMCPAppName] = &config.ProviderEntry{Source: config.ProviderSource{Registry: bearerMCPRegistry}}
	result, err := bootstrap.BootstrapWithOptions(ctx, cfg, validFactories(), bootstrap.BootstrapOptions{
		RegistryResolver: &appregistry.MountService{ArtifactsDir: artifactsDir},
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	t.Cleanup(func() { _ = result.Close(context.Background()) })
	<-result.ProvidersReady
	ts := newRuntimeComposedServer(t, cfg, result)

	// advertised returns the app's advertised connection names, or nil when
	// the app is not listed (a stopped registry app may drop out entirely).
	advertised := func() []string {
		t.Helper()
		apps := fetchAppListings(t, ts)
		for _, app := range apps {
			if app.Name != bearerMCPAppName {
				continue
			}
			names := make([]string, 0, len(app.Connections))
			for _, conn := range app.Connections {
				names = append(names, conn.Name)
			}
			return names
		}
		return nil
	}
	mcpRoute := func() (string, bool) {
		return result.AppDefinitions.ConnectionName(invocation.ConnectionSurfaceMCP, bearerMCPAppName)
	}

	// Readers run against the server and overlay throughout the swaps so the
	// race detector sees concurrent publish, clear, and lookup.
	stop := make(chan struct{})
	var readers sync.WaitGroup
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if resp, err := http.Get(ts.URL + "/api/v1/apps"); err == nil {
					_, _ = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()
				}
				_, _ = mcpRoute()
				_, _ = result.AppDefinitions.AppEntry(bearerMCPAppName)
			}
		}()
	}

	for round := range 3 {
		if err := result.AppRestarter.StartApp(ctx, bearerMCPAppName, v1); err != nil {
			t.Fatalf("round %d StartApp(v1): %v", round, err)
		}
		if got := advertised(); !slices.Equal(got, []string{conn1}) {
			t.Fatalf("round %d v1 connections = %q, want [%s]", round, got, conn1)
		}
		if got, ok := mcpRoute(); !ok || got != conn1 {
			t.Fatalf("round %d v1 MCP route = %q, %v; want %q", round, got, ok, conn1)
		}

		if err := result.AppRestarter.StartApp(ctx, bearerMCPAppName, v2); err != nil {
			t.Fatalf("round %d StartApp(v2): %v", round, err)
		}
		if got := advertised(); !slices.Equal(got, []string{conn2}) {
			t.Fatalf("round %d v2 connections = %q, want [%s]", round, got, conn2)
		}
		if got, ok := mcpRoute(); !ok || got != conn2 {
			t.Fatalf("round %d v2 MCP route = %q, %v; want %q", round, got, ok, conn2)
		}

		if err := result.AppRestarter.StopApp(ctx, bearerMCPAppName); err != nil {
			t.Fatalf("round %d StopApp: %v", round, err)
		}
		if _, ok := mcpRoute(); ok {
			t.Fatalf("round %d MCP route still published after StopApp", round)
		}
		if _, ok := result.AppDefinitions.AppEntry(bearerMCPAppName); ok {
			t.Fatalf("round %d app entry still published after StopApp", round)
		}
		if got := advertised(); len(got) != 0 {
			t.Fatalf("round %d connections after StopApp = %q, want none", round, got)
		}
	}
	close(stop)
	readers.Wait()
}

func bearerMCPManifestYAMLFor(version, connection, upstreamURL string) string {
	return `kind: app
source: github.com/valon-technologies/berkadia-tools/apps/vci-brain
version: ` + version + `
displayName: ` + bearerMCPDisplay + `
description: ` + bearerMCPDesc + `
spec:
  surfaces:
    mcp:
      connection: ` + connection + `
      url: ` + upstreamURL + `
  connections:
    ` + connection + `:
      displayName: MCP
      mode: subject
      auth:
        type: bearer
        credentials:
          - name: token
            label: VCI Brain API Token
            description: Personal access token for the upstream MCP server.
`
}

// writeBearerMCPPackage lays out a package matching the published vci-brain
// artifact: a manifest plus a static catalog, no executable.
func writeBearerMCPPackage(t *testing.T, dir, upstreamURL string) string {
	t.Helper()
	return writeBearerMCPPackageFor(t, dir, bearerMCPVersion, bearerMCPConnection, upstreamURL)
}

func writeBearerMCPPackageFor(t *testing.T, dir, version, connection, upstreamURL string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(bearerMCPManifestYAMLFor(version, connection, upstreamURL)), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	catalog := "name: " + bearerMCPAppName + "\noperations:\n- id: " + bearerMCPOperation + "\n  method: GET\n  description: List brains.\n"
	if err := os.WriteFile(filepath.Join(dir, "catalog.yaml"), []byte(catalog), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	return dir
}

// pathSourcedBearerMCPEntry mirrors the entry config loading produces for a
// `source.path` app after manifest resolution.
func pathSourcedBearerMCPEntry(t *testing.T, packageDir string) *config.ProviderEntry {
	t.Helper()
	manifestPath := filepath.Join(packageDir, "manifest.yaml")
	_, manifest, err := packageio.ReadManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadManifestFile: %v", err)
	}
	return &config.ProviderEntry{
		Source:               config.ProviderSource{Path: manifestPath},
		ResolvedManifestPath: manifestPath,
		ResolvedManifest:     manifest,
		DisplayName:          manifest.DisplayName,
		Description:          manifest.Description,
	}
}

// newRuntimeComposedServer builds the public server from a bootstrap result
// with the same connection inputs internal/server/runtime.go passes.
func newRuntimeComposedServer(t *testing.T, cfg *config.Config, result *bootstrap.Result) *httptest.Server {
	t.Helper()
	connMaps, err := bootstrap.BuildConnectionMaps(cfg)
	if err != nil {
		t.Fatalf("BuildConnectionMaps: %v", err)
	}
	srv, err := server.New(server.Config{
		Services:             result.Services,
		Providers:            result.Providers,
		Invoker:              result.Invoker,
		FeatureFlags:         featureflags.AllEnabled(),
		ProviderKinds:        bootstrap.ProviderAuthorizationKinds(cfg),
		DefaultConnection:    connMaps.DefaultConnection,
		CatalogConnection:    connMaps.APIConnection,
		MCPConnection:        connMaps.MCPConnection,
		ConnectionAuth:       result.ConnectionAuth,
		ManualConnectionAuth: result.ManualConnectionAuth,
		AppDefs:              cfg.Apps,
		AppDefinitions:       result.AppDefinitions,
		StateSecret:          []byte("0123456789abcdef0123456789abcdef"),
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	t.Cleanup(srv.Close)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return ts
}

type appListingConnection struct {
	Name             string   `json:"name"`
	AuthTypes        []string `json:"authTypes"`
	CredentialFields []struct {
		Name string `json:"name"`
	} `json:"credentialFields"`
}

type appListing struct {
	Name        string                 `json:"name"`
	DisplayName string                 `json:"displayName"`
	Description string                 `json:"description"`
	Connections []appListingConnection `json:"connections"`
}

func (a appListing) connection(name string) (appListingConnection, bool) {
	for _, conn := range a.Connections {
		if conn.Name == name {
			return conn, true
		}
	}
	return appListingConnection{}, false
}

func fetchAppListing(t *testing.T, ts *httptest.Server, name string) appListing {
	t.Helper()
	for _, app := range fetchAppListings(t, ts) {
		if app.Name == name {
			return app
		}
	}
	t.Fatalf("app %q missing from /api/v1/apps", name)
	return appListing{}
}

func fetchAppListings(t *testing.T, ts *httptest.Server) []appListing {
	t.Helper()
	resp, err := http.Get(ts.URL + "/api/v1/apps")
	if err != nil {
		t.Fatalf("GET /api/v1/apps: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/v1/apps status = %d body = %s", resp.StatusCode, body)
	}
	var apps []appListing
	if err := json.Unmarshal(body, &apps); err != nil {
		t.Fatalf("decode /api/v1/apps: %v; body = %s", err, body)
	}
	return apps
}

// connectManual posts a single-field credential the way both clients do:
// the CLI (gestalt/cli/src/commands/apps/connect.rs ManualCredentials::request)
// and the web connect panel send one field as the raw `credential` string and
// use the `credentials` map only for multi-field auth.
func connectManual(t *testing.T, ts *httptest.Server, app, connection, credential string) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"integration": app,
		"connection":  connection,
		"credential":  credential,
	})
	if err != nil {
		t.Fatalf("marshal connect-manual: %v", err)
	}
	resp, err := http.Post(ts.URL+"/api/v1/auth/connect-manual", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("POST connect-manual: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("connect-manual status = %d body = %s", resp.StatusCode, body)
	}
}

type bearerMCPUpstream struct {
	*httptest.Server
	mu   sync.Mutex
	seen []string
}

func (u *bearerMCPUpstream) authorizations() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.seen...)
}

// newBearerMCPUpstream serves one MCP tool and rejects any request that does
// not carry the expected bearer token, like the hosted vci-brain server.
func newBearerMCPUpstream(t *testing.T, token string) *bearerMCPUpstream {
	t.Helper()
	mcpSrv := mcpserver.NewMCPServer("vci-brain-upstream", "1.0.0")
	mcpSrv.AddTool(
		mcpgo.NewTool(bearerMCPOperation, mcpgo.WithDescription("List brains")),
		func(context.Context, mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			return mcpgo.NewToolResultText(`{"brains":["berkadia"]}`), nil
		},
	)
	inner := mcpserver.NewStreamableHTTPServer(mcpSrv, mcpserver.WithStateLess(true))
	u := &bearerMCPUpstream{}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		u.mu.Lock()
		u.seen = append(u.seen, auth)
		u.mu.Unlock()
		if auth != "Bearer "+token {
			w.Header().Set("WWW-Authenticate", `Bearer realm="vci-brain"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(u.Close)
	return u
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", dst, err)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", src, err)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(src, entry.Name()))
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dst, entry.Name()), data, 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if strings.TrimSpace(v) == want {
			return true
		}
	}
	return false
}
