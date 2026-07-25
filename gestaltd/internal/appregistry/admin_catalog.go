package appregistry

import (
	"sort"
	"time"
)

// PendingVersionsForAdmin returns pending versions not already published, newest
// startedAt first.
func PendingVersionsForAdmin(pending *PendingIndex, published map[string]struct{}) []PendingVersion {
	if pending == nil || len(pending.Pending) == 0 {
		return nil
	}
	out := make([]PendingVersion, 0, len(pending.Pending))
	for version, entry := range pending.Pending {
		if _, ok := published[version]; ok {
			continue
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].Version > out[j].Version
		}
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	return out
}

// FailedVersionsForAdmin returns failed versions not already published or
// pending, newest failedAt first.
func FailedVersionsForAdmin(failed *FailedIndex, published, pending map[string]struct{}) []FailedVersion {
	if failed == nil || len(failed.Failed) == 0 {
		return nil
	}
	out := make([]FailedVersion, 0, len(failed.Failed))
	for version, entry := range failed.Failed {
		if _, ok := published[version]; ok {
			continue
		}
		if _, ok := pending[version]; ok {
			continue
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].FailedAt.Equal(out[j].FailedAt) {
			return out[i].Version > out[j].Version
		}
		return out[i].FailedAt.After(out[j].FailedAt)
	})
	return out
}

// PublishedVersionKeys returns version keys present in a published app index.
func PublishedVersionKeys(index *Index, appName string) map[string]struct{} {
	if index == nil || len(index.Apps) == 0 {
		return map[string]struct{}{}
	}
	appVersions, ok := index.Apps[appName]
	if !ok || len(appVersions.Versions) == 0 {
		return map[string]struct{}{}
	}
	out := make(map[string]struct{}, len(appVersions.Versions))
	for version := range appVersions.Versions {
		out[version] = struct{}{}
	}
	return out
}

// PendingVersionKeys returns version keys present in a pending catalog.
func PendingVersionKeys(pending *PendingIndex) map[string]struct{} {
	if pending == nil || len(pending.Pending) == 0 {
		return map[string]struct{}{}
	}
	out := make(map[string]struct{}, len(pending.Pending))
	for version := range pending.Pending {
		out[version] = struct{}{}
	}
	return out
}

// DurationSecondsBetween returns whole seconds between start and end when start
// is non-zero and end is after start.
func DurationSecondsBetween(start, end time.Time) (int64, bool) {
	if start.IsZero() || end.IsZero() || !end.After(start) {
		return 0, false
	}
	return int64(end.Sub(start.UTC()).Seconds()), true
}
