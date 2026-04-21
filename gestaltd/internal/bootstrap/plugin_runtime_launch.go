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

func preparePluginRuntimeLaunch(name string, entry *config.ProviderEntry, command string, args []string, cleanup func(), caps pluginruntime.Capabilities) (pluginRuntimeLaunch, error) {
	launch := pluginRuntimeLaunch{
		command: command,
		args:    slices.Clone(args),
		cleanup: cleanup,
	}
	if caps.HostPathExecution {
		return launch, nil
	}
	if entry == nil {
		return pluginRuntimeLaunch{}, fmt.Errorf("plugin entry is required for non-local plugin runtime")
	}
	manifestPath := strings.TrimSpace(entry.ResolvedManifestPath)
	if manifestPath == "" {
		return pluginRuntimeLaunch{}, fmt.Errorf("resolved manifest path is required for non-local plugin runtime")
	}
	if strings.TrimSpace(caps.ExecutionGOOS) == "" || strings.TrimSpace(caps.ExecutionGOARCH) == "" {
		return pluginRuntimeLaunch{}, fmt.Errorf("non-local plugin runtime must declare execution goos/goarch")
	}

	bundleDir, err := providerhost.NewPluginTempDir("gestalt-plugin-runtime-bundle-*")
	if err != nil {
		return pluginRuntimeLaunch{}, fmt.Errorf("create plugin runtime bundle dir: %w", err)
	}
	bundleCleanup := func() {
		_ = os.RemoveAll(bundleDir)
	}

	localCommand, err := stagePluginRuntimeBundle(name, manifestPath, bundleDir, caps.ExecutionGOOS, caps.ExecutionGOARCH)
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
	launch.bundleDir = bundleDir
	launch.cleanup = chainCleanup(cleanup, bundleCleanup)
	return launch, nil
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
	artifact, err := providerpkg.ArtifactForPlatform(manifest, goos, goarch)
	if err != nil {
		return "", fmt.Errorf("prepare plugin runtime bundle: %w", err)
	}
	return filepath.Join(bundleDir, filepath.FromSlash(artifact.Path)), nil
}
