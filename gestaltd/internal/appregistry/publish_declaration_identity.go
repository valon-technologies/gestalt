package appregistry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

const PublishDeclarationSchemaVersion = "gestaltd.app.publish.declaration.v1"

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
	if err := validatePublishReleaseMetadata(declaration.ReleaseMetadata, manifest, declaration.Artifacts); err != nil {
		return fmt.Errorf("%w: %v", ErrPublishDeclarationInvalid, err)
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
		platform, err := normalizePublishPlatform(artifact.Platform)
		if err != nil {
			return fmt.Errorf("%w: artifact platform: %v", ErrPublishDeclarationInvalid, err)
		}
		if err := validatePublishArtifactFilename(artifact.Filename); err != nil {
			return fmt.Errorf("%w: artifact %q filename: %v", ErrPublishDeclarationInvalid, platform, err)
		}
		if _, err := normalizePublishArtifactSHA256(artifact.SHA256); err != nil {
			return fmt.Errorf("%w: artifact %q sha256: %v", ErrPublishDeclarationInvalid, platform, err)
		}
		if artifact.Size <= 0 {
			return fmt.Errorf("%w: artifact %q size must be greater than zero", ErrPublishDeclarationInvalid, platform)
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
		canonical, err := normalizePublishPlatform(required)
		if err != nil {
			return fmt.Errorf("%w: required platform %q: %v", ErrPublishDeclarationInvalid, required, err)
		}
		if _, ok := platforms[canonical]; !ok {
			return fmt.Errorf("%w: %q", ErrPublishRequiredPlatform, canonical)
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

func validatePublishReleaseMetadata(release *providerrelease.Metadata, manifest *providermanifestv1.Manifest, artifacts []PublishDeclarationArtifact) error {
	if release == nil || manifest == nil {
		return fmt.Errorf("release metadata and manifest are required")
	}
	if strings.TrimSpace(release.Version) != strings.TrimSpace(manifest.Version) {
		return fmt.Errorf("releaseMetadata version %q does not match manifest version %q", release.Version, manifest.Version)
	}
	if strings.TrimSpace(release.Package) != strings.TrimSpace(manifest.Source) {
		return fmt.Errorf("releaseMetadata package %q does not match manifest source %q", release.Package, manifest.Source)
	}
	releaseArtifacts, err := providerrelease.ArtifactsByTarget(release.Artifacts)
	if err != nil {
		return err
	}
	for _, artifact := range artifacts {
		platform, err := normalizePublishPlatform(artifact.Platform)
		if err != nil {
			return err
		}
		releaseArtifact, ok := releaseArtifacts[platform]
		if !ok {
			return fmt.Errorf("releaseMetadata has no artifact for platform %q", platform)
		}
		if strings.TrimSpace(releaseArtifact.Path) != strings.TrimSpace(artifact.Filename) {
			return fmt.Errorf("releaseMetadata artifact path %q does not match declaration filename %q for platform %q", releaseArtifact.Path, artifact.Filename, platform)
		}
		releaseDigest, err := normalizePublishArtifactSHA256(releaseArtifact.SHA256)
		if err != nil {
			return fmt.Errorf("releaseMetadata artifact %q sha256: %w", platform, err)
		}
		declDigest, err := normalizePublishArtifactSHA256(artifact.SHA256)
		if err != nil {
			return fmt.Errorf("declaration artifact %q sha256: %w", platform, err)
		}
		if releaseDigest != declDigest {
			return fmt.Errorf("releaseMetadata artifact sha256 for platform %q does not match declaration", platform)
		}
	}
	return nil
}

func canonicalPublishDeclaration(declaration *PublishDeclaration) (*PublishDeclaration, error) {
	if declaration == nil {
		return nil, fmt.Errorf("declaration is required")
	}
	canonical := *declaration
	if declaration.Manifest != nil {
		manifestCopy := *declaration.Manifest
		canonical.Manifest = &manifestCopy
	}
	if declaration.ReleaseMetadata != nil {
		releaseCopy := *declaration.ReleaseMetadata
		canonical.ReleaseMetadata = &releaseCopy
	}
	canonical.ManifestPath = strings.TrimSpace(canonical.ManifestPath)
	canonical.SourceRef = strings.ToLower(strings.TrimSpace(canonical.SourceRef))
	canonical.BuilderVersion = strings.TrimSpace(canonical.BuilderVersion)
	if canonical.PublicationKind == "" {
		canonical.PublicationKind = PublicationKindLocal
	}
	canonical.LocalSource = cloneLocalSourceState(canonical.LocalSource)
	canonical.Artifacts = make([]PublishDeclarationArtifact, len(declaration.Artifacts))
	for i, artifact := range declaration.Artifacts {
		platform, err := normalizePublishPlatform(artifact.Platform)
		if err != nil {
			return nil, err
		}
		digest, err := normalizePublishArtifactSHA256(artifact.SHA256)
		if err != nil {
			return nil, err
		}
		if err := validatePublishArtifactFilename(artifact.Filename); err != nil {
			return nil, err
		}
		canonical.Artifacts[i] = PublishDeclarationArtifact{
			Platform: platform,
			Filename: strings.TrimSpace(artifact.Filename),
			SHA256:   digest,
			Size:     artifact.Size,
		}
	}
	sort.Slice(canonical.Artifacts, func(i, j int) bool {
		if canonical.Artifacts[i].Platform != canonical.Artifacts[j].Platform {
			return canonical.Artifacts[i].Platform < canonical.Artifacts[j].Platform
		}
		return canonical.Artifacts[i].Filename < canonical.Artifacts[j].Filename
	})
	return &canonical, nil
}

func DeclarationDigest(declaration *PublishDeclaration) (string, error) {
	canonical, err := canonicalPublishDeclaration(declaration)
	if err != nil {
		return "", err
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
		Manifest:        canonical.Manifest,
		ManifestPath:    canonical.ManifestPath,
		ReleaseMetadata: canonical.ReleaseMetadata,
		Artifacts:       canonical.Artifacts,
		PublicationKind: canonical.PublicationKind,
		SourceRef:       canonical.SourceRef,
		LocalSource:     canonical.LocalSource,
		BuilderVersion:  canonical.BuilderVersion,
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

func declarationSourceRef(declaration *PublishDeclaration) string {
	if declaration == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(declaration.SourceRef))
}

func publishedExpectation(app, version, publishID, digest, sourceRef string) PublishedCommitExpectation {
	return PublishedCommitExpectation{
		App: app, Version: version, PublishID: publishID,
		DeclarationDigest: digest, SourceRef: sourceRef,
	}
}

func versionConflictError(version string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrPublishVersionConflict) {
		return fmt.Errorf("%w: version %q is already published with different identity", ErrPublishVersionConflict, version)
	}
	return err
}
