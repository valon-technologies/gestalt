package providerpkg

import (
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

type SourceReleaseBuildMode int

const (
	SourceReleaseBuildNone SourceReleaseBuildMode = iota
	SourceReleaseBuildDeclared
)

func (m SourceReleaseBuildMode) RequiresPlatformBuild() bool {
	return m == SourceReleaseBuildDeclared
}

type ResolvedSourceReleaseBuild struct {
	Kind string
	Mode SourceReleaseBuildMode
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

	if SourceBuildProducesOutput(manifest) {
		return ResolvedSourceReleaseBuild{Kind: kind, Mode: SourceReleaseBuildDeclared}, nil
	}
	if releaseRequiresBuildForKind(manifest, kind) {
		return ResolvedSourceReleaseBuild{}, missingDeclaredSourceBuildError(manifest, kind)
	}
	return ResolvedSourceReleaseBuild{Kind: kind, Mode: SourceReleaseBuildNone}, nil
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
