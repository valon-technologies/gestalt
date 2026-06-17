package providerdrivers

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc"
	"gopkg.in/yaml.v3"
)

func TestAuthenticationFactoryForwardsRuntimeDepsToExecutableProvider(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const callbackURL = "http://127.0.0.1:18088/auth/callback"

	authManifest := componentProviderManifestPath(t, setupGoContractProviderDir(t, dir, providermanifestv1.KindAuthentication, "local", authContractProviderSource(callbackURL)))
	auth, err := AuthenticationFactory(contractRuntimeNode(t, "local", authManifest), AuthenticationDeps{
		DefaultCallbackURL: callbackURL,
		HostServices: []runtimehost.HostService{{
			Name: "test",
			Register: func(*grpc.Server) {
			},
		}},
	})
	if err != nil {
		t.Fatalf("AuthenticationFactory: %v", err)
	}
	defer closeProviderIfSupported(t, auth)

	ctx := context.Background()
	authorizeResp, err := auth.Authorize(ctx, &core.AuthorizeRequest{
		ResponseType: "code",
		ClientID:     core.DefaultOAuthClientID,
		RedirectURI:  callbackURL,
		State:        "host-state",
	})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	parsed, err := url.Parse(authorizeResp.RedirectURI)
	if err != nil {
		t.Fatalf("url.Parse(redirect): %v", err)
	}
	code := parsed.Query().Get("code")
	if code == "" {
		t.Fatal("authorize redirect did not include code")
	}

	tokenResp, err := auth.Token(ctx, &core.TokenRequest{
		GrantType:   "authorization_code",
		Code:        code,
		RedirectURI: callbackURL,
		ClientID:    core.DefaultOAuthClientID,
	})
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tokenResp.AccessToken == "" {
		t.Fatal("Token returned empty access token")
	}

	introspectResp, err := auth.Introspect(ctx, &core.IntrospectRequest{Token: tokenResp.AccessToken})
	if err != nil {
		t.Fatalf("Introspect: %v", err)
	}
	if introspectResp == nil || !introspectResp.Active || introspectResp.Subject != "user:generated-auth@example.com" {
		t.Fatalf("Introspect = %+v, want active subject user:generated-auth@example.com", introspectResp)
	}
}

func setupGoContractProviderDir(t *testing.T, baseDir, kind, name, source string) string {
	t.Helper()

	providerDir := filepath.Join(baseDir, kind, name)
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", providerDir, err)
	}
	writeTestFile(t, providerDir, "go.mod", []byte(testutil.GeneratedProviderModuleSource(t, "example.com/providers/"+kind+"/"+name)), 0o644)
	writeTestFile(t, providerDir, "go.sum", testutil.GeneratedProviderModuleSum(t), 0o644)
	writeTestFile(t, providerDir, "provider.go", []byte(source), 0o644)
	writeTestFile(t, providerDir, filepath.Join("cmd", "provider", "main.go"), []byte(fmt.Sprintf(`package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	providerpkg %q
	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := gestalt.ServeAuthenticationProvider(ctx, providerpkg.New()); err != nil {
		fmt.Fprintf(os.Stderr, "error: %%v\n", err)
		os.Exit(1)
	}
}
`, "example.com/providers/"+kind+"/"+name)), 0o644)
	artifactRel := ".gestalt/build/provider"
	writeTestFile(t, providerDir, "build.sh", []byte("mkdir -p .gestalt/build\ngo build -o .gestalt/build/provider ./cmd/provider\n"), 0o755)
	writeManifestFile(t, providerDir, &providermanifestv1.Manifest{
		Kind:        kind,
		Source:      "github.com/test/providers/" + name,
		Version:     "0.0.1-alpha.1",
		DisplayName: name,
		Spec:        &providermanifestv1.Spec{},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"go.mod", "go.sum", "provider.go", "cmd", "build.sh"},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactRel},
	})
	return providerDir
}

func componentProviderManifestPath(t *testing.T, providerDir string) string {
	t.Helper()

	manifestPath, err := providerpkg.FindManifestFile(providerDir)
	if err != nil {
		t.Fatalf("FindManifestFile(%s): %v", providerDir, err)
	}
	return manifestPath
}

func contractRuntimeNode(t *testing.T, name, manifestPath string) yaml.Node {
	t.Helper()

	var node yaml.Node
	if err := node.Encode(map[string]any{
		"name":         name,
		"manifestPath": manifestPath,
	}); err != nil {
		t.Fatalf("encode runtime node: %v", err)
	}
	return node
}

func closeProviderIfSupported(t *testing.T, provider any) {
	t.Helper()

	closer, ok := provider.(interface{ Close() error })
	if !ok {
		return
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("close provider: %v", err)
	}
}

func authContractProviderSource(wantCallbackURL string) string {
	source := testutil.GeneratedAuthPackageSource()
	source = strings.Replace(source, `"net/url"
	"strings"`, `"net/url"
	"os"
	"strings"`, 1)
	source = strings.Replace(source, `func (p *Provider) Authorize(_ context.Context, req *gestalt.AuthorizeRequest) (*gestalt.AuthorizeResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("authorize request is required")
	}
	redirectURI := strings.TrimSpace(req.RedirectURI)
	if redirectURI == "" {
		return nil, fmt.Errorf("redirect_uri is required")
	}`, fmt.Sprintf(`func (p *Provider) Authorize(_ context.Context, req *gestalt.AuthorizeRequest) (*gestalt.AuthorizeResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("authorize request is required")
	}
	redirectURI := strings.TrimSpace(req.RedirectURI)
	if redirectURI == "" {
		return nil, fmt.Errorf("redirect_uri is required")
	}
	if os.Getenv("GESTALT_HOST_SERVICE_SOCKET") == "" {
		return nil, fmt.Errorf("GESTALT_HOST_SERVICE_SOCKET is not set")
	}
	if redirectURI != %q {
		return nil, fmt.Errorf("redirect_uri = %%q, want %%q", redirectURI, %q)
	}`, wantCallbackURL, wantCallbackURL), 1)
	source = strings.Replace(source, `	if strings.Count(req.Token, ".") == 2 {
		return &gestalt.IntrospectResponse{
			Active:   true,
			Subject:  "user:jwt@example.com",
			ClientID: "gestaltd",
		}, nil
	}`, `	if strings.Count(req.Token, ".") == 2 {
		return &gestalt.IntrospectResponse{Active: false}, nil
	}`, 1)
	return source
}

func writeTestFile(t *testing.T, dir, name string, data []byte, perm os.FileMode) {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, perm); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeManifestFile(t *testing.T, dir string, manifest *providermanifestv1.Manifest) {
	t.Helper()

	data, err := yaml.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	writeTestFile(t, dir, "manifest.yaml", data, 0o644)
}
