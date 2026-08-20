package server

import (
	"testing"
)

func TestParsePrometheusAndSummarizeByProvider(t *testing.T) {
	t.Parallel()

	text := `
# HELP gestaltd_operation_count_total Operation invocations
# TYPE gestaltd_operation_count_total counter
gestaltd_operation_count_total{gestalt_provider="slack",gestalt_operation="post"} 3
gestaltd_operation_count_total{gestalt_provider="httpbin",gestalt_operation="get"} 9
gestaltd_operation_error_count_total{gestalt_provider="slack",gestalt_operation="post"} 1
gestaltd_operation_duration_seconds_sum{gestalt_provider="slack",gestalt_operation="post"} 0.4
gestaltd_operation_duration_seconds_count{gestalt_provider="slack",gestalt_operation="post"} 3
go_goroutines 12
`
	parsed, err := parsePrometheus(text)
	if err != nil {
		t.Fatalf("parsePrometheus: %v", err)
	}
	samples := samplesForProvider(parsed, "slack")
	got := summarizeAppMetrics("slack", samples)
	if !got.Available || got.App != "slack" {
		t.Fatalf("summary = %#v", got)
	}
	if got.Requests != 3 || got.Errors != 1 {
		t.Fatalf("totals = %#v", got)
	}
	if got.DurationSecondsSum != 0.4 || got.DurationSecondsCount != 3 {
		t.Fatalf("duration = %#v", got)
	}
	if len(got.Operations) != 1 || got.Operations[0].Operation != "post" {
		t.Fatalf("operations = %#v", got.Operations)
	}
	if got.Operations[0].Requests != 3 || got.Operations[0].Errors != 1 {
		t.Fatalf("operation row = %#v", got.Operations[0])
	}
}

func TestSamplesForProviderUsesGestaltdProviderName(t *testing.T) {
	t.Parallel()

	text := `gestaltd_operation_count_total{gestaltd_provider_name="ai-spend-tracker",gestalt_operation="list"} 2
`
	parsed, err := parsePrometheus(text)
	if err != nil {
		t.Fatalf("parsePrometheus: %v", err)
	}
	samples := samplesForProvider(parsed, "ai-spend-tracker")
	if len(samples) != 1 {
		t.Fatalf("samples = %#v", samples)
	}
}

func TestSummarizeAppMetricsEmpty(t *testing.T) {
	t.Parallel()

	got := summarizeAppMetrics("quiet-app", nil)
	if !got.Available || got.App != "quiet-app" || len(got.Operations) != 0 || got.Requests != 0 {
		t.Fatalf("empty summary = %#v", got)
	}
}

func TestSummarizeAppMetricsIgnoresNonOperationSeries(t *testing.T) {
	t.Parallel()

	text := `
gestaltd_operation_count_total{gestalt_provider="slack",gestalt_operation="post"} 3
http_server_request_duration_seconds_count{gestalt_provider="slack",gestalt_operation="get",code="200"} 40
go_goroutines{gestalt_provider="slack"} 12
`
	parsed, err := parsePrometheus(text)
	if err != nil {
		t.Fatalf("parsePrometheus: %v", err)
	}
	got := summarizeAppMetrics("slack", samplesForProvider(parsed, "slack"))
	if got.Requests != 3 || got.Errors != 0 {
		t.Fatalf("totals = %#v", got)
	}
	if len(got.Operations) != 1 || got.Operations[0].Operation != "post" || got.Operations[0].Requests != 3 {
		t.Fatalf("operations = %#v", got.Operations)
	}
}

func TestParsePrometheusHistogramSumAndCount(t *testing.T) {
	t.Parallel()

	text := `
# TYPE gestaltd_operation_duration_seconds histogram
gestaltd_operation_duration_seconds_bucket{gestalt_provider="slack",gestalt_operation="post",le="0.1"} 1
gestaltd_operation_duration_seconds_bucket{gestalt_provider="slack",gestalt_operation="post",le="+Inf"} 3
gestaltd_operation_duration_seconds_sum{gestalt_provider="slack",gestalt_operation="post"} 0.4
gestaltd_operation_duration_seconds_count{gestalt_provider="slack",gestalt_operation="post"} 3
`
	parsed, err := parsePrometheus(text)
	if err != nil {
		t.Fatalf("parsePrometheus: %v", err)
	}
	got := summarizeAppMetrics("slack", samplesForProvider(parsed, "slack"))
	if got.DurationSecondsSum != 0.4 || got.DurationSecondsCount != 3 {
		t.Fatalf("duration = %#v", got)
	}
}

func TestParsePrometheusEscapedProviderLabel(t *testing.T) {
	t.Parallel()

	text := `gestaltd_operation_count_total{gestalt_provider="app\"quoted",gestalt_operation="list"} 2
`
	parsed, err := parsePrometheus(text)
	if err != nil {
		t.Fatalf("parsePrometheus: %v", err)
	}
	samples := samplesForProvider(parsed, `app"quoted`)
	if len(samples) != 1 || samples[0].value != 2 {
		t.Fatalf("samples = %#v", samples)
	}
}

func TestParsePrometheusRejectsMalformedText(t *testing.T) {
	t.Parallel()

	_, err := parsePrometheus("not a metric line {")
	if err == nil {
		t.Fatal("expected parse error")
	}
}
