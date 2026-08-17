package appregistry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

const defaultCatalogUpdateAttempts = 5

// PublishProgress reports publish lifecycle events to callers such as the CLI.
type PublishProgress struct {
	Start  func(format string, args ...any)
	Status func(format string, args ...any)
	Done   func(format string, args ...any)
	Log    func(line string)
}

// Writer publishes immutable app registry versions through a RegistryObjectStore.
type Writer struct {
	Store           RegistryObjectStore
	CatalogAttempts int
	RetentionPolicy RetentionPolicy
}

// PublishRequest is the typed input for Writer.Publish and Writer.Preflight.
type PublishRequest struct {
	Manifest  PublishManifest
	SourceRef string
}

func (w *Writer) catalogAttempts() int {
	if w == nil || w.CatalogAttempts <= 0 {
		return defaultCatalogUpdateAttempts
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
	for _, object := range append([]PublishObject{plan.EntryObject}, plan.ArtifactObjects...) {
		if err := w.preflightImmutableObject(object); err != nil {
			return err
		}
	}
	winningEntry, err := w.resolveWinningEntry(plan)
	if err != nil {
		return err
	}
	if err := w.preflightIndex(plan, winningEntry); err != nil {
		return err
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
	if err != nil {
		return result, err
	}
	result.Artifacts = immutable.Artifacts
	result.Entry = immutable.Entry

	winningEntry, err := w.resolveWinningEntry(req.Manifest)
	if err != nil {
		return result, err
	}

	retentionOutcome, err := w.uploadRetention(req, winningEntry, progress)
	result.Retention = retentionOutcome
	if err != nil {
		return result, err
	}

	indexOutcome, err := w.uploadIndex(req, winningEntry, progress)
	result.Index = indexOutcome
	if err != nil {
		return result, err
	}
	return result, nil
}

func (w *Writer) preflightIndex(plan PublishManifest, entry Entry) error {
	_, existing, err := w.Store.ReadObject(plan.IndexObject.StorageURL)
	if err != nil {
		return err
	}
	index, err := decodeIndexOrEmpty(existing)
	if err != nil {
		return fmt.Errorf("decode existing app index: %w", err)
	}
	metadataPath := AppVersionEntryPath(plan.AppName, plan.Version)
	_, _, err = UpsertAppIndex(index, entry, metadataPath, plan.DisplayName, plan.Description)
	return err
}

func (w *Writer) resolveWinningEntry(plan PublishManifest) (Entry, error) {
	_, existing, err := w.Store.ReadObject(plan.EntryObject.StorageURL)
	if err != nil {
		return Entry{}, err
	}
	if len(existing) == 0 {
		return plan.Entry, nil
	}
	winning, err := DecodeEntry(existing)
	if err != nil {
		return Entry{}, fmt.Errorf("decode winning entry: %w", err)
	}
	if !EntriesEqualIgnoringPublishedAt(plan.Entry, *winning) {
		return Entry{}, fmt.Errorf("%s: %w; %s", plan.EntryObject.StorageURL, ErrRegistryEntryConflict, RepublishCorruptObjectGuidance)
	}
	return *winning, nil
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
	if object.Kind == PublishObjectKindEntry {
		skipped, err := w.reconcileExistingEntryObject(object)
		if err != nil {
			return err
		}
		if skipped {
			return nil
		}
		return fmt.Errorf("%s: %w; %s", object.StorageURL, ErrRegistryEntryConflict, RepublishCorruptObjectGuidance)
	}
	return fmt.Errorf("%s already exists; %s", object.StorageURL, RepublishCorruptObjectGuidance)
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
			return ImmutablePublishOutcome{}, err
		}
		outcome.Artifacts = append(outcome.Artifacts, objectOutcome)
		if line != "" {
			stdoutLines = append(stdoutLines, line)
		}
	}
	entryOutcome, line, err := w.uploadImmutableObjectIfNeeded(plan.EntryObject, req.SourceRef)
	if err != nil {
		return ImmutablePublishOutcome{}, err
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
	described, err := w.Store.DescribeObject(object.StorageURL)
	if err != nil {
		return ImmutableObjectOutcome{}, "", err
	}
	if described.Generation != 0 && object.Kind == PublishObjectKindEntry {
		skipped, err := w.reconcileExistingEntryObject(object)
		if err != nil {
			return ImmutableObjectOutcome{}, "", err
		}
		if skipped {
			outcome, line := entryObjectSkippedOutcome(object)
			return outcome, line, nil
		}
		return ImmutableObjectOutcome{}, "", fmt.Errorf("%s: %w; %s", object.StorageURL, ErrRegistryEntryConflict, RepublishCorruptObjectGuidance)
	}
	if err := w.Store.WriteImmutableObject(WriteImmutableObjectInput{
		LocalPath:  object.LocalPath,
		StorageURL: object.StorageURL,
		SourceRef:  sourceRef,
		SHA256:     object.SHA256,
	}); err != nil {
		if object.Kind == PublishObjectKindEntry && isObjectGenerationPreconditionFailed(err) {
			skipped, reconcileErr := w.reconcileExistingEntryObject(object)
			if reconcileErr != nil {
				return ImmutableObjectOutcome{}, "", reconcileErr
			}
			if skipped {
				outcome, line := entryObjectSkippedOutcome(object)
				return outcome, line, nil
			}
			return ImmutableObjectOutcome{}, "", fmt.Errorf("%s: %w; %s", object.StorageURL, ErrRegistryEntryConflict, RepublishCorruptObjectGuidance)
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
		return w.entryObjectMatchesExistingBytes(object)
	}
	return false, nil
}

func (w *Writer) entryObjectMatchesExistingBytes(object PublishObject) (bool, error) {
	if object.Kind != PublishObjectKindEntry || object.LocalPath == "" {
		return false, nil
	}
	_, existing, err := w.Store.ReadObject(object.StorageURL)
	if err != nil {
		return false, err
	}
	if len(existing) == 0 {
		return false, nil
	}
	return EntryFileEquivalentIgnoringPublishedAt(object.LocalPath, existing)
}

func entryObjectSkippedOutcome(object PublishObject) (ImmutableObjectOutcome, string) {
	return ImmutableObjectOutcome{
		StorageURL: object.StorageURL,
		Outcome:    ObjectWriteOutcomeSkipped,
	}, fmt.Sprintf("skipped existing %s", object.StorageURL)
}

func (w *Writer) reconcileExistingEntryObject(object PublishObject) (bool, error) {
	matches, err := w.entryObjectMatchesExistingBytes(object)
	if err != nil {
		return false, err
	}
	return matches, nil
}

func (w *Writer) uploadIndex(req PublishRequest, entry Entry, progress PublishProgress) (CatalogWriteOutcome, error) {
	plan := req.Manifest
	indexPath := plan.IndexObject.StorageURL
	if progress.Start != nil {
		progress.Start("Updating app registry index")
	}
	for attempt := 1; attempt <= w.catalogAttempts(); attempt++ {
		if attempt > 1 && progress.Status != nil {
			progress.Status("App registry index changed concurrently; retrying attempt %d/%d", attempt, w.catalogAttempts())
		}
		generation, existing, err := w.Store.ReadObject(indexPath)
		if err != nil {
			return CatalogWriteOutcomeNotAttempted, err
		}
		index, err := decodeIndexOrEmpty(existing)
		if err != nil {
			return CatalogWriteOutcomeNotAttempted, fmt.Errorf("decode existing app index: %w", err)
		}
		metadataPath := AppVersionEntryPath(plan.AppName, plan.Version)
		updated, changed, err := UpsertAppIndex(index, entry, metadataPath, plan.DisplayName, plan.Description)
		if err != nil {
			return CatalogWriteOutcomeNotAttempted, err
		}
		if !changed {
			if progress.Done != nil {
				progress.Done("App registry index unchanged")
			}
			if progress.Log != nil {
				progress.Log(fmt.Sprintf("skipped unchanged index for %s %s", plan.AppName, plan.Version))
			}
			return CatalogWriteOutcomeUnchanged, nil
		}
		data, err := json.MarshalIndent(updated, "", "  ")
		if err != nil {
			return CatalogWriteOutcomeNotAttempted, err
		}
		tmpPath, err := WriteTempJSON("gestalt-app-index-*", append(data, '\n'))
		if err != nil {
			return CatalogWriteOutcomeNotAttempted, err
		}
		err = w.Store.WriteCatalogObject(WriteCatalogObjectInput{
			LocalPath:  tmpPath,
			StorageURL: indexPath,
			SourceRef:  req.SourceRef,
			Generation: generation,
		})
		_ = os.Remove(tmpPath)
		if err != nil {
			if isObjectGenerationPreconditionFailed(err) && attempt < w.catalogAttempts() {
				if progress.Status != nil {
					progress.Status("App registry index update conflict; retrying")
				}
				continue
			}
			return CatalogWriteOutcomeNotAttempted, err
		}
		if progress.Done != nil {
			progress.Done("Updated app registry index")
		}
		if progress.Log != nil {
			progress.Log(fmt.Sprintf("updated %s", indexPath))
		}
		return CatalogWriteOutcomeUpdated, nil
	}
	return CatalogWriteOutcomeNotAttempted, fmt.Errorf("update %s: exceeded retry limit after concurrent index updates", indexPath)
}

func (w *Writer) uploadRetention(req PublishRequest, entry Entry, progress PublishProgress) (CatalogWriteOutcome, error) {
	plan := req.Manifest
	retentionPath := RetentionStorageURL(plan.IndexObject.StorageURL, plan.AppName)
	publishedAt := entry.PublishedAt
	if progress.Start != nil {
		progress.Start("Updating app registry retention catalog")
	}
	for attempt := 1; attempt <= w.catalogAttempts(); attempt++ {
		if attempt > 1 && progress.Status != nil {
			progress.Status("App registry retention catalog changed concurrently; retrying attempt %d/%d", attempt, w.catalogAttempts())
		}
		generation, existing, err := w.Store.ReadObject(retentionPath)
		if err != nil {
			return CatalogWriteOutcomeNotAttempted, err
		}
		index, err := decodeRetentionIndexOrEmpty(existing)
		if err != nil {
			return CatalogWriteOutcomeNotAttempted, fmt.Errorf("decode existing retention index: %w", err)
		}
		if !UpsertPublishedRetention(index, plan.Version, publishedAt, w.retentionPolicy()) {
			if progress.Done != nil {
				progress.Done("App registry retention catalog unchanged")
			}
			return CatalogWriteOutcomeUnchanged, nil
		}
		data, err := json.MarshalIndent(index, "", "  ")
		if err != nil {
			return CatalogWriteOutcomeNotAttempted, err
		}
		tmpPath, err := WriteTempJSON("gestalt-app-retention-*", append(data, '\n'))
		if err != nil {
			return CatalogWriteOutcomeNotAttempted, err
		}
		err = w.Store.WriteCatalogObject(WriteCatalogObjectInput{
			LocalPath:  tmpPath,
			StorageURL: retentionPath,
			SourceRef:  req.SourceRef,
			Generation: generation,
		})
		_ = os.Remove(tmpPath)
		if err != nil {
			if isObjectGenerationPreconditionFailed(err) && attempt < w.catalogAttempts() {
				continue
			}
			return CatalogWriteOutcomeNotAttempted, err
		}
		if progress.Done != nil {
			progress.Done("Updated app registry retention catalog")
		}
		if progress.Log != nil {
			progress.Log(fmt.Sprintf("updated %s", retentionPath))
		}
		return CatalogWriteOutcomeUpdated, nil
	}
	return CatalogWriteOutcomeNotAttempted, fmt.Errorf("update %s: exceeded retry limit after concurrent retention updates", retentionPath)
}

// LoadPublishStartedAt reads pending.json for an in-flight publish startedAt timestamp.
func LoadPublishStartedAt(store RegistryObjectStore, storageRoot, appName, version string) time.Time {
	if store == nil {
		return time.Time{}
	}
	pendingURL := StorageURL(storageRoot, AppPendingPath(appName))
	_, pendingData, err := store.ReadObject(pendingURL)
	if err != nil || len(pendingData) == 0 {
		return time.Time{}
	}
	pending, err := DecodePendingIndex(pendingData)
	if err != nil {
		return time.Time{}
	}
	startedAt, ok := PublishStartedAtFromPending(pending, version)
	if !ok {
		return time.Time{}
	}
	return startedAt
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

func isObjectGenerationPreconditionFailed(err error) bool {
	if errors.Is(err, ErrObjectPreconditionFailed) {
		return true
	}
	return gcloudPreconditionFailed(err)
}

// CatalogPreconditionFailed reports whether err is a generation/precondition conflict.
func CatalogPreconditionFailed(err error) bool {
	return isObjectGenerationPreconditionFailed(err)
}
