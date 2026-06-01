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

func lockConfigWithStatePaths(configFlags []string, state operator.StatePaths, platformFlag string, check bool) error {
	configPaths := operator.ResolveConfigPaths(configFlags)
	if platformFlag == "" && check {
		return operatorLifecycle().CheckLockAtPathsWithStatePaths(configPaths, state, nil)
	}
	if platformFlag == "" {
		_, err := operatorLifecycle().LockAtPathsWithStatePaths(configPaths, state)
		return err
	}

	value, err := expandReleasePlatformValue(platformFlag)
	if err != nil {
		return err
	}
	platforms, err := parseReleasePlatforms(value)
	if err != nil {
		return err
	}

	platArgs := make([]operator.LockPlatform, len(platforms))
	for i, p := range platforms {
		platArgs[i] = operator.LockPlatform{GOOS: p.GOOS, GOARCH: p.GOARCH}
	}

	if check {
		return operatorLifecycle().CheckLockAtPathsWithStatePaths(configPaths, state, platArgs)
	}
	_, err = operatorLifecycle().LockAtPathsWithPlatforms(configPaths, state, platArgs)
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

func loadConfigForExecutionAtPathsWithStatePaths(configPaths []string, state operator.StatePaths, locked bool) (*config.Config, error) {
	cfg, _, err := operatorLifecycle().LoadForExecutionAtPathsWithStatePaths(configPaths, state, locked)
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
