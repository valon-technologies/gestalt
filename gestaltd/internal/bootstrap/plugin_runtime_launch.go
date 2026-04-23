package bootstrap

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/pluginruntime"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

type pluginRuntimeLaunch struct {
	command   string
	args      []string
	bundleDir string
	cleanup   func()
}

func preparePluginRuntimeLaunch(name string, entry *config.ProviderEntry, command string, args []string, cleanup func(), behavior RuntimeBehavior) (pluginRuntimeLaunch, error) {
	launch := pluginRuntimeLaunch{
		command: command,
		args:    slices.Clone(args),
		cleanup: cleanup,
	}
	if behavior.LaunchMode == RuntimeLaunchModeHostPath {
		return launch, nil
	}
	if entry == nil {
		return pluginRuntimeLaunch{}, fmt.Errorf("plugin entry is required for non-local plugin runtime")
	}
	manifestPath := strings.TrimSpace(entry.ResolvedManifestPath)
	if manifestPath == "" {
		return pluginRuntimeLaunch{}, fmt.Errorf("resolved manifest path is required for non-local plugin runtime")
	}
	rootDir := filepath.Dir(manifestPath)
	if !behavior.ExecutionTarget.IsSet() {
		return pluginRuntimeLaunch{}, fmt.Errorf("non-local plugin runtime must declare execution goos/goarch")
	}

	bundleDir, err := providerhost.NewPluginTempDir("gestalt-plugin-runtime-bundle-*")
	if err != nil {
		return pluginRuntimeLaunch{}, fmt.Errorf("create plugin runtime bundle dir: %w", err)
	}
	bundleCleanup := func() {
		_ = os.RemoveAll(bundleDir)
	}

	localCommand, err := stagePluginRuntimeBundle(name, manifestPath, bundleDir, behavior.ExecutionTarget.GOOS, behavior.ExecutionTarget.GOARCH)
	if err != nil {
		bundleCleanup()
		return pluginRuntimeLaunch{}, err
	}
	rel, err := filepath.Rel(bundleDir, localCommand)
	if err != nil {
		bundleCleanup()
		return pluginRuntimeLaunch{}, fmt.Errorf("resolve plugin runtime bundle command: %w", err)
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		bundleCleanup()
		return pluginRuntimeLaunch{}, fmt.Errorf("plugin runtime bundle command %q is outside bundle %q", localCommand, bundleDir)
	}

	launch.command = path.Join(pluginruntime.HostedPluginBundleRoot, filepath.ToSlash(rel))
	launch.args = rewritePluginRuntimeBundleArgs(rootDir, launch.args)
	launch.bundleDir = bundleDir
	launch.cleanup = chainCleanup(cleanup, bundleCleanup)
	return launch, nil
}

func rewritePluginRuntimeBundleArgs(rootDir string, args []string) []string {
	if len(args) == 0 {
		return nil
	}
	rewritten := slices.Clone(args)
	for i, arg := range rewritten {
		rewritten[i] = rewritePluginRuntimeBundleArg(rootDir, arg)
	}
	return rewritten
}

func rewritePluginRuntimeBundleArg(rootDir, arg string) string {
	arg = strings.TrimSpace(arg)
	if arg == "" || !filepath.IsAbs(arg) {
		return arg
	}
	rel, err := filepath.Rel(rootDir, arg)
	if err != nil {
		return arg
	}
	switch {
	case rel == ".":
		return pluginruntime.HostedPluginBundleRoot
	case rel == "..", strings.HasPrefix(rel, ".."+string(filepath.Separator)):
		return arg
	default:
		return path.Join(pluginruntime.HostedPluginBundleRoot, filepath.ToSlash(rel))
	}
}

func stagePluginRuntimeBundle(name, manifestPath, bundleDir, goos, goarch string) (string, error) {
	rootDir := filepath.Dir(manifestPath)
	hasSourceReleaseTarget, err := providerpkg.HasSourceReleaseTarget(rootDir, providermanifestv1.KindPlugin)
	if err != nil {
		return "", fmt.Errorf("prepare plugin runtime bundle: detect source release target: %w", err)
	}
	if hasSourceReleaseTarget {
		staged, err := providerpkg.StageSourcePreparedInstallDir(manifestPath, bundleDir, providerpkg.StageSourcePreparedInstallOptions{
			Kind:       providermanifestv1.KindPlugin,
			PluginName: name,
			GOOS:       goos,
			GOARCH:     goarch,
		})
		if err != nil {
			return "", fmt.Errorf("prepare plugin runtime bundle: stage source install for %s/%s: %w", goos, goarch, err)
		}
		entry := providerpkg.EntrypointForKind(staged.Manifest, providermanifestv1.KindPlugin)
		if entry == nil || strings.TrimSpace(entry.ArtifactPath) == "" {
			return "", fmt.Errorf("prepare plugin runtime bundle: staged source install has no executable entrypoint")
		}
		return filepath.Join(bundleDir, filepath.FromSlash(entry.ArtifactPath)), nil
	}

	if err := providerpkg.CopyPackageDir(rootDir, bundleDir); err != nil {
		return "", fmt.Errorf("prepare plugin runtime bundle: copy package dir: %w", err)
	}
	_, manifest, _, err := providerpkg.LoadManifestFromPath(bundleDir)
	if err != nil {
		return "", fmt.Errorf("prepare plugin runtime bundle: load staged manifest: %w", err)
	}
	artifact, err := artifactForPlatform(manifest, goos, goarch)
	if err != nil {
		return "", fmt.Errorf("prepare plugin runtime bundle: %w", err)
	}
	return filepath.Join(bundleDir, filepath.FromSlash(artifact.Path)), nil
}

func artifactForPlatform(manifest *providermanifestv1.Manifest, goos, goarch string) (*providermanifestv1.Artifact, error) {
	if manifest == nil {
		return nil, fmt.Errorf("manifest is required")
	}
	for i := range manifest.Artifacts {
		artifact := &manifest.Artifacts[i]
		if artifact.OS == goos && artifact.Arch == goarch {
			return artifact, nil
		}
	}
	return nil, fmt.Errorf("no artifact for platform %s/%s", goos, goarch)
}
