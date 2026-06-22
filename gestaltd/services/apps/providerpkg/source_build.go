package providerpkg

import (
	"fmt"
	"maps"
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
	Inputs      []string
	PrepareOnly bool
}

type ResolvedSourceDev struct {
	Workdir      string
	Command      []string
	ReadyTimeout string
	// Env is copied into the child process before the reserved GESTALT_DEV*
	// contract variables (GESTALT_DEV, GESTALT_DEV_PORT, GESTALT_DEV_BASE_PATH),
	// which cannot be overridden via manifest dev.env.
	Env map[string]string
}

type SourceBuildOptions struct {
	GOOS   string
	GOARCH string
	LibC   string
	Output CommandOutput
}

type SourceExecutionIntent int

const (
	SourceExecutionIntentLocalRun SourceExecutionIntent = iota
	SourceExecutionIntentPackagedEntrypoint
	SourceExecutionIntentSynthesizedSDK
)

type SourceExecution struct {
	Command string
	Args    []string
	Workdir string
	Cleanup func()
}

type ResolvedSourceExecution struct {
	SourceExecution
	Intent SourceExecutionIntent
}

func EffectiveDev(manifest *providermanifestv1.Manifest) *ResolvedSourceDev {
	if manifest == nil || manifest.Dev == nil {
		return nil
	}
	return &ResolvedSourceDev{
		Workdir:      manifest.Dev.Workdir,
		Command:      append([]string(nil), manifest.Dev.Command...),
		ReadyTimeout: manifest.Dev.ReadyTimeout,
		Env:          maps.Clone(manifest.Dev.Env),
	}
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
	cmd.Stdout = commandStdout(opts.Output)
	cmd.Stderr = commandStderr(opts.Output)
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
			return "", "", fmt.Errorf("spec.assetRoot is required when build is set")
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

func ensurePrepareOnlyBuild(manifestPath string, manifest *providermanifestv1.Manifest, opts SourceBuildOptions) error {
	build := EffectiveSourceBuild(manifest)
	if build == nil || !build.PrepareOnly {
		return nil
	}
	return RunSourceBuild(manifestPath, manifest, opts)
}

func SourceBuildInputs(manifest *providermanifestv1.Manifest) []string {
	build := EffectiveSourceBuild(manifest)
	if build == nil {
		return nil
	}
	return append([]string(nil), build.Inputs...)
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
	if HasExplicitSourceRun(manifest) {
		if err := ensurePrepareOnlyBuild(manifestPath, manifest, opts); err != nil {
			return SourceExecution{}, err
		}
		return explicitRunExecution(filepath.Dir(manifestPath), manifest).SourceExecution, nil
	}
	if err := EnsureSourceBuildOutput(manifestPath, manifest, opts); err != nil {
		return SourceExecution{}, err
	}
	resolved, err := resolveSourceExecution(manifestPath, manifest, kind, opts, true)
	if err != nil {
		return SourceExecution{}, err
	}
	return resolved.SourceExecution, nil
}

func resolveSourceExecution(manifestPath string, manifest *providermanifestv1.Manifest, kind string, opts SourceBuildOptions, skipExplicitRun bool) (ResolvedSourceExecution, error) {
	rootDir := filepath.Dir(manifestPath)
	if !skipExplicitRun && HasExplicitSourceRun(manifest) {
		return explicitRunExecution(rootDir, manifest), nil
	}
	kind, err := sourceExecutionKind(manifest, kind)
	if err != nil {
		return ResolvedSourceExecution{}, err
	}
	exec := SourceExecution{Workdir: rootDir}
	entry := EntrypointForKind(manifest, kind)
	if entry != nil && strings.TrimSpace(entry.ArtifactPath) != "" {
		exec.Command = filepath.Join(rootDir, filepath.FromSlash(entry.ArtifactPath))
		exec.Args = append([]string(nil), entry.Args...)
		return ResolvedSourceExecution{
			SourceExecution: exec,
			Intent:          SourceExecutionIntentPackagedEntrypoint,
		}, nil
	} else {
		goos, goarch := SourceBuildTarget(opts)
		var err error
		switch providermanifestv1.NormalizeKind(kind) {
		case providermanifestv1.KindApp:
			exec.Command, exec.Args, exec.Cleanup, err = SourceProviderExecutionCommand(rootDir, goos, goarch)
		case providermanifestv1.KindIdentity, providermanifestv1.KindAuthorization, providermanifestv1.KindExternalCredentials, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindWorkflow, providermanifestv1.KindAgent, providermanifestv1.KindSecrets, providermanifestv1.KindRuntime:
			exec.Command, exec.Args, exec.Cleanup, err = SourceComponentExecutionCommand(rootDir, kind, goos, goarch)
		default:
			return ResolvedSourceExecution{}, fmt.Errorf("manifest does not define a %s entrypoint", kind)
		}
		if err != nil {
			return ResolvedSourceExecution{}, err
		}
	}
	return ResolvedSourceExecution{
		SourceExecution: exec,
		Intent:          SourceExecutionIntentSynthesizedSDK,
	}, nil
}

func explicitRunExecution(rootDir string, manifest *providermanifestv1.Manifest) ResolvedSourceExecution {
	return ResolvedSourceExecution{
		SourceExecution: SourceExecution{
			Command: manifest.Run[0],
			Args:    append([]string(nil), manifest.Run[1:]...),
			Workdir: rootDir,
		},
		Intent: SourceExecutionIntentLocalRun,
	}
}

func sourceExecutionKind(manifest *providermanifestv1.Manifest, kind string) (string, error) {
	if strings.TrimSpace(kind) != "" {
		return kind, nil
	}
	return ManifestKind(manifest)
}
