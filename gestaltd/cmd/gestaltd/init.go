package main

import (
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/operator"
	github "github.com/valon-technologies/gestalt/server/internal/pluginsource/github"
)

const (
	initLockfileName = operator.InitLockfileName
	lockVersion      = operator.LockVersion
)

type initLockfile = operator.Lockfile
type lockProviderEntry = operator.LockProviderEntry
type lockPluginEntry = operator.LockPluginEntry

func operatorLifecycle() *operator.Lifecycle {
	return operator.NewLifecycle(&github.GitHubResolver{})
}

func initConfig(configFlag string) error {
	return initConfigWithArtifactsDir(configFlag, "")
}

func initConfigWithArtifactsDir(configFlag, artifactsDir string) error {
	configPath := resolveConfigPath(configFlag)
	_, err := operatorLifecycle().InitAtPathWithArtifactsDir(configPath, artifactsDir)
	return err
}

func loadConfigForExecution(configFlag string, locked bool) (string, *config.Config, error) {
	return loadConfigForExecutionWithArtifactsDir(configFlag, "", locked)
}

func loadConfigForExecutionWithArtifactsDir(configFlag, artifactsDir string, locked bool) (string, *config.Config, error) {
	configPath := resolveConfigPath(configFlag)
	cfg, _, err := operatorLifecycle().LoadForExecutionAtPathWithArtifactsDir(configPath, artifactsDir, locked)
	if err != nil {
		return "", nil, err
	}
	return configPath, cfg, nil
}

func readLockfile(path string) (*initLockfile, error) {
	return operator.ReadLockfile(path)
}

func writeLockfile(path string, lock *initLockfile) error {
	return operator.WriteLockfile(path, lock)
}

func pluginFingerprint(name string, plugin *config.PluginDef, configDir string) (string, error) {
	return operator.PluginFingerprint(name, plugin, configDir)
}

func lockPluginKey(kind, name string) string {
	return operator.LockPluginKey(kind, name)
}
