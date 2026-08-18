package appregistry

import (
	"context"
	"errors"
	"fmt"
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

// DefaultPublishLimits returns the standard remote publish validation limits.
func DefaultPublishLimits() PublishLimits {
	return PublishLimits{}.withDefaults()
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

type preparedPublishAttempt struct {
	canonical     *PublishDeclaration
	publishID     string
	digest        string
	version       string
	stagingPrefix string
	sourceRef     string
	published     *Entry
}

// preparePublishAttempt validates a declaration, resolves publish identity, and
// returns an already-published entry when the index/entry pair is complete.
func (s *StatelessPublishService) preparePublishAttempt(appRegistry string, input AdminPublishInput) (*preparedPublishAttempt, error) {
	if s == nil || s.Store == nil {
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
	entry, err := s.loadMatchingPublished(input.App, version, publishID, digest, sourceRef)
	if err != nil {
		return nil, versionConflictError(version, err)
	}
	return &preparedPublishAttempt{
		canonical: canonical, publishID: publishID, digest: digest, version: version,
		stagingPrefix: stagingPrefix, sourceRef: sourceRef, published: entry,
	}, nil
}

func (s *StatelessPublishService) Begin(ctx context.Context, appRegistry string, input AdminPublishInput) (*AdminPublishResponse, error) {
	if s == nil || s.Signer == nil {
		return nil, ErrPublishUnavailable
	}
	prepared, err := s.preparePublishAttempt(appRegistry, input)
	if err != nil {
		return nil, err
	}
	if prepared.published != nil {
		return adminPublishResponse(prepared.publishID, input.App, s.Registry, prepared.version, PublishStatePublished, nil, prepared.published.PublishedAt), nil
	}
	uploads, err := s.signMissingUploads(prepared.stagingPrefix, prepared.canonical, s.limits())
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return adminPublishResponse(prepared.publishID, input.App, s.Registry, prepared.version, PublishStateUploading, uploads, time.Time{}), nil
}

func (s *StatelessPublishService) Finalize(ctx context.Context, appRegistry string, input AdminPublishInput) (*AdminPublishResponse, error) {
	if s == nil || s.Writer == nil {
		return nil, ErrPublishUnavailable
	}
	prepared, err := s.preparePublishAttempt(appRegistry, input)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.PublishID) != prepared.publishID {
		return nil, fmt.Errorf("%w: got %q, want %q", ErrPublishIDMismatch, input.PublishID, prepared.publishID)
	}
	if prepared.published != nil {
		return adminPublishResponse(prepared.publishID, input.App, s.Registry, prepared.version, PublishStatePublished, nil, prepared.published.PublishedAt), nil
	}

	publishedAt := s.now()
	manifest, err := BuildPublishManifestFromDeclaration(BuildPublishManifestFromDeclarationInput{
		StorageRoot: s.StorageRoot, PublicRoot: s.PublicRoot,
		DisplayName: input.DisplayName, Description: input.Description,
		Declaration: prepared.canonical, PublishID: prepared.publishID,
		DeclarationDigest: prepared.digest, Version: prepared.version, PublishedAt: publishedAt,
	})
	if err != nil {
		return nil, err
	}
	defer manifest.Cleanup()

	req := PublishRequest{Manifest: manifest, SourceRef: prepared.sourceRef}
	if err := s.Writer.Preflight(req, PublishProgress{}); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Promote staged uploads before committing immutable entry/index metadata. Partial
	// finals without an index entry are recoverable via identical finalize retries.
	if err := s.promoteStagingArtifacts(prepared.stagingPrefix, prepared.canonical, prepared.sourceRef); err != nil {
		return nil, err
	}
	result, err := s.publishUntilIndexed(req, prepared, input.App)
	if err != nil {
		return nil, err
	}
	if !publishIndexCommitted(result) {
		return nil, fmt.Errorf("registry index was not updated")
	}

	loaded, loadErr := LoadPublishedState(s.Store, s.StorageRoot, input.App, prepared.version)
	if loadErr != nil {
		return nil, loadErr
	}
	if loaded.State != PublishedLoadVerified {
		if loaded.Err != nil {
			return nil, loaded.Err
		}
		return nil, fmt.Errorf("%w: published version is not indexed", ErrPublishReconcileMismatch)
	}
	if err := verifyPublishedEntry(loaded.Entry, publishedExpectation(input.App, prepared.version, prepared.publishID, prepared.digest, prepared.sourceRef)); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return adminPublishResponse(prepared.publishID, input.App, s.Registry, prepared.version, PublishStatePublished, nil, loaded.Entry.PublishedAt), nil
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

type stagedArtifactPromotion struct {
	stagingURL     string
	destURL        string
	generation     int64
	expectedSHA256 string
}

func (s *StatelessPublishService) planStagedArtifactPromotions(stagingPrefix string, declaration *PublishDeclaration) ([]stagedArtifactPromotion, error) {
	layout, err := ResolvePublishLayout(declaration.Manifest.Source, declaration.Manifest.Version)
	if err != nil {
		return nil, err
	}
	planned := make([]stagedArtifactPromotion, 0, len(declaration.Artifacts))
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
		if err := verifyArtifactDescribed(described, artifact); err != nil {
			return nil, err
		}
		finalRel, err := PublishArtifactFinalRel(layout.ArtifactPrefix, artifact.Filename)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrPublishDeclarationInvalid, err)
		}
		planned = append(planned, stagedArtifactPromotion{
			stagingURL: stagingURL, destURL: StorageURL(s.StorageRoot, finalRel),
			generation: described.Generation, expectedSHA256: artifact.SHA256,
		})
	}
	return planned, nil
}

func (s *StatelessPublishService) promoteStagingArtifacts(stagingPrefix string, declaration *PublishDeclaration, sourceRef string) error {
	planned, err := s.planStagedArtifactPromotions(stagingPrefix, declaration)
	if err != nil {
		return err
	}
	for _, plan := range planned {
		if err := s.Store.PromoteObject(PromoteObjectInput{
			SourceURL: plan.stagingURL, SourceGeneration: plan.generation,
			DestURL: plan.destURL, ExpectedSHA256: plan.expectedSHA256, SourceRef: sourceRef,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *StatelessPublishService) publishUntilIndexed(req PublishRequest, prepared *preparedPublishAttempt, app string) (PublishResult, error) {
	attempts := DefaultCatalogUpdateAttempts
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
		if entry, matchErr := s.loadMatchingPublished(app, prepared.version, prepared.publishID, prepared.digest, prepared.sourceRef); matchErr == nil && entry != nil {
			return PublishResult{Index: CatalogWriteOutcomeUnchanged}, nil
		}
		if lastErr != nil && !CatalogPreconditionFailed(lastErr) {
			return lastResult, lastErr
		}
	}
	return lastResult, lastErr
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
