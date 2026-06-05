package operator

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

type materializedCachePrefetchStats struct {
	Duration     time.Duration
	Requests     int
	Eligible     int
	LocalHits    int
	RemoteHits   int
	RemoteMisses int
	Failures     int
	Bytes        int64
	Error        string
	Keys         []string
}

func (c materializedCache) Prefetch(ctx context.Context, requests []materializedCacheRequest, parallelism int) (stats materializedCachePrefetchStats) {
	start := time.Now()
	defer func() {
		stats.Duration = time.Since(start)
	}()
	if c.dir == "" || c.remote == nil || len(requests) == 0 {
		return stats
	}

	keysByDisplay := make(map[string]materializedCacheKey)
	stats.Requests = len(requests)
	for i := range requests {
		key, eligible, err := materializedCacheKeyForRequest(requests[i])
		if err != nil || !eligible {
			continue
		}
		stats.Eligible++
		if _, ok := keysByDisplay[key.Display]; ok {
			continue
		}
		keysByDisplay[key.Display] = key
		stats.Keys = append(stats.Keys, key.Display)
	}
	if len(stats.Keys) == 0 {
		return stats
	}
	slices.Sort(stats.Keys)
	if parallelism < 1 {
		parallelism = 1
	}

	var mu sync.Mutex
	jobs := make(chan materializedCacheKey)
	var wg sync.WaitGroup
	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for key := range jobs {
				result := c.prefetchRemoteObject(ctx, key)
				mu.Lock()
				stats.Bytes += result.Bytes
				switch {
				case result.LocalHit:
					stats.LocalHits++
				case result.RemoteHit:
					stats.RemoteHits++
				case result.RemoteMiss:
					stats.RemoteMisses++
				}
				if result.Failed {
					stats.Failures++
					if stats.Error == "" && result.Error != "" {
						stats.Error = result.Error
					}
				}
				mu.Unlock()
			}
		}()
	}
	var sendErr error
sendJobs:
	for _, display := range stats.Keys {
		select {
		case jobs <- keysByDisplay[display]:
		case <-ctx.Done():
			sendErr = ctx.Err()
			break sendJobs
		}
	}
	close(jobs)
	wg.Wait()
	if sendErr != nil {
		stats.Failures++
		if stats.Error == "" {
			stats.Error = sendErr.Error()
		}
	}
	return stats
}

type materializedCachePrefetchObjectResult struct {
	Bytes      int64
	LocalHit   bool
	RemoteHit  bool
	RemoteMiss bool
	Failed     bool
	Error      string
}

func (c materializedCache) prefetchRemoteObject(ctx context.Context, key materializedCacheKey) materializedCachePrefetchObjectResult {
	result := materializedCachePrefetchObjectResult{}
	entryDir := c.entryDir(key)
	if entry, err := c.readEntryForKey(entryDir, key); err == nil && validateMaterializedCacheEntryFiles(entryDir, entry) == nil {
		result.LocalHit = true
		return result
	}
	reader, err := c.remote.Get(ctx, key)
	if os.IsNotExist(err) {
		result.RemoteMiss = true
		return result
	}
	if err != nil {
		result.Failed = true
		result.Error = err.Error()
		return result
	}
	defer func() { _ = reader.Close() }()

	parentDir := filepath.Dir(entryDir)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		result.Failed = true
		result.Error = err.Error()
		return result
	}
	tmpDir, err := os.MkdirTemp(parentDir, "."+filepath.Base(entryDir)+".remote-*")
	if err != nil {
		result.Failed = true
		result.Error = err.Error()
		return result
	}
	keepTmp := false
	defer func() {
		if !keepTmp {
			_ = os.RemoveAll(tmpDir)
		}
	}()
	counter := &countingReader{reader: reader}
	if err := extractMaterializedCacheEntryArchive(counter, tmpDir); err != nil {
		result.Failed = true
		result.Error = err.Error()
		return result
	}
	result.Bytes = counter.bytes
	entry, err := c.readEntryForKey(tmpDir, key)
	if err == nil {
		err = validateMaterializedCacheEntryFiles(tmpDir, entry)
	}
	if err != nil {
		result.Failed = true
		result.Error = err.Error()
		return result
	}
	if err := os.RemoveAll(entryDir); err != nil {
		result.Failed = true
		result.Error = err.Error()
		return result
	}
	if err := os.Rename(tmpDir, entryDir); err != nil {
		result.Failed = true
		result.Error = err.Error()
		return result
	}
	keepTmp = true
	result.RemoteHit = true
	return result
}

type countingReader struct {
	reader io.Reader
	bytes  int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.bytes += int64(n)
	return n, err
}
