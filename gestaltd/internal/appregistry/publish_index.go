package appregistry

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	PublishStateUploading = "uploading"
	PublishStatePublished = "published"
)

type PublishedCommitExpectation struct {
	App               string
	Version           string
	PublishID         string
	DeclarationDigest string
	SourceRef         string
}

func LoadPublishedEntry(store RegistryObjectStore, storageRoot, appName, version string) (*Entry, error) {
	if store == nil {
		return nil, fmt.Errorf("registry store is required")
	}
	indexURL := StorageURL(storageRoot, AppIndexPath(appName))
	_, indexData, err := store.ReadObject(indexURL)
	if err != nil {
		return nil, err
	}
	index, err := decodeIndexOrEmpty(indexData)
	if err != nil {
		return nil, fmt.Errorf("decode index: %w", err)
	}
	if index == nil || index.Apps == nil {
		return nil, nil
	}
	appVersions, ok := index.Apps[appName]
	if !ok {
		return nil, nil
	}
	indexVersion, ok := appVersions.Versions[strings.TrimSpace(version)]
	if !ok {
		return nil, nil
	}
	entryURL := StorageURL(storageRoot, strings.TrimSpace(indexVersion.Metadata))
	_, entryData, err := store.ReadObject(entryURL)
	if err != nil {
		return nil, err
	}
	if len(entryData) == 0 {
		return nil, nil
	}
	return DecodeEntry(entryData)
}

func VerifyPublishedEntry(entry *Entry, expect PublishedCommitExpectation) error {
	if entry == nil {
		return fmt.Errorf("%w: entry is missing", ErrPublishReconcileMismatch)
	}
	if strings.TrimSpace(entry.App) != strings.TrimSpace(expect.App) {
		return fmt.Errorf("%w: app %q != %q", ErrPublishReconcileMismatch, entry.App, expect.App)
	}
	if strings.TrimSpace(entry.Version) != strings.TrimSpace(expect.Version) {
		return fmt.Errorf("%w: version %q != %q", ErrPublishReconcileMismatch, entry.Version, expect.Version)
	}
	if strings.TrimSpace(entry.PublishID) != strings.TrimSpace(expect.PublishID) {
		return fmt.Errorf("%w: publishId %q != %q", ErrPublishReconcileMismatch, entry.PublishID, expect.PublishID)
	}
	if strings.TrimSpace(entry.DeclarationDigest) != strings.TrimSpace(expect.DeclarationDigest) {
		return fmt.Errorf("%w: declarationDigest mismatch", ErrPublishReconcileMismatch)
	}
	if !strings.EqualFold(strings.TrimSpace(entry.SourceRef), strings.TrimSpace(expect.SourceRef)) {
		return fmt.Errorf("%w: sourceRef %q != %q", ErrPublishReconcileMismatch, entry.SourceRef, expect.SourceRef)
	}
	return nil
}

func publishIndexCommitted(result PublishResult) bool {
	return result.Index == CatalogWriteOutcomeUpdated || result.Index == CatalogWriteOutcomeUnchanged
}

// StoreIndexChecker reads publication state from a RegistryObjectStore-backed index.
type StoreIndexChecker struct {
	Store       WritableRegistryStore
	StorageRoot string
}

func (c StoreIndexChecker) VersionPublished(ctx context.Context, storageRoot, appName, version string) (bool, error) {
	_ = ctx
	if c.Store == nil {
		return false, nil
	}
	storageRoot = strings.TrimSpace(storageRoot)
	if storageRoot == "" {
		storageRoot = strings.TrimSpace(c.StorageRoot)
	}
	if storageRoot == "" {
		return false, fmt.Errorf("storage root is required")
	}
	entry, err := LoadPublishedEntry(c.Store, storageRoot, appName, version)
	if err != nil {
		return false, err
	}
	return entry != nil, nil
}

func (c StoreIndexChecker) matchesPublishedIdentity(
	storageRoot, appName string,
	declaration *PublishDeclaration,
	publishID, declarationDigest string,
) (*Entry, error) {
	if declaration == nil || declaration.Manifest == nil {
		return nil, fmt.Errorf("declaration is required")
	}
	version := strings.TrimSpace(declaration.Manifest.Version)
	entry, err := LoadPublishedEntry(c.Store, storageRoot, strings.TrimSpace(appName), version)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, nil
	}
	expect := PublishedCommitExpectation{
		App:               strings.TrimSpace(entry.App),
		Version:           version,
		PublishID:         publishID,
		DeclarationDigest: declarationDigest,
		SourceRef:         declarationSourceRef(declaration),
	}
	if err := VerifyPublishedEntry(entry, expect); err != nil {
		if strings.Contains(err.Error(), "publishId") || strings.Contains(err.Error(), "declarationDigest") || strings.Contains(err.Error(), "sourceRef") {
			return entry, ErrPublishVersionConflict
		}
		return entry, err
	}
	return entry, nil
}

func publishedAtFromEntry(entry *Entry) time.Time {
	if entry == nil || entry.PublishedAt.IsZero() {
		return time.Time{}
	}
	return entry.PublishedAt.UTC()
}
