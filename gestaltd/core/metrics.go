package core

import (
	"context"
	"time"
)

// OperationMetricsReader serves compact operation-metric snapshots for the web
// UI.
type OperationMetricsReader interface {
	Overview(context.Context) (*OperationMetricsOverview, error)
}

type OperationMetricRecord struct {
	RecordedAt time.Time
	Provider   string
	Operation  string
	Duration   time.Duration
	Failed     bool
}

type OperationMetricsRecorder interface {
	RecordOperationMetric(OperationMetricRecord)
}

type OperationMetrics interface {
	OperationMetricsReader
	OperationMetricsRecorder
}

type OperationMetricsOverview struct {
	Enabled       bool                         `json:"enabled"`
	Reason        string                       `json:"reason,omitempty"`
	GeneratedAt   time.Time                    `json:"generated_at,omitempty"`
	WindowSeconds int64                        `json:"window_seconds,omitempty"`
	BucketSeconds int64                        `json:"bucket_seconds,omitempty"`
	Summary       OperationMetricsSummary      `json:"summary"`
	Series        []OperationMetricsSeriesItem `json:"series,omitempty"`
	Providers     []OperationMetricsBreakdown  `json:"providers,omitempty"`
	Operations    []OperationMetricsBreakdown  `json:"operations,omitempty"`
}

type OperationMetricsSummary struct {
	Requests      int64   `json:"requests"`
	Errors        int64   `json:"errors"`
	ErrorRate     float64 `json:"error_rate"`
	AvgLatencyMs  float64 `json:"avg_latency_ms"`
	P95LatencyMs  float64 `json:"p95_latency_ms"`
	ThroughputRPS float64 `json:"throughput_rps"`
}

type OperationMetricsSeriesItem struct {
	Start         time.Time `json:"start"`
	Requests      int64     `json:"requests"`
	Errors        int64     `json:"errors"`
	ErrorRate     float64   `json:"error_rate"`
	P95LatencyMs  float64   `json:"p95_latency_ms"`
	ThroughputRPS float64   `json:"throughput_rps"`
}

type OperationMetricsBreakdown struct {
	Provider      string  `json:"provider"`
	Operation     string  `json:"operation,omitempty"`
	Requests      int64   `json:"requests"`
	Errors        int64   `json:"errors"`
	ErrorRate     float64 `json:"error_rate"`
	AvgLatencyMs  float64 `json:"avg_latency_ms"`
	P95LatencyMs  float64 `json:"p95_latency_ms"`
	ThroughputRPS float64 `json:"throughput_rps"`
}
