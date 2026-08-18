package appregistry

import (
	"errors"
	"fmt"
)

// Writer publishes immutable app registry versions through a RegistryObjectStore.
//
// Commit order is immutable artifacts, then the retention catalog, then index.json
// last. Only the index makes a version discoverable; retention may commit before
// the index during a failed publish, and readers must tolerate that transient gap.
type Writer struct {
	Store           RegistryObjectStore
	CatalogAttempts int
	RetentionPolicy RetentionPolicy
}

// PublishProgress reports publish lifecycle events to callers such as the CLI.
type PublishProgress struct {
	Start  func(format string, args ...any)
	Status func(format string, args ...any)
	Done   func(format string, args ...any)
	Log    func(line string)
}

// PublishRequest is the typed input for Writer.Publish and Writer.Preflight.
type PublishRequest struct {
	Manifest  PublishManifest
	SourceRef string
}

func (w *Writer) catalogAttempts() int {
	if w == nil || w.CatalogAttempts <= 0 {
		return DefaultCatalogUpdateAttempts
	}
	return w.CatalogAttempts
}

func (w *Writer) retentionPolicy() RetentionPolicy {
	if w == nil || (w.RetentionPolicy.UnusedRetention == 0 && w.RetentionPolicy.DeployedRetention == 0) {
		return DefaultRetentionPolicy()
	}
	return w.RetentionPolicy
}

// Preflight validates that immutable objects can be created and the index update would succeed.
func (w *Writer) Preflight(req PublishRequest, progress PublishProgress) error {
	if w == nil || w.Store == nil {
		return fmt.Errorf("registry writer store is required")
	}
	plan := req.Manifest
	objectCount := len(plan.ArtifactObjects) + 2
	if progress.Start != nil {
		progress.Start("Checking %d remote app objects before upload", objectCount)
	}
	if err := w.preflightIndex(plan); err != nil {
		return err
	}
	for _, object := range append([]PublishObject{plan.EntryObject}, plan.ArtifactObjects...) {
		if err := w.preflightImmutableObject(object); err != nil {
			return err
		}
	}
	if progress.Done != nil {
		progress.Done("Checked %d remote app objects", objectCount)
	}
	return nil
}

// Publish uploads immutable objects, updates the retention catalog, and commits
// the index last so no fallible registry write occurs after index commit.
func (w *Writer) Publish(req PublishRequest, progress PublishProgress) (PublishResult, error) {
	result := PublishResult{
		Retention: CatalogWriteOutcomeNotAttempted,
		Index:     CatalogWriteOutcomeNotAttempted,
	}
	immutable, err := w.uploadImmutableObjects(req, progress)
	result.Artifacts = immutable.Artifacts
	result.Entry = immutable.Entry
	if err != nil {
		return result, err
	}
	if err := w.adoptStoredEntryPublishedAt(&req.Manifest, immutable.Entry); err != nil {
		return result, err
	}

	retentionOutcome, err := w.uploadRetention(req, progress)
	result.Retention = retentionOutcome
	if err != nil {
		return result, err
	}

	indexOutcome, err := w.uploadIndex(req, progress)
	result.Index = indexOutcome
	if err != nil {
		return result, err
	}
	return result, nil
}

func (w *Writer) preflightIndex(plan PublishManifest) error {
	_, existing, err := w.Store.ReadObject(plan.IndexObject.StorageURL)
	if err != nil {
		return err
	}
	index, err := decodeIndexOrEmpty(existing)
	if err != nil {
		return fmt.Errorf("decode existing app index: %w", err)
	}
	metadataPath := AppVersionEntryPath(plan.AppName, plan.Version)
	_, _, err = UpsertAppIndex(index, plan.Entry, metadataPath, plan.DisplayName, plan.Description)
	return err
}

func (w *Writer) preflightImmutableObject(object PublishObject) error {
	matches, err := w.immutableObjectMatchesExisting(object)
	if err != nil {
		return err
	}
	if matches {
		return nil
	}
	described, err := w.Store.DescribeObject(object.StorageURL)
	if err != nil {
		return err
	}
	if described.Generation == 0 {
		return nil
	}
	return immutableObjectConflictError(object)
}

func immutableObjectConflictError(object PublishObject) error {
	if object.Kind == PublishObjectKindEntry {
		return fmt.Errorf("%s: %w; %s", object.StorageURL, ErrRegistryEntryConflict, RepublishCorruptObjectGuidance)
	}
	return fmt.Errorf("%s: %w; %s", object.StorageURL, ErrObjectPreconditionFailed, RepublishCorruptObjectGuidance)
}

func (w *Writer) uploadImmutableObjects(req PublishRequest, progress PublishProgress) (ImmutablePublishOutcome, error) {
	plan := req.Manifest
	objectCount := len(plan.ArtifactObjects) + 1
	if progress.Start != nil {
		progress.Start("Uploading %d immutable app objects", objectCount)
	}
	stdoutLines := make([]string, 0, objectCount)
	outcome := ImmutablePublishOutcome{
		Artifacts: make([]ImmutableObjectOutcome, 0, len(plan.ArtifactObjects)),
	}
	for _, object := range plan.ArtifactObjects {
		objectOutcome, line, err := w.uploadImmutableObjectIfNeeded(object, req.SourceRef)
		if err != nil {
			return outcome, err
		}
		outcome.Artifacts = append(outcome.Artifacts, objectOutcome)
		if line != "" {
			stdoutLines = append(stdoutLines, line)
		}
	}
	entryOutcome, line, err := w.uploadImmutableObjectIfNeeded(plan.EntryObject, req.SourceRef)
	if err != nil {
		return outcome, err
	}
	outcome.Entry = entryOutcome
	if line != "" {
		stdoutLines = append(stdoutLines, line)
	}
	if progress.Done != nil {
		progress.Done("Processed %d immutable app objects", objectCount)
	}
	for _, line := range stdoutLines {
		if progress.Log != nil {
			progress.Log(line)
		}
	}
	return outcome, nil
}

func (w *Writer) uploadImmutableObjectIfNeeded(object PublishObject, sourceRef string) (ImmutableObjectOutcome, string, error) {
	matches, err := w.immutableObjectMatchesExisting(object)
	if err != nil {
		return ImmutableObjectOutcome{}, "", err
	}
	if matches {
		return ImmutableObjectOutcome{
			StorageURL: object.StorageURL,
			Outcome:    ObjectWriteOutcomeSkipped,
		}, fmt.Sprintf("skipped existing %s", object.StorageURL), nil
	}
	if err := w.Store.WriteImmutableObject(WriteImmutableObjectInput{
		LocalPath:  object.LocalPath,
		StorageURL: object.StorageURL,
		SourceRef:  sourceRef,
		SHA256:     object.SHA256,
	}); err != nil {
		if errors.Is(err, ErrObjectPreconditionFailed) {
			matches, matchErr := w.immutableObjectMatchesExisting(object)
			if matchErr == nil && matches {
				return ImmutableObjectOutcome{
					StorageURL: object.StorageURL,
					Outcome:    ObjectWriteOutcomeSkipped,
				}, fmt.Sprintf("skipped existing %s", object.StorageURL), nil
			}
			return ImmutableObjectOutcome{}, "", immutableObjectConflictError(object)
		}
		return ImmutableObjectOutcome{}, "", err
	}
	return ImmutableObjectOutcome{
		StorageURL: object.StorageURL,
		Outcome:    ObjectWriteOutcomeUploaded,
	}, fmt.Sprintf("uploaded %s", object.StorageURL), nil
}

func (w *Writer) immutableObjectMatchesExisting(object PublishObject) (bool, error) {
	described, err := w.Store.DescribeObject(object.StorageURL)
	if err != nil {
		return false, err
	}
	if described.Generation == 0 {
		return false, nil
	}
	if object.SHA256 != "" && described.SHA256 == object.SHA256 {
		return true, nil
	}
	if object.Kind == PublishObjectKindEntry && object.LocalPath != "" {
		_, existing, err := w.Store.ReadObject(object.StorageURL)
		if err != nil {
			return false, err
		}
		return EntryFileEquivalentIgnoringPublishedAt(object.LocalPath, existing)
	}
	return false, nil
}

func (w *Writer) adoptStoredEntryPublishedAt(plan *PublishManifest, entryOutcome ImmutableObjectOutcome) error {
	if w == nil || w.Store == nil || plan == nil {
		return nil
	}
	if entryOutcome.Outcome != ObjectWriteOutcomeSkipped {
		return nil
	}
	storageURL := plan.EntryObject.StorageURL
	described, err := w.Store.DescribeObject(storageURL)
	if err != nil {
		return err
	}
	if described.Generation == 0 {
		return nil
	}
	_, data, err := w.Store.ReadObject(storageURL)
	if err != nil {
		return err
	}
	stored, err := DecodeEntry(data)
	if err != nil {
		return err
	}
	if stored.PublishedAt.IsZero() {
		return nil
	}
	plan.Entry.PublishedAt = stored.PublishedAt.UTC()
	return nil
}

func (w *Writer) uploadIndex(req PublishRequest, progress PublishProgress) (CatalogWriteOutcome, error) {
	plan := req.Manifest
	indexPath := plan.IndexObject.StorageURL
	metadataPath := AppVersionEntryPath(plan.AppName, plan.Version)
	return compareAndSwapCatalog(w.Store, indexPath, req.SourceRef, w.catalogAttempts(), progress,
		decodeIndexOrEmpty,
		func(index *Index) (*Index, bool, error) {
			return UpsertAppIndex(index, plan.Entry, metadataPath, plan.DisplayName, plan.Description)
		},
		catalogCASLabels{
			start:         "Updating app registry index",
			doneUnchanged: "App registry index unchanged",
			doneUpdated:   "Updated app registry index",
			retryStatus: func(attempt, max int) string {
				return fmt.Sprintf("App registry index changed concurrently; retrying attempt %d/%d", attempt, max)
			},
			retryConflict: "App registry index update conflict; retrying",
			logUnchanged: func(storageURL string) string {
				return fmt.Sprintf("skipped unchanged index for %s %s", plan.AppName, plan.Version)
			},
			logUpdated: func(storageURL string) string {
				return fmt.Sprintf("updated %s", storageURL)
			},
		},
	)
}

func (w *Writer) uploadRetention(req PublishRequest, progress PublishProgress) (CatalogWriteOutcome, error) {
	plan := req.Manifest
	retentionPath := RetentionStorageURL(plan.IndexObject.StorageURL, plan.AppName)
	publishedAt := plan.Entry.PublishedAt
	policy := w.retentionPolicy()
	return compareAndSwapCatalog(w.Store, retentionPath, req.SourceRef, w.catalogAttempts(), progress,
		decodeRetentionIndexOrEmpty,
		func(index *RetentionIndex) (*RetentionIndex, bool, error) {
			changed := UpsertPublishedRetention(index, plan.Version, publishedAt, policy)
			return index, changed, nil
		},
		catalogCASLabels{
			start:         "Updating app registry retention catalog",
			doneUnchanged: "App registry retention catalog unchanged",
			doneUpdated:   "Updated app registry retention catalog",
			retryStatus: func(attempt, max int) string {
				return fmt.Sprintf("App registry retention catalog changed concurrently; retrying attempt %d/%d", attempt, max)
			},
			logUpdated: func(storageURL string) string {
				return fmt.Sprintf("updated %s", storageURL)
			},
		},
	)
}

func decodeIndexOrEmpty(existing []byte) (*Index, error) {
	if len(existing) == 0 {
		return NewEmptyIndex(), nil
	}
	return DecodeIndex(existing)
}

func decodeRetentionIndexOrEmpty(existing []byte) (*RetentionIndex, error) {
	if len(existing) == 0 {
		return NewEmptyRetentionIndex(), nil
	}
	return DecodeRetentionIndex(existing)
}

func isCatalogPreconditionFailed(err error) bool {
	if errors.Is(err, ErrObjectPreconditionFailed) {
		return true
	}
	return gcloudPreconditionFailed(err)
}

// CatalogPreconditionFailed reports whether err is a generation/precondition conflict.
func CatalogPreconditionFailed(err error) bool {
	return isCatalogPreconditionFailed(err)
}
