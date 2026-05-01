package telemetryutil

import (
	"os"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

const DefaultServiceName = "gestaltd"

func BuildResource(serviceName string, attrs map[string]string) *resource.Resource {
	if serviceName == "" {
		serviceName = DefaultServiceName
	}
	attrs = resourceAttrsWithDefaults(attrs)

	resourceAttrs := []attribute.KeyValue{
		semconv.ServiceName(serviceName),
	}
	for k, v := range attrs {
		resourceAttrs = append(resourceAttrs, attribute.String(k, v))
	}
	return resource.NewWithAttributes(semconv.SchemaURL, resourceAttrs...)
}

func resourceAttrsWithDefaults(attrs map[string]string) map[string]string {
	merged := make(map[string]string, len(attrs)+6)
	for k, v := range attrs {
		merged[k] = v
	}
	setDefault := func(key, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		if _, ok := merged[key]; ok {
			return
		}
		merged[key] = value
	}

	setDefault("service.version", firstEnv(
		"OTEL_SERVICE_VERSION",
		"GESTALT_SERVICE_VERSION",
		"SERVICE_VERSION",
	))
	setDefault("git.commit.sha", firstEnv(
		"GIT_COMMIT_SHA",
		"GITHUB_SHA",
		"COMMIT_SHA",
		"SOURCE_VERSION",
	))

	if service := strings.TrimSpace(os.Getenv("K_SERVICE")); service != "" {
		setDefault("cloud.provider", semconv.CloudProviderGCP.Value.AsString())
		setDefault("cloud.platform", semconv.CloudPlatformGCPCloudRun.Value.AsString())
		setDefault("faas.name", service)
		setDefault("faas.version", os.Getenv("K_REVISION"))
		setDefault("gcp.cloud_run.configuration.name", os.Getenv("K_CONFIGURATION"))
	}
	return merged
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}
