package appregistry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

const (
	PublishDeclarationSchemaVersion = "gestaltd.app.publish.declaration.v1"
	PublishStateUploading           = "uploading"
	PublishStatePublished           = "published"
)

var (
	ErrPublishDeclarationInvalid  = errors.New("publish declaration is invalid")
	ErrPublishVersionConflict     = errors.New("publish version conflict")
	ErrPublishUnavailable         = errors.New("app registry publish is unavailable")
	ErrPublishUploadMissing       = errors.New("publish upload is missing")
	ErrPublishUploadMismatch      = errors.New("publish upload mismatch")
	ErrPublishArtifactLimit       = errors.New("publish artifact limit exceeded")
	ErrPublishRequiredPlatform    = errors.New("required publish platform missing")
	ErrPublishAppIdentityMismatch = errors.New("publish app identity mismatch")
	ErrPublishRegistryNotEnrolled = errors.New("app is not enrolled in the registry")
	ErrPublishIDMismatch          = errors.New("publish id mismatch")
	ErrPublishReconcileMismatch   = errors.New("published registry entry does not match publish declaration")
)

func PublishHTTPStatus(err error) int {
	switch {
	case errors.Is(err, ErrPublishUnavailable):
		return 503
	case errors.Is(err, ErrPublishDeclarationInvalid),
		errors.Is(err, ErrPublishRequiredPlatform),
		errors.Is(err, ErrPublishArtifactLimit),
		errors.Is(err, ErrPublishAppIdentityMismatch),
		errors.Is(err, ErrPublishUploadMissing),
		errors.Is(err, ErrPublishIDMismatch):
		return 400
	case errors.Is(err, ErrPublishVersionConflict),
		errors.Is(err, ErrPublishUploadMismatch),
		errors.Is(err, ErrPublishReconcileMismatch):
		return 409
	case errors.Is(err, ErrPublishRegistryNotEnrolled):
		return 404
	default:
		return 502
	}
}

type PublishDeclaration struct {
	Schema          string                       `json:"schema"`
	Manifest        *providermanifestv1.Manifest `json:"manifest"`
	ManifestPath    string                       `json:"manifestPath,omitempty"`
	ReleaseMetadata *providerrelease.Metadata    `json:"releaseMetadata"`
	Artifacts       []PublishDeclarationArtifact `json:"artifacts"`
	PublicationKind PublicationKind              `json:"publicationKind,omitempty"`
	SourceRef       string                       `json:"sourceRef,omitempty"`
	LocalSource     *LocalSourceState            `json:"localSource,omitempty"`
	BuilderVersion  string                       `json:"builderVersion,omitempty"`
}

type PublishDeclarationArtifact struct {
	Platform string `json:"platform"`
	Filename string `json:"filename"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size,omitempty"`
}

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
	App             string
	Registry        string
	StorageRoot     string
	PublicRoot      string
	PublishID       string
	DisplayName     string
	Description     string
	GestaltdVersion string
	Declaration     *PublishDeclaration
}

type StatelessPublishService struct {
	Store  RegistryObjectStore
	Signer RegistryUploadSigner
	Writer *Writer
	Limits PublishLimits
	Now    func() time.Time
}

func ValidatePublishDeclaration(appName string, declaration *PublishDeclaration, limits PublishLimits) error {
	if declaration == nil {
		return fmt.Errorf("%w: declaration is required", ErrPublishDeclarationInvalid)
	}
	if strings.TrimSpace(declaration.Schema) != PublishDeclarationSchemaVersion {
		return fmt.Errorf("%w: unsupported schema %q", ErrPublishDeclarationInvalid, declaration.Schema)
	}
	manifest := declaration.Manifest
	if manifest == nil {
		return fmt.Errorf("%w: manifest is required", ErrPublishDeclarationInvalid)
	}
	manifestApp, err := AppNameFromManifestSource(manifest.Source)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPublishDeclarationInvalid, err)
	}
	if manifestApp != strings.TrimSpace(appName) {
		return fmt.Errorf("%w: manifest app %q does not match route app %q", ErrPublishAppIdentityMismatch, manifestApp, appName)
	}
	if declaration.ReleaseMetadata == nil {
		return fmt.Errorf("%w: releaseMetadata is required", ErrPublishDeclarationInvalid)
	}
	if err := providerrelease.ValidateMetadata(declaration.ReleaseMetadata); err != nil {
		return fmt.Errorf("%w: releaseMetadata: %v", ErrPublishDeclarationInvalid, err)
	}
	if len(declaration.Artifacts) == 0 {
		return fmt.Errorf("%w: at least one artifact is required", ErrPublishDeclarationInvalid)
	}
	limits = limits.withDefaults()
	if limits.MaxArtifacts > 0 && len(declaration.Artifacts) > limits.MaxArtifacts {
		return fmt.Errorf("%w: got %d, limit %d", ErrPublishArtifactLimit, len(declaration.Artifacts), limits.MaxArtifacts)
	}
	platforms := make(map[string]struct{}, len(declaration.Artifacts))
	for _, artifact := range declaration.Artifacts {
		platform := strings.TrimSpace(artifact.Platform)
		filename := strings.TrimSpace(artifact.Filename)
		digest := strings.ToLower(strings.TrimSpace(artifact.SHA256))
		if platform == "" || filename == "" || digest == "" {
			return fmt.Errorf("%w: artifact platform, filename, and sha256 are required", ErrPublishDeclarationInvalid)
		}
		if len(digest) != 64 {
			return fmt.Errorf("%w: artifact %q sha256 must be 64 hex characters", ErrPublishDeclarationInvalid, platform)
		}
		if limits.MaxArtifactBytes > 0 && artifact.Size > limits.MaxArtifactBytes {
			return fmt.Errorf("%w: artifact %q exceeds size limit", ErrPublishArtifactLimit, platform)
		}
		if _, ok := platforms[platform]; ok {
			return fmt.Errorf("%w: duplicate platform %q", ErrPublishDeclarationInvalid, platform)
		}
		platforms[platform] = struct{}{}
	}
	for _, required := range limits.RequiredPlatforms {
		required = strings.TrimSpace(required)
		if required == "" {
			continue
		}
		if _, ok := platforms[required]; !ok {
			return fmt.Errorf("%w: %q", ErrPublishRequiredPlatform, required)
		}
	}
	publicationKind := declaration.PublicationKind
	if publicationKind == "" {
		publicationKind = PublicationKindLocal
	}
	sourceRef := strings.ToLower(strings.TrimSpace(declaration.SourceRef))
	if err := ValidatePublishInputWithOptions(manifest, manifest.Version, sourceRef, PublishValidationOptions{
		PublicationKind: publicationKind,
	}); err != nil {
		return fmt.Errorf("%w: %v", ErrPublishDeclarationInvalid, err)
	}
	if err := validateLocalSourceState(declaration.LocalSource); err != nil {
		return fmt.Errorf("%w: localSource: %v", ErrPublishDeclarationInvalid, err)
	}
	return nil
}

func DeclarationDigest(declaration *PublishDeclaration) (string, error) {
	if declaration == nil {
		return "", fmt.Errorf("declaration is required")
	}
	artifacts := append([]PublishDeclarationArtifact(nil), declaration.Artifacts...)
	sort.Slice(artifacts, func(i, j int) bool {
		if artifacts[i].Platform != artifacts[j].Platform {
			return artifacts[i].Platform < artifacts[j].Platform
		}
		return artifacts[i].Filename < artifacts[j].Filename
	})
	for i := range artifacts {
		artifacts[i].Platform = strings.TrimSpace(artifacts[i].Platform)
		artifacts[i].Filename = strings.TrimSpace(artifacts[i].Filename)
		artifacts[i].SHA256 = strings.ToLower(strings.TrimSpace(artifacts[i].SHA256))
	}
	kind := declaration.PublicationKind
	if kind == "" {
		kind = PublicationKindLocal
	}
	payload, err := json.Marshal(struct {
		Schema          string                       `json:"schema"`
		Manifest        *providermanifestv1.Manifest `json:"manifest"`
		ManifestPath    string                       `json:"manifestPath,omitempty"`
		ReleaseMetadata *providerrelease.Metadata    `json:"releaseMetadata"`
		Artifacts       []PublishDeclarationArtifact `json:"artifacts"`
		PublicationKind PublicationKind              `json:"publicationKind,omitempty"`
		SourceRef       string                       `json:"sourceRef,omitempty"`
		LocalSource     *LocalSourceState            `json:"localSource,omitempty"`
		BuilderVersion  string                       `json:"builderVersion,omitempty"`
	}{
		Schema:          PublishDeclarationSchemaVersion,
		Manifest:        declaration.Manifest,
		ManifestPath:    strings.TrimSpace(declaration.ManifestPath),
		ReleaseMetadata: declaration.ReleaseMetadata,
		Artifacts:       artifacts,
		PublicationKind: kind,
		SourceRef:       strings.ToLower(strings.TrimSpace(declaration.SourceRef)),
		LocalSource:     declaration.LocalSource,
		BuilderVersion:  strings.TrimSpace(declaration.BuilderVersion),
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func DerivePublishID(app, version, declarationDigest string) string {
	payload := strings.TrimSpace(app) + "\x00" + strings.TrimSpace(version) + "\x00" + strings.TrimSpace(declarationDigest)
	sum := sha256.Sum256([]byte(payload))
	return "pub_" + hex.EncodeToString(sum[:16])
}

func PublishStagingPrefix(appName, version, declarationDigest string) string {
	return path.Join("apps", strings.TrimSpace(appName), "publish-staging", strings.TrimSpace(version), strings.TrimSpace(declarationDigest))
}

func PublishStagingArtifactPath(stagingPrefix, platform, filename string) string {
	return path.Join(stagingPrefix, "artifacts", strings.TrimSpace(platform), strings.TrimSpace(filename))
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

func (s *StatelessPublishService) Begin(ctx context.Context, input AdminPublishInput) (*AdminPublishResponse, error) {
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
	if entry, err := s.matchPublishedIdentity(input.StorageRoot, input.App, declaration, publishID, digest); err != nil {
		if entry != nil && errors.Is(err, ErrPublishVersionConflict) {
			return nil, fmt.Errorf("%w: version %q is already published with different identity", ErrPublishVersionConflict, version)
		}
		return nil, err
	} else if entry != nil {
		return adminPublishResponse(publishID, input.App, input.Registry, version, PublishStatePublished, nil, entry.PublishedAt), nil
	}
	uploads, err := s.signMissingUploads(input.StorageRoot, stagingPrefix, declaration, limits)
	if err != nil {
		return nil, err
	}
	_ = ctx
	return adminPublishResponse(publishID, input.App, input.Registry, version, PublishStateUploading, uploads, time.Time{}), nil
}

func (s *StatelessPublishService) Finalize(ctx context.Context, input AdminPublishInput) (*AdminPublishResponse, error) {
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
	sourceRef := strings.ToLower(strings.TrimSpace(declaration.SourceRef))

	if entry, matchErr := s.matchPublishedIdentity(input.StorageRoot, input.App, declaration, publishID, digest); matchErr != nil {
		if entry != nil && errors.Is(matchErr, ErrPublishVersionConflict) {
			return nil, fmt.Errorf("%w: version %q is already published with different identity", ErrPublishVersionConflict, version)
		}
		return nil, matchErr
	} else if entry != nil {
		return adminPublishResponse(publishID, input.App, input.Registry, version, PublishStatePublished, nil, entry.PublishedAt), nil
	}

	if err := s.promoteStagingArtifacts(stagingPrefix, declaration, input.StorageRoot, sourceRef); err != nil {
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
	result, err := s.publishWithConcurrentRetry(input, declaration, publishID, digest, req)
	if err != nil {
		return nil, err
	}
	if !publishIndexCommitted(result) {
		if entry, matchErr := s.matchPublishedIdentity(input.StorageRoot, input.App, declaration, publishID, digest); matchErr != nil || entry == nil {
			return nil, fmt.Errorf("registry index was not updated")
		}
	}

	entry, loadErr := LoadPublishedEntry(s.Store, input.StorageRoot, input.App, version)
	if loadErr != nil {
		return nil, loadErr
	}
	if err := verifyPublishedEntry(entry, input.App, version, publishID, digest, sourceRef); err != nil {
		if entry != nil {
			if retryEntry, retryErr := s.matchPublishedIdentity(input.StorageRoot, input.App, declaration, publishID, digest); retryErr == nil && retryEntry != nil {
				return adminPublishResponse(publishID, input.App, input.Registry, version, PublishStatePublished, nil, retryEntry.PublishedAt), nil
			}
		}
		return nil, err
	}
	_ = ctx
	return adminPublishResponse(publishID, input.App, input.Registry, version, PublishStatePublished, nil, entry.PublishedAt), nil
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
	stagingPrefix = PublishStagingPrefix(app, version, digest)
	return publishID, digest, version, stagingPrefix, nil
}

func (s *StatelessPublishService) matchPublishedIdentity(
	storageRoot, appName string,
	declaration *PublishDeclaration,
	publishID, declarationDigest string,
) (*Entry, error) {
	if declaration == nil || declaration.Manifest == nil {
		return nil, fmt.Errorf("declaration is required")
	}
	version := strings.TrimSpace(declaration.Manifest.Version)
	entry, err := LoadPublishedEntry(s.Store, storageRoot, strings.TrimSpace(appName), version)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, nil
	}
	sourceRef := strings.ToLower(strings.TrimSpace(declaration.SourceRef))
	if err := verifyPublishedEntry(entry, appName, version, publishID, declarationDigest, sourceRef); err != nil {
		if strings.Contains(err.Error(), "publishId") || strings.Contains(err.Error(), "declarationDigest") || strings.Contains(err.Error(), "sourceRef") {
			return entry, ErrPublishVersionConflict
		}
		return entry, err
	}
	return entry, nil
}

func verifyPublishedEntry(entry *Entry, app, version, publishID, declarationDigest, sourceRef string) error {
	if entry == nil {
		return fmt.Errorf("%w: entry is missing", ErrPublishReconcileMismatch)
	}
	if strings.TrimSpace(entry.App) != strings.TrimSpace(app) {
		return fmt.Errorf("%w: app %q != %q", ErrPublishReconcileMismatch, entry.App, app)
	}
	if strings.TrimSpace(entry.Version) != strings.TrimSpace(version) {
		return fmt.Errorf("%w: version %q != %q", ErrPublishReconcileMismatch, entry.Version, version)
	}
	if strings.TrimSpace(entry.PublishID) != strings.TrimSpace(publishID) {
		return fmt.Errorf("%w: publishId %q != %q", ErrPublishReconcileMismatch, entry.PublishID, publishID)
	}
	if strings.TrimSpace(entry.DeclarationDigest) != strings.TrimSpace(declarationDigest) {
		return fmt.Errorf("%w: declarationDigest mismatch", ErrPublishReconcileMismatch)
	}
	if !strings.EqualFold(strings.TrimSpace(entry.SourceRef), strings.TrimSpace(sourceRef)) {
		return fmt.Errorf("%w: sourceRef %q != %q", ErrPublishReconcileMismatch, entry.SourceRef, sourceRef)
	}
	return nil
}

func publishIndexCommitted(result PublishResult) bool {
	return result.Index == CatalogWriteOutcomeUpdated || result.Index == CatalogWriteOutcomeUnchanged
}

func verifyArtifactDescribed(described ObjectDescription, artifact PublishDeclarationArtifact) error {
	if described.Generation == 0 {
		return fmt.Errorf("%w: %s", ErrPublishUploadMissing, artifact.Platform)
	}
	if !strings.EqualFold(strings.TrimSpace(described.SHA256), strings.TrimSpace(artifact.SHA256)) {
		return fmt.Errorf("%w: %s", ErrPublishUploadMismatch, artifact.Platform)
	}
	if artifact.Size > 0 && described.Size > 0 && described.Size != artifact.Size {
		return fmt.Errorf("%w: %s size mismatch", ErrPublishUploadMismatch, artifact.Platform)
	}
	return nil
}

func (s *StatelessPublishService) signMissingUploads(storageRoot, stagingPrefix string, declaration *PublishDeclaration, limits PublishLimits) ([]AdminPublishUpload, error) {
	uploads := make([]AdminPublishUpload, 0, len(declaration.Artifacts))
	expiresAt := s.now().Add(limits.UploadURLTTL)
	sourceRef := strings.ToLower(strings.TrimSpace(declaration.SourceRef))
	for _, artifact := range declaration.Artifacts {
		stagingURL := StorageURL(storageRoot, PublishStagingArtifactPath(stagingPrefix, artifact.Platform, artifact.Filename))
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
			SourceRef: sourceRef, ExpiresAt: expiresAt,
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

func (s *StatelessPublishService) promoteStagingArtifacts(stagingPrefix string, declaration *PublishDeclaration, storageRoot, sourceRef string) error {
	layout, err := ResolvePublishLayout(declaration.Manifest.Source, declaration.Manifest.Version)
	if err != nil {
		return err
	}
	for _, artifact := range declaration.Artifacts {
		stagingURL := StorageURL(storageRoot, PublishStagingArtifactPath(stagingPrefix, artifact.Platform, artifact.Filename))
		described, err := s.Store.DescribeObject(stagingURL)
		if err != nil {
			return err
		}
		if err := verifyArtifactDescribed(described, artifact); err != nil {
			return err
		}
		filename := strings.TrimSpace(artifact.Filename)
		finalRel := filepath.ToSlash(filepath.Join(layout.ArtifactPrefix, filename))
		if err := s.Store.PromoteObject(PromoteObjectInput{
			SourceURL: stagingURL, SourceGeneration: described.Generation,
			DestURL: StorageURL(storageRoot, finalRel), ExpectedSHA256: artifact.SHA256, SourceRef: sourceRef,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *StatelessPublishService) publishWithConcurrentRetry(
	input AdminPublishInput,
	declaration *PublishDeclaration,
	publishID, digest string,
	req PublishRequest,
) (PublishResult, error) {
	var lastResult PublishResult
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		lastResult, lastErr = s.Writer.Publish(req, PublishProgress{})
		if lastErr == nil && publishIndexCommitted(lastResult) {
			return lastResult, nil
		}
		if entry, matchErr := s.matchPublishedIdentity(input.StorageRoot, input.App, declaration, publishID, digest); matchErr == nil && entry != nil {
			return lastResult, nil
		}
		if lastErr != nil && !errors.Is(lastErr, ErrObjectPreconditionFailed) && !CatalogPreconditionFailed(lastErr) {
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
	sourceRef := strings.ToLower(strings.TrimSpace(declaration.SourceRef))
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
			Target: strings.TrimSpace(artifact.Platform), Filename: filename,
			StorageURL: StorageURL(input.StorageRoot, rel), PublicURL: PublicURL(input.PublicRoot, rel),
			SHA256: strings.ToLower(strings.TrimSpace(artifact.SHA256)),
		})
	}
	entry, err := BuildEntry(BuildEntryInput{
		Manifest: declaration.Manifest, Version: version, SourceRef: sourceRef,
		ManifestPath: strings.TrimSpace(declaration.ManifestPath), PublicationKind: publicationKind,
		PublishID: publishID, BuilderVersion: builderVersion, DeclarationDigest: digest,
		LocalSource: cloneLocalSourceState(declaration.LocalSource), Release: declaration.ReleaseMetadata,
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
			StorageURL: StorageURL(input.StorageRoot, layout.EntryPath),
			PublicURL:  PublicURL(input.PublicRoot, layout.EntryPath), SHA256: entryDigest,
		},
		IndexObject: PublishObject{
			Kind:       PublishObjectKindIndex,
			StorageURL: StorageURL(input.StorageRoot, layout.IndexPath),
			PublicURL:  PublicURL(input.PublicRoot, layout.IndexPath),
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
