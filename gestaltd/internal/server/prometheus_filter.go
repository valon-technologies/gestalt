package server

import (
	"math"
	"sort"
	"strconv"
	"strings"
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

func parsePrometheus(text string) []prometheusSample {
	samples := make([]prometheusSample, 0)
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		sample, ok := parsePrometheusLine(line)
		if ok {
			samples = append(samples, sample)
		}
	}
	return samples
}

func parsePrometheusLine(line string) (prometheusSample, bool) {
	nameEnd := 0
	for nameEnd < len(line) {
		c := line[nameEnd]
		if c == '{' || c == ' ' || c == '\t' {
			break
		}
		nameEnd++
	}
	if nameEnd == 0 {
		return prometheusSample{}, false
	}
	name := line[:nameEnd]
	rest := strings.TrimSpace(line[nameEnd:])
	labels := map[string]string{}
	if strings.HasPrefix(rest, "{") {
		end := strings.IndexByte(rest, '}')
		if end < 0 {
			return prometheusSample{}, false
		}
		labels = parsePrometheusLabels(rest[1:end])
		rest = strings.TrimSpace(rest[end+1:])
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return prometheusSample{}, false
	}
	value, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || math.IsNaN(value) {
		return prometheusSample{}, false
	}
	return prometheusSample{name: name, labels: labels, value: value}, true
}

func parsePrometheusLabels(raw string) map[string]string {
	labels := map[string]string{}
	rest := strings.TrimSpace(raw)
	for rest != "" {
		eq := strings.IndexByte(rest, '=')
		if eq <= 0 {
			break
		}
		key := strings.TrimSpace(rest[:eq])
		rest = strings.TrimSpace(rest[eq+1:])
		if !strings.HasPrefix(rest, `"`) {
			break
		}
		rest = rest[1:]
		var value strings.Builder
		escaped := false
		closed := false
		for i := 0; i < len(rest); i++ {
			c := rest[i]
			if escaped {
				value.WriteByte(c)
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				rest = strings.TrimSpace(strings.TrimPrefix(rest[i+1:], ","))
				closed = true
				break
			}
			value.WriteByte(c)
		}
		if !closed || key == "" {
			break
		}
		labels[key] = value.String()
	}
	return labels
}

func sampleProvider(sample prometheusSample) string {
	if value := strings.TrimSpace(sample.labels[promLabelProvider]); value != "" {
		return value
	}
	return strings.TrimSpace(sample.labels[promLabelProviderName])
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
