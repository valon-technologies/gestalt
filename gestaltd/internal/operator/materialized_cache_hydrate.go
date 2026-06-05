package operator

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type materializedCacheHydrateStats struct {
	Duration time.Duration
	List     time.Duration
	Entries  int
	Restored int
	Failures int
	Bytes    int64
	Error    string
}

func (c materializedCache) HydrateRemote(ctx context.Context, parallelism int) (stats materializedCacheHydrateStats) {
	start := time.Now()
	defer func() {
		stats.Duration = time.Since(start)
	}()
	if c.dir == "" || c.remote == nil {
		return stats
	}

	listStart := time.Now()
	objects, err := c.remote.List(ctx)
	stats.List = time.Since(listStart)
	stats.Entries = len(objects)
	if err != nil {
		stats.Failures++
		stats.Error = err.Error()
		return stats
	}
	if len(objects) == 0 {
		return stats
	}
	if parallelism < 1 {
		parallelism = 1
	}

	var mu sync.Mutex
	jobs := make(chan string)
	var wg sync.WaitGroup
	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for object := range jobs {
				result := c.hydrateRemoteObject(ctx, object)
				mu.Lock()
				stats.Bytes += result.Bytes
				if result.Restored {
					stats.Restored++
				}
				if result.Failed {
					stats.Failures++
				}
				mu.Unlock()
			}
		}()
	}
sendJobs:
	for _, object := range objects {
		select {
		case jobs <- object:
		case <-ctx.Done():
			break sendJobs
		}
	}
	close(jobs)
	wg.Wait()
	return stats
}

type materializedCacheHydrateObjectResult struct {
	Bytes    int64
	Restored bool
	Failed   bool
}

func (c materializedCache) hydrateRemoteObject(ctx context.Context, display string) materializedCacheHydrateObjectResult {
	result := materializedCacheHydrateObjectResult{}
	key, ok := materializedCacheKeyFromDisplay(display)
	if !ok {
		result.Failed = true
		return result
	}
	entryDir := c.entryDir(key)
	if entry, err := c.readEntryForKey(entryDir, key); err == nil && validateMaterializedCacheEntryFiles(entryDir, entry) == nil {
		result.Restored = true
		return result
	}
	parentDir := filepath.Dir(entryDir)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		result.Failed = true
		return result
	}
	reader, err := c.remote.Get(ctx, display)
	if err != nil {
		result.Failed = true
		return result
	}
	defer func() { _ = reader.Close() }()

	tmpDir, err := os.MkdirTemp(parentDir, "."+filepath.Base(entryDir)+".remote-*")
	if err != nil {
		result.Failed = true
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
		return result
	}
	result.Bytes = counter.bytes
	entry, err := c.readEntryForKey(tmpDir, key)
	if err != nil || validateMaterializedCacheEntryFiles(tmpDir, entry) != nil {
		result.Failed = true
		return result
	}
	if err := os.RemoveAll(entryDir); err != nil {
		result.Failed = true
		return result
	}
	if err := os.Rename(tmpDir, entryDir); err != nil {
		result.Failed = true
		return result
	}
	keepTmp = true
	result.Restored = true
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
