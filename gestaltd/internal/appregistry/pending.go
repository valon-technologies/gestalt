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
	PendingIndexSchemaVersion = 1
	FailedIndexSchemaVersion  = 1

	PendingFileName = "pending.json"
	FailedFileName  = "failed.json"

	PendingPhasePublishing = "publishing"

	FailedReasonWorkflowFailed = "workflow_failed"
	FailedReasonStale          = "stale"

	PendingStaleAfter = 30 * time.Minute
	FailedRetainFor   = 30 * 24 * time.Hour
)

type PendingIndex struct {
	SchemaVersion int                       `json:"schemaVersion"`
	App           string                    `json:"app"`
	Pending       map[string]PendingVersion `json:"pending"`
}

type PendingVersion struct {
	Version     string       `json:"version"`
	SourceRef   string       `json:"sourceRef"`
	Repository  string       `json:"repository,omitempty"`
	StartedAt   time.Time    `json:"startedAt"`
	UpdatedAt   time.Time    `json:"updatedAt"`
	Phase       string       `json:"phase"`
	Publication *Publication `json:"publication,omitempty"`
}

type FailedIndex struct {
	SchemaVersion int                      `json:"schemaVersion"`
	App           string                   `json:"app"`
	Failed        map[string]FailedVersion `json:"failed"`
}

type FailedVersion struct {
	Version     string       `json:"version"`
	SourceRef   string       `json:"sourceRef"`
	Repository  string       `json:"repository,omitempty"`
	StartedAt   time.Time    `json:"startedAt"`
	FailedAt    time.Time    `json:"failedAt"`
	Reason      string       `json:"reason"`
	Publication *Publication `json:"publication,omitempty"`
}

func AppPendingPath(appName string) string {
	return path.Join("apps", appName, PendingFileName)
}

func AppFailedPath(appName string) string {
	return path.Join("apps", appName, FailedFileName)
}

func NewEmptyPendingIndex(appName string) *PendingIndex {
	return &PendingIndex{
		SchemaVersion: PendingIndexSchemaVersion,
		App:           strings.TrimSpace(appName),
		Pending:       map[string]PendingVersion{},
	}
}

func NewEmptyFailedIndex(appName string) *FailedIndex {
	return &FailedIndex{
		SchemaVersion: FailedIndexSchemaVersion,
		App:           strings.TrimSpace(appName),
		Failed:        map[string]FailedVersion{},
	}
}

func DecodePendingIndex(data []byte) (*PendingIndex, error) {
	var index PendingIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("decode pending index: %w", err)
	}
	if err := validatePendingIndex(&index); err != nil {
		return nil, err
	}
	return &index, nil
}

func DecodeFailedIndex(data []byte) (*FailedIndex, error) {
	var index FailedIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("decode failed index: %w", err)
	}
	if err := validateFailedIndex(&index); err != nil {
		return nil, err
	}
	return &index, nil
}

func validatePendingIndex(index *PendingIndex) error {
	if index == nil {
		return fmt.Errorf("pending index is required")
	}
	if index.SchemaVersion != PendingIndexSchemaVersion {
		return fmt.Errorf("unsupported pending index schema version %d", index.SchemaVersion)
	}
	if strings.TrimSpace(index.App) == "" {
		return fmt.Errorf("pending index app is required")
	}
	if index.Pending == nil {
		return fmt.Errorf("pending index pending map is required")
	}
	for version, entry := range index.Pending {
		if err := validatePendingVersion(index.App, version, entry); err != nil {
			return fmt.Errorf("pending index version %q: %w", version, err)
		}
	}
	return nil
}

func validateFailedIndex(index *FailedIndex) error {
	if index == nil {
		return fmt.Errorf("failed index is required")
	}
	if index.SchemaVersion != FailedIndexSchemaVersion {
		return fmt.Errorf("unsupported failed index schema version %d", index.SchemaVersion)
	}
	if strings.TrimSpace(index.App) == "" {
		return fmt.Errorf("failed index app is required")
	}
	if index.Failed == nil {
		return fmt.Errorf("failed index failed map is required")
	}
	for version, entry := range index.Failed {
		if err := validateFailedVersion(index.App, version, entry); err != nil {
			return fmt.Errorf("failed index version %q: %w", version, err)
		}
	}
	return nil
}

func validatePendingVersion(appName, mapKey string, entry PendingVersion) error {
	version := strings.TrimSpace(entry.Version)
	if version == "" {
		return fmt.Errorf("version is required")
	}
	if mapKey != version {
		return fmt.Errorf("map key %q does not match version field %q", mapKey, version)
	}
	if err := source.ValidateVersion(version); err != nil {
		return fmt.Errorf("version: %w", err)
	}
	if err := validateSourceRef(entry.SourceRef); err != nil {
		return fmt.Errorf("sourceRef: %w", err)
	}
	if repository := strings.TrimSpace(entry.Repository); repository != "" {
		if err := validateEntryRepository(repository, appName); err != nil {
			return fmt.Errorf("repository: %w", err)
		}
	}
	if entry.StartedAt.IsZero() {
		return fmt.Errorf("startedAt is required")
	}
	if entry.UpdatedAt.IsZero() {
		return fmt.Errorf("updatedAt is required")
	}
	if entry.Phase != PendingPhasePublishing {
		return fmt.Errorf("phase must be %q", PendingPhasePublishing)
	}
	if err := validatePublication(entry.Publication); err != nil {
		return fmt.Errorf("publication: %w", err)
	}
	return nil
}

func validateFailedVersion(appName, mapKey string, entry FailedVersion) error {
	version := strings.TrimSpace(entry.Version)
	if version == "" {
		return fmt.Errorf("version is required")
	}
	if mapKey != version {
		return fmt.Errorf("map key %q does not match version field %q", mapKey, version)
	}
	if err := source.ValidateVersion(version); err != nil {
		return fmt.Errorf("version: %w", err)
	}
	if err := validateSourceRef(entry.SourceRef); err != nil {
		return fmt.Errorf("sourceRef: %w", err)
	}
	if repository := strings.TrimSpace(entry.Repository); repository != "" {
		if err := validateEntryRepository(repository, appName); err != nil {
			return fmt.Errorf("repository: %w", err)
		}
	}
	if entry.StartedAt.IsZero() {
		return fmt.Errorf("startedAt is required")
	}
	if entry.FailedAt.IsZero() {
		return fmt.Errorf("failedAt is required")
	}
	switch entry.Reason {
	case FailedReasonWorkflowFailed, FailedReasonStale:
	default:
		return fmt.Errorf("reason must be %q or %q", FailedReasonWorkflowFailed, FailedReasonStale)
	}
	if err := validatePublication(entry.Publication); err != nil {
		return fmt.Errorf("publication: %w", err)
	}
	return nil
}

func IndexContainsVersion(index *Index, appName, version string) bool {
	if index == nil || len(index.Apps) == 0 {
		return false
	}
	appVersions, ok := index.Apps[strings.TrimSpace(appName)]
	if !ok {
		return false
	}
	_, ok = appVersions.Versions[strings.TrimSpace(version)]
	return ok
}

// PrunePendingIndex moves stale pending entries to failed and drops entries for
// versions already published. The second return value reports whether failed
// changed.
func PrunePendingIndex(pending *PendingIndex, failed *FailedIndex, published *Index, now time.Time) (bool, bool) {
	if pending == nil || len(pending.Pending) == 0 {
		return false, false
	}
	if failed == nil {
		failed = NewEmptyFailedIndex(pending.App)
	}
	if failed.Failed == nil {
		failed.Failed = map[string]FailedVersion{}
	}
	now = now.UTC()
	pendingChanged := false
	failedChanged := false
	for version, entry := range pending.Pending {
		if IndexContainsVersion(published, pending.App, version) {
			delete(pending.Pending, version)
			pendingChanged = true
			continue
		}
		if now.Sub(entry.StartedAt.UTC()) > PendingStaleAfter {
			failed.Failed[version] = failedVersionFromPending(entry, now, FailedReasonStale)
			delete(pending.Pending, version)
			pendingChanged = true
			failedChanged = true
		}
	}
	return pendingChanged, failedChanged
}

// PruneFailedIndex removes old or already-published failed entries.
func PruneFailedIndex(failed *FailedIndex, published *Index, now time.Time) bool {
	if failed == nil || len(failed.Failed) == 0 {
		return false
	}
	now = now.UTC()
	changed := false
	for version, entry := range failed.Failed {
		if IndexContainsVersion(published, failed.App, version) {
			delete(failed.Failed, version)
			changed = true
			continue
		}
		if now.Sub(entry.FailedAt.UTC()) > FailedRetainFor {
			delete(failed.Failed, version)
			changed = true
		}
	}
	return changed
}

func failedVersionFromPending(entry PendingVersion, failedAt time.Time, reason string) FailedVersion {
	return FailedVersion{
		Version:     entry.Version,
		SourceRef:   entry.SourceRef,
		Repository:  entry.Repository,
		StartedAt:   entry.StartedAt.UTC(),
		FailedAt:    failedAt.UTC(),
		Reason:      reason,
		Publication: clonePublication(entry.Publication),
	}
}

// RemoveFailedVersion deletes one failed entry. The second return value reports
// whether the version was present.
func RemoveFailedVersion(index *FailedIndex, version string) bool {
	if index == nil || index.Failed == nil {
		return false
	}
	version = strings.TrimSpace(version)
	if _, ok := index.Failed[version]; !ok {
		return false
	}
	delete(index.Failed, version)
	return true
}

// UpsertPendingVersion inserts or updates one pending version. startedAt is
// preserved when the version already exists.
func UpsertPendingVersion(index *PendingIndex, appName string, version PendingVersion, now time.Time) (*PendingIndex, bool) {
	if index == nil {
		index = NewEmptyPendingIndex(appName)
	}
	if index.Pending == nil {
		index.Pending = map[string]PendingVersion{}
	}
	now = now.UTC()
	version.Version = strings.TrimSpace(version.Version)
	version.SourceRef = strings.ToLower(strings.TrimSpace(version.SourceRef))
	version.Repository = strings.TrimSpace(version.Repository)
	version.Phase = PendingPhasePublishing
	version.UpdatedAt = now
	if existing, ok := index.Pending[version.Version]; ok {
		version.StartedAt = existing.StartedAt.UTC()
	} else {
		version.StartedAt = now
	}
	version.Publication = clonePublication(version.Publication)
	index.Pending[version.Version] = version
	return index, true
}

// RemovePendingVersion deletes one pending version. The second return value
// reports whether the version was present.
func RemovePendingVersion(index *PendingIndex, version string) (*PendingVersion, bool) {
	if index == nil || index.Pending == nil {
		return nil, false
	}
	version = strings.TrimSpace(version)
	entry, ok := index.Pending[version]
	if !ok {
		return nil, false
	}
	delete(index.Pending, version)
	return &entry, true
}

// RecordFailedVersion writes a failed entry from a removed pending version.
func RecordFailedVersion(index *FailedIndex, appName string, pending PendingVersion, failedAt time.Time, reason string) (*FailedIndex, bool) {
	if index == nil {
		index = NewEmptyFailedIndex(appName)
	}
	if index.Failed == nil {
		index.Failed = map[string]FailedVersion{}
	}
	version := strings.TrimSpace(pending.Version)
	if _, exists := index.Failed[version]; exists {
		return index, false
	}
	index.Failed[version] = failedVersionFromPending(pending, failedAt, reason)
	return index, true
}

// PublishStartedAtFromPending returns the pending startedAt timestamp for a
// version when present.
func PublishStartedAtFromPending(index *PendingIndex, version string) (time.Time, bool) {
	if index == nil || index.Pending == nil {
		return time.Time{}, false
	}
	pending, ok := index.Pending[strings.TrimSpace(version)]
	if !ok || pending.StartedAt.IsZero() {
		return time.Time{}, false
	}
	return pending.StartedAt.UTC(), true
}
