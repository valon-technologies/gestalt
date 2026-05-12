package providerpkg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

type ResolvedSourceBuild struct {
	Label   string
	Workdir string
	Command []string
	Output  string
	Inputs  []string
}

func EffectiveSourceBuild(manifest *providermanifestv1.Manifest) *ResolvedSourceBuild {
	if manifest == nil {
		return nil
	}
	if manifest.Build != nil {
		return &ResolvedSourceBuild{
			Label:   "build",
			Workdir: manifest.Build.Workdir,
			Command: append([]string(nil), manifest.Build.Command...),
			Output:  manifest.Build.Output,
			Inputs:  append([]string(nil), manifest.Build.Inputs...),
		}
	}
	if manifest.Release != nil && manifest.Release.Build != nil {
		return &ResolvedSourceBuild{
			Label:   "release.build",
			Workdir: manifest.Release.Build.Workdir,
			Command: append([]string(nil), manifest.Release.Build.Command...),
		}
	}
	return nil
}

func RunSourceReleaseBuild(manifestPath string, manifest *providermanifestv1.Manifest) error {
	build := EffectiveSourceBuild(manifest)
	if build == nil {
		return nil
	}
	if len(build.Command) == 0 {
		return fmt.Errorf("%s.command is required", build.Label)
	}

	rootDir := filepath.Dir(manifestPath)
	workdir := rootDir
	if build.Workdir != "" && build.Workdir != "." {
		workdir = filepath.Join(rootDir, filepath.FromSlash(build.Workdir))
	}

	info, err := os.Stat(workdir)
	if err != nil {
		return fmt.Errorf("stat %s.workdir %q: %w", build.Label, build.Workdir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s.workdir %q is not a directory", build.Label, build.Workdir)
	}
	if err := ensureReleaseBuildDependencies(workdir, build.Label); err != nil {
		return err
	}

	cmd := exec.Command(build.Command[0], build.Command[1:]...)
	cmd.Dir = workdir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run %s.command: %w", build.Label, err)
	}
	return nil
}

func ensureReleaseBuildDependencies(workdir, label string) error {
	packagePath := filepath.Join(workdir, typeScriptProjectFile)
	if _, err := os.Stat(packagePath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s package.json: %w", label, err)
	}
	if _, err := os.Stat(filepath.Join(workdir, "bun.lock")); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s bun.lock: %w", label, err)
	}
	if info, err := os.Stat(filepath.Join(workdir, "node_modules")); err == nil {
		if info.IsDir() {
			return nil
		}
		return fmt.Errorf("%s node_modules path %q is not a directory", label, filepath.Join(workdir, "node_modules"))
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s node_modules: %w", label, err)
	}

	bunPath, err := DetectBunExecutable()
	if err != nil {
		return err
	}
	if err := ensureTypeScriptDependencies(bunPath, workdir, label); err != nil {
		return fmt.Errorf("prepare %s dependencies: %w", label, err)
	}
	return nil
}
