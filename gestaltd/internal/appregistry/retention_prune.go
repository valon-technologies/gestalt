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
	RetentionPruneLockHistorical = "lock_historical"
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
		if !entry.EverDeployed {
			if UnusedVersionExpired(entry, policy, now) {
				actions = append(actions, RetentionPruneAction{Version: version, Kind: RetentionPruneDeleteUnused})
			}
			continue
		}
		if entry.LockedAt != nil && !entry.LockedAt.IsZero() {
			if _, ok := published[version]; ok {
				actions = append(actions, RetentionPruneAction{Version: version, Kind: RetentionPruneDeleteArtifact})
			}
			continue
		}
		if entry.DeployableUntil != nil && !now.Before(entry.DeployableUntil.UTC()) {
			actions = append(actions, RetentionPruneAction{Version: version, Kind: RetentionPruneLockHistorical})
			if _, ok := published[version]; ok {
				actions = append(actions, RetentionPruneAction{Version: version, Kind: RetentionPruneDeleteArtifact})
			}
		}
	}
	return actions
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
	changed := false
	switch action.Kind {
	case RetentionPruneDeleteUnused:
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
	case RetentionPruneLockHistorical:
		if retention != nil && retention.Versions != nil {
			entry, ok := retention.Versions[action.Version]
			if ok && (entry.LockedAt == nil || entry.LockedAt.IsZero()) {
				lockedAt := now.UTC()
				entry.LockedAt = &lockedAt
				retention.Versions[action.Version] = entry
				changed = true
			}
		}
	}
	return changed
}
