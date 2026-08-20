package server

import (
	"fmt"
	"math"
	"sort"
	"strings"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

const (
	promLabelProvider         = "gestalt_provider"
	promLabelProviderName     = "gestaltd_provider_name"
	promLabelOperation        = "gestalt_operation"
	promMetricOpCount         = "gestaltd_operation_count_total"
	promMetricOpErrors        = "gestaltd_operation_error_count_total"
	promMetricOpDurationSum   = "gestaltd_operation_duration_seconds_sum"
	promMetricOpDurationCount = "gestaltd_operation_duration_seconds_count"
)

type prometheusSample struct {
	name   string
	labels map[string]string
	value  float64
}

type appAdminOperationMetric struct {
	Operation            string  `json:"operation"`
	Requests             float64 `json:"requests"`
	Errors               float64 `json:"errors"`
	DurationSecondsSum   float64 `json:"durationSecondsSum"`
	DurationSecondsCount float64 `json:"durationSecondsCount"`
}

type appAdminMetricsResponse struct {
	App                  string                    `json:"app"`
	Available            bool                      `json:"available"`
	Requests             float64                   `json:"requests"`
	Errors               float64                   `json:"errors"`
	DurationSecondsSum   float64                   `json:"durationSecondsSum"`
	DurationSecondsCount float64                   `json:"durationSecondsCount"`
	Operations           []appAdminOperationMetric `json:"operations"`
}

func parsePrometheus(text string) ([]prometheusSample, error) {
	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(strings.NewReader(text))
	if err != nil {
		return nil, fmt.Errorf("parse prometheus text: %w", err)
	}
	samples := make([]prometheusSample, 0)
	for _, family := range families {
		if family == nil {
			continue
		}
		samples = append(samples, prometheusSamplesFromFamily(family)...)
	}
	return samples, nil
}

func prometheusSamplesFromFamily(family *dto.MetricFamily) []prometheusSample {
	name := family.GetName()
	samples := make([]prometheusSample, 0, len(family.GetMetric()))
	for _, metric := range family.GetMetric() {
		if metric == nil {
			continue
		}
		labels := prometheusLabelsFromMetric(metric)
		switch family.GetType() {
		case dto.MetricType_HISTOGRAM, dto.MetricType_GAUGE_HISTOGRAM:
			hist := metric.GetHistogram()
			samples = append(samples,
				prometheusSample{name: name + "_sum", labels: labels, value: hist.GetSampleSum()},
				prometheusSample{name: name + "_count", labels: labels, value: float64(hist.GetSampleCount())},
			)
		case dto.MetricType_SUMMARY:
			summary := metric.GetSummary()
			samples = append(samples,
				prometheusSample{name: name + "_sum", labels: labels, value: summary.GetSampleSum()},
				prometheusSample{name: name + "_count", labels: labels, value: float64(summary.GetSampleCount())},
			)
		default:
			value, ok := prometheusScalarValue(metric)
			if !ok {
				continue
			}
			samples = append(samples, prometheusSample{name: name, labels: labels, value: value})
		}
	}
	return samples
}

func prometheusLabelsFromMetric(metric *dto.Metric) map[string]string {
	labels := make(map[string]string, len(metric.GetLabel()))
	for _, pair := range metric.GetLabel() {
		if pair == nil {
			continue
		}
		key := strings.TrimSpace(pair.GetName())
		if key == "" {
			continue
		}
		labels[key] = pair.GetValue()
	}
	return labels
}

func prometheusScalarValue(metric *dto.Metric) (float64, bool) {
	switch {
	case metric.Counter != nil:
		return metric.GetCounter().GetValue(), true
	case metric.Gauge != nil:
		return metric.GetGauge().GetValue(), true
	case metric.Untyped != nil:
		return metric.GetUntyped().GetValue(), true
	default:
		return 0, false
	}
}

func sampleProvider(sample prometheusSample) string {
	if value := strings.TrimSpace(sample.labels[promLabelProvider]); value != "" {
		return value
	}
	return strings.TrimSpace(sample.labels[promLabelProviderName])
}

func isAppOperationMetric(name string) bool {
	switch name {
	case promMetricOpCount, promMetricOpErrors, promMetricOpDurationSum, promMetricOpDurationCount:
		return true
	default:
		return false
	}
}

func samplesForProvider(samples []prometheusSample, provider string) []prometheusSample {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return nil
	}
	out := make([]prometheusSample, 0)
	for _, sample := range samples {
		if sampleProvider(sample) == provider {
			out = append(out, sample)
		}
	}
	return out
}

func summarizeAppMetrics(app string, samples []prometheusSample) appAdminMetricsResponse {
	byOp := map[string]*appAdminOperationMetric{}
	response := appAdminMetricsResponse{
		App:       app,
		Available: true,
	}
	for _, sample := range samples {
		if math.IsNaN(sample.value) || !isAppOperationMetric(sample.name) {
			continue
		}
		operation := strings.TrimSpace(sample.labels[promLabelOperation])
		if operation == "" {
			operation = "unknown"
		}
		row := byOp[operation]
		if row == nil {
			row = &appAdminOperationMetric{Operation: operation}
			byOp[operation] = row
		}
		switch sample.name {
		case promMetricOpCount:
			row.Requests += sample.value
			response.Requests += sample.value
		case promMetricOpErrors:
			row.Errors += sample.value
			response.Errors += sample.value
		case promMetricOpDurationSum:
			row.DurationSecondsSum += sample.value
			response.DurationSecondsSum += sample.value
		case promMetricOpDurationCount:
			row.DurationSecondsCount += sample.value
			response.DurationSecondsCount += sample.value
		}
	}
	names := make([]string, 0, len(byOp))
	for name := range byOp {
		names = append(names, name)
	}
	sort.Strings(names)
	response.Operations = make([]appAdminOperationMetric, 0, len(names))
	for _, name := range names {
		response.Operations = append(response.Operations, *byOp[name])
	}
	return response
}
