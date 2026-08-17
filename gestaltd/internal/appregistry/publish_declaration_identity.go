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

// NormalizeAndValidatePublishDeclaration deep-clones declaration, validates the clone
// (including mutation-prone providerrelease validation), and returns the canonical form.
// Caller input is never modified.
func NormalizeAndValidatePublishDeclaration(appName string, declaration *PublishDeclaration, limits PublishLimits) (*PublishDeclaration, error) {
	if declaration == nil {
		return nil, fmt.Errorf("%w: declaration is required", ErrPublishDeclarationInvalid)
	}
	clone, err := deepClonePublishDeclaration(declaration)
	if err != nil {
		return nil, fmt.Errorf("%w: clone declaration: %v", ErrPublishDeclarationInvalid, err)
	}
	if err := validatePublishDeclaration(appName, clone, limits); err != nil {
		return nil, err
	}
	return canonicalPublishDeclaration(clone)
}

func ValidatePublishDeclaration(appName string, declaration *PublishDeclaration, limits PublishLimits) error {
	_, err := NormalizeAndValidatePublishDeclaration(appName, declaration, limits)
	return err
}

func deepClonePublishDeclaration(declaration *PublishDeclaration) (*PublishDeclaration, error) {
	data, err := json.Marshal(declaration)
	if err != nil {
		return nil, err
	}
	var clone PublishDeclaration
	if err := json.Unmarshal(data, &clone); err != nil {
		return nil, err
	}
	return &clone, nil
}

func validatePublishDeclaration(appName string, declaration *PublishDeclaration, limits PublishLimits) error {
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
	if strings.TrimSpace(declaration.BuilderVersion) == "" {
		return fmt.Errorf("%w: builderVersion is required", ErrPublishDeclarationInvalid)
	}
	return nil
}

func validatePublishReleaseMetadata(release *providerrelease.Metadata, manifest *providermanifestv1.Manifest, artifacts []PublishDeclarationArtifact) error {
	if release == nil || manifest == nil {
		return fmt.Errorf("release metadata and manifest are required")
	}
	releaseVersion := strings.TrimSpace(release.Version)
	manifestVersion := strings.TrimSpace(manifest.Version)
	if releaseVersion != manifestVersion {
		return fmt.Errorf("releaseMetadata version %q does not match manifest version %q", release.Version, manifest.Version)
	}
	releasePackage := strings.TrimSpace(release.Package)
	manifestSource := strings.TrimSpace(manifest.Source)
	if releasePackage != manifestSource {
		return fmt.Errorf("releaseMetadata package %q does not match manifest source %q", release.Package, manifest.Source)
	}
	releaseKind := providermanifestv1.NormalizeKind(release.Kind)
	manifestKind := providermanifestv1.NormalizeKind(manifest.Kind)
	if releaseKind != manifestKind {
		return fmt.Errorf("releaseMetadata kind %q does not match manifest kind %q", release.Kind, manifest.Kind)
	}
	releaseArtifacts, err := canonicalReleaseArtifacts(release.Artifacts)
	if err != nil {
		return err
	}
	declPlatforms := make(map[string]struct{}, len(artifacts))
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
		declPlatforms[platform] = struct{}{}
	}
	for platform := range releaseArtifacts {
		if _, ok := declPlatforms[platform]; !ok {
			return fmt.Errorf("declaration has no artifact for releaseMetadata platform %q", platform)
		}
	}
	return nil
}

func canonicalPublishDeclaration(declaration *PublishDeclaration) (*PublishDeclaration, error) {
	if declaration == nil {
		return nil, fmt.Errorf("declaration is required")
	}
	canonical := *declaration
	canonical.Schema = PublishDeclarationSchemaVersion
	if declaration.Manifest != nil {
		manifestCopy := *declaration.Manifest
		manifestCopy.Kind = providermanifestv1.NormalizeKind(manifestCopy.Kind)
		manifestCopy.Source = strings.TrimSpace(manifestCopy.Source)
		manifestCopy.Version = strings.TrimSpace(manifestCopy.Version)
		canonical.Manifest = &manifestCopy
	}
	if declaration.ReleaseMetadata != nil {
		releaseCopy := *declaration.ReleaseMetadata
		releaseCopy.Kind = providermanifestv1.NormalizeKind(releaseCopy.Kind)
		releaseCopy.Package = strings.TrimSpace(releaseCopy.Package)
		releaseCopy.Version = strings.TrimSpace(releaseCopy.Version)
		if releaseCopy.StaticValidation != nil && releaseCopy.StaticValidation.Manifest != nil {
			staticCopy := *releaseCopy.StaticValidation
			manifestCopy := *staticCopy.Manifest
			manifestCopy.Kind = providermanifestv1.NormalizeKind(manifestCopy.Kind)
			manifestCopy.Source = strings.TrimSpace(manifestCopy.Source)
			manifestCopy.Version = strings.TrimSpace(manifestCopy.Version)
			staticCopy.Manifest = &manifestCopy
			releaseCopy.StaticValidation = &staticCopy
		}
		canonicalArtifacts, err := canonicalReleaseArtifacts(releaseCopy.Artifacts)
		if err != nil {
			return nil, err
		}
		releaseCopy.Artifacts = canonicalArtifacts
		canonical.ReleaseMetadata = &releaseCopy
	}
	canonical.ManifestPath = strings.TrimSpace(canonical.ManifestPath)
	canonical.SourceRef = strings.ToLower(strings.TrimSpace(canonical.SourceRef))
	canonical.BuilderVersion = strings.TrimSpace(canonical.BuilderVersion)
	if canonical.PublicationKind == "" {
		canonical.PublicationKind = PublicationKindLocal
	}
	canonical.LocalSource = normalizeLocalSourceState(canonical.LocalSource)
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

func canonicalReleaseArtifacts(artifacts providerrelease.Artifacts) (providerrelease.Artifacts, error) {
	if len(artifacts) == 0 {
		return nil, nil
	}
	canonical := make(providerrelease.Artifacts, len(artifacts))
	for target, artifact := range artifacts {
		canonicalTarget, err := canonicalReleaseArtifactTarget(target)
		if err != nil {
			return nil, err
		}
		if _, ok := canonical[canonicalTarget]; ok {
			return nil, fmt.Errorf("duplicate release artifact target %q", canonicalTarget)
		}
		path := strings.TrimSpace(artifact.Path)
		digest, err := normalizePublishArtifactSHA256(artifact.SHA256)
		if err != nil {
			return nil, fmt.Errorf("release artifact %q sha256: %w", canonicalTarget, err)
		}
		canonical[canonicalTarget] = providerrelease.Artifact{
			Path:   path,
			SHA256: digest,
		}
	}
	return canonical, nil
}

func canonicalReleaseArtifactTarget(target string) (string, error) {
	target = strings.TrimSpace(target)
	switch target {
	case "":
		return "", fmt.Errorf("release artifact target is required")
	case providerrelease.GenericTarget:
		return providerrelease.GenericTarget, nil
	default:
		return normalizePublishPlatform(target)
	}
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
