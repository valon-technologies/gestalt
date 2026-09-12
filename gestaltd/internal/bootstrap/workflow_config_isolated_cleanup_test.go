package bootstrap

import (
	"context"
	"testing"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
)

func seedOverlayOnlyPlan11Definition(provider *fakeWorkflowProvider) {
	provider.definitions = map[string]*coreworkflow.Definition{
		"cfg_plan11_temporal_controlled_probe": {
			ID:           "cfg_plan11_temporal_controlled_probe",
			Generation:   1,
			CreatedBy:    workflowConfigOwnerSubjectID(),
			ProviderName: "temporal",
		},
	}
}

func TestIsolatedPreparationPreservesOverlayOnlyDefinition(t *testing.T) {
	t.Parallel()

	cfg, runtime, provider, decls := testWorkflowReconcileEnv(t)
	seedOverlayOnlyPlan11Definition(provider)

	// HTTP A / isolated-prep image: local desired set lacks overlay-only probe key.
	if err := reconcileWorkflowConfigDefinitions(
		context.Background(),
		cfg,
		runtime,
		decls,
		nil,
		workflowConfigReconcileOptions{allowDestructiveCleanup: false},
	); err != nil {
		t.Fatalf("isolated reconcile: %v", err)
	}
	if len(provider.deletedDefinitions) != 0 {
		t.Fatalf("deleted = %#v, want none under isolated preparation", provider.deletedDefinitions)
	}
	if provider.definitions["cfg_plan11_temporal_controlled_probe"] == nil {
		t.Fatal("overlay-only cfg_plan11_temporal_controlled_probe was deleted")
	}
	if provider.definitions["cfg_backup"] == nil {
		t.Fatal("expected local cfg_backup to be applied during isolated reconcile")
	}
}

func TestOrdinaryModeRemovesAbsentConfigManagedDefinitions(t *testing.T) {
	t.Parallel()

	cfg, runtime, provider, decls := testWorkflowReconcileEnv(t)
	seedOverlayOnlyPlan11Definition(provider)

	if err := reconcileWorkflowConfigDefinitions(
		context.Background(),
		cfg,
		runtime,
		decls,
		nil,
		workflowConfigReconcileOptions{allowDestructiveCleanup: true},
	); err != nil {
		t.Fatalf("ordinary reconcile: %v", err)
	}
	if provider.definitions["cfg_plan11_temporal_controlled_probe"] != nil {
		t.Fatal("overlay-only cfg_plan11_temporal_controlled_probe should be removed in ordinary mode")
	}
}

func TestPromoteTemporalWorkersPreservesOverlayOnlyDefinitionWhenDeferred(t *testing.T) {
	t.Parallel()

	cfg, runtime, provider, decls := testWorkflowReconcileEnv(t)
	seedOverlayOnlyPlan11Definition(provider)
	promotable := &promotableTestWorkflowProvider{}

	reconcileOpts := workflowConfigReconcileOptions{allowDestructiveCleanup: false}
	result := &Result{
		deferSharedStartupWrites: true,
		ExtraWorkflows:           []coreworkflow.Provider{promotable},
		startupWorkflowConfigReconcile: func(ctx context.Context) error {
			return reconcileWorkflowConfigDefinitions(ctx, cfg, runtime, decls, nil, reconcileOpts)
		},
	}

	if err := result.PromoteTemporalWorkers(context.Background()); err != nil {
		t.Fatalf("PromoteTemporalWorkers: %v", err)
	}
	if promotable.promoteCalls != 1 {
		t.Fatalf("promoteCalls = %d, want 1", promotable.promoteCalls)
	}
	if !result.TemporalWorkersPromoted() {
		t.Fatal("expected deferred promotion bookkeeping to complete")
	}
	if len(provider.deletedDefinitions) != 0 {
		t.Fatalf("deleted = %#v, want none after explicit promotion with deferred writes", provider.deletedDefinitions)
	}
	if provider.definitions["cfg_plan11_temporal_controlled_probe"] == nil {
		t.Fatal("overlay-only cfg_plan11_temporal_controlled_probe was deleted during explicit promotion")
	}
}

func TestBootstrapWiresNonDestructiveCleanupForIsolatedPreparation(t *testing.T) {
	t.Parallel()

	deferSharedStartupWrites := !resolvePromoteSharedStateOnActivate(&config.Config{
		Server: config.ServerConfig{PromoteSharedStateOnActivate: boolPtr(false)},
	})
	opts := workflowConfigReconcileOptions{allowDestructiveCleanup: !deferSharedStartupWrites}
	if opts.allowDestructiveCleanup {
		t.Fatal("expected isolated preparation to disable destructive cfg_* cleanup")
	}

	deferSharedStartupWrites = !resolvePromoteSharedStateOnActivate(&config.Config{})
	opts = workflowConfigReconcileOptions{allowDestructiveCleanup: !deferSharedStartupWrites}
	if !opts.allowDestructiveCleanup {
		t.Fatal("expected ordinary mode to keep destructive cfg_* cleanup enabled")
	}
}
