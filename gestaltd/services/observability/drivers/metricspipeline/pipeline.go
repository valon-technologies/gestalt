package metricspipeline

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	prometheusexporter "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

type Result struct {
	MeterProvider *sdkmetric.MeterProvider
	Prometheus    http.Handler
}

func Build(res *resource.Resource, cfg Config, extraReaders ...sdkmetric.Reader) (*Result, error) {
	settings := cfg.normalize()

	readers := make([]sdkmetric.Reader, 0, len(extraReaders)+2)
	for _, reader := range extraReaders {
		if reader != nil {
			readers = append(readers, reader)
		}
	}

	var promHandler http.Handler
	if settings.prometheusEnabled {
		registry := prometheus.NewRegistry()
		exporter, err := prometheusexporter.New(
			prometheusexporter.WithRegisterer(registry),
			prometheusexporter.WithoutScopeInfo(),
		)
		if err != nil {
			return nil, fmt.Errorf("building prometheus exporter: %w", err)
		}
		readers = append(readers, exporter)
		promHandler = promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	}

	opts := []sdkmetric.Option{sdkmetric.WithResource(res)}
	for _, reader := range readers {
		opts = append(opts, sdkmetric.WithReader(reader))
	}

	return &Result{
		MeterProvider: sdkmetric.NewMeterProvider(opts...),
		Prometheus:    promHandler,
	}, nil
}
