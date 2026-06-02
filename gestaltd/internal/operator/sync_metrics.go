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

	syncArchiveCacheResultHit         = "hit"
	syncArchiveCacheResultMiss        = "miss"
	syncArchiveCacheResultInvalid     = "invalid"
	syncArchiveCacheResultRejected    = "rejected"
	syncArchiveCacheResultDisabled    = "disabled"
	syncArchiveCacheResultUncacheable = "uncacheable"

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
	Cache        SyncMetricsArchiveCache   `json:"cache"`
	Downloads    SyncMetricsDownloads      `json:"downloads"`
	Fetches      []SyncMetricsArchiveFetch `json:"fetches"`
}

type SyncMetricsArchiveCache struct {
	Configured  bool   `json:"configured"`
	Enabled     bool   `json:"enabled"`
	Dir         string `json:"dir"`
	Eligible    int    `json:"eligible"`
	Disabled    int    `json:"disabled"`
	Uncacheable int    `json:"uncacheable"`
	Hits        int    `json:"hits"`
	Misses      int    `json:"misses"`
	Invalid     int    `json:"invalid"`
	Rejected    int    `json:"rejected"`
	Puts        int    `json:"puts"`
	PutFailures int    `json:"put_failures"`
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
	CacheResult     string  `json:"cache_result"`
	Downloaded      bool    `json:"downloaded"`
	CachePutFailed  bool    `json:"cache_put_failed"`
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
	Eligible         bool
	Disabled         bool
	Uncacheable      bool
	CacheResult      string
	Downloaded       bool
	CachePut         bool
	CachePutFailed   bool
	Bytes            int64
	Duration         time.Duration
	DownloadDuration time.Duration
}

type syncArchiveDownloadObserver struct {
	metrics *SyncMetricsRecorder
	subject string
}

func newSyncArchiveDownloadObserver(metrics *SyncMetricsRecorder, subject string) syncArchiveDownloadObserver {
	return syncArchiveDownloadObserver{
		metrics: metrics,
		subject: subject,
	}
}

func (o syncArchiveDownloadObserver) beginArchiveFetch(sourceKind, sha string, cacheEligible, useCache bool) *archiveFetchTracker {
	return newArchiveFetchTracker(o.metrics, o.subject, sourceKind, sha, cacheEligible, useCache)
}

type archiveFetchTracker struct {
	metrics       *SyncMetricsRecorder
	event         syncArchiveMetricsEvent
	start         time.Time
	downloadStart time.Time
	canceled      bool
}

func newArchiveFetchTracker(metrics *SyncMetricsRecorder, subject, sourceKind, sha string, cacheEligible, useCache bool) *archiveFetchTracker {
	if metrics == nil {
		return nil
	}
	tracker := &archiveFetchTracker{
		metrics: metrics,
		start:   time.Now(),
		event: syncArchiveMetricsEvent{
			Subject:     subject,
			SourceKind:  sourceKind,
			SHA256:      sha,
			Eligible:    cacheEligible,
			Disabled:    cacheEligible && !useCache,
			Uncacheable: !cacheEligible,
		},
	}
	switch {
	case useCache:
		tracker.event.CacheResult = syncArchiveCacheResultMiss
	case tracker.event.Disabled:
		tracker.event.CacheResult = syncArchiveCacheResultDisabled
	default:
		tracker.event.CacheResult = syncArchiveCacheResultUncacheable
	}
	return tracker
}

func (t *archiveFetchTracker) setCacheResult(result string) {
	if t == nil {
		return
	}
	t.event.CacheResult = result
}

func (t *archiveFetchTracker) setCacheLookupResult(result archiveCacheLookupResult) {
	if t == nil {
		return
	}
	switch result {
	case archiveCacheHit:
		t.setCacheResult(syncArchiveCacheResultHit)
	case archiveCacheMiss:
		t.setCacheResult(syncArchiveCacheResultMiss)
	case archiveCacheInvalid:
		t.setCacheResult(syncArchiveCacheResultInvalid)
	case archiveCacheRejected:
		t.setCacheResult(syncArchiveCacheResultRejected)
	}
}

func (t *archiveFetchTracker) setCachePut(failed bool) {
	if t == nil {
		return
	}
	t.event.CachePut = true
	t.event.CachePutFailed = failed
}

func (t *archiveFetchTracker) setBytesFromPath(path string) {
	if t == nil {
		return
	}
	t.event.Bytes = archiveFileSize(path)
}

func (t *archiveFetchTracker) startDownload() {
	if t == nil {
		return
	}
	t.downloadStart = time.Now()
}

func (t *archiveFetchTracker) finishDownload(remote bool, path string) {
	if t == nil {
		return
	}
	t.event.Downloaded = remote
	t.event.Bytes = archiveFileSize(path)
	if remote && !t.downloadStart.IsZero() {
		t.event.DownloadDuration = time.Since(t.downloadStart)
	}
}

func (t *archiveFetchTracker) cancel() {
	if t == nil {
		return
	}
	t.canceled = true
}

func (t *archiveFetchTracker) record() {
	if t == nil || t.canceled {
		return
	}
	t.event.Duration = time.Since(t.start)
	t.metrics.RecordArchiveFetch(t.event)
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
	r.metrics.Archives.Cache.Configured = cacheConfigured
	r.metrics.Archives.Cache.Enabled = cacheEnabled
	r.metrics.Archives.Cache.Dir = cacheDir
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
	if event.Eligible {
		r.metrics.Archives.Cache.Eligible++
	}
	if event.Disabled {
		r.metrics.Archives.Cache.Disabled++
	}
	if event.Uncacheable {
		r.metrics.Archives.Cache.Uncacheable++
	}
	switch event.CacheResult {
	case syncArchiveCacheResultHit:
		r.metrics.Archives.Cache.Hits++
	case syncArchiveCacheResultMiss:
		r.metrics.Archives.Cache.Misses++
	case syncArchiveCacheResultInvalid:
		r.metrics.Archives.Cache.Invalid++
	case syncArchiveCacheResultRejected:
		r.metrics.Archives.Cache.Rejected++
	}
	if event.CachePut {
		if event.CachePutFailed {
			r.metrics.Archives.Cache.PutFailures++
		} else {
			r.metrics.Archives.Cache.Puts++
		}
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
		CacheResult:     event.CacheResult,
		Downloaded:      event.Downloaded,
		CachePutFailed:  event.CachePutFailed,
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
	metrics.Artifacts.Items = append([]SyncMetricsArtifactRecord(nil), r.metrics.Artifacts.Items...)
	metrics.Output.Roots = append([]SyncMetricsOutputRoot(nil), r.metrics.Output.Roots...)
	if metrics.Archives.Fetches == nil {
		metrics.Archives.Fetches = []SyncMetricsArchiveFetch{}
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
