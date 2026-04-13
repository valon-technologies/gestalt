package main

import (
	"context"
	"os"

	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/operator"
	github "github.com/valon-technologies/gestalt/server/internal/pluginsource/github"
	sourceoverride "github.com/valon-technologies/gestalt/server/internal/pluginsource/override"
)

func operatorLifecycle() *operator.Lifecycle {
	sourceResolver := &sourceoverride.Resolver{
		Root: os.Getenv("GESTALT_SOURCE_OVERRIDE_DIR"),
		Next: &github.GitHubResolver{},
	}
	return operator.NewLifecycle(sourceResolver).WithConfigSecretResolver(func(ctx context.Context, cfg *config.Config) error {
		return bootstrap.ResolveConfigSecrets(ctx, cfg, buildFactories())
	})
}

func initConfigWithArtifactsDir(configFlag, artifactsDir, platformFlag string) error {
	configPath := resolveConfigPath(configFlag)
	if platformFlag == "" {
		_, err := operatorLifecycle().InitAtPathWithArtifactsDir(configPath, artifactsDir)
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

	_, err = operatorLifecycle().InitAtPathWithPlatforms(configPath, artifactsDir, platArgs)
	return err
}

func loadConfigForExecutionWithArtifactsDir(configFlag, artifactsDir string, locked bool) (string, *config.Config, error) {
	configPath := resolveConfigPath(configFlag)
	cfg, _, err := operatorLifecycle().LoadForExecutionAtPathWithArtifactsDir(configPath, artifactsDir, locked)
	if err != nil {
		return "", nil, err
	}
	return configPath, cfg, nil
}
