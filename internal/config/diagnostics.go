package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type sqliteDatastoreYAML struct {
	Path string `yaml:"path"`
}

// DatastoreWarnings returns actionable warnings for risky datastore
// configurations without rejecting supported single-instance setups.
func DatastoreWarnings(cfg *Config, getenv func(string) string) []string {
	if cfg == nil || cfg.Datastore.Provider != "sqlite" {
		return nil
	}

	path := sqliteDatastorePath(cfg.Datastore.Config)
	var warnings []string

	if isTempSQLitePath(path) {
		warnings = append(warnings, fmt.Sprintf(
			"sqlite datastore path %q uses temporary storage; data will be lost on restart. Use a mounted persistent volume or switch to a shared datastore such as postgres.",
			path,
		))
	} else if runtime := sqliteEphemeralRuntime(getenv); runtime != "" {
		warnings = append(warnings, fmt.Sprintf(
			"sqlite datastore is not durable on %s; data will not survive instance replacement and is not shared across instances. Use a shared datastore such as postgres or mysql.",
			runtime,
		))
	} else if isLocalSQLitePath(path) {
		warnings = append(warnings, fmt.Sprintf(
			"sqlite datastore path %q uses local filesystem storage. This is appropriate for local development or single-instance deployments with mounted persistent storage.",
			path,
		))
	}

	if runtime := sqliteSingleInstanceRuntime(getenv); runtime != "" {
		warnings = append(warnings, fmt.Sprintf(
			"sqlite datastore is single-instance only. This deployment appears to run on %s; keep it at one replica and ensure %q is backed by persistent storage.",
			runtime,
			path,
		))
	}

	return warnings
}

func sqliteDatastorePath(node yaml.Node) string {
	var cfg sqliteDatastoreYAML
	if err := node.Decode(&cfg); err != nil || strings.TrimSpace(cfg.Path) == "" {
		return "./gestalt.db"
	}
	return cfg.Path
}

func isLocalSQLitePath(path string) bool {
	return !filepath.IsAbs(path)
}

func isTempSQLitePath(path string) bool {
	clean := filepath.Clean(path)
	tempPrefixes := []string{
		"/tmp",
		"/var/tmp",
		filepath.Clean(os.TempDir()),
	}
	for _, prefix := range tempPrefixes {
		if clean == prefix || strings.HasPrefix(clean, prefix+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func sqliteEphemeralRuntime(getenv func(string) string) string {
	if getenv == nil {
		return ""
	}
	switch {
	case getenv("K_SERVICE") != "":
		return "Cloud Run"
	case getenv("AWS_LAMBDA_FUNCTION_NAME") != "":
		return "AWS Lambda"
	default:
		return ""
	}
}

func sqliteSingleInstanceRuntime(getenv func(string) string) string {
	if getenv == nil {
		return ""
	}
	switch {
	case getenv("KUBERNETES_SERVICE_HOST") != "":
		return "Kubernetes"
	case getenv("RAILWAY_ENVIRONMENT") != "":
		return "Railway"
	case getenv("RENDER") != "":
		return "Render"
	case getenv("FLY_APP_NAME") != "":
		return "Fly.io"
	case getenv("ECS_CONTAINER_METADATA_URI_V4") != "":
		return "Amazon ECS"
	case getenv("ECS_CONTAINER_METADATA_URI") != "":
		return "Amazon ECS"
	default:
		return ""
	}
}
