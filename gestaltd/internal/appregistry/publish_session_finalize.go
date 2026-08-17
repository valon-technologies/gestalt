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
	if reconciled, reconcileErr := s.reconcilePublishedSession(ctx, session, input.PublicRoot); reconcileErr != nil {
		return nil, reconcileErr
	} else if reconciled != nil && reconciled.State == core.AppRegistryPublishSessionPublished {
		declaration, decodeErr := DecodePublishDeclaration(reconciled.DeclarationJSON)
		if decodeErr != nil {
			return nil, decodeErr
		}
		manifest, manifestErr := s.buildFinalManifest(input, reconciled, declaration)
		if manifestErr != nil {
			return nil, manifestErr
		}
		return &FinalizePublishSessionResult{Session: reconciled, Manifest: manifest}, nil
	}
	if session.State == core.AppRegistryPublishSessionFailed {
		return nil, fmt.Errorf("%w: %s", ErrPublishSessionFailed, strings.TrimSpace(session.FailureReason))
	}
	updated, err := s.Sessions.Update(ctx, session.ID, func(current *core.AppRegistryPublishSession) error {
		if current.State == core.AppRegistryPublishSessionPublished {
			return nil
		}
		if current.State == core.AppRegistryPublishSessionFailed {
			return fmt.Errorf("%w: %s", ErrPublishSessionFailed, strings.TrimSpace(current.FailureReason))
		}
		current.State = core.AppRegistryPublishSessionFinalizing
		return nil
	})
	if err != nil {
		return nil, err
	}
	session = updated

	declaration, err := DecodePublishDeclaration(session.DeclarationJSON)
	if err != nil {
		_, _ = s.failSession(ctx, session.ID, err)
		return nil, err
	}
	sourceRef := strings.ToLower(strings.TrimSpace(declaration.SourceRef))
	if err := s.verifyAndPromoteUploads(session, declaration, input.StorageRoot, sourceRef); err != nil {
		if isTerminalPublishConflict(err) {
			_, _ = s.failSession(ctx, session.ID, err)
		}
		return nil, err
	}
	manifest, err := s.buildFinalManifest(input, session, declaration)
	if err != nil {
		_, _ = s.failSession(ctx, session.ID, err)
		return nil, err
	}
	defer func() { _ = os.Remove(manifest.EntryObject.LocalPath) }()

	req := PublishRequest{Manifest: manifest, SourceRef: sourceRef}
	if err := s.Writer.Preflight(req, PublishProgress{}); err != nil {
		if isTerminalPublishConflict(err) {
			_, _ = s.failSession(ctx, session.ID, err)
		}
		return nil, err
	}
	indexCommitted := false
	if err := s.Writer.Publish(req, PublishProgress{}); err != nil {
		if s.indexContainsVersion(ctx, input.PublicRoot, input.App, session.Version) {
			indexCommitted = true
		} else {
			return nil, err
		}
	} else {
		indexCommitted = true
	}
	if !indexCommitted {
		return nil, fmt.Errorf("registry index was not updated")
	}
	publishedAt := s.now()
	finalSession, err := s.Sessions.Update(ctx, session.ID, func(current *core.AppRegistryPublishSession) error {
		current.State = core.AppRegistryPublishSessionPublished
		current.PublishedAt = publishedAt
		current.FailureReason = ""
		if current.StagingMarkedStale.IsZero() {
			current.StagingMarkedStale = publishedAt
		}
		return nil
	})
	if err != nil {
		reconciled, reconcileErr := s.reconcilePublishedSession(ctx, session, input.PublicRoot)
		if reconcileErr != nil {
			return nil, reconcileErr
		}
		if reconciled == nil {
			return nil, err
		}
		finalSession = reconciled
	}
	return &FinalizePublishSessionResult{Session: finalSession, Manifest: manifest}, nil
}

func (s *PublishSessionService) reconcilePublishedSession(ctx context.Context, session *core.AppRegistryPublishSession, publicRoot string) (*core.AppRegistryPublishSession, error) {
	if s == nil || session == nil || s.Index == nil {
		return nil, nil
	}
	if session.State == core.AppRegistryPublishSessionPublished {
		return session, nil
	}
	publicRoot = strings.TrimSpace(publicRoot)
	if publicRoot == "" {
		return nil, nil
	}
	published, err := s.Index.VersionPublished(ctx, publicRoot, session.App, session.Version)
	if err != nil || !published {
		return nil, err
	}
	return s.Sessions.Update(ctx, session.ID, func(current *core.AppRegistryPublishSession) error {
		if current.State == core.AppRegistryPublishSessionPublished {
			return nil
		}
		now := s.now()
		current.State = core.AppRegistryPublishSessionPublished
		if current.PublishedAt.IsZero() {
			current.PublishedAt = now
		}
		if current.StagingMarkedStale.IsZero() {
			current.StagingMarkedStale = now
		}
		current.FailureReason = ""
		return nil
	})
}

func (s *PublishSessionService) indexContainsVersion(ctx context.Context, publicRoot, app, version string) bool {
	if s == nil || s.Index == nil {
		return false
	}
	published, err := s.Index.VersionPublished(ctx, publicRoot, app, version)
	return err == nil && published
}

func (s *PublishSessionService) verifyAndPromoteUploads(
	session *core.AppRegistryPublishSession,
	declaration *PublishDeclaration,
	storageRoot, sourceRef string,
) error {
	layout, err := ResolvePublishLayout(declaration.Manifest.Source, session.Version)
	if err != nil {
		return err
	}
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
			return err
		}
	}
	return nil
}

func (s *PublishSessionService) buildFinalManifest(
	input FinalizePublishSessionInput,
	session *core.AppRegistryPublishSession,
	declaration *PublishDeclaration,
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
		PublishedAt:       s.now(),
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

func (s *PublishSessionService) failSession(ctx context.Context, id string, cause error) (*core.AppRegistryPublishSession, error) {
	reason := "finalization failed"
	if cause != nil {
		reason = strings.TrimSpace(cause.Error())
	}
	return s.Sessions.Update(ctx, id, func(current *core.AppRegistryPublishSession) error {
		current.State = core.AppRegistryPublishSessionFailed
		current.FailureReason = reason
		if current.StagingMarkedStale.IsZero() {
			current.StagingMarkedStale = s.now()
		}
		return nil
	})
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
