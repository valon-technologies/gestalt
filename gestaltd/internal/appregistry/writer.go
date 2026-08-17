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

// PublishResult summarizes uploaded and skipped registry objects.
type PublishResult struct {
	Uploaded []string
	Skipped  []string
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

// Publish uploads immutable objects and updates index and retention catalogs last.
func (w *Writer) Publish(req PublishRequest, progress PublishProgress) error {
	if err := w.uploadImmutableObjects(req, progress); err != nil {
		return err
	}
	if err := w.uploadIndex(req, progress); err != nil {
		return err
	}
	return w.uploadRetention(req, progress)
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
	return fmt.Errorf("%s already exists; %s", object.StorageURL, RepublishCorruptObjectGuidance)
}

func (w *Writer) uploadImmutableObjects(req PublishRequest, progress PublishProgress) error {
	plan := req.Manifest
	objectCount := len(plan.ArtifactObjects) + 1
	if progress.Start != nil {
		progress.Start("Uploading %d immutable app objects", objectCount)
	}
	stdoutLines := make([]string, 0, objectCount)
	for _, object := range plan.ArtifactObjects {
		line, err := w.uploadImmutableObjectIfNeeded(object, req.SourceRef)
		if err != nil {
			return err
		}
		if line != "" {
			stdoutLines = append(stdoutLines, line)
		}
	}
	line, err := w.uploadImmutableObjectIfNeeded(plan.EntryObject, req.SourceRef)
	if err != nil {
		return err
	}
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
	return nil
}

func (w *Writer) uploadImmutableObjectIfNeeded(object PublishObject, sourceRef string) (string, error) {
	matches, err := w.immutableObjectMatchesExisting(object)
	if err != nil {
		return "", err
	}
	if matches {
		return fmt.Sprintf("skipped existing %s", object.StorageURL), nil
	}
	if err := w.Store.WriteImmutableObject(WriteImmutableObjectInput{
		LocalPath:  object.LocalPath,
		StorageURL: object.StorageURL,
		SourceRef:  sourceRef,
		SHA256:     object.SHA256,
	}); err != nil {
		return "", err
	}
	return fmt.Sprintf("uploaded %s", object.StorageURL), nil
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

func (w *Writer) uploadIndex(req PublishRequest, progress PublishProgress) error {
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
			return err
		}
		index, err := decodeIndexOrEmpty(existing)
		if err != nil {
			return fmt.Errorf("decode existing app index: %w", err)
		}
		metadataPath := AppVersionEntryPath(plan.AppName, plan.Version)
		updated, changed, err := UpsertAppIndex(index, plan.Entry, metadataPath, plan.DisplayName, plan.Description)
		if err != nil {
			return err
		}
		if !changed {
			if progress.Done != nil {
				progress.Done("App registry index unchanged")
			}
			if progress.Log != nil {
				progress.Log(fmt.Sprintf("skipped unchanged index for %s %s", plan.AppName, plan.Version))
			}
			return nil
		}
		data, err := json.MarshalIndent(updated, "", "  ")
		if err != nil {
			return err
		}
		tmpPath, err := WriteTempJSON("gestalt-app-index-*", append(data, '\n'))
		if err != nil {
			return err
		}
		err = w.Store.WriteCatalogObject(WriteCatalogObjectInput{
			LocalPath:  tmpPath,
			StorageURL: indexPath,
			SourceRef:  req.SourceRef,
			Generation: generation,
		})
		_ = os.Remove(tmpPath)
		if err != nil {
			if isCatalogPreconditionFailed(err) && attempt < w.catalogAttempts() {
				if progress.Status != nil {
					progress.Status("App registry index update conflict; retrying")
				}
				continue
			}
			return err
		}
		if progress.Done != nil {
			progress.Done("Updated app registry index")
		}
		if progress.Log != nil {
			progress.Log(fmt.Sprintf("updated %s", indexPath))
		}
		return nil
	}
	return fmt.Errorf("update %s: exceeded retry limit after concurrent index updates", indexPath)
}

func (w *Writer) uploadRetention(req PublishRequest, progress PublishProgress) error {
	plan := req.Manifest
	retentionPath := RetentionStorageURL(plan.IndexObject.StorageURL, plan.AppName)
	if progress.Start != nil {
		progress.Start("Updating app registry retention catalog")
	}
	for attempt := 1; attempt <= w.catalogAttempts(); attempt++ {
		if attempt > 1 && progress.Status != nil {
			progress.Status("App registry retention catalog changed concurrently; retrying attempt %d/%d", attempt, w.catalogAttempts())
		}
		generation, existing, err := w.Store.ReadObject(retentionPath)
		if err != nil {
			return err
		}
		index, err := decodeRetentionIndexOrEmpty(existing)
		if err != nil {
			return fmt.Errorf("decode existing retention index: %w", err)
		}
		if !UpsertPublishedRetention(index, plan.Version, plan.Entry.PublishedAt, w.retentionPolicy()) {
			if progress.Done != nil {
				progress.Done("App registry retention catalog unchanged")
			}
			return nil
		}
		data, err := json.MarshalIndent(index, "", "  ")
		if err != nil {
			return err
		}
		tmpPath, err := WriteTempJSON("gestalt-app-retention-*", append(data, '\n'))
		if err != nil {
			return err
		}
		err = w.Store.WriteCatalogObject(WriteCatalogObjectInput{
			LocalPath:  tmpPath,
			StorageURL: retentionPath,
			SourceRef:  req.SourceRef,
			Generation: generation,
		})
		_ = os.Remove(tmpPath)
		if err != nil {
			if isCatalogPreconditionFailed(err) && attempt < w.catalogAttempts() {
				continue
			}
			return err
		}
		if progress.Done != nil {
			progress.Done("Updated app registry retention catalog")
		}
		if progress.Log != nil {
			progress.Log(fmt.Sprintf("updated %s", retentionPath))
		}
		return nil
	}
	return fmt.Errorf("update %s: exceeded retry limit after concurrent retention updates", retentionPath)
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
