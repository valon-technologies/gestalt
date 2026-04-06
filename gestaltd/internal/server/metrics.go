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
	connectMethodOAuth     = "oauth"
	connectMethodManual    = "manual"
)

var (
	serverAttrProvider   = attribute.Key("gestalt.provider")
	serverAttrResult     = attribute.Key("gestalt.result")
	serverAttrAuthMethod = attribute.Key("gestalt.auth_method")
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

func recordIntegrationConnectMetric(ctx context.Context, provider, method string) {
	counter, err := otel.GetMeterProvider().Meter(serverMeterName).Int64Counter(
		"gestaltd.integration.connect.count",
		metric.WithDescription("Counts successful integration connection completions."),
	)
	if err != nil {
		otel.Handle(err)
		return
	}

	counter.Add(ctx, 1, metric.WithAttributes(
		serverAttrProvider.String(metricAttrValue(provider)),
		serverAttrAuthMethod.String(metricAttrValue(method)),
		serverAttrResult.String("success"),
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
