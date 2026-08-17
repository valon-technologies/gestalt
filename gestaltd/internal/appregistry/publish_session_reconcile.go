package appregistry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
)

var (
	ErrPublishReconcileMismatch  = errors.New("published registry entry does not match publish session")
	ErrPublishFinalizeInProgress = errors.New("publish session finalization in progress")
)

type PublishedCommitExpectation struct {
	App               string
	Version           string
	PublishID         string
	DeclarationDigest string
	SourceRef         string
	PublishedAt       time.Time
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
	if !expect.PublishedAt.IsZero() && !entry.PublishedAt.Equal(expect.PublishedAt.UTC()) {
		return fmt.Errorf("%w: publishedAt %v != %v", ErrPublishReconcileMismatch, entry.PublishedAt, expect.PublishedAt)
	}
	return nil
}

func (s *PublishSessionService) reconcilePublishedSession(
	ctx context.Context,
	session *core.AppRegistryPublishSession,
	storageRoot string,
	sourceRef string,
	publishedAt time.Time,
) (*core.AppRegistryPublishSession, error) {
	if s == nil || session == nil || s.Sessions == nil || s.Store == nil {
		return nil, nil
	}
	if session.State == core.AppRegistryPublishSessionFailed {
		return nil, fmt.Errorf("%w: %s", ErrPublishSessionFailed, strings.TrimSpace(session.FailureReason))
	}
	if session.State == core.AppRegistryPublishSessionPublished {
		return session, nil
	}
	entry, err := LoadPublishedEntry(s.Store, storageRoot, session.App, session.Version)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, nil
	}
	expect := PublishedCommitExpectation{
		App:               session.App,
		Version:           session.Version,
		PublishID:         session.ID,
		DeclarationDigest: session.DeclarationDigest,
		SourceRef:         sourceRef,
	}
	switch {
	case !session.FinalizePublishedAt.IsZero():
		expect.PublishedAt = session.FinalizePublishedAt.UTC()
	case !publishedAt.IsZero():
		expect.PublishedAt = publishedAt.UTC()
	default:
		expect.PublishedAt = entry.PublishedAt.UTC()
	}
	if err := VerifyPublishedEntry(entry, expect); err != nil {
		return nil, err
	}
	session, err = s.ensureFinalizeClaimForReconcile(ctx, session)
	if err != nil {
		return nil, err
	}
	markAt := expect.PublishedAt
	if markAt.IsZero() {
		markAt = entry.PublishedAt.UTC()
	}
	return s.markPublished(ctx, session, markAt)
}

func (s *PublishSessionService) ensureFinalizeClaimForReconcile(ctx context.Context, session *core.AppRegistryPublishSession) (*core.AppRegistryPublishSession, error) {
	if session.State == core.AppRegistryPublishSessionFinalizing {
		now := s.now()
		if !session.FinalizeClaimExpiresAt.IsZero() && session.FinalizeClaimExpiresAt.After(now) {
			return session, nil
		}
	}
	claimed, err := s.Sessions.ClaimFinalize(ctx, session.ID, s.limits().FinalizeClaimLeaseTTL)
	if err != nil {
		return nil, mapFinalizeClaimConflict(ctx, s, session.ID, err)
	}
	return claimed, nil
}

func (s *PublishSessionService) markPublished(ctx context.Context, session *core.AppRegistryPublishSession, publishedAt time.Time) (*core.AppRegistryPublishSession, error) {
	return s.Sessions.MarkPublished(ctx, session.ID, session.FinalizeClaimToken, session.Revision, publishedAt)
}

func publishIndexCommitted(result PublishResult) bool {
	return result.Index == CatalogWriteOutcomeUpdated || result.Index == CatalogWriteOutcomeUnchanged
}
