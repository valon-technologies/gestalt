package operator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

const maxLockDriftLines = 20

type lockDrift struct {
	status string
	path   string
}

func diagnoseLockfileDrift(expected, committed *Lockfile) []lockDrift {
	expectedPortable := providerLockfileFromLockfile(expected)
	committedPortable := providerLockfileFromLockfile(committed)
	var drifts []lockDrift
	forEachPortableBucketPair(expectedPortable, committedPortable, func(path string, expectedEntries, committedEntries map[string]portableLockEntry) {
		names := map[string]struct{}{}
		for name := range expectedEntries {
			names[name] = struct{}{}
		}
		for name := range committedEntries {
			names[name] = struct{}{}
		}
		for _, name := range slices.Sorted(maps.Keys(names)) {
			providerPath := path + "." + name
			expectedEntry, expectedFound := expectedEntries[name]
			committedEntry, committedFound := committedEntries[name]
			switch {
			case !committedFound:
				drifts = append(drifts, lockDrift{status: "missing", path: providerPath})
			case !expectedFound:
				drifts = append(drifts, lockDrift{status: "extra", path: providerPath})
			case !portableLockEntryEqual(expectedEntry, committedEntry):
				drifts = append(drifts, lockDrift{status: "stale", path: providerPath})
			}
		}
	})
	return drifts
}

func portableLockEntryEqual(a, b portableLockEntry) bool {
	aData, aErr := json.Marshal(a)
	bData, bErr := json.Marshal(b)
	return aErr == nil && bErr == nil && bytes.Equal(aData, bData)
}

func formatLockDriftReport(drifts []lockDrift) string {
	if len(drifts) == 0 {
		return ""
	}
	lineCount := min(len(drifts), maxLockDriftLines)
	lines := make([]string, 0, lineCount+1)
	for _, drift := range drifts[:lineCount] {
		lines = append(lines, drift.status+" "+drift.path)
	}
	if remaining := len(drifts) - lineCount; remaining > 0 {
		lines = append(lines, fmt.Sprintf("and %d more lockfile drift(s)", remaining))
	}
	return strings.Join(lines, "\n")
}
