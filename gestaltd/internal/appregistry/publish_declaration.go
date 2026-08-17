package appregistry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

const PublishDeclarationSchemaVersion = "gestaltd.app.publish.declaration.v1"

var (
	ErrPublishDeclarationInvalid  = errors.New("publish declaration is invalid")
	ErrPublishVersionConflict     = errors.New("publish version conflict")
	ErrPublishSessionUnavailable  = errors.New("app registry publish is unavailable")
	ErrPublishUploadMissing       = errors.New("publish upload is missing")
	ErrPublishUploadMismatch      = errors.New("publish upload mismatch")
	ErrPublishLeaseExpired        = errors.New("publish upload lease expired")
	ErrPublishSessionFailed       = errors.New("publish session failed")
	ErrPublishArtifactLimit       = errors.New("publish artifact limit exceeded")
	ErrPublishRequiredPlatform    = errors.New("required publish platform missing")
	ErrPublishAppIdentityMismatch = errors.New("publish app identity mismatch")
	ErrPublishRegistryNotEnrolled = errors.New("app is not enrolled in the registry")
)

// PublishDeclaration is the canonical release declaration for a remote publish session.
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

func ValidatePublishDeclaration(appName string, declaration *PublishDeclaration, limits PublishSessionLimits) error {
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
	canonical, err := canonicalDeclarationJSON(declaration)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func PublishSessionDedupeKey(app, version, declarationDigest string) string {
	return strings.TrimSpace(app) + "\x00" + strings.TrimSpace(version) + "\x00" + strings.TrimSpace(declarationDigest)
}

func canonicalDeclarationJSON(declaration *PublishDeclaration) ([]byte, error) {
	if declaration == nil {
		return nil, fmt.Errorf("declaration is required")
	}
	normalized := *declaration
	normalized.Schema = PublishDeclarationSchemaVersion
	normalized.ManifestPath = strings.TrimSpace(normalized.ManifestPath)
	normalized.SourceRef = strings.ToLower(strings.TrimSpace(normalized.SourceRef))
	normalized.BuilderVersion = strings.TrimSpace(normalized.BuilderVersion)
	if normalized.PublicationKind == "" {
		normalized.PublicationKind = PublicationKindLocal
	}
	sort.Slice(normalized.Artifacts, func(i, j int) bool {
		left := normalized.Artifacts[i]
		right := normalized.Artifacts[j]
		if left.Platform != right.Platform {
			return left.Platform < right.Platform
		}
		return left.Filename < right.Filename
	})
	for i := range normalized.Artifacts {
		normalized.Artifacts[i].Platform = strings.TrimSpace(normalized.Artifacts[i].Platform)
		normalized.Artifacts[i].Filename = strings.TrimSpace(normalized.Artifacts[i].Filename)
		normalized.Artifacts[i].SHA256 = strings.ToLower(strings.TrimSpace(normalized.Artifacts[i].SHA256))
	}
	return json.Marshal(normalized)
}

func PublishStagingPrefix(appName, publishID string) string {
	return path.Join("apps", strings.TrimSpace(appName), "publish-staging", strings.TrimSpace(publishID))
}

func PublishStagingArtifactPath(stagingPrefix, platform, filename string) string {
	return path.Join(stagingPrefix, "artifacts", strings.TrimSpace(platform), strings.TrimSpace(filename))
}

func EncodePublishDeclaration(declaration *PublishDeclaration) ([]byte, error) {
	return json.Marshal(declaration)
}

func DecodePublishDeclaration(data []byte) (*PublishDeclaration, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("publish declaration is required")
	}
	var declaration PublishDeclaration
	if err := json.Unmarshal(data, &declaration); err != nil {
		return nil, fmt.Errorf("decode publish declaration: %w", err)
	}
	return &declaration, nil
}
