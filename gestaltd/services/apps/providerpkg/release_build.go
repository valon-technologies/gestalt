package providerpkg

import (
	"strings"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

type SourceReleaseBuildMode int

const (
	SourceReleaseBuildNone SourceReleaseBuildMode = iota
	SourceReleaseBuildDeclared
	SourceReleaseBuildImplicitGo
	SourceReleaseBuildPrebuilt
)

func (m SourceReleaseBuildMode) RequiresPlatformBuild() bool {
	return m == SourceReleaseBuildDeclared || m == SourceReleaseBuildImplicitGo
}

type ResolvedSourceReleaseBuild struct {
	Kind       string
	Mode       SourceReleaseBuildMode
	Entrypoint *providermanifestv1.Entrypoint
}

// ResolveSourceReleaseBuild is the canonical release-build decision for a source
// provider tree. When root is empty, implicit Go detection is skipped.
func ResolveSourceReleaseBuild(root string, manifest *providermanifestv1.Manifest) (ResolvedSourceReleaseBuild, error) {
	if manifest == nil {
		return ResolvedSourceReleaseBuild{}, nil
	}
	kind, err := ManifestKind(manifest)
	if err != nil {
		return ResolvedSourceReleaseBuild{}, err
	}
	if kind == providermanifestv1.KindUI {
		return ResolvedSourceReleaseBuild{Kind: kind, Mode: SourceReleaseBuildNone}, nil
	}
	if HasExplicitSourceRun(manifest) && (manifest.Build == nil || manifest.Build.PrepareOnly) {
		return ResolvedSourceReleaseBuild{Kind: kind, Mode: SourceReleaseBuildNone}, nil
	}

	entry, err := effectiveReleaseEntrypoint(manifest, kind)
	if err != nil {
		return ResolvedSourceReleaseBuild{}, err
	}
	if entry == nil {
		return ResolvedSourceReleaseBuild{Kind: kind, Mode: SourceReleaseBuildNone}, nil
	}

	if SourceBuildProducesOutput(manifest) {
		return ResolvedSourceReleaseBuild{Kind: kind, Mode: SourceReleaseBuildDeclared, Entrypoint: entry}, nil
	}
	if root != "" && HasGoProviderPackage(root) && SupportsImplicitGoBuild(kind) {
		return ResolvedSourceReleaseBuild{Kind: kind, Mode: SourceReleaseBuildImplicitGo, Entrypoint: entry}, nil
	}
	if manifest.Entrypoint != nil && strings.TrimSpace(manifest.Entrypoint.ArtifactPath) != "" {
		return ResolvedSourceReleaseBuild{Kind: kind, Mode: SourceReleaseBuildPrebuilt, Entrypoint: entry}, nil
	}
	if releaseRequiresBuildForKind(manifest, kind) {
		return ResolvedSourceReleaseBuild{}, missingDeclaredSourceBuildError(manifest, kind)
	}
	return ResolvedSourceReleaseBuild{Kind: kind, Mode: SourceReleaseBuildNone, Entrypoint: entry}, nil
}

func effectiveReleaseEntrypoint(manifest *providermanifestv1.Manifest, kind string) (*providermanifestv1.Entrypoint, error) {
	entry, err := EffectiveSourceEntrypointForKind(manifest, kind)
	if err != nil {
		if releaseRequiresBuildForKind(manifest, kind) {
			return nil, missingDeclaredSourceBuildError(manifest, kind)
		}
		return nil, nil
	}
	if entry == nil || strings.TrimSpace(entry.ArtifactPath) == "" {
		if releaseRequiresBuildForKind(manifest, kind) {
			return nil, missingDeclaredSourceBuildError(manifest, kind)
		}
		return nil, nil
	}
	return entry, nil
}

func SourceReleaseBuildProducesOutput(root string, manifest *providermanifestv1.Manifest) (bool, error) {
	if SourceBuildProducesOutput(manifest) {
		return true, nil
	}
	resolved, err := ResolveSourceReleaseBuild(root, manifest)
	if err != nil {
		return false, err
	}
	return resolved.Mode.RequiresPlatformBuild(), nil
}
