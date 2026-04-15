package main

import (
	"context"

	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/operator"
	github "github.com/valon-technologies/gestalt/server/internal/pluginsource/github"
)

func operatorLifecycle() *operator.Lifecycle {
	return operator.NewLifecycle(&github.GitHubResolver{}).WithConfigSecretResolver(func(ctx context.Context, cfg *config.Config) error {
		return bootstrap.ResolveConfigSecrets(ctx, cfg, buildFactories())
	})
}

func initConfigWithArtifactsDir(configFlags []string, artifactsDir, platformFlag string) error {
	configPaths := resolveConfigPaths(configFlags)
	if platformFlag == "" {
		_, err := operatorLifecycle().InitAtPathsWithArtifactsDir(configPaths, artifactsDir)
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

	platArgs := make([]struct{ GOOS, GOARCH, LibC string }, len(platforms))
	for i, p := range platforms {
		platArgs[i] = struct{ GOOS, GOARCH, LibC string }{p.GOOS, p.GOARCH, ""}
	}

	_, err = operatorLifecycle().InitAtPathsWithPlatforms(configPaths, artifactsDir, platArgs)
	return err
}

func loadConfigForExecutionWithArtifactsDir(configFlags []string, artifactsDir string, locked bool) ([]string, *config.Config, error) {
	configPaths := resolveConfigPaths(configFlags)
	cfg, _, err := operatorLifecycle().LoadForExecutionAtPathsWithArtifactsDir(configPaths, artifactsDir, locked)
	if err != nil {
		return nil, nil, err
	}
	return configPaths, cfg, nil
}
