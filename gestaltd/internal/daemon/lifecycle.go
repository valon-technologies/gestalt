package daemon

import (
	"context"

	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/operator"
)

func operatorLifecycle() *operator.Lifecycle {
	return operator.NewLifecycle().WithConfigSecretResolver(func(ctx context.Context, cfg *config.Config) error {
		return bootstrap.ResolveConfigSecrets(ctx, cfg, buildFactories())
	}).WithSourceAuthSecretResolver(func(ctx context.Context, cfg *config.Config) error {
		return bootstrap.ResolveSourceAuthSecrets(ctx, cfg, buildFactories())
	})
}

func lockConfigWithStatePaths(configFlags []string, state operator.StatePaths, check bool) error {
	configPaths := operator.ResolveConfigPaths(configFlags)
	if check {
		return operatorLifecycle().CheckLockAtPathsWithStatePaths(configPaths, state)
	}
	_, err := operatorLifecycle().LockAtPathsWithStatePaths(configPaths, state)
	return err
}

func syncConfigWithStatePaths(configFlags []string, state operator.StatePaths, check bool) error {
	return syncConfigWithStatePathsOptions(configFlags, state, check, operator.SyncOptions{Parallelism: 1})
}

func syncConfigWithStatePathsOptions(configFlags []string, state operator.StatePaths, check bool, opts operator.SyncOptions) error {
	configPaths := operator.ResolveConfigPaths(configFlags)
	if check {
		return operatorLifecycle().CheckSyncAtPathsWithStatePathsOptions(configPaths, state, opts)
	}
	return operatorLifecycle().SyncAtPathsWithStatePathsOptions(configPaths, state, opts)
}

func loadConfigForExecutionAtPathsWithStatePaths(configPaths []string, state operator.StatePaths, locked bool, forcedDevUIKeys ...string) (*config.Config, error) {
	lc := operatorLifecycle().WithDevServeEligible(!locked)
	if len(forcedDevUIKeys) > 0 {
		lc = lc.WithForcedDevUIKeys(forcedDevUIKeys)
	}
	cfg, _, err := lc.LoadForExecutionAtPathsWithStatePaths(configPaths, state, locked)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func loadConfigForValidationWithStatePaths(configFlags []string, state operator.StatePaths, opts validateConfigOptions) ([]string, *config.Config, error) {
	configPaths := operator.ResolveConfigPaths(configFlags)
	var (
		cfg *config.Config
		err error
	)
	if opts.Runtime {
		cfg, err = operatorLifecycle().LoadForValidationAtPathsWithStatePaths(configPaths, state)
	} else {
		cfg, err = operatorLifecycle().LoadForStaticValidationAtPathsWithStatePaths(configPaths, state, operator.StaticValidationOptions{Platform: opts.Platform})
	}
	if err != nil {
		return nil, nil, err
	}
	return configPaths, cfg, nil
}
