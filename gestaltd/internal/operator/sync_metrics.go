package operator

import (
	"cmp"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	syncActionMaterialize = "materialize"
	syncActionCheck       = "check"

	syncArchiveSourceRemoteReleaseMetadata = "remote_release_metadata"
	syncArchiveSourceLocal                 = "local"

	syncCacheResultHit         = "hit"
	syncCacheResultMiss        = "miss"
	syncCacheResultInvalid     = "invalid"
	syncCacheResultDisabled    = "disabled"
	syncCacheResultUncacheable = "uncacheable"
	syncCachePutSuccess        = "success"
	syncCachePutFailure        = "failure"
	syncCachePutSkipped        = "skipped"

	syncArtifactSourceRemoteArchive = "remote_archive"
	syncArtifactSourceLocalArchive  = "local_archive"
	syncArtifactSourceLocalSource   = "local_source"
	syncArtifactSourceGitSource     = "git_source"
	syncArtifactSourcePathBacked    = "path_backed"

	syncArtifactResultReused       = "reused"
	syncArtifactResultMaterialized = "materialized"
	syncArtifactResultPathBacked   = "path_backed"

	syncArtifactReasonFresh             = "fresh"
	syncArtifactReasonPreparedMissing   = "prepared_missing"
	syncArtifactReasonMetadataMissing   = "metadata_missing"
	syncArtifactReasonMetadataStale     = "metadata_stale"
	syncArtifactReasonFingerprintStale  = "fingerprint_stale"
	syncArtifactReasonManifestStale     = "manifest_stale"
	syncArtifactReasonAssetRootMissing  = "asset_root_missing"
	syncArtifactReasonExecutableMissing = "executable_missing"
	syncArtifactReasonSourcePathBacked  = "source_path_backed"
)

type SyncMetrics struct {
	Sync      SyncMetricsSync      `json:"sync"`
	Inputs    SyncMetricsInputs    `json:"inputs"`
	Artifacts SyncMetricsArtifacts `json:"artifacts"`
	Cache     SyncMetricsCache     `json:"cache"`
	Archives  SyncMetricsArchives  `json:"archives"`
	Phases    SyncMetricsPhases    `json:"phases"`
	Output    SyncMetricsOutput    `json:"output"`
}

type SyncMetricsSync struct {
	Action          string  `json:"action"`
	DurationSeconds float64 `json:"duration_seconds"`
	ArtifactsDir    string  `json:"artifacts_dir"`
	LockfilePath    string  `json:"lockfile_path"`
}

type SyncMetricsInputs struct {
	ConfigPaths []string `json:"config_paths"`
	Locked      bool     `json:"locked"`
	Check       bool     `json:"check"`
	Parallelism int      `json:"parallelism"`
}

type SyncMetricsArtifacts struct {
	Considered int                         `json:"considered"`
	Items      []SyncMetricsArtifactRecord `json:"items"`
}

type SyncMetricsArtifactRecord struct {
	Subject                 string  `json:"subject"`
	Kind                    string  `json:"kind"`
	Name                    string  `json:"name"`
	SourceKind              string  `json:"source_kind"`
	Result                  string  `json:"result"`
	Reason                  string  `json:"reason,omitempty"`
	RelativePath            string  `json:"relative_path,omitempty"`
	DurationSeconds         float64 `json:"duration_seconds"`
	PrepareDurationSeconds  float64 `json:"prepare_duration_seconds,omitempty"`
	ActivateDurationSeconds float64 `json:"activate_duration_seconds,omitempty"`
}

type SyncMetricsArchives struct {
	Requests     int                       `json:"requests"`
	UniqueSHA256 int                       `json:"unique_sha256"`
	Downloads    SyncMetricsDownloads      `json:"downloads"`
	Fetches      []SyncMetricsArchiveFetch `json:"fetches"`
}

type SyncMetricsCache struct {
	Configured    bool                    `json:"configured"`
	Enabled       bool                    `json:"enabled"`
	Dir           string                  `json:"dir"`
	Mode          string                  `json:"mode,omitempty"`
	BucketVersion string                  `json:"bucket_version,omitempty"`
	Eligible      int                     `json:"eligible"`
	Disabled      int                     `json:"disabled"`
	Uncacheable   int                     `json:"uncacheable"`
	Hits          int                     `json:"hits"`
	Misses        int                     `json:"misses"`
	Invalid       int                     `json:"invalid"`
	Restore       SyncMetricsCacheRestore `json:"restore"`
	Put           SyncMetricsCachePut     `json:"put"`
	Entries       []SyncMetricsCacheEntry `json:"entries"`
}

type SyncMetricsCacheRestore struct {
	DurationSeconds float64 `json:"duration_seconds"`
	ListSeconds     float64 `json:"list_seconds"`
	Entries         int     `json:"entries"`
	Restored        int     `json:"restored"`
	Failures        int     `json:"failures"`
	Bytes           int64   `json:"bytes"`
	Error           string  `json:"error,omitempty"`
}

type SyncMetricsCachePut struct {
	Successes             int     `json:"successes"`
	Failures              int     `json:"failures"`
	LocalInspectSeconds   float64 `json:"local_inspect_seconds"`
	LocalWriteSeconds     float64 `json:"local_write_seconds"`
	RemoteExistsSeconds   float64 `json:"remote_exists_seconds"`
	RemoteArchiveSeconds  float64 `json:"remote_archive_seconds"`
	RemoteUploadSeconds   float64 `json:"remote_upload_seconds"`
	RemoteSkippedExisting int     `json:"remote_skipped_existing"`
}

type SyncMetricsCacheEntry struct {
	Subject         string  `json:"subject"`
	SourceKind      string  `json:"source_kind"`
	Key             string  `json:"key,omitempty"`
	SHA256          string  `json:"archive_sha256,omitempty"`
	Platform        string  `json:"platform,omitempty"`
	Result          string  `json:"result"`
	Put             string  `json:"put"`
	Bytes           int64   `json:"bytes,omitempty"`
	Files           int     `json:"files,omitempty"`
	DurationSeconds float64 `json:"duration_seconds"`
}

type SyncMetricsDownloads struct {
	Count           int     `json:"count"`
	Bytes           int64   `json:"bytes"`
	DurationSeconds float64 `json:"duration_seconds"`
}

type SyncMetricsArchiveFetch struct {
	Subject         string  `json:"subject"`
	SourceKind      string  `json:"source_kind"`
	SHA256          string  `json:"sha256,omitempty"`
	Downloaded      bool    `json:"downloaded"`
	Bytes           int64   `json:"bytes"`
	DurationSeconds float64 `json:"duration_seconds"`
}

type SyncMetricsPhases struct {
	LoadSeconds          float64 `json:"load_seconds"`
	MaterializeSeconds   float64 `json:"materialize_seconds"`
	ValidateSeconds      float64 `json:"validate_seconds"`
	OutputMeasureSeconds float64 `json:"output_measure_seconds"`
}

type SyncMetricsOutput struct {
	Measured bool                    `json:"measured"`
	Files    int                     `json:"files,omitempty"`
	Bytes    int64                   `json:"bytes,omitempty"`
	Roots    []SyncMetricsOutputRoot `json:"roots"`
}

type SyncMetricsOutputRoot struct {
	Subject      string `json:"subject"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	RelativePath string `json:"relative_path,omitempty"`
	Files        int    `json:"files,omitempty"`
	Bytes        int64  `json:"bytes,omitempty"`
}

type SyncMetricsRecorder struct {
	mu         sync.Mutex
	metrics    SyncMetrics
	seenSHA    map[string]struct{}
	fetches    []SyncMetricsArchiveFetch
	cache      []SyncMetricsCacheEntry
	artifacts  []SyncMetricsArtifactRecord
	totalStart time.Time
	phaseStart time.Time
}

type syncArtifactMetricsEvent struct {
	Subject          string
	Kind             string
	Name             string
	SourceKind       string
	Result           string
	Reason           string
	RelativePath     string
	Duration         time.Duration
	PrepareDuration  time.Duration
	ActivateDuration time.Duration
}

type syncArchiveMetricsEvent struct {
	Subject          string
	SourceKind       string
	SHA256           string
	Downloaded       bool
	Bytes            int64
	Duration         time.Duration
	DownloadDuration time.Duration
}

type syncCacheMetricsEvent struct {
	Subject    string
	SourceKind string
	Key        string
	SHA256     string
	Platform   string
	Result     string
	Lookup     bool
	Put        bool
	PutFailed  bool
	Bytes      int64
	Files      int
	Duration   time.Duration
	PutTimings materializedCachePutTimings
}

func NewSyncMetricsRecorder() *SyncMetricsRecorder {
	return &SyncMetricsRecorder{
		seenSHA: make(map[string]struct{}),
	}
}

func (r *SyncMetricsRecorder) Begin(action string, configPaths []string, check bool, parallelism int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	r.totalStart = now
	r.phaseStart = now
	r.metrics.Sync.Action = action
	r.metrics.Inputs.ConfigPaths = append([]string(nil), configPaths...)
	r.metrics.Inputs.Locked = true
	r.metrics.Inputs.Check = check
	r.metrics.Inputs.Parallelism = parallelism
}

func (r *SyncMetricsRecorder) SetPaths(artifactsDir, lockfilePath, cacheDir string, cacheConfigured, cacheEnabled bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metrics.Sync.ArtifactsDir = artifactsDir
	r.metrics.Sync.LockfilePath = lockfilePath
	r.metrics.Cache.Configured = cacheConfigured
	r.metrics.Cache.Enabled = cacheEnabled
	r.metrics.Cache.Dir = cacheDir
	if cacheConfigured {
		r.metrics.Cache.Mode = "materialized"
		r.metrics.Cache.BucketVersion = materializedCacheBucketVersion
	}
}

func (r *SyncMetricsRecorder) FinishLoadPhase() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	r.metrics.Phases.LoadSeconds = roundedSeconds(now.Sub(r.phaseStart))
	r.phaseStart = now
}

func (r *SyncMetricsRecorder) SetArtifactRoots(roots []PreparedArtifactRoot) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metrics.Artifacts.Considered = len(roots)
}

func (r *SyncMetricsRecorder) FinishMaterializePhase() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	r.metrics.Phases.MaterializeSeconds = roundedSeconds(now.Sub(r.phaseStart))
	r.phaseStart = now
}

func (r *SyncMetricsRecorder) FinishValidatePhase() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	r.metrics.Phases.ValidateSeconds = roundedSeconds(now.Sub(r.phaseStart))
	r.phaseStart = now
}

func (r *SyncMetricsRecorder) RecordOutputStats(measure bool, artifactsDir string, roots []PreparedArtifactRoot) {
	if r == nil {
		return
	}
	if !measure {
		r.SetOutputStats(false, 0, 0, nil)
		return
	}
	outputStart := time.Now()
	measured, files, bytes, outputRoots, err := measurePreparedOutputRoots(artifactsDir, roots)
	if err != nil {
		measured = false
		files = 0
		bytes = 0
		outputRoots = nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metrics.Output = SyncMetricsOutput{
		Measured: measured,
		Files:    files,
		Bytes:    bytes,
		Roots:    outputRoots,
	}
	r.metrics.Phases.OutputMeasureSeconds = roundedSeconds(time.Since(outputStart))
}

func (r *SyncMetricsRecorder) RecordArchiveFetch(event syncArchiveMetricsEvent) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	r.metrics.Archives.Requests++
	if event.SHA256 != "" {
		r.seenSHA[event.SHA256] = struct{}{}
		r.metrics.Archives.UniqueSHA256 = len(r.seenSHA)
	}
	if event.Downloaded {
		r.metrics.Archives.Downloads.Count++
		r.metrics.Archives.Downloads.Bytes += event.Bytes
		r.metrics.Archives.Downloads.DurationSeconds = roundedSeconds(secondsDuration(r.metrics.Archives.Downloads.DurationSeconds) + event.DownloadDuration)
	}

	fetch := SyncMetricsArchiveFetch{
		Subject:         event.Subject,
		SourceKind:      event.SourceKind,
		SHA256:          event.SHA256,
		Downloaded:      event.Downloaded,
		Bytes:           event.Bytes,
		DurationSeconds: roundedSeconds(event.Duration),
	}
	r.fetches = append(r.fetches, fetch)
	slices.SortFunc(r.fetches, func(a, b SyncMetricsArchiveFetch) int {
		switch {
		case a.DurationSeconds > b.DurationSeconds:
			return -1
		case a.DurationSeconds < b.DurationSeconds:
			return 1
		default:
			if n := cmp.Compare(a.Subject, b.Subject); n != 0 {
				return n
			}
			if n := cmp.Compare(a.SourceKind, b.SourceKind); n != 0 {
				return n
			}
			return cmp.Compare(a.SHA256, b.SHA256)
		}
	})
}

func (r *SyncMetricsRecorder) RecordCacheEntry(event syncCacheMetricsEvent) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if event.Lookup {
		switch event.Result {
		case syncCacheResultHit:
			r.metrics.Cache.Eligible++
			r.metrics.Cache.Hits++
		case syncCacheResultMiss:
			r.metrics.Cache.Eligible++
			r.metrics.Cache.Misses++
		case syncCacheResultInvalid:
			r.metrics.Cache.Eligible++
			r.metrics.Cache.Invalid++
		case syncCacheResultDisabled:
			r.metrics.Cache.Disabled++
		case syncCacheResultUncacheable:
			r.metrics.Cache.Uncacheable++
		}
	}
	put := syncCachePutSkipped
	if event.Put {
		if event.PutFailed {
			r.metrics.Cache.Put.Failures++
			put = syncCachePutFailure
		} else {
			r.metrics.Cache.Put.Successes++
			put = syncCachePutSuccess
		}
		r.metrics.Cache.Put.LocalInspectSeconds = roundedSeconds(secondsDuration(r.metrics.Cache.Put.LocalInspectSeconds) + event.PutTimings.LocalInspect)
		r.metrics.Cache.Put.LocalWriteSeconds = roundedSeconds(secondsDuration(r.metrics.Cache.Put.LocalWriteSeconds) + event.PutTimings.LocalWrite)
		r.metrics.Cache.Put.RemoteExistsSeconds = roundedSeconds(secondsDuration(r.metrics.Cache.Put.RemoteExistsSeconds) + event.PutTimings.RemoteExists)
		r.metrics.Cache.Put.RemoteArchiveSeconds = roundedSeconds(secondsDuration(r.metrics.Cache.Put.RemoteArchiveSeconds) + event.PutTimings.RemoteArchive)
		r.metrics.Cache.Put.RemoteUploadSeconds = roundedSeconds(secondsDuration(r.metrics.Cache.Put.RemoteUploadSeconds) + event.PutTimings.RemoteUpload)
		if event.PutTimings.RemoteSkippedExisting {
			r.metrics.Cache.Put.RemoteSkippedExisting++
		}
	}
	entry := SyncMetricsCacheEntry{
		Subject:         event.Subject,
		SourceKind:      event.SourceKind,
		Key:             event.Key,
		SHA256:          event.SHA256,
		Platform:        event.Platform,
		Result:          event.Result,
		Put:             put,
		Bytes:           event.Bytes,
		Files:           event.Files,
		DurationSeconds: roundedSeconds(event.Duration),
	}
	r.cache = append(r.cache, entry)
	slices.SortFunc(r.cache, func(a, b SyncMetricsCacheEntry) int {
		if n := cmp.Compare(a.Subject, b.Subject); n != 0 {
			return n
		}
		if n := cmp.Compare(a.SourceKind, b.SourceKind); n != 0 {
			return n
		}
		return cmp.Compare(a.Key, b.Key)
	})
	r.metrics.Cache.Entries = append([]SyncMetricsCacheEntry(nil), r.cache...)
}

func (r *SyncMetricsRecorder) RecordCacheRestore(stats materializedCacheHydrateStats) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metrics.Cache.Restore = SyncMetricsCacheRestore{
		DurationSeconds: roundedSeconds(stats.Duration),
		ListSeconds:     roundedSeconds(stats.List),
		Entries:         stats.Entries,
		Restored:        stats.Restored,
		Failures:        stats.Failures,
		Bytes:           stats.Bytes,
		Error:           stats.Error,
	}
}

func (r *SyncMetricsRecorder) RecordArtifact(event syncArtifactMetricsEvent) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	record := SyncMetricsArtifactRecord{
		Subject:                 event.Subject,
		Kind:                    event.Kind,
		Name:                    event.Name,
		SourceKind:              event.SourceKind,
		Result:                  event.Result,
		Reason:                  event.Reason,
		RelativePath:            event.RelativePath,
		DurationSeconds:         roundedSeconds(event.Duration),
		PrepareDurationSeconds:  roundedSeconds(event.PrepareDuration),
		ActivateDurationSeconds: roundedSeconds(event.ActivateDuration),
	}
	r.artifacts = append(r.artifacts, record)
	sortArtifactRecords(r.artifacts)
	r.metrics.Artifacts.Items = append([]SyncMetricsArtifactRecord(nil), r.artifacts...)
}

func (r *SyncMetricsRecorder) SetOutputStats(measured bool, files int, bytes int64, roots []SyncMetricsOutputRoot) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metrics.Output = SyncMetricsOutput{
		Measured: measured,
		Files:    files,
		Bytes:    bytes,
		Roots:    append([]SyncMetricsOutputRoot(nil), roots...),
	}
}

func (r *SyncMetricsRecorder) Finish() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metrics.Sync.DurationSeconds = roundedSeconds(time.Since(r.totalStart))
}

func (r *SyncMetricsRecorder) Snapshot() SyncMetrics {
	if r == nil {
		return SyncMetrics{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	metrics := r.metrics
	metrics.Inputs.ConfigPaths = append([]string(nil), r.metrics.Inputs.ConfigPaths...)
	fetches := append([]SyncMetricsArchiveFetch(nil), r.fetches...)
	metrics.Archives.Fetches = fetches
	metrics.Cache.Entries = append([]SyncMetricsCacheEntry(nil), r.metrics.Cache.Entries...)
	metrics.Artifacts.Items = append([]SyncMetricsArtifactRecord(nil), r.metrics.Artifacts.Items...)
	metrics.Output.Roots = append([]SyncMetricsOutputRoot(nil), r.metrics.Output.Roots...)
	if metrics.Archives.Fetches == nil {
		metrics.Archives.Fetches = []SyncMetricsArchiveFetch{}
	}
	if metrics.Cache.Entries == nil {
		metrics.Cache.Entries = []SyncMetricsCacheEntry{}
	}
	if metrics.Artifacts.Items == nil {
		metrics.Artifacts.Items = []SyncMetricsArtifactRecord{}
	}
	if metrics.Output.Roots == nil {
		metrics.Output.Roots = []SyncMetricsOutputRoot{}
	}
	return metrics
}

func roundedSeconds(d time.Duration) float64 {
	return float64((d + 500*time.Microsecond).Milliseconds()) / 1000
}

func secondsDuration(seconds float64) time.Duration {
	return time.Duration(seconds * float64(time.Second))
}

func measurePreparedOutputRoots(artifactsDir string, roots []PreparedArtifactRoot) (bool, int, int64, []SyncMetricsOutputRoot, error) {
	roots = dedupePreparedArtifactRoots(roots)
	if len(roots) == 0 {
		return false, 0, 0, nil, nil
	}
	var files int
	var bytes int64
	var outputRoots []SyncMetricsOutputRoot
	for _, root := range roots {
		var rootFiles int
		var rootBytes int64
		rootExists := true
		err := filepath.WalkDir(root.DestDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if path == root.DestDir && os.IsNotExist(err) {
					rootExists = false
					return nil
				}
				return err
			}
			if d.IsDir() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if info.Mode().IsRegular() {
				rootFiles++
				rootBytes += info.Size()
			}
			return nil
		})
		if err != nil {
			return false, 0, 0, nil, err
		}
		if !rootExists {
			continue
		}
		files += rootFiles
		bytes += rootBytes
		outputRoots = append(outputRoots, SyncMetricsOutputRoot{
			Subject:      root.Subject,
			Kind:         root.Kind,
			Name:         root.Name,
			RelativePath: relativeArtifactPath(artifactsDir, root.DestDir),
			Files:        rootFiles,
			Bytes:        rootBytes,
		})
	}
	sortOutputRoots(outputRoots)
	return true, files, bytes, outputRoots, nil
}

func dedupePreparedArtifactRoots(roots []PreparedArtifactRoot) []PreparedArtifactRoot {
	seen := make(map[string]struct{}, len(roots))
	var cleaned []PreparedArtifactRoot
	for _, root := range roots {
		if root.DestDir == "" {
			continue
		}
		root.DestDir = filepath.Clean(root.DestDir)
		if _, ok := seen[root.DestDir]; ok {
			continue
		}
		seen[root.DestDir] = struct{}{}
		cleaned = append(cleaned, root)
	}
	slices.SortFunc(cleaned, func(a, b PreparedArtifactRoot) int {
		if n := cmp.Compare(a.Kind, b.Kind); n != 0 {
			return n
		}
		if n := cmp.Compare(a.Name, b.Name); n != 0 {
			return n
		}
		return cmp.Compare(a.Subject, b.Subject)
	})
	return cleaned
}

func sortArtifactRecords(records []SyncMetricsArtifactRecord) {
	slices.SortFunc(records, func(a, b SyncMetricsArtifactRecord) int {
		switch {
		case a.DurationSeconds > b.DurationSeconds:
			return -1
		case a.DurationSeconds < b.DurationSeconds:
			return 1
		}
		if n := cmp.Compare(a.Kind, b.Kind); n != 0 {
			return n
		}
		if n := cmp.Compare(a.Name, b.Name); n != 0 {
			return n
		}
		return cmp.Compare(a.Subject, b.Subject)
	})
}

func sortOutputRoots(roots []SyncMetricsOutputRoot) {
	slices.SortFunc(roots, func(a, b SyncMetricsOutputRoot) int {
		switch {
		case a.Bytes > b.Bytes:
			return -1
		case a.Bytes < b.Bytes:
			return 1
		}
		if n := cmp.Compare(a.Kind, b.Kind); n != 0 {
			return n
		}
		if n := cmp.Compare(a.Name, b.Name); n != 0 {
			return n
		}
		return cmp.Compare(a.Subject, b.Subject)
	})
}

func relativeArtifactPath(artifactsDir, path string) string {
	if artifactsDir == "" || path == "" {
		return ""
	}
	rel, err := filepath.Rel(artifactsDir, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return ""
	}
	return filepath.ToSlash(rel)
}
