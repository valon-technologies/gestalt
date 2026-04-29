package main

import (
	"context"

	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/operator"
)

func operatorLifecycle() *operator.Lifecycle {
	return operator.NewLifecycle().WithConfigSecretResolver(func(ctx context.Context, cfg *config.Config) error {
		return bootstrap.ResolveConfigSecrets(ctx, cfg, buildFactories())
	})
}

func initConfigWithStatePaths(configFlags []string, state operator.StatePaths, platformFlag string) error {
	configPaths := operator.ResolveConfigPaths(configFlags)
	if platformFlag == "" {
		_, err := operatorLifecycle().InitAtPathsWithStatePaths(configPaths, state)
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

	platArgs := make([]struct{ GOOS, GOARCH string }, len(platforms))
	for i, p := range platforms {
		platArgs[i] = struct{ GOOS, GOARCH string }{p.GOOS, p.GOARCH}
	}

	_, err = operatorLifecycle().InitAtPathsWithPlatforms(configPaths, state, platArgs)
	return err
}

func loadConfigForExecutionAtPathsWithStatePaths(configPaths []string, state operator.StatePaths, locked bool) (*config.Config, error) {
	cfg, _, err := operatorLifecycle().LoadForExecutionAtPathsWithStatePaths(configPaths, state, locked)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func loadConfigForValidationWithStatePaths(configFlags []string, state operator.StatePaths) ([]string, *config.Config, error) {
	configPaths := operator.ResolveConfigPaths(configFlags)
	cfg, err := operatorLifecycle().LoadForValidationAtPathsWithStatePaths(configPaths, state)
	if err != nil {
		return nil, nil, err
	}
	return configPaths, cfg, nil
}
