package appregistry

import (
	"errors"
	"fmt"
	"strings"
)

type PublishedLoadState int

const (
	PublishedLoadAbsent PublishedLoadState = iota
	PublishedLoadVerified
	PublishedLoadCorrupt
)

type PublishedLoadResult struct {
	State PublishedLoadState
	Entry *Entry
	Err   error
}

type PublishedCommitExpectation struct {
	App               string
	Version           string
	PublishID         string
	DeclarationDigest string
	SourceRef         string
}

func LoadPublishedState(store RegistryObjectStore, storageRoot, appName, version string) (PublishedLoadResult, error) {
	if store == nil {
		return PublishedLoadResult{}, fmt.Errorf("registry store is required")
	}
	appName = strings.TrimSpace(appName)
	version = strings.TrimSpace(version)
	indexURL := StorageURL(storageRoot, AppIndexPath(appName))
	_, indexData, err := store.ReadObject(indexURL)
	if err != nil {
		return PublishedLoadResult{}, err
	}
	index, err := decodeIndexOrEmpty(indexData)
	if err != nil {
		return PublishedLoadResult{}, fmt.Errorf("decode index: %w", err)
	}
	if index == nil || index.Apps == nil {
		return PublishedLoadResult{State: PublishedLoadAbsent}, nil
	}
	appVersions, ok := index.Apps[appName]
	if !ok || appVersions.Versions == nil {
		return PublishedLoadResult{State: PublishedLoadAbsent}, nil
	}
	indexVersion, ok := appVersions.Versions[version]
	if !ok {
		return PublishedLoadResult{State: PublishedLoadAbsent}, nil
	}
	metadataPath := strings.TrimSpace(indexVersion.Metadata)
	if metadataPath == "" {
		return PublishedLoadResult{
			State: PublishedLoadCorrupt,
			Err:   fmt.Errorf("%w: index metadata path is empty", ErrPublishReconcileMismatch),
		}, nil
	}
	entryURL := StorageURL(storageRoot, metadataPath)
	_, entryData, err := store.ReadObject(entryURL)
	if err != nil {
		return PublishedLoadResult{}, err
	}
	if len(entryData) == 0 {
		return PublishedLoadResult{
			State: PublishedLoadCorrupt,
			Err:   fmt.Errorf("%w: index references missing entry metadata", ErrPublishReconcileMismatch),
		}, nil
	}
	entry, err := DecodeEntry(entryData)
	if err != nil {
		return PublishedLoadResult{
			State: PublishedLoadCorrupt,
			Err:   fmt.Errorf("%w: decode entry: %v", ErrPublishReconcileMismatch, err),
		}, nil
	}
	projected := indexVersionFromEntry(*entry, metadataPath)
	if !indexVersionsEqual(projected, indexVersion) {
		return PublishedLoadResult{
			State: PublishedLoadCorrupt,
			Entry: entry,
			Err:   fmt.Errorf("%w: index-projected identity differs from entry", ErrPublishReconcileMismatch),
		}, nil
	}
	return PublishedLoadResult{State: PublishedLoadVerified, Entry: entry}, nil
}

func verifyPublishedEntry(entry *Entry, expect PublishedCommitExpectation) error {
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
		return fmt.Errorf("%w: publishId %q != %q", ErrPublishIdentityMismatch, entry.PublishID, expect.PublishID)
	}
	if strings.TrimSpace(entry.DeclarationDigest) != strings.TrimSpace(expect.DeclarationDigest) {
		return fmt.Errorf("%w: declarationDigest mismatch", ErrPublishIdentityMismatch)
	}
	if !strings.EqualFold(strings.TrimSpace(entry.SourceRef), strings.TrimSpace(expect.SourceRef)) {
		return fmt.Errorf("%w: sourceRef %q != %q", ErrPublishIdentityMismatch, entry.SourceRef, expect.SourceRef)
	}
	return nil
}

func (s *StatelessPublishService) loadMatchingPublished(app, version, publishID, digest, sourceRef string) (*Entry, error) {
	loaded, err := LoadPublishedState(s.Store, s.StorageRoot, app, version)
	if err != nil {
		return nil, err
	}
	switch loaded.State {
	case PublishedLoadAbsent:
		return nil, nil
	case PublishedLoadCorrupt:
		return loaded.Entry, loaded.Err
	case PublishedLoadVerified:
		expect := PublishedCommitExpectation{
			App: app, Version: version, PublishID: publishID,
			DeclarationDigest: digest, SourceRef: sourceRef,
		}
		if err := verifyPublishedEntry(loaded.Entry, expect); err != nil {
			if errors.Is(err, ErrPublishIdentityMismatch) {
				return loaded.Entry, ErrPublishVersionConflict
			}
			return loaded.Entry, err
		}
		return loaded.Entry, nil
	default:
		return nil, fmt.Errorf("unknown published load state")
	}
}

func publishIndexCommitted(result PublishResult) bool {
	return result.Index == CatalogWriteOutcomeUpdated || result.Index == CatalogWriteOutcomeUnchanged
}
