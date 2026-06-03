package daemon

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/valon-technologies/gestalt/server/internal/operator"
)

const (
	syncOutputFormatText = "text"
	syncOutputFormatJSON = "json"
)

type syncOutputDocument struct {
	Schema  syncOutputSchema `json:"schema"`
	Command string           `json:"command"`
	operator.SyncMetrics
}

type syncOutputSchema struct {
	Version string `json:"version"`
}

func validateSyncOutputFormat(format string) error {
	switch format {
	case syncOutputFormatText, syncOutputFormatJSON:
		return nil
	default:
		return fmt.Errorf("invalid --output-format %q; expected %q or %q", format, syncOutputFormatText, syncOutputFormatJSON)
	}
}

func writeSyncJSON(w io.Writer, metrics operator.SyncMetrics) error {
	metrics = normalizeSyncMetricsForJSON(metrics)
	doc := syncOutputDocument{
		Schema:      syncOutputSchema{Version: "2"},
		Command:     "sync",
		SyncMetrics: metrics,
	}
	encoder := json.NewEncoder(w)
	return encoder.Encode(doc)
}

func normalizeSyncMetricsForJSON(metrics operator.SyncMetrics) operator.SyncMetrics {
	if metrics.Archives.Fetches == nil {
		metrics.Archives.Fetches = []operator.SyncMetricsArchiveFetch{}
	}
	if metrics.Cache.Entries == nil {
		metrics.Cache.Entries = []operator.SyncMetricsCacheEntry{}
	}
	if metrics.Artifacts.Items == nil {
		metrics.Artifacts.Items = []operator.SyncMetricsArtifactRecord{}
	}
	if metrics.Output.Roots == nil {
		metrics.Output.Roots = []operator.SyncMetricsOutputRoot{}
	}
	return metrics
}

func writeSyncText(w io.Writer, metrics operator.SyncMetrics, verbose bool) error {
	action := "Prepared"
	if metrics.Sync.Action == "check" {
		action = "Checked"
	}
	if _, err := fmt.Fprintf(w, "Loaded %d prepared artifacts from lock/config.\n", metrics.Artifacts.Considered); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Fetched %d archives: %d downloads.\n", metrics.Archives.Requests, metrics.Archives.Downloads.Count); err != nil {
		return err
	}
	if verbose {
		cacheState := "disabled"
		if metrics.Cache.Enabled {
			cacheState = "enabled"
		}
		if metrics.Cache.Configured {
			if _, err := fmt.Fprintf(w, "Cache: %s materialized cache at %s.\n", cacheState, metrics.Cache.Dir); err != nil {
				return err
			}
		} else {
			if _, err := fmt.Fprintln(w, "Cache: disabled."); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "Cache: %d eligible, %d disabled, %d uncacheable, %d hits, %d misses, %d invalid, %d put successes, %d put failures.\n",
			metrics.Cache.Eligible,
			metrics.Cache.Disabled,
			metrics.Cache.Uncacheable,
			metrics.Cache.Hits,
			metrics.Cache.Misses,
			metrics.Cache.Invalid,
			metrics.Cache.Put.Successes,
			metrics.Cache.Put.Failures,
		); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "Downloads: %d archives, %s, cumulative download time %.3fs.\n", metrics.Archives.Downloads.Count, formatIECBytes(metrics.Archives.Downloads.Bytes), metrics.Archives.Downloads.DurationSeconds); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "Phases: load %.3fs, materialize %.3fs, validate %.3fs, output measure %.3fs.\n", metrics.Phases.LoadSeconds, metrics.Phases.MaterializeSeconds, metrics.Phases.ValidateSeconds, metrics.Phases.OutputMeasureSeconds); err != nil {
			return err
		}
		if metrics.Output.Measured {
			if _, err := fmt.Fprintf(w, "Prepared output: %d files, %s.\n", metrics.Output.Files, formatIECBytes(metrics.Output.Bytes)); err != nil {
				return err
			}
		} else {
			if _, err := fmt.Fprintln(w, "Prepared output: not measured."); err != nil {
				return err
			}
		}
		if len(metrics.Archives.Fetches) > 0 {
			if _, err := fmt.Fprintln(w, "Archive fetches:"); err != nil {
				return err
			}
			for _, fetch := range metrics.Archives.Fetches {
				if _, err := fmt.Fprintf(w, "  %s: downloaded=%t, %s, %.3fs\n", fetch.Subject, fetch.Downloaded, formatIECBytes(fetch.Bytes), fetch.DurationSeconds); err != nil {
					return err
				}
			}
		}
		if len(metrics.Cache.Entries) > 0 {
			if _, err := fmt.Fprintln(w, "Cache entries:"); err != nil {
				return err
			}
			for i := range metrics.Cache.Entries {
				entry := &metrics.Cache.Entries[i]
				if _, err := fmt.Fprintf(w, "  %s: %s, put=%s, %d files, %s, %.3fs\n", entry.Subject, entry.Result, entry.Put, entry.Files, formatIECBytes(entry.Bytes), entry.DurationSeconds); err != nil {
					return err
				}
			}
		}
		if len(metrics.Artifacts.Items) > 0 {
			if _, err := fmt.Fprintln(w, "Artifacts:"); err != nil {
				return err
			}
			for i := range metrics.Artifacts.Items {
				artifact := &metrics.Artifacts.Items[i]
				if _, err := fmt.Fprintf(w, "  %s: %s %s, reason=%s, prepare %.3fs, activate %.3fs, total %.3fs\n", artifact.Subject, artifact.SourceKind, artifact.Result, artifact.Reason, artifact.PrepareDurationSeconds, artifact.ActivateDurationSeconds, artifact.DurationSeconds); err != nil {
					return err
				}
			}
		}
		if len(metrics.Output.Roots) > 0 {
			if _, err := fmt.Fprintln(w, "Prepared output roots:"); err != nil {
				return err
			}
			for _, root := range metrics.Output.Roots {
				if _, err := fmt.Fprintf(w, "  %s: %d files, %s, %s\n", root.Subject, root.Files, formatIECBytes(root.Bytes), root.RelativePath); err != nil {
					return err
				}
			}
		}
	}
	_, err := fmt.Fprintf(w, "%s artifacts in %s in %.3fs.\n", action, metrics.Sync.ArtifactsDir, metrics.Sync.DurationSeconds)
	return err
}

func formatIECBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div := int64(unit)
	exp := 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
