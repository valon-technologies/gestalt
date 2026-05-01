package providerdrivers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	corecrypto "github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/core/session"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/plugins/providerpkg"
	"github.com/valon-technologies/gestalt/server/services/testutil"
	"gopkg.in/yaml.v3"
)

func TestAuthenticationFactoryForwardsRuntimeDepsToExecutableProvider(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const (
		callbackURL   = "http://127.0.0.1:18088/auth/callback"
		encryptionKey = "factory-adapter-contract-key"
	)

	authManifest := componentProviderManifestPath(t, setupGoContractProviderDir(t, dir, providermanifestv1.KindAuthentication, "local", authContractProviderSource(callbackURL)))
	auth, err := AuthenticationFactory(contractRuntimeNode(t, "local", authManifest), AuthenticationDeps{
		DefaultCallbackURL: callbackURL,
		SessionKey:         corecrypto.DeriveKey(encryptionKey),
	})
	if err != nil {
		t.Fatalf("AuthenticationFactory: %v", err)
	}
	defer closeProviderIfSupported(t, auth)

	if _, err := auth.LoginURL("host-state"); err != nil {
		t.Fatalf("LoginURL: %v", err)
	}

	token, err := session.IssueToken(&core.UserIdentity{
		Email:       "session@example.com",
		DisplayName: "Session User",
	}, corecrypto.DeriveKey(encryptionKey), time.Minute)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	identity, err := auth.ValidateToken(context.Background(), token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if identity == nil || identity.Email != "session@example.com" {
		t.Fatalf("ValidateToken identity = %+v, want session@example.com", identity)
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

	artifactRel := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "gestalt-"+name))
	artifactPath := filepath.Join(providerDir, filepath.FromSlash(artifactRel))
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(artifactPath), err)
	}
	if _, err := providerpkg.BuildSourceComponentReleaseBinary(providerDir, artifactPath, kind, runtime.GOOS, runtime.GOARCH); err != nil {
		t.Fatalf("BuildSourceComponentReleaseBinary(%s): %v", providerDir, err)
	}
	binData, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("read provider artifact: %v", err)
	}
	sum := sha256.Sum256(binData)
	writeManifestFile(t, providerDir, &providermanifestv1.Manifest{
		Kind:        kind,
		Source:      "github.com/test/providers/" + name,
		Version:     "0.0.1-alpha.1",
		DisplayName: name,
		Spec:        &providermanifestv1.Spec{},
		Artifacts: []providermanifestv1.Artifact{
			{OS: runtime.GOOS, Arch: runtime.GOARCH, Path: artifactRel, SHA256: hex.EncodeToString(sum[:])},
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
	source = strings.Replace(source, `func (p *Provider) BeginLogin(_ context.Context, req *gestalt.BeginLoginRequest) (*gestalt.BeginLoginResponse, error) {
	return &gestalt.BeginLoginResponse{
		AuthorizationUrl: "https://auth.example.test/login?state=idp-state&prompt=consent",
	}, nil
}`, fmt.Sprintf(`func (p *Provider) BeginLogin(_ context.Context, req *gestalt.BeginLoginRequest) (*gestalt.BeginLoginResponse, error) {
	if req.GetCallbackUrl() != %q {
		return nil, fmt.Errorf("callback URL = %%q, want %%q", req.GetCallbackUrl(), %q)
	}
	return &gestalt.BeginLoginResponse{
		AuthorizationUrl: "https://auth.example.test/login?state=idp-state&prompt=consent",
	}, nil
}`, wantCallbackURL, wantCallbackURL), 1)
	source = strings.Replace(source, `func (p *Provider) ValidateExternalToken(_ context.Context, token string) (*gestalt.AuthenticatedUser, error) {
	if token == "" {
		return nil, fmt.Errorf("token is required")
	}
	if strings.Count(token, ".") == 2 {
		return &gestalt.AuthenticatedUser{
			Email:       "jwt@example.com",
			DisplayName: "Validated JWT User",
		}, nil
	}
	return &gestalt.AuthenticatedUser{
		Email:       token + "@example.com",
		DisplayName: "Validated User",
	}, nil
}`, `func (p *Provider) ValidateExternalToken(_ context.Context, token string) (*gestalt.AuthenticatedUser, error) {
	return nil, fmt.Errorf("external token fallback should not be used for %q", token)
}`, 1)
	source = strings.Replace(source, `
	"strings"`, ``, 1)
	return source
}

func writeTestFile(t *testing.T, dir, name string, data []byte, perm os.FileMode) {
	t.Helper()

	path := filepath.Join(dir, name)
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
