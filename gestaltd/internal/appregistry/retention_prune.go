package appregistry

import (
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
)

// RetentionPruneAction describes one retention cleanup decision.
type RetentionPruneAction struct {
	Version string
	Kind    string
}

const (
	RetentionPruneDeleteUnused   = "delete_unused"
	RetentionPruneDeleteArtifact = "delete_artifact"
)

// EvaluateRetentionPrune returns prune actions for one app catalog.
func EvaluateRetentionPrune(
	index *Index,
	retention *RetentionIndex,
	appName string,
	desiredVersion string,
	deployedVersions map[string]struct{},
	policy RetentionPolicy,
	now time.Time,
) []RetentionPruneAction {
	_ = policy
	if retention == nil || len(retention.Versions) == 0 {
		return nil
	}
	now = now.UTC()
	appName = strings.TrimSpace(appName)
	desiredVersion = strings.TrimSpace(desiredVersion)
	published := PublishedVersionKeys(index, appName)
	actions := make([]RetentionPruneAction, 0)
	for version, entry := range retention.Versions {
		if version == desiredVersion {
			continue
		}
		if _, deployed := deployedVersions[version]; deployed {
			entry.EverDeployed = true
		}
		if !RetentionExpired(entry, now) {
			continue
		}
		if !entry.EverDeployed {
			actions = append(actions, RetentionPruneAction{Version: version, Kind: RetentionPruneDeleteUnused})
			continue
		}
		if _, ok := published[version]; ok {
			actions = append(actions, RetentionPruneAction{Version: version, Kind: RetentionPruneDeleteArtifact})
		}
	}
	return actions
}

// ShouldApplyRetentionPruneAction re-checks one action against a fresh retention row.
// Lean toward keeping versions when expiresAt was cleared or extended concurrently.
func ShouldApplyRetentionPruneAction(action RetentionPruneAction, entry RetentionVersion, now time.Time) bool {
	switch action.Kind {
	case RetentionPruneDeleteUnused:
		return !entry.EverDeployed && RetentionExpired(entry, now)
	case RetentionPruneDeleteArtifact:
		return entry.EverDeployed && RetentionExpired(entry, now)
	default:
		return false
	}
}

// DeployedVersionsFromChangeRequests returns versions that entered the deploy chain.
func DeployedVersionsFromChangeRequests(requests []*core.AppVersionChangeRequest) map[string]struct{} {
	out := map[string]struct{}{}
	for _, request := range requests {
		if request == nil {
			continue
		}
		if version := strings.TrimSpace(request.ToVersion); version != "" {
			out[version] = struct{}{}
		}
		fromVersion := strings.TrimSpace(request.FromVersion)
		if fromVersion != "" && fromVersion != FirstInstallFromVersion {
			out[fromVersion] = struct{}{}
		}
	}
	return out
}

// ApplyRetentionPruneAction mutates in-memory catalogs for one prune action.
func ApplyRetentionPruneAction(index *Index, retention *RetentionIndex, appName string, action RetentionPruneAction, now time.Time) bool {
	_ = now
	if action.Kind != RetentionPruneDeleteUnused {
		return false
	}
	changed := false
	if index != nil {
		if appVersions, ok := index.Apps[appName]; ok {
			if _, exists := appVersions.Versions[action.Version]; exists {
				delete(appVersions.Versions, action.Version)
				index.Apps[appName] = appVersions
				changed = true
			}
		}
	}
	if RemoveRetentionVersion(retention, action.Version) {
		changed = true
	}
	return changed
}
