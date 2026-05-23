package providerpkg

import (
	"errors"
	"fmt"

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
	case providermanifestv1.KindAuthentication, providermanifestv1.KindAuthorization, providermanifestv1.KindExternalCredentials, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindWorkflow, providermanifestv1.KindAgent, providermanifestv1.KindSecrets, providermanifestv1.KindRuntime:
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
	hasSource, err := HasSourceReleaseTarget(root, kind)
	if err != nil {
		return fmt.Errorf("detect source %s package: %w", kind, err)
	}
	if hasSource {
		return nil
	}
	return LocalOnlyRunReleaseError(kind)
}

func HasSourceReleaseTarget(root, kind string) (bool, error) {
	switch kind {
	case providermanifestv1.KindApp:
		return HasSourceProviderPackage(root)
	case providermanifestv1.KindUI:
		return false, nil
	case providermanifestv1.KindAuthentication, providermanifestv1.KindAuthorization, providermanifestv1.KindExternalCredentials, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindWorkflow, providermanifestv1.KindAgent, providermanifestv1.KindSecrets, providermanifestv1.KindRuntime:
		return HasSourceComponentPackage(root, kind)
	default:
		return false, fmt.Errorf("unsupported release build target kind %q", kind)
	}
}

func ValidateSourceReleaseTarget(root, kind, goos, goarch string) error {
	switch kind {
	case providermanifestv1.KindApp:
		return ValidateSourceProviderRelease(root, goos, goarch)
	case providermanifestv1.KindAuthentication, providermanifestv1.KindAuthorization, providermanifestv1.KindExternalCredentials, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindWorkflow, providermanifestv1.KindAgent, providermanifestv1.KindSecrets, providermanifestv1.KindRuntime:
		return ValidateSourceComponentRelease(root, kind, goos, goarch)
	default:
		return fmt.Errorf("unsupported release build target kind %q", kind)
	}
}

func BuildSourceReleaseBinary(root, outputPath, pluginName, kind, goos, goarch string) error {
	switch kind {
	case providermanifestv1.KindApp:
		_, err := BuildSourceProviderReleaseBinary(root, outputPath, pluginName, goos, goarch)
		return err
	case providermanifestv1.KindAuthentication, providermanifestv1.KindAuthorization, providermanifestv1.KindExternalCredentials, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindWorkflow, providermanifestv1.KindAgent, providermanifestv1.KindSecrets, providermanifestv1.KindRuntime:
		_, err := BuildSourceComponentReleaseBinary(root, outputPath, kind, goos, goarch)
		return err
	default:
		return fmt.Errorf("unsupported release build target kind %q", kind)
	}
}

func IsMissingSourceReleaseTarget(err error, kind string) bool {
	switch kind {
	case providermanifestv1.KindApp:
		return errors.Is(err, ErrNoSourceProviderPackage)
	case providermanifestv1.KindAuthentication, providermanifestv1.KindAuthorization, providermanifestv1.KindExternalCredentials, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindWorkflow, providermanifestv1.KindAgent, providermanifestv1.KindSecrets, providermanifestv1.KindRuntime:
		return errors.Is(err, ErrNoSourceComponentPackage)
	default:
		return false
	}
}

func MissingSourceReleaseTargetError(kind string) error {
	switch kind {
	case providermanifestv1.KindApp:
		return fmt.Errorf("no Go, Rust, Python, or TypeScript provider package found")
	case providermanifestv1.KindAuthorization:
		return fmt.Errorf("no Go, Rust, Python, or TypeScript authorization source package found")
	case providermanifestv1.KindExternalCredentials:
		return fmt.Errorf("no Go externalcredentials source package found")
	case providermanifestv1.KindAuthentication, providermanifestv1.KindCache, providermanifestv1.KindIndexedDB, providermanifestv1.KindS3, providermanifestv1.KindWorkflow, providermanifestv1.KindAgent, providermanifestv1.KindSecrets:
		return fmt.Errorf("no Go, Rust, Python, or TypeScript %s source package found", kind)
	case providermanifestv1.KindRuntime:
		return fmt.Errorf("no Go runtime source package found")
	default:
		return fmt.Errorf("unsupported release build target kind %q", kind)
	}
}

func LocalOnlyRunReleaseError(kind string) error {
	switch kind {
	case providermanifestv1.KindApp:
		return fmt.Errorf("run is local-only and cannot be packaged; add SDK-native provider metadata or object-form build.command with entrypoint.artifactPath")
	case providermanifestv1.KindAuthentication, providermanifestv1.KindAuthorization, providermanifestv1.KindExternalCredentials, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindWorkflow, providermanifestv1.KindAgent, providermanifestv1.KindSecrets, providermanifestv1.KindRuntime:
		return fmt.Errorf("run is local-only and cannot be packaged; add SDK-native %s provider metadata or object-form build.command with entrypoint.artifactPath", kind)
	default:
		return fmt.Errorf("run is local-only and cannot be packaged for %q", kind)
	}
}
