package testutil

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func SDKGoModulePath(t *testing.T) string {
	t.Helper()
	root, ok := repoRoot()
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(root, "sdk", "go")
}

func ExampleProviderPluginPath(t *testing.T) string {
	t.Helper()
	return MustExampleProviderPluginPath()
}

func MustExampleProviderPluginPath() string {
	root, ok := repoRoot()
	if !ok {
		panic("runtime.Caller failed")
	}
	return filepath.Join(root, "gestaltd", "internal", "testutil", "testdata", "provider-go")
}

func CopyExampleProviderPlugin(t *testing.T, dst string) {
	t.Helper()

	src := ExampleProviderPluginPath(t)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", dst, err)
	}
	if err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = out.Close()
			return err
		}
		return out.Close()
	}); err != nil {
		t.Fatalf("copy example provider plugin: %v", err)
	}

	goModPath := filepath.Join(dst, "go.mod")
	goMod, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("read %s: %v", goModPath, err)
	}
	updated := rewriteModuleLine(
		t,
		string(goMod),
		"replace github.com/valon-technologies/gestalt/sdk/go => ",
		"replace github.com/valon-technologies/gestalt/sdk/go => "+SDKGoModulePath(t),
	)
	if err := os.WriteFile(goModPath, []byte(updated), 0o644); err != nil {
		t.Fatalf("write %s: %v", goModPath, err)
	}
}

func repoRoot() (string, bool) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", false
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..")), true
}

func GeneratedProviderPackageSource() string {
	return `package provider

import (
	"context"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) Configure(context.Context, string, map[string]any) error { return nil }

func (p *Provider) generatedOp(context.Context, struct{}, gestalt.Request) (gestalt.Response[map[string]any], error) {
	return gestalt.OK(map[string]any{}), nil
}

var Router = gestalt.MustRouter(
	gestalt.Register(gestalt.Operation[struct{}, map[string]any]{ID: "generated_op"}, (*Provider).generatedOp),
)
`
}

func GeneratedAuthPackageSource() string {
	return `package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) Configure(context.Context, string, map[string]any) error { return nil }

func (p *Provider) Metadata() gestalt.ProviderMetadata {
	return gestalt.ProviderMetadata{
		Kind:        gestalt.ProviderKindAuth,
		Name:        "generated-auth",
		DisplayName: "Generated Auth",
	}
}

func (p *Provider) BeginLogin(context.Context, gestalt.BeginLoginRequest) (*gestalt.BeginLoginResponse, error) {
	return &gestalt.BeginLoginResponse{
		AuthorizationURL: "https://auth.example.test/login?state=idp-state&prompt=consent",
	}, nil
}

func (p *Provider) CompleteLogin(_ context.Context, req gestalt.CompleteLoginRequest) (*gestalt.AuthenticatedUser, error) {
	if req.Query["state"] != "idp-state" {
		return nil, fmt.Errorf("unexpected state %q", req.Query["state"])
	}
	if req.Query["prompt"] != "consent" {
		return nil, fmt.Errorf("unexpected prompt %q", req.Query["prompt"])
	}
	return &gestalt.AuthenticatedUser{
		Email:       "generated-auth@example.com",
		DisplayName: "Generated Auth User",
	}, nil
}

func (p *Provider) ValidateExternalToken(_ context.Context, token string) (*gestalt.AuthenticatedUser, error) {
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
}

func (p *Provider) SessionTTL() time.Duration { return 90 * time.Minute }
`
}

func GeneratedDatastorePackageSource() string {
	return `package datastore

import (
	"context"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

type Provider struct {
	users  map[string]*gestalt.StoredUser
	tokens map[string]*gestalt.StoredIntegrationToken
}

func New() *Provider {
	return &Provider{
		users:  map[string]*gestalt.StoredUser{},
		tokens: map[string]*gestalt.StoredIntegrationToken{},
	}
}

func (p *Provider) Configure(context.Context, string, map[string]any) error { return nil }

func (p *Provider) Metadata() gestalt.ProviderMetadata {
	return gestalt.ProviderMetadata{
		Kind:        gestalt.ProviderKindDatastore,
		Name:        "generated-datastore",
		DisplayName: "Generated Datastore",
	}
}

func (p *Provider) Warnings() []string { return []string{"generated datastore warning"} }

func (p *Provider) HealthCheck(context.Context) error { return nil }

func (p *Provider) Migrate(context.Context) error { return nil }

func (p *Provider) GetUser(_ context.Context, id string) (*gestalt.StoredUser, error) {
	return p.users[id], nil
}

func (p *Provider) FindOrCreateUser(_ context.Context, email string) (*gestalt.StoredUser, error) {
	if user, ok := p.users[email]; ok {
		return user, nil
	}
	now := time.Now().UTC().Truncate(time.Second)
	user := &gestalt.StoredUser{
		ID:        email,
		Email:     email,
		CreatedAt: now,
		UpdatedAt: now,
	}
	p.users[email] = user
	return user, nil
}

func (p *Provider) PutIntegrationToken(_ context.Context, token *gestalt.StoredIntegrationToken) error {
	cloned := *token
	cloned.AccessTokenSealed = append([]byte(nil), token.AccessTokenSealed...)
	cloned.RefreshTokenSealed = append([]byte(nil), token.RefreshTokenSealed...)
	cloned.ConnectionParams = cloneStringMap(token.ConnectionParams)
	p.tokens[token.UserID] = &cloned
	return nil
}

func (p *Provider) GetIntegrationToken(_ context.Context, userID, _, _, _ string) (*gestalt.StoredIntegrationToken, error) {
	if token, ok := p.tokens[userID]; ok {
		cloned := *token
		cloned.AccessTokenSealed = append([]byte(nil), token.AccessTokenSealed...)
		cloned.RefreshTokenSealed = append([]byte(nil), token.RefreshTokenSealed...)
		cloned.ConnectionParams = cloneStringMap(token.ConnectionParams)
		return &cloned, nil
	}
	return nil, nil
}

func (p *Provider) ListIntegrationTokens(_ context.Context, userID, _, _ string) ([]*gestalt.StoredIntegrationToken, error) {
	token, err := p.GetIntegrationToken(context.Background(), userID, "", "", "")
	if err != nil || token == nil {
		return nil, err
	}
	return []*gestalt.StoredIntegrationToken{token}, nil
}

func (p *Provider) DeleteIntegrationToken(_ context.Context, id string) error {
	delete(p.tokens, id)
	return nil
}

func (p *Provider) PutAPIToken(context.Context, *gestalt.StoredAPIToken) error { return nil }
func (p *Provider) GetAPITokenByHash(context.Context, string) (*gestalt.StoredAPIToken, error) {
	return nil, nil
}
func (p *Provider) ListAPITokens(context.Context, string) ([]*gestalt.StoredAPIToken, error) {
	return nil, nil
}
func (p *Provider) RevokeAPIToken(context.Context, string, string) error { return nil }
func (p *Provider) RevokeAllAPITokens(context.Context, string) (int64, error) {
	return 0, nil
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
`
}

func GeneratedSecretsPackageSource() string {
	return `package secrets

import (
	"context"
	"fmt"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

type Provider struct {
	secrets map[string]string
}

func New() *Provider {
	return &Provider{
		secrets: map[string]string{
			"generated-secret": "generated-secret-value",
			"source-token":     "ghp_inline_auth_source_token",
		},
	}
}

func (p *Provider) Configure(context.Context, string, map[string]any) error { return nil }

func (p *Provider) Metadata() gestalt.ProviderMetadata {
	return gestalt.ProviderMetadata{
		Kind:        gestalt.ProviderKindSecrets,
		Name:        "generated-secrets",
		DisplayName: "Generated Secrets",
	}
}

func (p *Provider) GetSecret(_ context.Context, name string) (string, error) {
	if value, ok := p.secrets[name]; ok {
		return value, nil
	}
	return "", fmt.Errorf("secret %q not found", name)
}
`
}

func GeneratedProviderModuleSource(t *testing.T, module string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(ExampleProviderPluginPath(t), "go.mod"))
	if err != nil {
		t.Fatalf("ReadFile(example go.mod): %v", err)
	}
	source := rewriteModuleLine(t, string(data), "module ", "module "+module)
	return rewriteModuleLine(
		t,
		source,
		"replace github.com/valon-technologies/gestalt/sdk/go => ",
		"replace github.com/valon-technologies/gestalt/sdk/go => "+SDKGoModulePath(t),
	)
}

func GeneratedProviderModuleSum(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(ExampleProviderPluginPath(t), "go.sum"))
	if err != nil {
		t.Fatalf("ReadFile(example go.sum): %v", err)
	}
	return data
}

func rewriteModuleLine(t *testing.T, source, prefix, replacement string) string {
	t.Helper()
	lines := strings.Split(source, "\n")
	for i := range lines {
		if strings.HasPrefix(lines[i], prefix) {
			lines[i] = replacement
			return strings.Join(lines, "\n")
		}
	}
	t.Fatalf("missing line prefix %q", prefix)
	return ""
}
