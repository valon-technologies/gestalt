package providerpkg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

type ResolvedSourceBuild struct {
	Label       string
	Workdir     string
	Command     []string
	Output      string
	Inputs      []string
	PrepareOnly bool
}

type SourceBuildOptions struct {
	GOOS   string
	GOARCH string
	LibC   string
}

type SourceExecution struct {
	Command string
	Args    []string
	Workdir string
	Cleanup func()
}

func EffectiveSourceBuild(manifest *providermanifestv1.Manifest) *ResolvedSourceBuild {
	if manifest == nil {
		return nil
	}
	if manifest.Build != nil {
		return &ResolvedSourceBuild{
			Label:       "build",
			Workdir:     manifest.Build.Workdir,
			Command:     append([]string(nil), manifest.Build.Command...),
			Output:      manifest.Build.Output,
			Inputs:      append([]string(nil), manifest.Build.Inputs...),
			PrepareOnly: manifest.Build.PrepareOnly,
		}
	}
	return nil
}

func SourceBuildProducesOutput(manifest *providermanifestv1.Manifest) bool {
	build := EffectiveSourceBuild(manifest)
	return build != nil && !build.PrepareOnly
}

func RunSourceBuild(manifestPath string, manifest *providermanifestv1.Manifest, opts SourceBuildOptions) error {
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

	var outputRel, outputKind, outputPath string
	if !build.PrepareOnly {
		var err error
		outputRel, outputKind, err = SourceBuildOutput(manifest)
		if err != nil {
			return err
		}
		outputPath = filepath.Join(rootDir, filepath.FromSlash(outputRel))
		if err := os.RemoveAll(outputPath); err != nil {
			return fmt.Errorf("remove %s output %q: %w", build.Label, outputRel, err)
		}
	}

	cmd := exec.Command(build.Command[0], build.Command[1:]...)
	cmd.Dir = workdir
	cmd.Env = sourceBuildEnv(os.Environ(), opts)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run %s.command: %w", build.Label, err)
	}
	if build.PrepareOnly {
		return nil
	}
	return verifySourceBuildOutput(outputPath, outputRel, outputKind, opts)
}

func SourceBuildOutput(manifest *providermanifestv1.Manifest) (rel string, kind string, err error) {
	if build := EffectiveSourceBuild(manifest); build != nil && build.PrepareOnly {
		return "", "", nil
	}
	manifestKind, err := ManifestKind(manifest)
	if err != nil {
		return "", "", err
	}
	if manifestKind == providermanifestv1.KindUI {
		output := SourceUIBuildOutput(manifest)
		if strings.TrimSpace(output) == "" {
			return "", "", fmt.Errorf("build.output or spec.assetRoot is required when build is set")
		}
		return output, providermanifestv1.KindUI, nil
	}
	entry := EntrypointForKind(manifest, manifestKind)
	if entry == nil || strings.TrimSpace(entry.ArtifactPath) == "" {
		return "", "", fmt.Errorf("entrypoint.artifactPath is required when build is set")
	}
	return entry.ArtifactPath, "executable", nil
}

func verifySourceBuildOutput(outputPath, outputRel, outputKind string, opts SourceBuildOptions) error {
	info, err := os.Stat(outputPath)
	if err != nil {
		return fmt.Errorf("build output %q not found: %w", outputRel, err)
	}
	switch outputKind {
	case providermanifestv1.KindUI:
		if !info.IsDir() {
			return fmt.Errorf("build output %q must be a directory", outputRel)
		}
	case "executable":
		if info.IsDir() {
			return fmt.Errorf("build output %q must be an executable file", outputRel)
		}
		goos := opts.GOOS
		if goos == "" {
			goos = runtime.GOOS
		}
		if goos != windowsOS && info.Mode()&0o111 == 0 {
			return fmt.Errorf("build output %q must be executable", outputRel)
		}
	default:
		return fmt.Errorf("unsupported build output kind %q", outputKind)
	}
	return nil
}

func sourceBuildEnv(base []string, opts SourceBuildOptions) []string {
	goos := opts.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	goarch := opts.GOARCH
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	env := append([]string(nil), base...)
	env = append(env,
		"GOOS="+goos,
		"GOARCH="+goarch,
		"GESTALT_TARGET_OS="+goos,
		"GESTALT_TARGET_ARCH="+goarch,
		"GESTALT_TARGET_PLATFORM="+goos+"/"+goarch,
	)
	if opts.LibC != "" {
		env = append(env, "GESTALT_TARGET_LIBC="+opts.LibC)
	}
	return env
}

func SourceBuildTarget(opts SourceBuildOptions) (string, string) {
	goos := opts.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	goarch := opts.GOARCH
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	return goos, goarch
}

func EnsureSourceBuildOutput(manifestPath string, manifest *providermanifestv1.Manifest, opts SourceBuildOptions) error {
	if err := RunSourceBuild(manifestPath, manifest, opts); err != nil {
		return err
	}
	if !SourceBuildProducesOutput(manifest) {
		return nil
	}
	outputRel, outputKind, err := SourceBuildOutput(manifest)
	if err != nil {
		if EffectiveSourceBuild(manifest) == nil {
			return nil
		}
		return err
	}
	outputPath := filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(outputRel))
	return verifySourceBuildOutput(outputPath, outputRel, outputKind, opts)
}

func SourceBuildInputs(manifest *providermanifestv1.Manifest) []string {
	build := EffectiveSourceBuild(manifest)
	if build == nil {
		return nil
	}
	return append([]string(nil), build.Inputs...)
}

func SourceManifestExecutionCommand(manifestPath, kind string, opts SourceBuildOptions) (string, []string, func(), error) {
	exec, err := SourceManifestExecution(manifestPath, kind, opts)
	if err != nil {
		return "", nil, nil, err
	}
	return exec.Command, exec.Args, exec.Cleanup, nil
}

func SourceManifestExecution(manifestPath, kind string, opts SourceBuildOptions) (SourceExecution, error) {
	absoluteManifestPath, err := filepath.Abs(manifestPath)
	if err != nil {
		return SourceExecution{}, fmt.Errorf("resolve manifest path: %w", err)
	}
	manifestPath = absoluteManifestPath
	_, manifest, err := ReadSourceManifestFile(manifestPath)
	if err != nil {
		return SourceExecution{}, err
	}
	if err := EnsureSourceBuildOutput(manifestPath, manifest, opts); err != nil {
		return SourceExecution{}, err
	}
	return sourceManifestExecution(manifestPath, manifest, kind, opts)
}

func sourceManifestExecution(manifestPath string, manifest *providermanifestv1.Manifest, kind string, opts SourceBuildOptions) (SourceExecution, error) {
	if strings.TrimSpace(kind) == "" {
		var err error
		kind, err = ManifestKind(manifest)
		if err != nil {
			return SourceExecution{}, err
		}
	}
	rootDir := filepath.Dir(manifestPath)
	exec := SourceExecution{Workdir: rootDir}
	entry := EntrypointForKind(manifest, kind)
	if entry != nil && strings.TrimSpace(entry.ArtifactPath) != "" {
		exec.Command = filepath.Join(rootDir, filepath.FromSlash(entry.ArtifactPath))
		exec.Args = append([]string(nil), entry.Args...)
	} else {
		goos, goarch := SourceBuildTarget(opts)
		var err error
		switch providermanifestv1.NormalizeKind(kind) {
		case providermanifestv1.KindPlugin:
			exec.Command, exec.Args, exec.Cleanup, err = SourceProviderExecutionCommand(rootDir, goos, goarch)
		case providermanifestv1.KindAuthentication, providermanifestv1.KindAuthorization, providermanifestv1.KindExternalCredentials, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindWorkflow, providermanifestv1.KindAgent, providermanifestv1.KindSecrets, providermanifestv1.KindRuntime:
			exec.Command, exec.Args, exec.Cleanup, err = SourceComponentExecutionCommand(rootDir, kind, goos, goarch)
		default:
			return SourceExecution{}, fmt.Errorf("manifest does not define a %s entrypoint", kind)
		}
		if err != nil {
			return SourceExecution{}, err
		}
	}
	return applySourceRunCommandPrefix(exec, manifest), nil
}

func applySourceRunCommandPrefix(exec SourceExecution, manifest *providermanifestv1.Manifest) SourceExecution {
	if manifest == nil || manifest.Run == nil || len(manifest.Run.CommandPrefix) == 0 || exec.Command == "" {
		return exec
	}
	prefix := append([]string(nil), manifest.Run.CommandPrefix...)
	original := append([]string{exec.Command}, exec.Args...)
	exec.Command = prefix[0]
	exec.Args = append(append([]string(nil), prefix[1:]...), original...)
	return exec
}
