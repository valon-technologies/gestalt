package operator

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

type pathDomainTask struct {
	name    string
	domains []string
	run     func() error
}

func runPathDomainTasks(tasks []pathDomainTask, parallelism int) error {
	if len(tasks) == 0 {
		return nil
	}
	if parallelism < 1 {
		parallelism = 1
	}
	if parallelism > len(tasks) {
		parallelism = len(tasks)
	}

	pending := make([]queuedPathDomainTask, 0, len(tasks))
	for i, task := range tasks {
		pending = append(pending, queuedPathDomainTask{index: i, task: task})
	}
	var errs []taskError

	for len(pending) > 0 && len(errs) == 0 {
		batch := nextPathDomainBatch(pending, parallelism)
		results := make(chan taskError, len(batch))
		for _, queued := range batch {
			queued := queued
			go func() {
				results <- taskError{index: queued.index, err: queued.task.run()}
			}()
		}
		for range batch {
			result := <-results
			if result.err != nil {
				errs = append(errs, result)
			}
		}
		pending = removeQueuedPathDomainTasks(pending, batch)
	}

	if len(errs) == 0 {
		return nil
	}
	slices.SortFunc(errs, func(a, b taskError) int {
		return a.index - b.index
	})
	return errs[0].err
}

type taskError struct {
	index int
	err   error
}

type queuedPathDomainTask struct {
	index int
	task  pathDomainTask
}

func nextPathDomainBatch(pending []queuedPathDomainTask, parallelism int) []queuedPathDomainTask {
	batch := make([]queuedPathDomainTask, 0, parallelism)
	activeDomains := make([]string, 0)
	for _, queued := range pending {
		if len(batch) == parallelism {
			break
		}
		if pathDomainsConflictAny(activeDomains, queued.task.domains) {
			continue
		}
		batch = append(batch, queued)
		activeDomains = append(activeDomains, queued.task.domains...)
	}
	return batch
}

func removeQueuedPathDomainTasks(pending, batch []queuedPathDomainTask) []queuedPathDomainTask {
	selected := make(map[int]struct{}, len(batch))
	for _, queued := range batch {
		selected[queued.index] = struct{}{}
	}
	next := pending[:0]
	for _, queued := range pending {
		if _, ok := selected[queued.index]; ok {
			continue
		}
		next = append(next, queued)
	}
	return next
}

func pathDomainsConflictAny(active []string, requested []string) bool {
	for _, activeDomain := range active {
		for _, requestedDomain := range requested {
			if pathDomainsConflict(activeDomain, requestedDomain) {
				return true
			}
		}
	}
	return false
}

func pathDomainsConflict(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	return a == b || pathWithinRoot(a, b) || pathWithinRoot(b, a)
}

func normalizePathDomains(paths ...string) ([]string, error) {
	domains := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		domain, err := canonicalPathDomain(path)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[domain]; ok {
			continue
		}
		seen[domain] = struct{}{}
		domains = append(domains, domain)
	}
	slices.Sort(domains)
	return domains, nil
}

func canonicalPathDomain(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path domain %q: %w", path, err)
	}
	clean := filepath.Clean(abs)
	current := clean
	suffix := ""
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			if suffix != "" {
				return filepath.Clean(filepath.Join(resolved, suffix)), nil
			}
			return filepath.Clean(resolved), nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return clean, nil
		}
		if suffix == "" {
			suffix = filepath.Base(current)
		} else {
			suffix = filepath.Join(filepath.Base(current), suffix)
		}
		current = parent
	}
}
