package metricutil_test

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
)

func TestHTTPServerAttrsIngressAndClientKinds(t *testing.T) {
	t.Parallel()

	attrs := metricutil.HTTPServerAttrs(metricutil.HTTPMetricDims{
		IngressKind:  metricutil.IngressKindAppInvokeV1,
		ClientKind:   metricutil.ClientKindCLI,
		ClientApp:    metricutil.ClientAppUnknown,
		SubjectLabel: metricutil.SubjectLabelUnknown,
	})
	if len(attrs) != 4 {
		t.Fatalf("len(attrs) = %d, want 4", len(attrs))
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
	if attrs[3] != attribute.String("gestaltd.subject.label", metricutil.SubjectLabelUnknown) {
		t.Fatalf("subject label attr = %+v", attrs[3])
	}
}

func TestHTTPServerAttrsOmitsIngressRedundantDimsWhenIngressKindPresent(t *testing.T) {
	t.Parallel()

	labeler := &otelhttp.Labeler{}
	labeler.Add(attribute.String("gestaltd.ingress.kind", metricutil.IngressKindAppInvokeV1))
	ctx := otelhttp.ContextWithLabeler(context.Background(), labeler)

	metricutil.AddHTTPServerMetricDims(ctx, metricutil.HTTPMetricDims{
		ProviderName:   "sample",
		OperationName:  "list",
		Transport:      "rest",
		ConnectionMode: "none",
		Surface:        metricutil.InvocationSurfaceHTTP,
	})

	attrs := labeler.Get()
	for _, attr := range attrs {
		switch string(attr.Key) {
		case "gestaltd.operation.transport", "gestaltd.connection.mode", "gestaltd.invocation.surface":
			t.Fatalf("ingress request should omit %s, got %+v", attr.Key, attr)
		}
	}
	if !containsAttr(attrs, "gestaltd.provider.name", "sample") {
		t.Fatalf("expected provider attr, got %+v", attrs)
	}
}

func containsAttr(attrs []attribute.KeyValue, key, value string) bool {
	for _, attr := range attrs {
		if string(attr.Key) == key && attr.Value.AsString() == value {
			return true
		}
	}
	return false
}

func TestHTTPServerAttrsOmitsUnsetDimensions(t *testing.T) {
	t.Parallel()

	if attrs := metricutil.HTTPServerAttrs(metricutil.HTTPMetricDims{}); len(attrs) != 0 {
		t.Fatalf("expected no attrs for empty dims, got %+v", attrs)
	}
}
