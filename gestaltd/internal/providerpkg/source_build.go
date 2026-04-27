package providerpkg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func RunSourceReleaseBuild(manifestPath string, manifest *providermanifestv1.Manifest) error {
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
	if err := ensureReleaseBuildDependencies(workdir); err != nil {
		return err
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

func ensureReleaseBuildDependencies(workdir string) error {
	packagePath := filepath.Join(workdir, typeScriptProjectFile)
	if _, err := os.Stat(packagePath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat release.build package.json: %w", err)
	}
	if _, err := os.Stat(filepath.Join(workdir, "bun.lock")); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat release.build bun.lock: %w", err)
	}
	if info, err := os.Stat(filepath.Join(workdir, "node_modules")); err == nil {
		if info.IsDir() {
			return nil
		}
		return fmt.Errorf("release build node_modules path %q is not a directory", filepath.Join(workdir, "node_modules"))
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat release build node_modules: %w", err)
	}

	bunPath, err := DetectBunExecutable()
	if err != nil {
		return err
	}
	if err := ensureTypeScriptDependencies(bunPath, workdir, "release build"); err != nil {
		return fmt.Errorf("prepare release.build dependencies: %w", err)
	}
	return nil
}
