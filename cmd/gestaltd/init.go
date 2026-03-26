package main

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/operator"
	"github.com/valon-technologies/gestalt/internal/provider"
)

const (
	initLockfileName     = operator.InitLockfileName
	preparedProvidersDir = operator.PreparedProvidersDir
	lockVersion          = operator.LockVersion
)

type initLockfile = operator.Lockfile
type lockProviderEntry = operator.LockProviderEntry
type lockPluginEntry = operator.LockPluginEntry

func runInit(args []string) error {
	fs := flag.NewFlagSet("gestaltd init", flag.ContinueOnError)
	fs.Usage = func() { printInitUsage(fs.Output()) }
	configPath := fs.String("config", "", "path to config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	return initConfig(*configPath)
}

func operatorLifecycle() *operator.Lifecycle {
	return operator.NewLifecycle(func(ctx context.Context, name string, upstream config.UpstreamDef) (*provider.Definition, error) {
		return loadAPIUpstream(ctx, name, upstream, nil)
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
