package metricutil_test

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"go.opentelemetry.io/otel/attribute"
)

func TestHTTPServerAttrsIngressAndClientKinds(t *testing.T) {
	t.Parallel()

	attrs := metricutil.HTTPServerAttrs(metricutil.HTTPMetricDims{
		IngressKind: metricutil.IngressKindAppInvokeV1,
		ClientKind:  metricutil.ClientKindCLI,
		ClientApp:   metricutil.ClientAppUnknown,
	})
	if len(attrs) != 3 {
		t.Fatalf("len(attrs) = %d, want 3", len(attrs))
	}
	if attrs[0] != attribute.String("gestaltd.ingress.kind", metricutil.IngressKindAppInvokeV1) {
		t.Fatalf("ingress attr = %+v", attrs[0])
	}
	if attrs[1] != attribute.String("gestaltd.client.kind", metricutil.ClientKindCLI) {
		t.Fatalf("client attr = %+v", attrs[1])
	}
	if attrs[2] != attribute.String("gestaltd.client.app", metricutil.ClientAppUnknown) {
		t.Fatalf("client app attr = %+v", attrs[2])
	}
}

func TestHTTPServerAttrsOmitsUnsetDimensions(t *testing.T) {
	t.Parallel()

	if attrs := metricutil.HTTPServerAttrs(metricutil.HTTPMetricDims{}); len(attrs) != 0 {
		t.Fatalf("expected no attrs for empty dims, got %+v", attrs)
	}
}
