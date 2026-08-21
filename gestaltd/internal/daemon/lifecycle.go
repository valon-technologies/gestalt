package daemon

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/operator"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

func operatorLifecycle() *operator.Lifecycle {
	return operator.NewLifecycle().
		WithConfigSecretResolver(func(ctx context.Context, cfg *config.Config) error {
			return bootstrap.ResolveConfigSecrets(ctx, cfg, buildFactories())
		}).WithSourceAuthSecretResolver(func(ctx context.Context, cfg *config.Config) error {
		return bootstrap.ResolveSourceAuthSecrets(ctx, cfg, buildFactories())
	})
}

func operatorLifecycleWithCLIProgress(showSourceOutput bool) *operator.Lifecycle {
	reporter := currentCLIReporter()
	return operatorLifecycle().
		WithSourceCommandOutput(sourceCommandOutput(reporter, showSourceOutput)).
		WithProgress(newLifecycleProgressReporter(reporter))
}

func operatorLifecycleWithCLIOutput() *operator.Lifecycle {
	reporter := currentCLIReporter()
	return operatorLifecycle().WithSourceCommandOutput(sourceCommandOutput(reporter, false))
}

func sourceCommandOutput(reporter *TerminalReporter, showSuccess bool) providerpkg.CommandOutput {
	output := reporter.ChildOutput()
	if (showSuccess || reporter.policy.Verbose) && !reporter.policy.Quiet {
		return providerpkg.CommandOutput{Stdout: output, Stderr: output}
	}
	return providerpkg.CommandOutputCaptureOnFailure(output)
}

func newLifecycleProgressReporter(reporter *TerminalReporter) operator.LifecycleProgress {
	if reporter == nil {
		return nil
	}
	var activity *TerminalActivity
	return func(event operator.LifecycleProgressEvent) {
		switch event.Status {
		case operator.LifecycleProgressStarted:
			activity = reporter.Start(formatLifecycleProgressStart(event))
		case operator.LifecycleProgressCompleted, operator.LifecycleProgressNoop:
			message := formatLifecycleProgressComplete(event)
			if activity != nil {
				activity.Finish(message)
				activity = nil
				return
			}
			reporter.Status(message)
		}
	}
}

func formatLifecycleProgressStart(event operator.LifecycleProgressEvent) string {
	switch event.Phase {
	case operator.LifecyclePhaseConfig:
		return "Loading configuration"
	case operator.LifecyclePhaseLock:
		return "Resolving lockfile"
	case operator.LifecyclePhaseInstall:
		if event.Operation == operator.LifecycleOperationServe {
			return "Preparing artifacts for serve"
		}
		return "Preparing artifacts"
	default:
		return "Preparing lifecycle"
	}
}

func formatLifecycleProgressComplete(event operator.LifecycleProgressEvent) string {
	switch {
	case event.Status == operator.LifecycleProgressNoop && event.Operation == operator.LifecycleOperationLock && event.Phase == operator.LifecyclePhaseLock && event.Reason == "not_required":
		return "Lockfile not required"
	case event.Status == operator.LifecycleProgressNoop:
		return "No artifact changes needed"
	case event.Operation == operator.LifecycleOperationLock && event.Phase == operator.LifecyclePhaseLock && event.Reason == "written":
		return fmt.Sprintf("Lockfile written to %s", event.Path)
	case event.Operation == operator.LifecycleOperationLock && event.Phase == operator.LifecyclePhaseLock && event.Reason == "current":
		return fmt.Sprintf("Lockfile is up to date: %s", event.Path)
	case event.Operation == operator.LifecycleOperationLock && event.Phase == operator.LifecyclePhaseLock:
		return "Lockfile ready"
	case event.Operation == operator.LifecycleOperationSync && event.Phase == operator.LifecyclePhaseComplete:
		return "Artifacts ready"
	case event.Operation == operator.LifecycleOperationServe && event.Phase == operator.LifecyclePhaseComplete:
		return "Artifacts ready for serve"
	case event.Phase == operator.LifecyclePhaseConfig:
		return "Configuration loaded"
	case event.Phase == operator.LifecyclePhaseLock:
		return "Lockfile resolved"
	case event.Phase == operator.LifecyclePhaseInstall:
		return "Artifacts prepared"
	default:
		return "Lifecycle step complete"
	}
}

func lockConfig(configFlags []string, lockfilePath, artifactsDir string, check bool) error {
	configPaths := operator.ResolveConfigPaths(configFlags)
	if check {
		return operatorLifecycleWithCLIProgress(false).CheckLockAtPaths(configPaths, lockfilePath, artifactsDir)
	}
	_, err := operatorLifecycleWithCLIProgress(false).LockAtPaths(configPaths, lockfilePath, artifactsDir)
	return err
}

func syncConfig(configFlags []string, lockfilePath, artifactsDir string, check bool) error {
	return syncConfigOptions(configFlags, lockfilePath, artifactsDir, check, operator.SyncOptions{Parallelism: 1}, false)
}

func syncConfigOptions(configFlags []string, lockfilePath, artifactsDir string, check bool, opts operator.SyncOptions, showSourceOutput bool) error {
	configPaths := operator.ResolveConfigPaths(configFlags)
	lc := operatorLifecycleWithCLIProgress(showSourceOutput)
	if check {
		return lc.CheckSyncAtPathsOptions(configPaths, lockfilePath, artifactsDir, opts)
	}
	return lc.SyncAtPathsOptions(configPaths, lockfilePath, artifactsDir, opts)
}

func loadConfigForExecutionAtPaths(configPaths []string, lockfilePath, artifactsDir string, locked, noSync bool, remotePreview bool, forcedDevAppKeys ...string) (*config.Config, error) {
	lc := operatorLifecycleWithCLIProgress(false).WithDevServeEligible(!locked)
	if remotePreview {
		lc = lc.WithRemotePreviewServe(true)
	}
	if len(forcedDevAppKeys) > 0 {
		lc = lc.WithForcedDevAppKeys(forcedDevAppKeys)
	}
	cfg, _, err := lc.LoadForExecutionAtPaths(configPaths, lockfilePath, artifactsDir, locked, noSync)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func loadConfigForValidation(configFlags []string, lockfilePath, artifactsDir string, opts validateConfigOptions) ([]string, *config.Config, error) {
	configPaths := operator.ResolveConfigPaths(configFlags)
	var (
		cfg *config.Config
		err error
	)
	if opts.Runtime {
		cfg, err = operatorLifecycleWithCLIOutput().LoadForValidationAtPaths(configPaths, lockfilePath, artifactsDir)
	} else {
		cfg, err = operatorLifecycleWithCLIOutput().LoadForStaticValidationAtPaths(configPaths, lockfilePath, artifactsDir, operator.StaticValidationOptions{Platform: opts.Platform})
	}
	if err != nil {
		return nil, nil, err
	}
	return configPaths, cfg, nil
}
