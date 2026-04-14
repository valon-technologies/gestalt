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

func MustExampleDatastoreProviderPath() string {
	root, ok := repoRoot()
	if !ok {
		panic("runtime.Caller failed")
	}
	return filepath.Join(root, "gestaltd", "internal", "testutil", "testdata", "provider-rust-datastore")
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

func (p *Provider) BeginLogin(_ context.Context, req *gestalt.BeginLoginRequest) (*gestalt.BeginLoginResponse, error) {
	return &gestalt.BeginLoginResponse{
		AuthorizationUrl: "https://auth.example.test/login?state=idp-state&prompt=consent",
	}, nil
}

func (p *Provider) CompleteLogin(_ context.Context, req *gestalt.CompleteLoginRequest) (*gestalt.AuthenticatedUser, error) {
	if req.GetQuery()["state"] != "idp-state" {
		return nil, fmt.Errorf("unexpected state %q", req.GetQuery()["state"])
	}
	if req.GetQuery()["prompt"] != "consent" {
		return nil, fmt.Errorf("unexpected prompt %q", req.GetQuery()["prompt"])
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

func GeneratedCachePackageSource() string {
	return `package cache

import (
	"context"
	"sync"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/protobuf/types/known/emptypb"
)

type Provider struct {
	proto.UnimplementedCacheServer
	mu     sync.Mutex
	values map[string][]byte
}

func New() *Provider {
	return &Provider{values: map[string][]byte{}}
}

func (p *Provider) Configure(context.Context, string, map[string]any) error { return nil }

func (p *Provider) Metadata() gestalt.ProviderMetadata {
	return gestalt.ProviderMetadata{
		Kind:        gestalt.ProviderKindCache,
		Name:        "generated-cache",
		DisplayName: "Generated Cache",
	}
}

func (p *Provider) Get(_ context.Context, req *proto.CacheGetRequest) (*proto.CacheGetResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	value, ok := p.values[req.GetKey()]
	if !ok {
		return &proto.CacheGetResponse{}, nil
	}
	return &proto.CacheGetResponse{Found: true, Value: append([]byte(nil), value...)}, nil
}

func (p *Provider) GetMany(_ context.Context, req *proto.CacheGetManyRequest) (*proto.CacheGetManyResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	entries := make([]*proto.CacheResult, 0, len(req.GetKeys()))
	for _, key := range req.GetKeys() {
		entry := &proto.CacheResult{Key: key}
		if value, ok := p.values[key]; ok {
			entry.Found = true
			entry.Value = append([]byte(nil), value...)
		}
		entries = append(entries, entry)
	}
	return &proto.CacheGetManyResponse{Entries: entries}, nil
}

func (p *Provider) Set(_ context.Context, req *proto.CacheSetRequest) (*emptypb.Empty, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.values[req.GetKey()] = append([]byte(nil), req.GetValue()...)
	return &emptypb.Empty{}, nil
}

func (p *Provider) SetMany(_ context.Context, req *proto.CacheSetManyRequest) (*emptypb.Empty, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, entry := range req.GetEntries() {
		p.values[entry.GetKey()] = append([]byte(nil), entry.GetValue()...)
	}
	return &emptypb.Empty{}, nil
}

func (p *Provider) Delete(_ context.Context, req *proto.CacheDeleteRequest) (*proto.CacheDeleteResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	_, ok := p.values[req.GetKey()]
	delete(p.values, req.GetKey())
	return &proto.CacheDeleteResponse{Deleted: ok}, nil
}

func (p *Provider) DeleteMany(_ context.Context, req *proto.CacheDeleteManyRequest) (*proto.CacheDeleteManyResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	var deleted int64
	for _, key := range req.GetKeys() {
		if _, ok := p.values[key]; ok {
			delete(p.values, key)
			deleted++
		}
	}
	return &proto.CacheDeleteManyResponse{Deleted: deleted}, nil
}

func (p *Provider) Touch(_ context.Context, req *proto.CacheTouchRequest) (*proto.CacheTouchResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	_, ok := p.values[req.GetKey()]
	return &proto.CacheTouchResponse{Touched: ok}, nil
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
