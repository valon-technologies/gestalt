package metricutil_test

import (
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

func TestHTTPServerAttrsOmitsUnsetDimensions(t *testing.T) {
	t.Parallel()

	if attrs := metricutil.HTTPServerAttrs(metricutil.HTTPMetricDims{}); len(attrs) != 0 {
		t.Fatalf("expected no attrs for empty dims, got %+v", attrs)
	}
}

func TestAddHTTPServerLabelerAttrs(t *testing.T) {
	t.Parallel()

	labeler := &otelhttp.Labeler{}
	metricutil.AddHTTPServerLabelerAttrs(labeler, metricutil.HTTPMetricDims{
		SubjectLabel: "user@example.com",
	})
	attrs := labeler.Get()
	if len(attrs) != 1 {
		t.Fatalf("len(attrs) = %d, want 1", len(attrs))
	}
	if attrs[0] != attribute.String("gestaltd.subject.label", "user@example.com") {
		t.Fatalf("subject label attr = %+v", attrs[0])
	}

	metricutil.AddHTTPServerLabelerAttrs(labeler, metricutil.HTTPMetricDims{
		SubjectLabel: "other@example.com",
	})
	if len(labeler.Get()) != 1 {
		t.Fatalf("expected existing subject label attr to remain, got %+v", labeler.Get())
	}
}
