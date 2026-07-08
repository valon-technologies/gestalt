package daemon

import (
	"context"

	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/operator"
)

func operatorLifecycle() *operator.Lifecycle {
	return operator.NewLifecycle().
		WithConfigSecretResolver(func(ctx context.Context, cfg *config.Config) error {
			return bootstrap.ResolveConfigSecrets(ctx, cfg, buildFactories())
		}).WithSourceAuthSecretResolver(func(ctx context.Context, cfg *config.Config) error {
		return bootstrap.ResolveSourceAuthSecrets(ctx, cfg, buildFactories())
	})
}

func lockConfig(configFlags []string, lockfilePath, artifactsDir string, check bool) error {
	configPaths := operator.ResolveConfigPaths(configFlags)
	if check {
		return operatorLifecycle().CheckLockAtPaths(configPaths, lockfilePath, artifactsDir)
	}
	_, err := operatorLifecycle().LockAtPaths(configPaths, lockfilePath, artifactsDir)
	return err
}

func syncConfig(configFlags []string, lockfilePath, artifactsDir string, check bool) error {
	return syncConfigOptions(configFlags, lockfilePath, artifactsDir, check, operator.SyncOptions{Parallelism: 1})
}

func syncConfigOptions(configFlags []string, lockfilePath, artifactsDir string, check bool, opts operator.SyncOptions) error {
	configPaths := operator.ResolveConfigPaths(configFlags)
	if check {
		return operatorLifecycle().CheckSyncAtPathsOptions(configPaths, lockfilePath, artifactsDir, opts)
	}
	return operatorLifecycle().SyncAtPathsOptions(configPaths, lockfilePath, artifactsDir, opts)
}

func loadConfigForExecutionAtPaths(configPaths []string, lockfilePath, artifactsDir string, locked, noSync bool, forcedDevAppKeys []string) (*config.Config, error) {
	lc := operatorLifecycle().WithDevServeEligible(!locked)
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
		cfg, err = operatorLifecycle().LoadForValidationAtPaths(configPaths, lockfilePath, artifactsDir)
	} else {
		cfg, err = operatorLifecycle().LoadForStaticValidationAtPaths(configPaths, lockfilePath, artifactsDir, operator.StaticValidationOptions{Platform: opts.Platform})
	}
	if err != nil {
		return nil, nil, err
	}
	return configPaths, cfg, nil
}
