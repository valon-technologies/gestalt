package bootstrap

import (
	"context"
	"errors"
	"testing"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
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

type promotableTestWorkflowProvider struct {
	startupTestWorkflowProvider
	promoteCalls int
	promoteErr   error
}

func (p *promotableTestWorkflowProvider) PromoteWorkers(context.Context) error {
	p.promoteCalls++
	return p.promoteErr
}

func TestPromoteWorkflowProvidersInvokesPromotableProviders(t *testing.T) {
	t.Parallel()

	provider := &promotableTestWorkflowProvider{}
	if err := promoteWorkflowProviders(context.Background(), []coreworkflow.Provider{provider}); err != nil {
		t.Fatalf("promoteWorkflowProviders: %v", err)
	}
	if provider.promoteCalls != 1 {
		t.Fatalf("promoteCalls = %d, want 1", provider.promoteCalls)
	}
}

func TestPromoteWorkflowProvidersFailsWhenNoProviderHandlesPromotion(t *testing.T) {
	t.Parallel()

	err := promoteWorkflowProviders(context.Background(), []coreworkflow.Provider{&startupTestWorkflowProvider{}})
	if err == nil {
		t.Fatal("expected explicit promotion to fail when provider is not promotable")
	}
}

func TestPromoteWorkflowProvidersPropagatesProviderError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("set worker deployment current version: boom")
	provider := &promotableTestWorkflowProvider{
		promoteErr: wantErr,
	}
	err := promoteWorkflowProviders(context.Background(), []coreworkflow.Provider{provider})
	if !errors.Is(err, wantErr) {
		t.Fatalf("promoteWorkflowProviders error = %v, want %v", err, wantErr)
	}
}

func TestWorkflowCleanupWrapperForwardsPromoteWorkers(t *testing.T) {
	t.Parallel()

	inner := &promotableTestWorkflowProvider{}
	wrapped := &workflowProviderWithCleanup{Provider: inner}
	if err := promoteWorkflowProviders(context.Background(), []coreworkflow.Provider{wrapped}); err != nil {
		t.Fatalf("promoteWorkflowProviders: %v", err)
	}
	if inner.promoteCalls != 1 {
		t.Fatalf("inner promoteCalls = %d, want 1", inner.promoteCalls)
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
