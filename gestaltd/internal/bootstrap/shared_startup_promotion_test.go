package bootstrap

import (
	"context"
	"testing"

	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

func TestResolvePromoteSharedStateOnActivateDefaultsTrue(t *testing.T) {
	t.Setenv(promoteSharedStateOnActivateEnv, "")
	if !resolvePromoteSharedStateOnActivate(&config.Config{}) {
		t.Fatal("expected default promote-on-activate to be true")
	}
}

func TestResolvePromoteSharedStateOnActivateHonorsConfig(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{Server: config.ServerConfig{PromoteSharedStateOnActivate: boolPtr(false)}}
	if resolvePromoteSharedStateOnActivate(cfg) {
		t.Fatal("expected config false to disable promote-on-activate")
	}
}

func TestPendingAppSHAWriterFlushesDeferredWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := &coretesting.StubIndexedDB{}
	if _, err := coredata.New(db); err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	writer := newPendingAppSHAWriter(db, true)
	writer.Record("github", "sha-1")
	if err := writer.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	shas := readAppSHAs(ctx, db)
	if shas["github"] != "sha-1" {
		t.Fatalf("stored sha = %q, want sha-1", shas["github"])
	}
}
