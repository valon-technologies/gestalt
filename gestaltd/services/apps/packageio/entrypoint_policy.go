package packageio

import (
	"fmt"
	"path"
	"strings"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/source"
)

type entrypointPolicy struct {
	mayDefault       bool
	omitInValidation bool
}

func entrypointPolicyFor(manifest *providermanifestv1.Manifest, kind string) entrypointPolicy {
	if manifest == nil || manifest.Entrypoint != nil {
		return entrypointPolicy{}
	}
	if len(manifest.Run) > 0 && (manifest.Build == nil || manifest.Build.PrepareOnly) {
		return entrypointPolicy{omitInValidation: true}
	}

	switch kind {
	case providermanifestv1.KindUI:
		if manifest.Build != nil && !manifest.Build.PrepareOnly {
			return entrypointPolicy{omitInValidation: true}
		}
		return entrypointPolicy{}
	case providermanifestv1.KindApp:
		if manifest.IsDeclarativeOnlyProvider() ||
			(manifest.Spec != nil && manifest.Spec.IsSpecLoaded()) ||
			(manifest.Spec != nil && manifest.Spec.IsManifestBacked()) {
			return entrypointPolicy{omitInValidation: true}
		}
		if manifest.Build != nil && !manifest.Build.PrepareOnly {
			return entrypointPolicy{mayDefault: true, omitInValidation: true}
		}
		return entrypointPolicy{omitInValidation: manifest.Build == nil || manifest.Build.PrepareOnly}
	case providermanifestv1.KindIdentity, providermanifestv1.KindAuthorization, providermanifestv1.KindExternalCredentials, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindWorkflow, providermanifestv1.KindAgent, providermanifestv1.KindSecrets, providermanifestv1.KindRuntime:
		if manifest.Build != nil && !manifest.Build.PrepareOnly {
			return entrypointPolicy{mayDefault: true, omitInValidation: true}
		}
		if manifest.Build == nil {
			return entrypointPolicy{mayDefault: true, omitInValidation: true}
		}
		return entrypointPolicy{omitInValidation: manifest.Build.PrepareOnly}
	default:
		return entrypointPolicy{}
	}
}

// DefaultSourceEntrypointArtifactPath returns .gestalt/build/<last source segment>
// for a source manifest. The path uses forward slashes.
func DefaultSourceEntrypointArtifactPath(manifest *providermanifestv1.Manifest) (string, error) {
	if manifest == nil {
		return "", fmt.Errorf("manifest is required")
	}
	if manifest.Source == "" {
		return "", fmt.Errorf("manifest source is required")
	}
	src, err := source.Parse(manifest.Source)
	if err != nil {
		return "", fmt.Errorf("manifest source: %w", err)
	}
	return path.Join(".gestalt", "build", src.AppName()), nil
}

// EffectiveSourceEntrypointForKind returns the explicit entrypoint when set, or the
// default source executable artifact path. It returns a copy and never mutates manifest.
func EffectiveSourceEntrypointForKind(manifest *providermanifestv1.Manifest, kind string) (*providermanifestv1.Entrypoint, error) {
	if manifest == nil {
		return nil, fmt.Errorf("manifest is required")
	}
	if kind == "" {
		var err error
		kind, err = ManifestKind(manifest)
		if err != nil {
			return nil, err
		}
	}
	entry := EntrypointForKind(manifest, kind)
	if entry != nil && strings.TrimSpace(entry.ArtifactPath) != "" {
		cloned := *entry
		cloned.Args = append([]string(nil), entry.Args...)
		return &cloned, nil
	}
	if !entrypointPolicyFor(manifest, kind).mayDefault {
		return nil, fmt.Errorf("entrypoint.artifactPath is required")
	}
	defaultPath, err := DefaultSourceEntrypointArtifactPath(manifest)
	if err != nil {
		return nil, err
	}
	cloned := &providermanifestv1.Entrypoint{ArtifactPath: defaultPath}
	if entry != nil {
		cloned.Args = append([]string(nil), entry.Args...)
	}
	return cloned, nil
}

// SourceEntrypointMayDefault reports whether a source manifest may rely on the
// default .gestalt/build/<source-name> artifact path in executable contexts.
func SourceEntrypointMayDefault(manifest *providermanifestv1.Manifest, kind string) bool {
	return entrypointPolicyFor(manifest, kind).mayDefault
}

func sourceEntrypointOmissionAllowed(manifest *providermanifestv1.Manifest, kind string) bool {
	return entrypointPolicyFor(manifest, kind).omitInValidation
}
