package appregistry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	PublishStateUploading = "uploading"
	PublishStatePublished = "published"
)

type PublishLimits struct {
	UploadURLTTL      time.Duration
	MaxArtifacts      int
	MaxArtifactBytes  int64
	RequiredPlatforms []string
}

func (l PublishLimits) withDefaults() PublishLimits {
	if l.UploadURLTTL <= 0 {
		l.UploadURLTTL = time.Hour
	}
	if l.MaxArtifacts <= 0 {
		l.MaxArtifacts = 16
	}
	if l.MaxArtifactBytes <= 0 {
		l.MaxArtifactBytes = 512 << 20
	}
	if len(l.RequiredPlatforms) == 0 {
		l.RequiredPlatforms = []string{"linux/amd64", "darwin/arm64"}
	}
	return l
}

type AdminPublishResponse struct {
	PublishID   string               `json:"publishId"`
	App         string               `json:"app"`
	Registry    string               `json:"registry"`
	Version     string               `json:"version"`
	State       string               `json:"state"`
	Uploads     []AdminPublishUpload `json:"uploads,omitempty"`
	PublishedAt string               `json:"publishedAt,omitempty"`
}

type AdminPublishUpload struct {
	Platform  string            `json:"platform"`
	UploadURL string            `json:"uploadUrl"`
	ExpiresAt string            `json:"expiresAt"`
	Headers   map[string]string `json:"headers,omitempty"`
}

type AdminPublishInput struct {
	App         string
	PublishID   string
	DisplayName string
	Description string
	Declaration *PublishDeclaration
}

type StatelessPublishService struct {
	Registry    string
	StorageRoot string
	PublicRoot  string
	Store       RegistryObjectStoreWithPromoter
	Signer      RegistryUploadSigner
	Writer      *Writer
	Limits      PublishLimits
	Now         func() time.Time
}

func (s *StatelessPublishService) ensureWritableRegistry(appRegistry string) error {
	if s == nil {
		return ErrPublishUnavailable
	}
	if strings.TrimSpace(appRegistry) != strings.TrimSpace(s.Registry) {
		return ErrPublishRegistryNotEnrolled
	}
	return nil
}

func (s *StatelessPublishService) Begin(ctx context.Context, appRegistry string, input AdminPublishInput) (*AdminPublishResponse, error) {
	if s == nil || s.Store == nil || s.Signer == nil {
		return nil, ErrPublishUnavailable
	}
	if err := s.ensureWritableRegistry(appRegistry); err != nil {
		return nil, err
	}
	limits := s.limits()
	canonical, err := NormalizeAndValidatePublishDeclaration(input.App, input.Declaration, limits)
	if err != nil {
		return nil, err
	}
	publishID, digest, version, stagingPrefix, err := s.resolveIdentity(input.App, canonical)
	if err != nil {
		return nil, err
	}
	sourceRef := declarationSourceRef(canonical)
	if entry, err := s.loadMatchingPublished(input.App, version, publishID, digest, sourceRef); err != nil {
		return nil, versionConflictError(version, err)
	} else if entry != nil {
		return adminPublishResponse(publishID, input.App, s.Registry, version, PublishStatePublished, nil, entry.PublishedAt), nil
	}
	uploads, err := s.signMissingUploads(stagingPrefix, canonical, limits)
	if err != nil {
		return nil, err
	}
	_ = ctx
	return adminPublishResponse(publishID, input.App, s.Registry, version, PublishStateUploading, uploads, time.Time{}), nil
}

func (s *StatelessPublishService) Finalize(ctx context.Context, appRegistry string, input AdminPublishInput) (*AdminPublishResponse, error) {
	if s == nil || s.Store == nil || s.Writer == nil {
		return nil, ErrPublishUnavailable
	}
	if err := s.ensureWritableRegistry(appRegistry); err != nil {
		return nil, err
	}
	limits := s.limits()
	canonical, err := NormalizeAndValidatePublishDeclaration(input.App, input.Declaration, limits)
	if err != nil {
		return nil, err
	}
	publishID, digest, version, stagingPrefix, err := s.resolveIdentity(input.App, canonical)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.PublishID) != publishID {
		return nil, fmt.Errorf("%w: got %q, want %q", ErrPublishIDMismatch, input.PublishID, publishID)
	}
	sourceRef := declarationSourceRef(canonical)

	if entry, matchErr := s.loadMatchingPublished(input.App, version, publishID, digest, sourceRef); matchErr != nil {
		return nil, versionConflictError(version, matchErr)
	} else if entry != nil {
		return adminPublishResponse(publishID, input.App, s.Registry, version, PublishStatePublished, nil, entry.PublishedAt), nil
	}

	publishedAt := s.now()
	manifest, err := s.buildFinalManifest(input, canonical, publishID, digest, version, publishedAt)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(manifest.EntryObject.LocalPath) }()

	req := PublishRequest{Manifest: manifest, SourceRef: sourceRef}
	if err := s.Writer.Preflight(req, PublishProgress{}); err != nil {
		return nil, err
	}
	if err := s.promoteStagingArtifacts(stagingPrefix, canonical, sourceRef); err != nil {
		return nil, err
	}
	result, err := s.publishWithCatalogRetry(req)
	if err != nil {
		return nil, err
	}
	if !publishIndexCommitted(result) {
		return nil, fmt.Errorf("registry index was not updated")
	}

	loaded, loadErr := LoadPublishedState(s.Store, s.StorageRoot, input.App, version)
	if loadErr != nil {
		return nil, loadErr
	}
	if loaded.State != PublishedLoadVerified {
		if loaded.Err != nil {
			return nil, loaded.Err
		}
		return nil, fmt.Errorf("%w: published version is not indexed", ErrPublishReconcileMismatch)
	}
	if err := verifyPublishedEntry(loaded.Entry, publishedExpectation(input.App, version, publishID, digest, sourceRef)); err != nil {
		return nil, err
	}
	_ = ctx
	return adminPublishResponse(publishID, input.App, s.Registry, version, PublishStatePublished, nil, loaded.Entry.PublishedAt), nil
}

func (s *StatelessPublishService) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *StatelessPublishService) limits() PublishLimits {
	if s == nil {
		return PublishLimits{}.withDefaults()
	}
	return s.Limits.withDefaults()
}

func (s *StatelessPublishService) resolveIdentity(app string, declaration *PublishDeclaration) (publishID, digest, version, stagingPrefix string, err error) {
	digest, err = DeclarationDigest(declaration)
	if err != nil {
		return "", "", "", "", err
	}
	version = strings.TrimSpace(declaration.Manifest.Version)
	publishID = DerivePublishID(app, version, digest)
	stagingPrefix, err = PublishStagingPrefix(app, version, digest)
	if err != nil {
		return "", "", "", "", err
	}
	return publishID, digest, version, stagingPrefix, nil
}

func verifyArtifactDescribed(described ObjectDescription, artifact PublishDeclarationArtifact) error {
	if described.Generation == 0 {
		return fmt.Errorf("%w: %s", ErrPublishUploadMissing, artifact.Platform)
	}
	expected, err := normalizePublishArtifactSHA256(artifact.SHA256)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrPublishUploadMismatch, artifact.Platform)
	}
	if !strings.EqualFold(strings.TrimSpace(described.SHA256), expected) {
		return fmt.Errorf("%w: %s", ErrPublishUploadMismatch, artifact.Platform)
	}
	if artifact.Size > 0 && described.Size > 0 && described.Size != artifact.Size {
		return fmt.Errorf("%w: %s size mismatch", ErrPublishUploadMismatch, artifact.Platform)
	}
	return nil
}

func (s *StatelessPublishService) signMissingUploads(stagingPrefix string, declaration *PublishDeclaration, limits PublishLimits) ([]AdminPublishUpload, error) {
	uploads := make([]AdminPublishUpload, 0, len(declaration.Artifacts))
	expiresAt := s.now().Add(limits.UploadURLTTL)
	for _, artifact := range declaration.Artifacts {
		stagingPath, err := PublishStagingArtifactPath(stagingPrefix, artifact.Platform, artifact.Filename)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrPublishDeclarationInvalid, err)
		}
		stagingURL := StorageURL(s.StorageRoot, stagingPath)
		described, err := s.Store.DescribeObject(stagingURL)
		if err != nil {
			return nil, err
		}
		if described.Generation != 0 {
			if err := verifyArtifactDescribed(described, artifact); err != nil && !errors.Is(err, ErrPublishUploadMissing) {
				return nil, err
			}
			continue
		}
		signed, err := s.Signer.SignCreateUpload(SignCreateUploadInput{
			StorageURL: stagingURL, SHA256: artifact.SHA256, ContentLength: artifact.Size,
			ExpiresAt: expiresAt,
		})
		if err != nil {
			return nil, err
		}
		uploads = append(uploads, AdminPublishUpload{
			Platform:  strings.TrimSpace(artifact.Platform),
			UploadURL: signed.UploadURL,
			ExpiresAt: signed.ExpiresAt.UTC().Format(time.RFC3339Nano),
			Headers:   signed.Headers,
		})
	}
	return uploads, nil
}

func (s *StatelessPublishService) promoteStagingArtifacts(stagingPrefix string, declaration *PublishDeclaration, sourceRef string) error {
	layout, err := ResolvePublishLayout(declaration.Manifest.Source, declaration.Manifest.Version)
	if err != nil {
		return err
	}
	for _, artifact := range declaration.Artifacts {
		stagingPath, err := PublishStagingArtifactPath(stagingPrefix, artifact.Platform, artifact.Filename)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrPublishDeclarationInvalid, err)
		}
		stagingURL := StorageURL(s.StorageRoot, stagingPath)
		described, err := s.Store.DescribeObject(stagingURL)
		if err != nil {
			return err
		}
		if err := verifyArtifactDescribed(described, artifact); err != nil {
			return err
		}
		finalRel, err := PublishArtifactFinalRel(layout.ArtifactPrefix, artifact.Filename)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrPublishDeclarationInvalid, err)
		}
		if err := s.Store.PromoteObject(PromoteObjectInput{
			SourceURL: stagingURL, SourceGeneration: described.Generation,
			DestURL: StorageURL(s.StorageRoot, finalRel), ExpectedSHA256: artifact.SHA256, SourceRef: sourceRef,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *StatelessPublishService) publishWithCatalogRetry(req PublishRequest) (PublishResult, error) {
	attempts := defaultCatalogUpdateAttempts
	if s != nil && s.Writer != nil && s.Writer.CatalogAttempts > 0 {
		attempts = s.Writer.CatalogAttempts
	}
	var lastResult PublishResult
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		lastResult, lastErr = s.Writer.Publish(req, PublishProgress{})
		if lastErr == nil && publishIndexCommitted(lastResult) {
			return lastResult, nil
		}
		if lastErr != nil && !isObjectGenerationPreconditionFailed(lastErr) {
			return lastResult, lastErr
		}
	}
	return lastResult, lastErr
}

func (s *StatelessPublishService) buildFinalManifest(
	input AdminPublishInput,
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
	layout, err := ResolvePublishLayout(declaration.Manifest.Source, version)
	if err != nil {
		return PublishManifest{}, err
	}
	artifacts := make([]PublishArtifact, 0, len(declaration.Artifacts))
	for _, artifact := range declaration.Artifacts {
		finalRel, err := PublishArtifactFinalRel(layout.ArtifactPrefix, artifact.Filename)
		if err != nil {
			return PublishManifest{}, err
		}
		digestHex, err := normalizePublishArtifactSHA256(artifact.SHA256)
		if err != nil {
			return PublishManifest{}, err
		}
		artifacts = append(artifacts, PublishArtifact{
			Target: strings.TrimSpace(artifact.Platform), Filename: strings.TrimSpace(artifact.Filename),
			StorageURL: StorageURL(s.StorageRoot, finalRel), PublicURL: PublicURL(s.PublicRoot, finalRel),
			SHA256: digestHex,
		})
	}
	entry, err := BuildEntry(BuildEntryInput{
		Manifest: declaration.Manifest, Version: version, SourceRef: sourceRef,
		ManifestPath: strings.TrimSpace(declaration.ManifestPath), PublicationKind: publicationKind,
		PublishID: publishID, BuilderVersion: builderVersion, DeclarationDigest: digest,
		LocalSource: normalizeLocalSourceState(declaration.LocalSource), Release: declaration.ReleaseMetadata,
		Artifacts: artifacts, PublishedAt: publishedAt.UTC(),
	})
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
			Kind: PublishObjectKindArchive, Target: artifact.Target,
			StorageURL: artifact.StorageURL, PublicURL: artifact.PublicURL, SHA256: artifact.SHA256,
		})
	}
	return PublishManifest{
		Schema: PublishPlanSchemaVersion, AppName: entry.App,
		DisplayName: input.DisplayName, Description: input.Description, Version: version,
		Entry: entry,
		EntryObject: PublishObject{
			Kind: PublishObjectKindEntry, LocalPath: entryPath,
			StorageURL: StorageURL(s.StorageRoot, layout.EntryPath),
			PublicURL:  PublicURL(s.PublicRoot, layout.EntryPath), SHA256: entryDigest,
		},
		IndexObject: PublishObject{
			Kind:       PublishObjectKindIndex,
			StorageURL: StorageURL(s.StorageRoot, layout.IndexPath),
			PublicURL:  PublicURL(s.PublicRoot, layout.IndexPath),
		},
		ArtifactObjects: artifactObjects,
	}, nil
}

func adminPublishResponse(publishID, app, registry, version, state string, uploads []AdminPublishUpload, publishedAt time.Time) *AdminPublishResponse {
	resp := &AdminPublishResponse{
		PublishID: publishID, App: app, Registry: registry, Version: version, State: state, Uploads: uploads,
	}
	if !publishedAt.IsZero() {
		resp.PublishedAt = publishedAt.UTC().Format(time.RFC3339Nano)
	}
	return resp
}
