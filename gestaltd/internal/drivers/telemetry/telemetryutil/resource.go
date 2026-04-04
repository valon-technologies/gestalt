package telemetryutil

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

const DefaultServiceName = "gestaltd"

func BuildResource(serviceName string, attrs map[string]string) *resource.Resource {
	if serviceName == "" {
		serviceName = DefaultServiceName
	}

	resourceAttrs := []attribute.KeyValue{
		semconv.ServiceName(serviceName),
	}
	for k, v := range attrs {
		resourceAttrs = append(resourceAttrs, attribute.String(k, v))
	}
	return resource.NewWithAttributes(semconv.SchemaURL, resourceAttrs...)
}
