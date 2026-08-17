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
)

// StatelessPublishService implements declaration-driven admin publishing without session state.
type StatelessPublishService struct {
	Store  WritableRegistryStore
	Signer RegistryUploadSigner
	Writer *Writer
	Index  StoreIndexChecker
	Limits PublishLimits
	Now    func() time.Time
}

type BeginPublishInput struct {
	App         string
	Registry    string
	StorageRoot string
	PublicRoot  string
	Declaration *PublishDeclaration
}

type BeginPublishResult struct {
	PublishID   string
	App         string
	Registry    string
	Version     string
	State       string
	Uploads     []PublishUpload
	PublishedAt time.Time
}

type FinalizePublishInput struct {
	App             string
	PublishID       string
	Registry        string
	StorageRoot     string
	PublicRoot      string
	DisplayName     string
	Description     string
	GestaltdVersion string
	Declaration     *PublishDeclaration
}

type FinalizePublishResult struct {
	PublishID   string
	App         string
	Registry    string
	Version     string
	State       string
	PublishedAt time.Time
}

func (s *StatelessPublishService) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *StatelessPublishService) limits() PublishLimits {
	if s == nil {
		return DefaultPublishLimits()
	}
	return s.Limits.normalized()
}

func (s *StatelessPublishService) resolveIdentity(app string, declaration *PublishDeclaration) (publishID, digest, version, stagingPrefix string, err error) {
	digest, err = DeclarationDigest(declaration)
	if err != nil {
		return "", "", "", "", err
	}
	version = strings.TrimSpace(declaration.Manifest.Version)
	publishID = DerivePublishID(app, version, digest)
	stagingPrefix = PublishStagingPrefix(app, version, digest)
	return publishID, digest, version, stagingPrefix, nil
}

func (s *StatelessPublishService) Begin(ctx context.Context, input BeginPublishInput) (*BeginPublishResult, error) {
	if s == nil || s.Store == nil || s.Signer == nil {
		return nil, ErrPublishUnavailable
	}
	limits := s.limits()
	declaration := input.Declaration
	if err := ValidatePublishDeclaration(input.App, declaration, limits); err != nil {
		return nil, err
	}
	publishID, digest, version, stagingPrefix, err := s.resolveIdentity(input.App, declaration)
	if err != nil {
		return nil, err
	}
	checker := s.indexChecker(input.StorageRoot)
	if entry, err := checker.matchesPublishedIdentity(input.StorageRoot, input.App, declaration, publishID, digest); err != nil {
		if entry != nil && err == ErrPublishVersionConflict {
			return nil, fmt.Errorf("%w: version %q is already published with different identity", ErrPublishVersionConflict, version)
		}
		return nil, err
	} else if entry != nil {
		return &BeginPublishResult{
			PublishID:   publishID,
			App:         input.App,
			Registry:    input.Registry,
			Version:     version,
			State:       PublishStatePublished,
			PublishedAt: publishedAtFromEntry(entry),
		}, nil
	}

	uploads, err := s.missingUploads(input.StorageRoot, stagingPrefix, declaration, limits)
	if err != nil {
		return nil, err
	}
	_ = ctx
	return &BeginPublishResult{
		PublishID: publishID,
		App:       input.App,
		Registry:  input.Registry,
		Version:   version,
		State:     PublishStateUploading,
		Uploads:   uploads,
	}, nil
}

func (s *StatelessPublishService) Finalize(ctx context.Context, input FinalizePublishInput) (*FinalizePublishResult, error) {
	if s == nil || s.Store == nil || s.Writer == nil {
		return nil, ErrPublishUnavailable
	}
	limits := s.limits()
	declaration := input.Declaration
	if err := ValidatePublishDeclaration(input.App, declaration, limits); err != nil {
		return nil, err
	}
	publishID, digest, version, stagingPrefix, err := s.resolveIdentity(input.App, declaration)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.PublishID) != publishID {
		return nil, fmt.Errorf("%w: got %q, want %q", ErrPublishIDMismatch, input.PublishID, publishID)
	}
	sourceRef := declarationSourceRef(declaration)

	checker := s.indexChecker(input.StorageRoot)
	if entry, matchErr := checker.matchesPublishedIdentity(input.StorageRoot, input.App, declaration, publishID, digest); matchErr != nil {
		if entry != nil && matchErr == ErrPublishVersionConflict {
			return nil, fmt.Errorf("%w: version %q is already published with different identity", ErrPublishVersionConflict, version)
		}
		return nil, matchErr
	} else if entry != nil {
		return &FinalizePublishResult{
			PublishID:   publishID,
			App:         input.App,
			Registry:    input.Registry,
			Version:     version,
			State:       PublishStatePublished,
			PublishedAt: publishedAtFromEntry(entry),
		}, nil
	}

	if err := s.verifyStagingUploads(stagingPrefix, declaration, input.StorageRoot); err != nil {
		return nil, err
	}
	if err := s.promoteUploads(stagingPrefix, declaration, input.StorageRoot, sourceRef); err != nil {
		return nil, err
	}

	publishedAt := s.now()
	manifest, err := s.buildFinalManifest(input, declaration, publishID, digest, version, publishedAt)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(manifest.EntryObject.LocalPath) }()

	req := PublishRequest{Manifest: manifest, SourceRef: sourceRef}
	if err := s.Writer.Preflight(req, PublishProgress{}); err != nil {
		return nil, err
	}
	result, err := s.publishWithConcurrentRetry(ctx, checker, input, declaration, publishID, digest, version, sourceRef, req)
	if err != nil {
		return nil, err
	}
	if !publishIndexCommitted(result) {
		return nil, fmt.Errorf("registry index was not updated")
	}

	entry, loadErr := LoadPublishedEntry(s.Store, input.StorageRoot, input.App, version)
	if loadErr != nil {
		return nil, loadErr
	}
	if err := VerifyPublishedEntry(entry, PublishedCommitExpectation{
		App:               input.App,
		Version:           version,
		PublishID:         publishID,
		DeclarationDigest: digest,
		SourceRef:         sourceRef,
	}); err != nil {
		if entry != nil {
			if retryEntry, retryErr := checker.matchesPublishedIdentity(input.StorageRoot, input.App, declaration, publishID, digest); retryErr == nil && retryEntry != nil {
				return &FinalizePublishResult{
					PublishID:   publishID,
					App:         input.App,
					Registry:    input.Registry,
					Version:     version,
					State:       PublishStatePublished,
					PublishedAt: publishedAtFromEntry(retryEntry),
				}, nil
			}
		}
		return nil, err
	}
	_ = ctx
	return &FinalizePublishResult{
		PublishID:   publishID,
		App:         input.App,
		Registry:    input.Registry,
		Version:     version,
		State:       PublishStatePublished,
		PublishedAt: publishedAtFromEntry(entry),
	}, nil
}

func (s *StatelessPublishService) publishWithConcurrentRetry(
	ctx context.Context,
	checker StoreIndexChecker,
	input FinalizePublishInput,
	declaration *PublishDeclaration,
	publishID, digest, version, sourceRef string,
	req PublishRequest,
) (PublishResult, error) {
	_ = ctx
	var lastResult PublishResult
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		lastResult, lastErr = s.Writer.Publish(req, PublishProgress{})
		if lastErr == nil || publishIndexCommitted(lastResult) {
			return lastResult, lastErr
		}
		if entry, matchErr := checker.matchesPublishedIdentity(input.StorageRoot, input.App, declaration, publishID, digest); matchErr == nil && entry != nil {
			return lastResult, nil
		}
		if !errors.Is(lastErr, ErrObjectPreconditionFailed) && !CatalogPreconditionFailed(lastErr) {
			return lastResult, lastErr
		}
	}
	return lastResult, lastErr
}

func (s *StatelessPublishService) indexChecker(storageRoot string) StoreIndexChecker {
	if s != nil && s.Index.Store != nil {
		return s.Index
	}
	return StoreIndexChecker{Store: s.Store, StorageRoot: storageRoot}
}

func (s *StatelessPublishService) missingUploads(storageRoot, stagingPrefix string, declaration *PublishDeclaration, limits PublishLimits) ([]PublishUpload, error) {
	uploads := make([]PublishUpload, 0, len(declaration.Artifacts))
	expiresAt := s.now().Add(limits.UploadURLTTL)
	sourceRef := declarationSourceRef(declaration)
	for _, artifact := range declaration.Artifacts {
		stagingPath := PublishStagingArtifactPath(stagingPrefix, artifact.Platform, artifact.Filename)
		stagingURL := StorageURL(storageRoot, stagingPath)
		described, err := s.Store.DescribeObject(stagingURL)
		if err != nil {
			return nil, err
		}
		if described.Generation != 0 {
			if !strings.EqualFold(strings.TrimSpace(described.SHA256), strings.TrimSpace(artifact.SHA256)) {
				return nil, fmt.Errorf("%w: %s", ErrPublishUploadMismatch, artifact.Platform)
			}
			if artifact.Size > 0 && described.Size > 0 && described.Size != artifact.Size {
				return nil, fmt.Errorf("%w: %s size mismatch", ErrPublishUploadMismatch, artifact.Platform)
			}
			continue
		}
		signed, err := s.Signer.SignCreateUpload(SignCreateUploadInput{
			StorageURL:    stagingURL,
			SHA256:        artifact.SHA256,
			ContentLength: artifact.Size,
			SourceRef:     sourceRef,
			ExpiresAt:     expiresAt,
		})
		if err != nil {
			return nil, err
		}
		uploads = append(uploads, PublishUpload{
			Platform:  strings.TrimSpace(artifact.Platform),
			UploadURL: signed.UploadURL,
			ExpiresAt: signed.ExpiresAt,
			Headers:   cloneSignedUploadHeaders(signed.Headers),
		})
	}
	return uploads, nil
}

func (s *StatelessPublishService) verifyStagingUploads(stagingPrefix string, declaration *PublishDeclaration, storageRoot string) error {
	for _, artifact := range declaration.Artifacts {
		stagingPath := PublishStagingArtifactPath(stagingPrefix, artifact.Platform, artifact.Filename)
		stagingURL := StorageURL(storageRoot, stagingPath)
		described, err := s.Store.DescribeObject(stagingURL)
		if err != nil {
			return err
		}
		if described.Generation == 0 {
			return fmt.Errorf("%w: %s", ErrPublishUploadMissing, artifact.Platform)
		}
		if !strings.EqualFold(strings.TrimSpace(described.SHA256), strings.TrimSpace(artifact.SHA256)) {
			return fmt.Errorf("%w: %s", ErrPublishUploadMismatch, artifact.Platform)
		}
		if artifact.Size > 0 && described.Size > 0 && described.Size != artifact.Size {
			return fmt.Errorf("%w: %s size mismatch", ErrPublishUploadMismatch, artifact.Platform)
		}
	}
	return nil
}

func (s *StatelessPublishService) promoteUploads(stagingPrefix string, declaration *PublishDeclaration, storageRoot, sourceRef string) error {
	layout, err := ResolvePublishLayout(declaration.Manifest.Source, declaration.Manifest.Version)
	if err != nil {
		return err
	}
	for _, artifact := range declaration.Artifacts {
		stagingPath := PublishStagingArtifactPath(stagingPrefix, artifact.Platform, artifact.Filename)
		stagingURL := StorageURL(storageRoot, stagingPath)
		described, err := s.Store.DescribeObject(stagingURL)
		if err != nil {
			return err
		}
		if described.Generation == 0 {
			return fmt.Errorf("%w: %s", ErrPublishUploadMissing, artifact.Platform)
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

func (s *StatelessPublishService) buildFinalManifest(
	input FinalizePublishInput,
	declaration *PublishDeclaration,
	publishID, digest, version string,
	publishedAt time.Time,
) (PublishManifest, error) {
	publicationKind := declaration.PublicationKind
	if publicationKind == "" {
		publicationKind = PublicationKindLocal
	}
	sourceRef := declarationSourceRef(declaration)
	builderVersion := strings.TrimSpace(declaration.BuilderVersion)
	if builderVersion == "" {
		builderVersion = strings.TrimSpace(input.GestaltdVersion)
	}
	layout, err := ResolvePublishLayout(declaration.Manifest.Source, version)
	if err != nil {
		return PublishManifest{}, err
	}
	artifacts := make([]PublishArtifact, 0, len(declaration.Artifacts))
	for _, artifact := range declaration.Artifacts {
		filename := strings.TrimSpace(artifact.Filename)
		rel := filepath.ToSlash(filepath.Join(layout.ArtifactPrefix, filename))
		artifacts = append(artifacts, PublishArtifact{
			Target:     strings.TrimSpace(artifact.Platform),
			Filename:   filename,
			StorageURL: StorageURL(input.StorageRoot, rel),
			PublicURL:  PublicURL(input.PublicRoot, rel),
			SHA256:     strings.ToLower(strings.TrimSpace(artifact.SHA256)),
		})
	}
	entryInput := BuildEntryInput{
		Manifest:          declaration.Manifest,
		Version:           version,
		SourceRef:         sourceRef,
		ManifestPath:      strings.TrimSpace(declaration.ManifestPath),
		PublicationKind:   publicationKind,
		PublishID:         publishID,
		BuilderVersion:    builderVersion,
		DeclarationDigest: digest,
		LocalSource:       cloneLocalSourceState(declaration.LocalSource),
		Release:           declaration.ReleaseMetadata,
		Artifacts:         artifacts,
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
		Version:     version,
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
