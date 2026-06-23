package providerpkg

import (
	"fmt"
	"strings"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func ReleaseRequiresBuild(manifest *providermanifestv1.Manifest) bool {
	kind, err := ManifestKind(manifest)
	if err != nil {
		return false
	}
	return releaseRequiresBuildForKind(manifest, kind)
}

func releaseRequiresBuildForKind(manifest *providermanifestv1.Manifest, kind string) bool {
	switch kind {
	case providermanifestv1.KindApp:
		return manifest != nil && manifest.Entrypoint == nil && (manifest.Spec == nil || !manifest.Spec.IsManifestBacked())
	case providermanifestv1.KindIdentity, providermanifestv1.KindAuthorization, providermanifestv1.KindExternalCredentials, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindWorkflow, providermanifestv1.KindAgent, providermanifestv1.KindSecrets, providermanifestv1.KindRuntime:
		return EntrypointForKind(manifest, kind) == nil
	default:
		return false
	}
}

func HasExplicitSourceRun(manifest *providermanifestv1.Manifest) bool {
	return manifest != nil && len(manifest.Run) > 0
}

func ValidateExplicitRunPackaging(root string, manifest *providermanifestv1.Manifest) error {
	if !HasExplicitSourceRun(manifest) {
		return nil
	}
	kind, err := ManifestKind(manifest)
	if err != nil {
		return err
	}
	if kind == providermanifestv1.KindUI || !releaseRequiresBuildForKind(manifest, kind) {
		return nil
	}
	hasDeclaredBuild, err := HasSourceReleaseTarget(root, kind)
	if err != nil {
		return fmt.Errorf("validate %s release build: %w", kind, err)
	}
	if hasDeclaredBuild {
		return nil
	}
	return LocalOnlyRunReleaseError(kind)
}

func HasSourceReleaseTarget(root, kind string) (bool, error) {
	manifestPath, err := FindManifestFile(root)
	if err != nil {
		return false, err
	}
	_, manifest, err := ReadSourceManifestFile(manifestPath)
	if err != nil {
		return false, err
	}
	if SourceBuildProducesOutput(manifest) {
		return true, nil
	}
	ok, err := ImplicitGoBuildTarget(root, manifest)
	if err != nil {
		return false, err
	}
	return ok, nil
}

func MissingDeclaredSourceBuildError(manifest *providermanifestv1.Manifest, kind string) error {
	return missingDeclaredSourceBuildError(manifest, kind)
}

func LocalOnlyRunReleaseError(kind string) error {
	switch kind {
	case providermanifestv1.KindApp:
		return fmt.Errorf("run is local-only and cannot be packaged; add object-form build.command with entrypoint.artifactPath")
	case providermanifestv1.KindIdentity, providermanifestv1.KindAuthorization, providermanifestv1.KindExternalCredentials, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindWorkflow, providermanifestv1.KindAgent, providermanifestv1.KindSecrets, providermanifestv1.KindRuntime:
		return fmt.Errorf("run is local-only and cannot be packaged; add object-form build.command with entrypoint.artifactPath for kind %s", kind)
	default:
		return fmt.Errorf("run is local-only and cannot be packaged for %q", kind)
	}
}

func missingDeclaredSourceBuildError(manifest *providermanifestv1.Manifest, kind string) error {
	name := providerDisplayName(manifest)
	switch kind {
	case providermanifestv1.KindApp:
		return fmt.Errorf("%s: declare object-form build.command and entrypoint.artifactPath", name)
	default:
		return fmt.Errorf("%s: declare object-form build.command and entrypoint.artifactPath for kind %s", name, kind)
	}
}

func providerDisplayName(manifest *providermanifestv1.Manifest) string {
	if manifest == nil {
		return "provider"
	}
	if strings.TrimSpace(manifest.DisplayName) != "" {
		return strings.TrimSpace(manifest.DisplayName)
	}
	if strings.TrimSpace(manifest.Source) != "" {
		parts := strings.Split(strings.TrimSpace(manifest.Source), "/")
		if last := parts[len(parts)-1]; last != "" {
			return last
		}
	}
	return "provider"
}
