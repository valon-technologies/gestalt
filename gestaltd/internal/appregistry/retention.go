package appregistry

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/apps/source"
)

const (
	RetentionIndexSchemaVersion = 1
	RetentionFileName           = "retention.json"

	DefaultUnusedRetention   = 72 * time.Hour
	DefaultDeployedRetention = 720 * time.Hour
	FirstInstallFromVersion  = "registry:first-install"
)

const (
	DeploymentStateAvailable    = "available"
	DeploymentStateExpired      = "expired"
	DeploymentStateDesired      = "desired"
	DeploymentStateRedeployable = "redeployable"
	DeploymentStateLocked       = "locked"
)

type RetentionIndex struct {
	SchemaVersion int                         `json:"schemaVersion"`
	Versions      map[string]RetentionVersion `json:"versions"`
}

type RetentionVersion struct {
	PublishedAt       time.Time  `json:"publishedAt"`
	LastDeactivatedAt *time.Time `json:"lastDeactivatedAt,omitempty"`
	DeployableUntil   *time.Time `json:"deployableUntil,omitempty"`
	FirstDeployedAt   *time.Time `json:"firstDeployedAt,omitempty"`
	EverDeployed      bool       `json:"everDeployed"`
	LockedAt          *time.Time `json:"lockedAt,omitempty"`
}

type RetentionPolicy struct {
	UnusedRetention   time.Duration
	DeployedRetention time.Duration
}

func DefaultRetentionPolicy() RetentionPolicy {
	return RetentionPolicy{
		UnusedRetention:   DefaultUnusedRetention,
		DeployedRetention: DefaultDeployedRetention,
	}
}

func AppRetentionPath(appName string) string {
	return path.Join("apps", appName, RetentionFileName)
}

func NewEmptyRetentionIndex() *RetentionIndex {
	return &RetentionIndex{
		SchemaVersion: RetentionIndexSchemaVersion,
		Versions:      map[string]RetentionVersion{},
	}
}

func DecodeRetentionIndex(data []byte) (*RetentionIndex, error) {
	var index RetentionIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("decode retention index: %w", err)
	}
	if err := validateRetentionIndex(&index); err != nil {
		return nil, err
	}
	return &index, nil
}

func validateRetentionIndex(index *RetentionIndex) error {
	if index == nil {
		return fmt.Errorf("retention index is required")
	}
	if index.SchemaVersion != RetentionIndexSchemaVersion {
		return fmt.Errorf("unsupported retention index schema version %d", index.SchemaVersion)
	}
	if index.Versions == nil {
		return fmt.Errorf("retention index versions map is required")
	}
	for version, entry := range index.Versions {
		if err := validateRetentionVersion(version, entry); err != nil {
			return fmt.Errorf("retention index version %q: %w", version, err)
		}
	}
	return nil
}

func validateRetentionVersion(mapKey string, entry RetentionVersion) error {
	version := strings.TrimSpace(mapKey)
	if version == "" {
		return fmt.Errorf("version is required")
	}
	if err := source.ValidateVersion(version); err != nil {
		return fmt.Errorf("version: %w", err)
	}
	if entry.PublishedAt.IsZero() {
		return fmt.Errorf("publishedAt is required")
	}
	if entry.LastDeactivatedAt != nil && entry.LastDeactivatedAt.IsZero() {
		return fmt.Errorf("lastDeactivatedAt must not be zero")
	}
	if entry.DeployableUntil != nil && entry.DeployableUntil.IsZero() {
		return fmt.Errorf("deployableUntil must not be zero")
	}
	if entry.FirstDeployedAt != nil && entry.FirstDeployedAt.IsZero() {
		return fmt.Errorf("firstDeployedAt must not be zero")
	}
	if entry.LockedAt != nil && entry.LockedAt.IsZero() {
		return fmt.Errorf("lockedAt must not be zero")
	}
	return nil
}

// UpsertPublishedRetention records a newly published version in the retention overlay.
func UpsertPublishedRetention(index *RetentionIndex, version string, publishedAt time.Time) bool {
	if index == nil {
		return false
	}
	if index.Versions == nil {
		index.Versions = map[string]RetentionVersion{}
	}
	version = strings.TrimSpace(version)
	publishedAt = publishedAt.UTC()
	if existing, ok := index.Versions[version]; ok {
		if existing.PublishedAt.Equal(publishedAt) {
			return false
		}
		existing.PublishedAt = publishedAt
		index.Versions[version] = existing
		return true
	}
	index.Versions[version] = RetentionVersion{
		PublishedAt:  publishedAt,
		EverDeployed: false,
	}
	return true
}

// ApplyDesiredVersionTransition mirrors a fleet admission into retention.json.
func ApplyDesiredVersionTransition(index *RetentionIndex, fromVersion, toVersion string, policy RetentionPolicy, now time.Time) bool {
	if index == nil {
		return false
	}
	if index.Versions == nil {
		index.Versions = map[string]RetentionVersion{}
	}
	now = now.UTC()
	changed := false

	fromVersion = strings.TrimSpace(fromVersion)
	toVersion = strings.TrimSpace(toVersion)
	if fromVersion != "" && fromVersion != FirstInstallFromVersion {
		entry, ok := index.Versions[fromVersion]
		if !ok {
			entry = RetentionVersion{PublishedAt: now, EverDeployed: true}
		}
		entry.EverDeployed = true
		if entry.FirstDeployedAt == nil || entry.FirstDeployedAt.IsZero() {
			firstDeployed := now
			entry.FirstDeployedAt = &firstDeployed
		}
		deactivated := now
		entry.LastDeactivatedAt = &deactivated
		deadline := now.Add(policy.DeployedRetention)
		entry.DeployableUntil = &deadline
		index.Versions[fromVersion] = entry
		changed = true
	}

	if toVersion != "" {
		entry, ok := index.Versions[toVersion]
		if !ok {
			entry = RetentionVersion{PublishedAt: now}
		}
		entry.EverDeployed = true
		if entry.FirstDeployedAt == nil || entry.FirstDeployedAt.IsZero() {
			firstDeployed := now
			entry.FirstDeployedAt = &firstDeployed
		}
		entry.LastDeactivatedAt = nil
		entry.DeployableUntil = nil
		index.Versions[toVersion] = entry
		changed = true
	}
	return changed
}

// VersionDeploymentChain carries fleet deployment facts from the append-only
// change-request chain. Redeploy deadlines and deploy history are read only
// from change requests; retention.json supplies publish timestamps, unused
// retention, and sticky prune locks.
type VersionDeploymentChain struct {
	Deployed  map[string]struct{}
	Deadlines VersionDeploymentDeadlines
}

// VersionDeploymentDeadlines maps a deactivated version to its fixed redeploy
// deadline recorded as from_version_deployable_until on the change request.
type VersionDeploymentDeadlines map[string]time.Time

// VersionDeploymentChainFromChangeRequests projects deploy history and redeploy
// deadlines from fleet change requests.
func VersionDeploymentChainFromChangeRequests(requests []*core.AppVersionChangeRequest) VersionDeploymentChain {
	return VersionDeploymentChain{
		Deployed:  DeployedVersionsFromChangeRequests(requests),
		Deadlines:   DeployableUntilDeadlinesFromChangeRequests(requests),
	}
}

// DeployableUntilDeadlinesFromChangeRequests projects each outgoing version's
// fixed redeploy deadline from fleet change requests.
func DeployableUntilDeadlinesFromChangeRequests(requests []*core.AppVersionChangeRequest) VersionDeploymentDeadlines {
	deadlines := VersionDeploymentDeadlines{}
	if len(requests) == 0 {
		return deadlines
	}
	sorted := append([]*core.AppVersionChangeRequest(nil), requests...)
	sort.Slice(sorted, func(i, j int) bool {
		left := sorted[i]
		right := sorted[j]
		if left == nil || right == nil {
			return left != nil
		}
		if left.Timestamp.Equal(right.Timestamp) {
			return strings.TrimSpace(left.ID) < strings.TrimSpace(right.ID)
		}
		return left.Timestamp.Before(right.Timestamp)
	})
	for _, request := range sorted {
		if request == nil {
			continue
		}
		fromVersion := strings.TrimSpace(request.FromVersion)
		if fromVersion == "" || fromVersion == FirstInstallFromVersion {
			continue
		}
		if request.FromVersionDeployableUntil == nil || request.FromVersionDeployableUntil.IsZero() {
			continue
		}
		deadline := request.FromVersionDeployableUntil.UTC()
		deadlines[fromVersion] = deadline
	}
	return deadlines
}

// VersionDeploymentState resolves present-day deployability for one published version.
func VersionDeploymentState(version string, desiredVersion string, retention *RetentionIndex, policy RetentionPolicy, now time.Time, chain VersionDeploymentChain) (state string, deployableUntil *time.Time) {
	version = strings.TrimSpace(version)
	desiredVersion = strings.TrimSpace(desiredVersion)
	now = now.UTC()
	if version == desiredVersion && version != "" {
		return DeploymentStateDesired, nil
	}
	if chain.Deployed != nil {
		if _, wasDeployed := chain.Deployed[version]; wasDeployed {
			entry, _ := retentionVersion(retention, version)
			if entry.LockedAt != nil && !entry.LockedAt.IsZero() {
				return DeploymentStateLocked, chain.deployableUntil(version)
			}
			deadline := chain.deployableUntil(version)
			if deadline != nil && now.Before(deadline.UTC()) {
				return DeploymentStateRedeployable, cloneTimePtr(deadline)
			}
			if deadline != nil && !deadline.IsZero() {
				return DeploymentStateLocked, cloneTimePtr(deadline)
			}
			return DeploymentStateLocked, nil
		}
	}
	entry, ok := retentionVersion(retention, version)
	if !ok {
		return DeploymentStateAvailable, nil
	}
	if entry.LockedAt != nil && !entry.LockedAt.IsZero() {
		return DeploymentStateLocked, nil
	}
	unusedDeadline := entry.PublishedAt.UTC().Add(policy.UnusedRetention)
	if now.Before(unusedDeadline) {
		return DeploymentStateAvailable, nil
	}
	return DeploymentStateExpired, nil
}

func (c VersionDeploymentChain) deployableUntil(version string) *time.Time {
	if len(c.Deadlines) == 0 {
		return nil
	}
	deadline, ok := c.Deadlines[strings.TrimSpace(version)]
	if !ok || deadline.IsZero() {
		return nil
	}
	return cloneTimePtr(&deadline)
}

// VersionSelectable reports whether a version may be admitted while holding the install lock.
func VersionSelectable(version, desiredVersion string, retention *RetentionIndex, policy RetentionPolicy, now time.Time, chain VersionDeploymentChain) error {
	state, _ := VersionDeploymentState(version, desiredVersion, retention, policy, now, chain)
	switch state {
	case DeploymentStateAvailable, DeploymentStateRedeployable:
		return nil
	case DeploymentStateDesired:
		return ErrAppVersionAlreadyInstalled
	case DeploymentStateExpired:
		return ErrAppVersionExpired
	case DeploymentStateLocked:
		return ErrAppVersionLocked
	default:
		return ErrAppVersionExpired
	}
}

func retentionVersion(index *RetentionIndex, version string) (RetentionVersion, bool) {
	if index == nil || index.Versions == nil {
		return RetentionVersion{}, false
	}
	entry, ok := index.Versions[strings.TrimSpace(version)]
	return entry, ok
}

// DeployedVersionsFromRetention returns versions marked everDeployed in retention.json.
func DeployedVersionsFromRetention(index *RetentionIndex) map[string]struct{} {
	out := map[string]struct{}{}
	if index == nil || len(index.Versions) == 0 {
		return out
	}
	for version, entry := range index.Versions {
		if entry.EverDeployed {
			out[version] = struct{}{}
		}
	}
	return out
}

// LockExpiredVersions sets lockedAt on historical versions whose deployableUntil passed.
func LockExpiredVersions(index *RetentionIndex, desiredVersion string, now time.Time) bool {
	if index == nil || len(index.Versions) == 0 {
		return false
	}
	now = now.UTC()
	desiredVersion = strings.TrimSpace(desiredVersion)
	changed := false
	for version, entry := range index.Versions {
		if version == desiredVersion {
			continue
		}
		if entry.LockedAt != nil && !entry.LockedAt.IsZero() {
			continue
		}
		if !entry.EverDeployed || entry.DeployableUntil == nil {
			continue
		}
		if !now.Before(entry.DeployableUntil.UTC()) {
			lockedAt := now
			entry.LockedAt = &lockedAt
			index.Versions[version] = entry
			changed = true
		}
	}
	return changed
}

// RemoveRetentionVersion deletes one retention row.
func RemoveRetentionVersion(index *RetentionIndex, version string) bool {
	if index == nil || index.Versions == nil {
		return false
	}
	version = strings.TrimSpace(version)
	if _, ok := index.Versions[version]; !ok {
		return false
	}
	delete(index.Versions, version)
	return true
}

// UnusedVersionExpired reports whether a never-deployed version is past unused retention.
func UnusedVersionExpired(entry RetentionVersion, policy RetentionPolicy, now time.Time) bool {
	if entry.EverDeployed {
		return false
	}
	return !now.UTC().Before(entry.PublishedAt.UTC().Add(policy.UnusedRetention))
}
