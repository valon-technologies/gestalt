package main

import "github.com/valon-technologies/gestalt/server/internal/operator"

func defaultLocalConfigPath() string {
	return operator.DefaultLocalConfigPath()
}

func localConfigPaths() []string {
	return operator.LocalConfigPaths()
}

func generateDefaultConfig(configDir string) (string, error) {
	return operator.GenerateDefaultConfig(configDir)
}
