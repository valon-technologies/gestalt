package bootstrap

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"

	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
)

func entryWithArtifactSHA(sha string) *config.ProviderEntry {
	return &config.ProviderEntry{
		ResolvedManifest: &providermanifestv1.Manifest{
			Artifacts: []providermanifestv1.Artifact{
				{OS: runtime.GOOS, Arch: runtime.GOARCH, SHA256: sha},
			},
		},
	}
}

func TestCurrentAppSHA(t *testing.T) {
	t.Parallel()

	if got := currentAppSHA(entryWithArtifactSHA("abc123")); got != "abc123" {
		t.Fatalf("currentAppSHA = %q, want abc123", got)
	}
	if got := currentAppSHA(&config.ProviderEntry{}); got != "" {
		t.Fatalf("currentAppSHA with no manifest = %q, want empty", got)
	}
	if got := currentAppSHA(nil); got != "" {
		t.Fatalf("currentAppSHA(nil) = %q, want empty", got)
	}
}

func TestAppStartupCategorizer(t *testing.T) {
	t.Parallel()

	stored := map[string]string{"match": "sha-1"}

	cases := []struct {
		name         string
		autoActivate bool
		app          string
		entry        *config.ProviderEntry
		want         AppStartupCategory
	}{
		{name: "matching sha starts immediately", app: "match", entry: entryWithArtifactSHA("sha-1"), want: AppStartupNOOP},
		{name: "changed sha is deferred", app: "match", entry: entryWithArtifactSHA("sha-2"), want: AppStartupUpdate},
		{name: "no stored sha is deferred", app: "fresh", entry: entryWithArtifactSHA("sha-1"), want: AppStartupUpdate},
		{name: "no current sha is deferred", app: "match", entry: &config.ProviderEntry{}, want: AppStartupUpdate},
		{name: "autoActivate forces immediate", autoActivate: true, app: "match", entry: entryWithArtifactSHA("sha-2"), want: AppStartupNOOP},
		{name: "autoActivate immediate for path source", autoActivate: true, app: "fresh", entry: &config.ProviderEntry{}, want: AppStartupNOOP},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			categorize := newAppStartupCategorizer(stored, tc.autoActivate)
			if got := categorize(tc.app, tc.entry); got != tc.want {
				t.Fatalf("category = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestResolveAutoActivate(t *testing.T) {
	boolPtr := func(b bool) *bool { return &b }

	t.Run("explicit true wins over K_REVISION", func(t *testing.T) {
		t.Setenv("K_REVISION", "rev-123")
		cfg := &config.Config{}
		cfg.Server.AutoActivate = boolPtr(true)
		if !resolveAutoActivate(cfg) {
			t.Fatal("explicit autoActivate=true should win")
		}
	})
	t.Run("explicit false wins with no K_REVISION", func(t *testing.T) {
		t.Setenv("K_REVISION", "")
		cfg := &config.Config{}
		cfg.Server.AutoActivate = boolPtr(false)
		if resolveAutoActivate(cfg) {
			t.Fatal("explicit autoActivate=false should win")
		}
	})
	t.Run("defaults true without K_REVISION", func(t *testing.T) {
		t.Setenv("K_REVISION", "")
		if !resolveAutoActivate(&config.Config{}) {
			t.Fatal("should default to true when K_REVISION unset")
		}
	})
	t.Run("defaults false with K_REVISION", func(t *testing.T) {
		t.Setenv("K_REVISION", "rev-123")
		if resolveAutoActivate(&config.Config{}) {
			t.Fatal("should default to false when K_REVISION set")
		}
	})
}

func TestReadWriteAppSHAs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db := &coretesting.StubIndexedDB{}
	if _, err := coredata.New(db); err != nil {
		t.Fatalf("coredata.New: %v", err)
	}

	if got := readAppSHAs(ctx, db); len(got) != 0 {
		t.Fatalf("expected empty SHAs initially, got %v", got)
	}

	if err := writeAppSHA(ctx, db, "github", "sha-gh"); err != nil {
		t.Fatalf("writeAppSHA: %v", err)
	}
	if err := writeAppSHA(ctx, db, "slack", "sha-sl"); err != nil {
		t.Fatalf("writeAppSHA: %v", err)
	}

	got := readAppSHAs(ctx, db)
	if got["github"] != "sha-gh" || got["slack"] != "sha-sl" {
		t.Fatalf("readAppSHAs = %v, want github=sha-gh slack=sha-sl", got)
	}

	if err := writeAppSHA(ctx, db, "github", "sha-gh-2"); err != nil {
		t.Fatalf("writeAppSHA overwrite: %v", err)
	}
	if got := readAppSHAs(ctx, db); got["github"] != "sha-gh-2" {
		t.Fatalf("readAppSHAs after overwrite = %q, want sha-gh-2", got["github"])
	}
}

func TestReadAppSHAsNilDB(t *testing.T) {
	t.Parallel()
	if got := readAppSHAs(context.Background(), nil); len(got) != 0 {
		t.Fatalf("readAppSHAs(nil) = %v, want empty", got)
	}
}

func TestStartWritesSHAOnlyOnSuccessfulInstall(t *testing.T) {
	t.Parallel()

	reg := registry.New()
	t.Cleanup(func() { _ = CloseProviders(&reg.Providers) })

	builds := &preparedProviderBuilds{
		providers:      &reg.Providers,
		connAuth:       map[string]map[string]OAuthHandler{},
		manualConnAuth: map[string]map[string]ManualTokenExchanger{},
		pending: []pendingProviderBuild{
			{name: "ok", entry: &config.ProviderEntry{}, sha: "sha-ok"},
			{name: "fails", entry: &config.ProviderEntry{}, sha: "sha-fails"},
			{name: "no-sha", entry: &config.ProviderEntry{}},
		},
	}

	var mu sync.Mutex
	written := map[string]string{}
	builds.onInstalled = func(name, sha string) {
		mu.Lock()
		defer mu.Unlock()
		written[name] = sha
	}

	builder := func(_ context.Context, name string, _ *config.ProviderEntry, _ Deps) (*ProviderBuildResult, error) {
		if name == "fails" {
			return nil, errors.New("boom")
		}
		return &ProviderBuildResult{Provider: &coretesting.StubIntegration{N: name}}, nil
	}

	ready, _, _, _ := builds.Start(context.Background(), Deps{}, builder)
	<-ready

	mu.Lock()
	defer mu.Unlock()
	if got, ok := written["ok"]; !ok || got != "sha-ok" {
		t.Fatalf("expected ok -> sha-ok written, got %v", written)
	}
	if _, ok := written["fails"]; ok {
		t.Fatal("failed install must not persist its SHA")
	}
	if _, ok := written["no-sha"]; ok {
		t.Fatal("provider without an artifact SHA must not persist an empty SHA")
	}
}
