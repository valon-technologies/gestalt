package appregistry

import (
	"strings"
	"time"
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
	appName string,
	desiredVersion string,
	chain VersionDeploymentChain,
	policy RetentionPolicy,
	now time.Time,
) []RetentionPruneAction {
	if index == nil {
		return nil
	}
	appVersions, ok := index.Apps[strings.TrimSpace(appName)]
	if !ok || len(appVersions.Versions) == 0 {
		return nil
	}
	now = now.UTC()
	desiredVersion = strings.TrimSpace(desiredVersion)
	actions := make([]RetentionPruneAction, 0)
	for version, entry := range appVersions.Versions {
		version = strings.TrimSpace(version)
		if version == "" || version == desiredVersion {
			continue
		}
		if chain.Deployed != nil {
			if _, deployed := chain.Deployed[version]; deployed {
				deadline := chain.deployableUntil(version)
				if deadline != nil && !now.Before(deadline.UTC()) {
					actions = append(actions, RetentionPruneAction{Version: version, Kind: RetentionPruneDeleteArtifact})
				}
				continue
			}
		}
		if UnusedVersionExpired(entry.PublishedAt, policy, now) {
			actions = append(actions, RetentionPruneAction{Version: version, Kind: RetentionPruneDeleteUnused})
		}
	}
	return actions
}

// ApplyRetentionPruneAction mutates in-memory catalogs for one prune action.
func ApplyRetentionPruneAction(index *Index, appName string, action RetentionPruneAction) bool {
	if index == nil || action.Kind != RetentionPruneDeleteUnused {
		return false
	}
	appVersions, ok := index.Apps[strings.TrimSpace(appName)]
	if !ok {
		return false
	}
	if _, exists := appVersions.Versions[action.Version]; !exists {
		return false
	}
	delete(appVersions.Versions, action.Version)
	index.Apps[strings.TrimSpace(appName)] = appVersions
	return true
}
