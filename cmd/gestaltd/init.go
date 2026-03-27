package main

import (
	"context"

	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/operator"
	"github.com/valon-technologies/gestalt/internal/provider"
	providercompiler "github.com/valon-technologies/gestalt/internal/provider/compiler"
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
	return operator.NewLifecycle(func(ctx context.Context, name string, api config.APIDef) (*provider.Definition, error) {
		return providercompiler.LoadDefinition(ctx, name, api, nil)
	})
}

func initConfig(configFlag string) error {
	configPath := resolveConfigPath(configFlag)
	_, err := initConfigAtPath(configPath)
	return err
}

func initConfigAtPath(configPath string) (*initLockfile, error) {
	return operatorLifecycle().InitAtPath(configPath)
}

func loadConfigForExecution(configFlag string, locked bool) (string, *config.Config, map[string]string, error) {
	configPath := resolveConfigPath(configFlag)
	cfg, preparedProviders, err := operatorLifecycle().LoadForExecutionAtPath(configPath, locked)
	if err != nil {
		return "", nil, nil, err
	}
	return configPath, cfg, preparedProviders, nil
}

func readLockfile(path string) (*initLockfile, error) {
	return operator.ReadLockfile(path)
}

func writeLockfile(path string, lock *initLockfile) error {
	return operator.WriteLockfile(path, lock)
}

func pluginFingerprint(name string, plugin *config.ExecutablePluginDef, configMap map[string]any) (string, error) {
	return operator.PluginFingerprint(name, plugin, configMap)
}

func lockPluginKey(kind, name string) string {
	return operator.LockPluginKey(kind, name)
}
