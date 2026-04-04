package main

import (
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/operator"
	github "github.com/valon-technologies/gestalt/server/internal/pluginsource/github"
)

func operatorLifecycle() *operator.Lifecycle {
	return operator.NewLifecycle(&github.GitHubResolver{})
}

func initConfigWithArtifactsDir(configFlag, artifactsDir string) error {
	configPath := resolveConfigPath(configFlag)
	_, err := operatorLifecycle().InitAtPathWithArtifactsDir(configPath, artifactsDir)
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
