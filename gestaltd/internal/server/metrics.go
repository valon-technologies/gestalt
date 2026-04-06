package server

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	serverMeterName        = "gestaltd"
	unknownMetricAttrValue = "unknown"
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
		serverAttrProvider.String(metricAttrValue(provider)),
		serverAttrResult.String(metricResult(failed)),
	))
}

func metricAttrValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return unknownMetricAttrValue
	}
	return value
}

func metricResult(failed bool) string {
	if failed {
		return "error"
	}
	return "success"
}
