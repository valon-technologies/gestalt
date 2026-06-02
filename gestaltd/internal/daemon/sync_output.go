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
	doc := syncOutputDocument{
		Schema:      syncOutputSchema{Version: "1"},
		Command:     "sync",
		SyncMetrics: metrics,
	}
	encoder := json.NewEncoder(w)
	return encoder.Encode(doc)
}

func writeSyncText(w io.Writer, metrics operator.SyncMetrics, verbose bool) error {
	action := "Prepared"
	if metrics.Sync.Action == "check" {
		action = "Checked"
	}
	if _, err := fmt.Fprintf(w, "Loaded %d prepared artifacts from lock/config.\n", metrics.Artifacts.Considered); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Fetched %d archives: %d cache hits, %d downloads.\n", metrics.Archives.Requests, metrics.Archives.Cache.Hits, metrics.Archives.Downloads.Count); err != nil {
		return err
	}
	if verbose {
		cacheState := "disabled"
		if metrics.Archives.Cache.Enabled {
			cacheState = "enabled"
		}
		if metrics.Archives.Cache.Configured {
			if _, err := fmt.Fprintf(w, "Archive cache: %s at %s.\n", cacheState, metrics.Archives.Cache.Dir); err != nil {
				return err
			}
		} else {
			if _, err := fmt.Fprintln(w, "Archive cache: disabled."); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "Archive cache: %d eligible, %d disabled, %d uncacheable, %d hits, %d misses, %d invalid, %d rejected, %d puts, %d put failures.\n",
			metrics.Archives.Cache.Eligible,
			metrics.Archives.Cache.Disabled,
			metrics.Archives.Cache.Uncacheable,
			metrics.Archives.Cache.Hits,
			metrics.Archives.Cache.Misses,
			metrics.Archives.Cache.Invalid,
			metrics.Archives.Cache.Rejected,
			metrics.Archives.Cache.Puts,
			metrics.Archives.Cache.PutFailures,
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
		if len(metrics.Archives.SlowestFetches) > 0 {
			if _, err := fmt.Fprintln(w, "Slowest archive fetches:"); err != nil {
				return err
			}
			for _, fetch := range metrics.Archives.SlowestFetches {
				if _, err := fmt.Fprintf(w, "  %s: %s, downloaded=%t, %s, %.3fs\n", fetch.Subject, fetch.CacheResult, fetch.Downloaded, formatIECBytes(fetch.Bytes), fetch.DurationSeconds); err != nil {
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
