package operator

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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

	syncMetricsMaxSlowFetches = 5
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
	Considered int `json:"considered"`
}

type SyncMetricsArchives struct {
	Requests       int                       `json:"requests"`
	UniqueSHA256   int                       `json:"unique_sha256"`
	Cache          SyncMetricsArchiveCache   `json:"cache"`
	Downloads      SyncMetricsDownloads      `json:"downloads"`
	SlowestFetches []SyncMetricsArchiveFetch `json:"slowest_fetches"`
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
	Measured bool  `json:"measured"`
	Files    int   `json:"files,omitempty"`
	Bytes    int64 `json:"bytes,omitempty"`
}

type SyncMetricsRecorder struct {
	mu         sync.Mutex
	metrics    SyncMetrics
	seenSHA    map[string]struct{}
	fetches    []SyncMetricsArchiveFetch
	totalStart time.Time
	phaseStart time.Time
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

func (r *SyncMetricsRecorder) SetArtifactCount(artifactCount int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metrics.Artifacts.Considered = artifactCount
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

func (r *SyncMetricsRecorder) RecordOutputStats(measure bool, roots []string) {
	if r == nil {
		return
	}
	if !measure {
		r.SetOutputStats(false, 0, 0)
		return
	}
	outputStart := time.Now()
	measured, files, bytes, err := measurePreparedOutputRoots(roots)
	if err != nil {
		measured = false
		files = 0
		bytes = 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metrics.Output = SyncMetricsOutput{
		Measured: measured,
		Files:    files,
		Bytes:    bytes,
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
			return 0
		}
	})
	if len(r.fetches) > syncMetricsMaxSlowFetches {
		r.fetches = r.fetches[:syncMetricsMaxSlowFetches]
	}
}

func (r *SyncMetricsRecorder) SetOutputStats(measured bool, files int, bytes int64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metrics.Output = SyncMetricsOutput{
		Measured: measured,
		Files:    files,
		Bytes:    bytes,
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
	metrics.Archives.SlowestFetches = fetches
	return metrics
}

func roundedSeconds(d time.Duration) float64 {
	return float64((d + 500*time.Microsecond).Milliseconds()) / 1000
}

func secondsDuration(seconds float64) time.Duration {
	return time.Duration(seconds * float64(time.Second))
}

func measurePreparedOutputRoots(roots []string) (bool, int, int64, error) {
	roots = dedupeCleanPaths(roots)
	if len(roots) == 0 {
		return false, 0, 0, nil
	}
	var files int
	var bytes int64
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if path == root && os.IsNotExist(err) {
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
				files++
				bytes += info.Size()
			}
			return nil
		})
		if err != nil {
			return false, 0, 0, err
		}
	}
	return true, files, bytes, nil
}

func dedupeCleanPaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	var cleaned []string
	for _, path := range paths {
		if path == "" {
			continue
		}
		path = filepath.Clean(path)
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		cleaned = append(cleaned, path)
	}
	slices.Sort(cleaned)
	return cleaned
}
