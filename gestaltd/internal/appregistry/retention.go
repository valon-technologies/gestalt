package appregistry

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

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
	PublishedAt  time.Time  `json:"publishedAt"`
	EverDeployed bool       `json:"everDeployed"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
}

func (v *RetentionVersion) UnmarshalJSON(data []byte) error {
	type retentionVersionJSON struct {
		PublishedAt     time.Time  `json:"publishedAt"`
		EverDeployed    bool       `json:"everDeployed"`
		ExpiresAt       *time.Time `json:"expiresAt,omitempty"`
		DeployableUntil *time.Time `json:"deployableUntil,omitempty"`
	}
	var raw retentionVersionJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode retention version: %w", err)
	}
	v.PublishedAt = raw.PublishedAt
	v.EverDeployed = raw.EverDeployed
	v.ExpiresAt = raw.ExpiresAt
	if v.ExpiresAt == nil && raw.DeployableUntil != nil && !raw.DeployableUntil.IsZero() {
		v.ExpiresAt = cloneTimePtr(raw.DeployableUntil)
	}
	return nil
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
	if entry.ExpiresAt != nil && entry.ExpiresAt.IsZero() {
		return fmt.Errorf("expiresAt must not be zero")
	}
	return nil
}

// UpsertPublishedRetention records a newly published version in the retention overlay.
func UpsertPublishedRetention(index *RetentionIndex, version string, publishedAt time.Time, policy RetentionPolicy) bool {
	if index == nil {
		return false
	}
	if index.Versions == nil {
		index.Versions = map[string]RetentionVersion{}
	}
	version = strings.TrimSpace(version)
	publishedAt = publishedAt.UTC()
	expiresAt := publishedAt.Add(policy.UnusedRetention)
	if existing, ok := index.Versions[version]; ok {
		if existing.PublishedAt.Equal(publishedAt) && timePtrEqual(existing.ExpiresAt, &expiresAt) {
			return false
		}
		existing.PublishedAt = publishedAt
		if !existing.EverDeployed {
			existing.ExpiresAt = cloneTimePtr(&expiresAt)
		}
		index.Versions[version] = existing
		return true
	}
	index.Versions[version] = RetentionVersion{
		PublishedAt:  publishedAt,
		EverDeployed: false,
		ExpiresAt:    cloneTimePtr(&expiresAt),
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
		deadline := now.Add(policy.DeployedRetention)
		entry.ExpiresAt = cloneTimePtr(&deadline)
		index.Versions[fromVersion] = entry
		changed = true
	}

	if toVersion != "" {
		entry, ok := index.Versions[toVersion]
		if !ok {
			entry = RetentionVersion{PublishedAt: now}
		}
		entry.EverDeployed = true
		entry.ExpiresAt = nil
		index.Versions[toVersion] = entry
		changed = true
	}
	return changed
}

// VersionDeploymentState resolves present-day deployability for one published version.
func VersionDeploymentState(version string, desiredVersion string, retention *RetentionIndex, policy RetentionPolicy, now time.Time) (state string, deployableUntil *time.Time) {
	version = strings.TrimSpace(version)
	desiredVersion = strings.TrimSpace(desiredVersion)
	now = now.UTC()
	if version == desiredVersion && version != "" {
		return DeploymentStateDesired, nil
	}
	entry, ok := retentionVersion(retention, version)
	if !ok {
		return DeploymentStateAvailable, nil
	}
	if entry.EverDeployed {
		if entry.ExpiresAt == nil || entry.ExpiresAt.IsZero() {
			return DeploymentStateRedeployable, nil
		}
		if now.Before(entry.ExpiresAt.UTC()) {
			return DeploymentStateRedeployable, cloneTimePtr(entry.ExpiresAt)
		}
		return DeploymentStateLocked, cloneTimePtr(entry.ExpiresAt)
	}
	if entry.ExpiresAt != nil && !entry.ExpiresAt.IsZero() {
		if now.Before(entry.ExpiresAt.UTC()) {
			return DeploymentStateAvailable, nil
		}
		return DeploymentStateExpired, nil
	}
	unusedDeadline := entry.PublishedAt.UTC().Add(policy.UnusedRetention)
	if now.Before(unusedDeadline) {
		return DeploymentStateAvailable, nil
	}
	return DeploymentStateExpired, nil
}

// VersionSelectable reports whether a version may be admitted while holding the install lock.
func VersionSelectable(version, desiredVersion string, retention *RetentionIndex, policy RetentionPolicy, now time.Time) error {
	state, _ := VersionDeploymentState(version, desiredVersion, retention, policy, now)
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

// RetentionExpired reports whether a version's expiresAt deadline has passed.
// Missing expiresAt is treated as not expired (lean keep).
func RetentionExpired(entry RetentionVersion, now time.Time) bool {
	if entry.ExpiresAt == nil || entry.ExpiresAt.IsZero() {
		return false
	}
	return !now.UTC().Before(entry.ExpiresAt.UTC())
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

// UnusedVersionExpired reports whether a never-deployed version is past its expiry.
func UnusedVersionExpired(entry RetentionVersion, policy RetentionPolicy, now time.Time) bool {
	if entry.EverDeployed {
		return false
	}
	if entry.ExpiresAt != nil && !entry.ExpiresAt.IsZero() {
		return RetentionExpired(entry, now)
	}
	return !now.UTC().Before(entry.PublishedAt.UTC().Add(policy.UnusedRetention))
}

func timePtrEqual(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.UTC().Equal(b.UTC())
}
