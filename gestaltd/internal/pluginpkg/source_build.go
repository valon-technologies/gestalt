package pluginpkg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func RunSourceReleaseBuild(manifestPath string, manifest *pluginmanifestv1.Manifest) error {
	if manifest == nil || manifest.Release == nil || manifest.Release.Build == nil {
		return nil
	}

	rootDir := filepath.Dir(manifestPath)
	workdir := rootDir
	if manifest.Release.Build.Workdir != "" {
		workdir = filepath.Join(rootDir, filepath.FromSlash(manifest.Release.Build.Workdir))
	}

	info, err := os.Stat(workdir)
	if err != nil {
		return fmt.Errorf("stat release.build.workdir %q: %w", manifest.Release.Build.Workdir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("release.build.workdir %q is not a directory", manifest.Release.Build.Workdir)
	}

	cmd := exec.Command(manifest.Release.Build.Command[0], manifest.Release.Build.Command[1:]...)
	cmd.Dir = workdir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run release.build.command: %w", err)
	}
	return nil
}
