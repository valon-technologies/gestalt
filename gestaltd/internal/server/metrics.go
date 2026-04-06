package server

import (
	"context"

	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	serverMeterName = "gestaltd"
)

var (
	serverAttrProvider = attribute.Key("gestalt.provider")
	serverAttrResult   = attribute.Key("gestalt.result")
)

func recordOAuthCallbackMetric(ctx context.Context, provider string, failed bool) {
	counter, err := otel.GetMeterProvider().Meter(serverMeterName).Int64Counter(
		"gestaltd.oauth.callback.count",
		metric.WithDescription("Counts integration OAuth callback outcomes."),
	)
	if err != nil {
		otel.Handle(err)
		return
	}

	counter.Add(ctx, 1, metric.WithAttributes(
		serverAttrProvider.String(metricutil.AttrValue(provider)),
		serverAttrResult.String(metricutil.ResultValue(failed)),
	))
}
