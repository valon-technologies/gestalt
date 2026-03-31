package main

import (
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/operator"
	github "github.com/valon-technologies/gestalt/server/internal/pluginsource/github"
)

const (
	initLockfileName     = operator.InitLockfileName
	preparedProvidersDir = operator.PreparedProvidersDir
	lockVersion          = operator.LockVersion
)

type initLockfile = operator.Lockfile
type lockProviderEntry = operator.LockProviderEntry
type lockPluginEntry = operator.LockPluginEntry

func operatorLifecycle() *operator.Lifecycle {
	return operator.NewLifecycle(&github.GitHubResolver{})
}

func initConfig(configFlag string) error {
	configPath := resolveConfigPath(configFlag)
	_, err := initConfigAtPath(configPath)
	return err
}

func initConfigAtPath(configPath string) (*initLockfile, error) {
	return operatorLifecycle().InitAtPath(configPath)
}

func loadConfigForExecution(configFlag string, locked bool) (string, *config.Config, error) {
	configPath := resolveConfigPath(configFlag)
	cfg, _, err := operatorLifecycle().LoadForExecutionAtPath(configPath, locked)
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

func pluginFingerprint(name string, plugin *config.PluginDef, configMap map[string]any, configDir string) (string, error) {
	return operator.PluginFingerprint(name, plugin, configMap, configDir)
}

func lockPluginKey(kind, name string) string {
	return operator.LockPluginKey(kind, name)
}
