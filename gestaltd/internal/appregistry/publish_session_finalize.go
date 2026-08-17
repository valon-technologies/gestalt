package appregistry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

type FinalizePublishSessionInput struct {
	App             string
	PublishID       string
	StorageRoot     string
	PublicRoot      string
	DisplayName     string
	Description     string
	GestaltdVersion string
}

type FinalizePublishSessionResult struct {
	Session  *core.AppRegistryPublishSession
	Manifest PublishManifest
}

func (s *PublishSessionService) Finalize(ctx context.Context, input FinalizePublishSessionInput) (*FinalizePublishSessionResult, error) {
	if s == nil || s.Sessions == nil || s.Store == nil || s.Writer == nil {
		return nil, ErrPublishSessionUnavailable
	}
	session, err := s.loadSession(ctx, input.App, input.PublishID)
	if err != nil {
		return nil, err
	}
	if session.State == core.AppRegistryPublishSessionFailed {
		return nil, fmt.Errorf("%w: %s", ErrPublishSessionFailed, strings.TrimSpace(session.FailureReason))
	}
	declaration, err := DecodePublishDeclaration(session.DeclarationJSON)
	if err != nil {
		_, _ = s.failSession(ctx, session, err)
		return nil, err
	}
	sourceRef := strings.ToLower(strings.TrimSpace(declaration.SourceRef))

	if reconciled, reconcileErr := s.reconcilePublishedSession(ctx, session, input.StorageRoot, sourceRef, time.Time{}); reconcileErr != nil {
		return nil, reconcileErr
	} else if reconciled != nil && reconciled.State == core.AppRegistryPublishSessionPublished {
		manifest, manifestErr := s.buildFinalManifest(input, reconciled, declaration, reconciled.PublishedAt)
		if manifestErr != nil {
			return nil, manifestErr
		}
		return &FinalizePublishSessionResult{Session: reconciled, Manifest: manifest}, nil
	}

	if err := s.verifyStagingUploads(session, input.StorageRoot); err != nil {
		if isTerminalFinalizeError(err) {
			_, _ = s.failSession(ctx, session, err)
		}
		return nil, err
	}

	session, err = s.claimFinalize(ctx, session)
	if err != nil {
		if errors.Is(err, ErrPublishFinalizeInProgress) {
			if reconciled, reconcileErr := s.reconcilePublishedSession(ctx, session, input.StorageRoot, sourceRef, time.Time{}); reconcileErr == nil && reconciled != nil && reconciled.State == core.AppRegistryPublishSessionPublished {
				manifest, manifestErr := s.buildFinalManifest(input, reconciled, declaration, reconciled.PublishedAt)
				if manifestErr != nil {
					return nil, manifestErr
				}
				return &FinalizePublishSessionResult{Session: reconciled, Manifest: manifest}, nil
			}
		}
		return nil, err
	}
	publishedAt := session.FinalizePublishedAt
	if publishedAt.IsZero() {
		publishedAt = s.now()
	}

	if session, err = s.renewFinalizeClaim(ctx, session); err != nil {
		return nil, err
	}
	session, err = s.verifyAndPromoteUploads(ctx, session, declaration, input.StorageRoot, sourceRef)
	if err != nil {
		if isTerminalFinalizeError(err) {
			_, _ = s.failSession(ctx, session, err)
		}
		return nil, err
	}
	if session, err = s.renewFinalizeClaim(ctx, session); err != nil {
		return nil, err
	}
	manifest, err := s.buildFinalManifest(input, session, declaration, publishedAt)
	if err != nil {
		if isTerminalFinalizeError(err) {
			_, _ = s.failSession(ctx, session, err)
		}
		return nil, err
	}
	defer func() { _ = os.Remove(manifest.EntryObject.LocalPath) }()

	if session, err = s.renewFinalizeClaim(ctx, session); err != nil {
		return nil, err
	}
	req := PublishRequest{Manifest: manifest, SourceRef: sourceRef}
	if err := s.Writer.Preflight(req, PublishProgress{}); err != nil {
		if isTerminalFinalizeError(err) {
			_, _ = s.failSession(ctx, session, err)
		}
		return nil, err
	}
	if session, err = s.renewFinalizeClaim(ctx, session); err != nil {
		return nil, err
	}
	result, err := s.Writer.Publish(req, PublishProgress{})
	if err != nil && !publishIndexCommitted(result) {
		return nil, err
	}
	if !publishIndexCommitted(result) {
		return nil, fmt.Errorf("registry index was not updated")
	}
	if session, err = s.renewFinalizeClaim(ctx, session); err != nil {
		return nil, err
	}
	entry, loadErr := LoadPublishedEntry(s.Store, input.StorageRoot, session.App, session.Version)
	if loadErr != nil {
		return nil, loadErr
	}
	if err := VerifyPublishedEntry(entry, PublishedCommitExpectation{
		App:               session.App,
		Version:           session.Version,
		PublishID:         session.ID,
		DeclarationDigest: session.DeclarationDigest,
		SourceRef:         sourceRef,
		PublishedAt:       publishedAt,
	}); err != nil {
		return nil, err
	}

	finalSession, err := s.markPublished(ctx, session, publishedAt)
	if err != nil {
		if reconciled, reconcileErr := s.reconcilePublishedSession(ctx, session, input.StorageRoot, sourceRef, time.Time{}); reconcileErr != nil {
			return nil, reconcileErr
		} else if reconciled != nil {
			finalSession = reconciled
		} else {
			return nil, err
		}
	}
	return &FinalizePublishSessionResult{Session: finalSession, Manifest: manifest}, nil
}

func (s *PublishSessionService) claimFinalize(ctx context.Context, session *core.AppRegistryPublishSession) (*core.AppRegistryPublishSession, error) {
	claimed, err := s.Sessions.ClaimFinalize(ctx, session.ID, s.limits().FinalizeClaimLeaseTTL)
	if err != nil {
		if errors.Is(err, coredata.ErrPublishSessionFinalizeConflict) {
			return nil, ErrPublishFinalizeInProgress
		}
		return nil, err
	}
	return claimed, nil
}

func (s *PublishSessionService) renewFinalizeClaim(ctx context.Context, session *core.AppRegistryPublishSession) (*core.AppRegistryPublishSession, error) {
	if session == nil || strings.TrimSpace(session.FinalizeClaimToken) == "" {
		return session, nil
	}
	return s.Sessions.RenewFinalizeClaim(ctx, session.ID, session.FinalizeClaimToken, session.Revision, s.limits().FinalizeClaimLeaseTTL)
}

func (s *PublishSessionService) verifyStagingUploads(session *core.AppRegistryPublishSession, storageRoot string) error {
	for _, artifact := range session.Artifacts {
		stagingPath := PublishStagingArtifactPath(session.StagingPrefix, artifact.Platform, artifact.Filename)
		stagingURL := StorageURL(storageRoot, stagingPath)
		described, err := s.Store.DescribeObject(stagingURL)
		if err != nil {
			return err
		}
		if described.Generation == 0 {
			return fmt.Errorf("%w: %s", ErrPublishUploadMissing, artifact.Platform)
		}
		if strings.ToLower(strings.TrimSpace(described.SHA256)) != strings.ToLower(strings.TrimSpace(artifact.SHA256)) {
			return fmt.Errorf("%w: %s", ErrPublishUploadMismatch, artifact.Platform)
		}
		if artifact.Size > 0 && described.Size > 0 && described.Size != artifact.Size {
			return fmt.Errorf("%w: %s size mismatch", ErrPublishUploadMismatch, artifact.Platform)
		}
	}
	return nil
}

func (s *PublishSessionService) verifyAndPromoteUploads(
	ctx context.Context,
	session *core.AppRegistryPublishSession,
	declaration *PublishDeclaration,
	storageRoot, sourceRef string,
) (*core.AppRegistryPublishSession, error) {
	layout, err := ResolvePublishLayout(declaration.Manifest.Source, session.Version)
	if err != nil {
		return nil, err
	}
	for _, artifact := range session.Artifacts {
		var renewErr error
		session, renewErr = s.renewFinalizeClaim(ctx, session)
		if renewErr != nil {
			return nil, renewErr
		}
		stagingPath := PublishStagingArtifactPath(session.StagingPrefix, artifact.Platform, artifact.Filename)
		stagingURL := StorageURL(storageRoot, stagingPath)
		described, err := s.Store.DescribeObject(stagingURL)
		if err != nil {
			return nil, err
		}
		if described.Generation == 0 {
			return nil, fmt.Errorf("%w: %s", ErrPublishUploadMissing, artifact.Platform)
		}
		if strings.ToLower(strings.TrimSpace(described.SHA256)) != strings.ToLower(strings.TrimSpace(artifact.SHA256)) {
			return nil, fmt.Errorf("%w: %s", ErrPublishUploadMismatch, artifact.Platform)
		}
		if artifact.Size > 0 && described.Size > 0 && described.Size != artifact.Size {
			return nil, fmt.Errorf("%w: %s size mismatch", ErrPublishUploadMismatch, artifact.Platform)
		}
		filename := strings.TrimSpace(artifact.Filename)
		finalRel := filepath.ToSlash(filepath.Join(layout.ArtifactPrefix, filename))
		finalURL := StorageURL(storageRoot, finalRel)
		if err := s.Store.PromoteObject(PromoteObjectInput{
			SourceURL:        stagingURL,
			SourceGeneration: described.Generation,
			DestURL:          finalURL,
			ExpectedSHA256:   artifact.SHA256,
			SourceRef:        sourceRef,
		}); err != nil {
			return nil, err
		}
	}
	return session, nil
}

func (s *PublishSessionService) buildFinalManifest(
	input FinalizePublishSessionInput,
	session *core.AppRegistryPublishSession,
	declaration *PublishDeclaration,
	publishedAt time.Time,
) (PublishManifest, error) {
	publicationKind := declaration.PublicationKind
	if publicationKind == "" {
		publicationKind = PublicationKindLocal
	}
	sourceRef := strings.ToLower(strings.TrimSpace(declaration.SourceRef))
	builderVersion := strings.TrimSpace(declaration.BuilderVersion)
	if builderVersion == "" {
		builderVersion = strings.TrimSpace(input.GestaltdVersion)
	}
	artifacts := make([]PublishArtifact, 0, len(declaration.Artifacts))
	layout, err := ResolvePublishLayout(declaration.Manifest.Source, session.Version)
	if err != nil {
		return PublishManifest{}, err
	}
	for _, artifact := range declaration.Artifacts {
		platform := strings.TrimSpace(artifact.Platform)
		filename := strings.TrimSpace(artifact.Filename)
		rel := filepath.ToSlash(filepath.Join(layout.ArtifactPrefix, filename))
		artifacts = append(artifacts, PublishArtifact{
			Target:     platform,
			Filename:   filename,
			StorageURL: StorageURL(input.StorageRoot, rel),
			PublicURL:  PublicURL(input.PublicRoot, rel),
			SHA256:     strings.ToLower(strings.TrimSpace(artifact.SHA256)),
		})
	}
	entryInput := BuildEntryInput{
		Manifest:          declaration.Manifest,
		Version:           session.Version,
		SourceRef:         sourceRef,
		ManifestPath:      strings.TrimSpace(declaration.ManifestPath),
		PublicationKind:   publicationKind,
		PublishID:         session.ID,
		BuilderVersion:    builderVersion,
		DeclarationDigest: session.DeclarationDigest,
		LocalSource:       cloneLocalSourceState(declaration.LocalSource),
		Release:           declaration.ReleaseMetadata,
		Artifacts:         artifacts,
		PublishStartedAt:  session.PublishStartedAt,
		PublishedAt:       publishedAt.UTC(),
	}
	entry, err := BuildEntry(entryInput)
	if err != nil {
		return PublishManifest{}, err
	}
	entryData, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return PublishManifest{}, err
	}
	entryPath, err := WriteTempJSON("gestalt-publish-entry-*", entryData)
	if err != nil {
		return PublishManifest{}, err
	}
	entryDigest, err := SHA256File(entryPath)
	if err != nil {
		_ = os.Remove(entryPath)
		return PublishManifest{}, err
	}
	artifactObjects := make([]PublishObject, 0, len(artifacts))
	for _, artifact := range artifacts {
		artifactObjects = append(artifactObjects, PublishObject{
			Kind:       PublishObjectKindArchive,
			Target:     artifact.Target,
			StorageURL: artifact.StorageURL,
			PublicURL:  artifact.PublicURL,
			SHA256:     artifact.SHA256,
		})
	}
	return PublishManifest{
		Schema:      PublishPlanSchemaVersion,
		AppName:     entry.App,
		DisplayName: input.DisplayName,
		Description: input.Description,
		Version:     session.Version,
		Entry:       entry,
		EntryObject: PublishObject{
			Kind:       PublishObjectKindEntry,
			LocalPath:  entryPath,
			StorageURL: StorageURL(input.StorageRoot, layout.EntryPath),
			PublicURL:  PublicURL(input.PublicRoot, layout.EntryPath),
			SHA256:     entryDigest,
		},
		IndexObject: PublishObject{
			Kind:       PublishObjectKindIndex,
			StorageURL: StorageURL(input.StorageRoot, layout.IndexPath),
			PublicURL:  PublicURL(input.PublicRoot, layout.IndexPath),
		},
		ArtifactObjects: artifactObjects,
	}, nil
}

func (s *PublishSessionService) failSession(ctx context.Context, session *core.AppRegistryPublishSession, cause error) (*core.AppRegistryPublishSession, error) {
	reason := "finalization failed"
	if cause != nil {
		reason = strings.TrimSpace(cause.Error())
	}
	return s.Sessions.MarkFailed(ctx, session.ID, session.FinalizeClaimToken, session.Revision, reason)
}

func isTerminalPublishConflict(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrPublishVersionConflict) ||
		errors.Is(err, ErrObjectPreconditionFailed) ||
		strings.Contains(strings.ToLower(err.Error()), "already exists")
}

func newPublishID() string {
	return "pub_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

// PublishSessionPublicView redacts private publisher identity from public responses.
func PublishSessionPublicView(session *core.AppRegistryPublishSession) map[string]any {
	if session == nil {
		return nil
	}
	uploads := make([]map[string]any, 0, len(session.UploadLeases))
	for _, lease := range session.UploadLeases {
		uploads = append(uploads, map[string]any{
			"platform":  lease.Platform,
			"uploadUrl": lease.UploadURL,
			"expiresAt": lease.ExpiresAt.UTC().Format(time.RFC3339Nano),
		})
	}
	return map[string]any{
		"publishId": session.ID,
		"app":       session.App,
		"registry":  session.Registry,
		"version":   session.Version,
		"state":     string(session.State),
		"uploads":   uploads,
	}
}
