package override

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/pluginsource"
)

func TestResolve_UsesLocalOverrideWhenPresent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	localPath := filepath.Join(root, "valon-technologies", "gestalt-providers", "plugins", "httpbin")
	if err := os.MkdirAll(localPath, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(localPath, ManifestFile), []byte("kind: plugin\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(manifest): %v", err)
	}

	resolver := &Resolver{
		Root: root,
		Next: &stubResolver{
			t: t,
		},
	}
	src, err := pluginsource.Parse("github.com/valon-technologies/gestalt-providers/plugins/httpbin")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	pkg, err := resolver.Resolve(context.Background(), src, "0.0.1-alpha.1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if pkg == nil {
		t.Fatal("Resolve returned nil package")
	}
	if pkg.LocalPath != localPath {
		t.Fatalf("LocalPath = %q, want %q", pkg.LocalPath, localPath)
	}
	if pkg.Cleanup == nil {
		t.Fatal("Cleanup = nil, want no-op function")
	}
}

func TestResolve_FallsBackWhenOverrideMissing(t *testing.T) {
	t.Parallel()

	next := &stubResolver{
		t: t,
		result: &pluginsource.ResolvedPackage{
			LocalPath: "/tmp/fallback",
			Cleanup:   func() {},
		},
	}
	resolver := &Resolver{
		Root: t.TempDir(),
		Next: next,
	}
	src, err := pluginsource.Parse("github.com/valon-technologies/gestalt-providers/plugins/httpbin")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	pkg, err := resolver.Resolve(context.Background(), src, "0.0.1-alpha.1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !next.called {
		t.Fatal("fallback resolver was not called")
	}
	if pkg == nil || pkg.LocalPath != "/tmp/fallback" {
		t.Fatalf("pkg = %#v, want fallback package", pkg)
	}
}

func TestResolve_ReturnsFallbackErrorWithoutOverride(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("boom")
	resolver := &Resolver{
		Root: t.TempDir(),
		Next: &stubResolver{
			t:   t,
			err: wantErr,
		},
	}
	src, err := pluginsource.Parse("github.com/valon-technologies/gestalt-providers/plugins/httpbin")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	_, err = resolver.Resolve(context.Background(), src, "0.0.1-alpha.1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("Resolve error = %v, want %v", err, wantErr)
	}
}

func TestListPlatformArchives_ReturnsEmptyForLocalOverride(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	localPath := filepath.Join(root, "valon-technologies", "gestalt-providers", "plugins", "httpbin")
	if err := os.MkdirAll(localPath, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(localPath, ManifestFile), []byte("kind: plugin\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(manifest): %v", err)
	}

	next := &stubEnumerator{
		stubResolver: stubResolver{t: t},
	}
	resolver := &Resolver{
		Root: root,
		Next: next,
	}
	src, err := pluginsource.Parse("github.com/valon-technologies/gestalt-providers/plugins/httpbin")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	archives, err := resolver.ListPlatformArchives(context.Background(), src, "0.0.1-alpha.1")
	if err != nil {
		t.Fatalf("ListPlatformArchives: %v", err)
	}
	if next.listCalled {
		t.Fatal("fallback platform enumerator was called")
	}
	if len(archives) != 0 {
		t.Fatalf("archives = %#v, want empty", archives)
	}
}

type stubResolver struct {
	t      *testing.T
	called bool
	result *pluginsource.ResolvedPackage
	err    error
}

func (s *stubResolver) Resolve(_ context.Context, _ pluginsource.Source, _ string) (*pluginsource.ResolvedPackage, error) {
	s.called = true
	return s.result, s.err
}

type stubEnumerator struct {
	stubResolver
	listCalled bool
	result     []pluginsource.PlatformArchive
	err        error
}

func (s *stubEnumerator) ListPlatformArchives(_ context.Context, _ pluginsource.Source, _ string) ([]pluginsource.PlatformArchive, error) {
	s.listCalled = true
	return s.result, s.err
}
