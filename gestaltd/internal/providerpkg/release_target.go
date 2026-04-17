package providerpkg

import (
	"errors"
	"fmt"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

type releaseTarget struct {
	kind           string
	missingErr     error
	missingMessage string
	hasSource      func(string) (bool, error)
	validate       func(string, string, string) error
	buildBinary    func(string, string, string, string, string) (string, error)
}

func ReleaseRequiresBuild(manifest *providermanifestv1.Manifest) bool {
	kind, err := ManifestKind(manifest)
	if err != nil {
		return false
	}
	return releaseRequiresBuildForKind(manifest, kind)
}

func HasSourceReleaseTarget(root, kind string) (bool, error) {
	target, err := releaseTargetForKind(kind)
	if err != nil {
		return false, err
	}
	if target == nil {
		return false, nil
	}
	return target.hasSource(root)
}

func ValidateSourceReleaseTarget(root, kind, goos, goarch string) error {
	target, err := releaseTargetForKind(kind)
	if err != nil {
		return err
	}
	if target == nil {
		return fmt.Errorf("unsupported release build target kind %q", kind)
	}
	return target.validate(root, goos, goarch)
}

func BuildSourceReleaseBinary(root, outputPath, pluginName, kind, goos, goarch string) error {
	_, err := buildSourceReleaseBinaryResult(root, outputPath, pluginName, kind, goos, goarch)
	return err
}

func buildSourceReleaseBinaryResult(root, outputPath, pluginName, kind, goos, goarch string) (string, error) {
	target, err := releaseTargetForKind(kind)
	if err != nil {
		return "", err
	}
	if target == nil {
		return "", fmt.Errorf("unsupported release build target kind %q", kind)
	}
	return target.buildBinary(root, outputPath, pluginName, goos, goarch)
}

func IsMissingSourceReleaseTarget(err error, kind string) bool {
	target, targetErr := releaseTargetForKind(kind)
	if targetErr != nil || target == nil {
		return false
	}
	return errors.Is(err, target.missingErr)
}

func MissingSourceReleaseTargetError(kind string) error {
	target, err := releaseTargetForKind(kind)
	if err != nil {
		return err
	}
	if target == nil {
		return fmt.Errorf("unsupported release build target kind %q", kind)
	}
	return fmt.Errorf("%s", target.missingMessage)
}

func missingSourceReleaseTargetCause(kind string) error {
	target, err := releaseTargetForKind(kind)
	if err != nil {
		return err
	}
	if target == nil {
		return fmt.Errorf("unsupported release build target kind %q", kind)
	}
	return target.missingErr
}

func resolveSourceReleaseBuildTarget(root, kind string) (string, error) {
	target, err := releaseTargetForKind(kind)
	if err != nil {
		return "", err
	}
	if target == nil {
		return "", nil
	}
	ok, err := target.hasSource(root)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", target.missingErr
	}
	return target.kind, nil
}

func releaseRequiresBuildForKind(manifest *providermanifestv1.Manifest, kind string) bool {
	switch kind {
	case providermanifestv1.KindPlugin:
		return manifest != nil && manifest.Entrypoint == nil && (manifest.Spec == nil || !manifest.Spec.IsManifestBacked())
	case providermanifestv1.KindAuth, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindSecrets:
		return EntrypointForKind(manifest, kind) == nil
	default:
		return false
	}
}

func releaseTargetForKind(kind string) (*releaseTarget, error) {
	switch kind {
	case providermanifestv1.KindWebUI:
		return nil, nil
	case providermanifestv1.KindPlugin:
		return &releaseTarget{
			kind:           kind,
			missingErr:     ErrNoSourceProviderPackage,
			missingMessage: "no Go, Rust, Python, or TypeScript provider package found",
			hasSource:      HasSourceProviderPackage,
			validate:       ValidateSourceProviderRelease,
			buildBinary:    BuildSourceProviderReleaseBinary,
		}, nil
	case providermanifestv1.KindAuth, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindSecrets:
		return &releaseTarget{
			kind:           kind,
			missingErr:     ErrNoSourceComponentPackage,
			missingMessage: fmt.Sprintf("no Go, Rust, Python, or TypeScript %s source package found", kind),
			hasSource: func(root string) (bool, error) {
				return HasSourceComponentPackage(root, kind)
			},
			validate: func(root, goos, goarch string) error {
				return ValidateSourceComponentRelease(root, kind, goos, goarch)
			},
			buildBinary: func(root, outputPath, _ string, goos, goarch string) (string, error) {
				return BuildSourceComponentReleaseBinary(root, outputPath, kind, goos, goarch)
			},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported release build target kind %q", kind)
	}
}
