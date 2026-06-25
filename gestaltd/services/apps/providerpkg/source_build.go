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

type ResolvedSourceInstall struct {
	Workdir string
	Command []string
	Inputs  []string
	Env     map[string]string
}

type ResolvedSourceRun struct {
	Workdir string
	Command []string
	Env     map[string]string
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

func EffectiveSourceRun(manifest *providermanifestv1.Manifest) *ResolvedSourceRun {
	if manifest == nil || manifest.Run == nil {
		return nil
	}
	return &ResolvedSourceRun{
		Workdir: manifest.Run.Workdir,
		Command: append([]string(nil), manifest.Run.Command...),
		Env:     maps.Clone(manifest.Run.Env),
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

func EffectiveSourceInstall(manifest *providermanifestv1.Manifest) *ResolvedSourceInstall {
	if manifest == nil || manifest.Install == nil {
		return nil
	}
	return &ResolvedSourceInstall{
		Workdir: manifest.Install.Workdir,
		Command: append([]string(nil), manifest.Install.Command...),
		Inputs:  append([]string(nil), manifest.Install.Inputs...),
		Env:     maps.Clone(manifest.Install.Env),
	}
}

// RunSourceInstall execs the declared install command (no shell) from the
// source root. Side-effect only: no entrypoint artifact is verified. A nil
// install phase is a no-op. opts carries the target platform env that
// sourceBuildEnv injects; install.Env is merged on top (install wins).
func RunSourceInstall(manifestPath string, manifest *providermanifestv1.Manifest, opts SourceBuildOptions) error {
	install := EffectiveSourceInstall(manifest)
	if install == nil {
		return nil
	}
	return runSourcePhase(manifestPath, "install", install.Workdir, install.Command, envMapToSlice(install.Env), opts)
}

func envMapToSlice(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for key, value := range env {
		out = append(out, key+"="+value)
	}
	return out
}

// RunSourceBuild execs the declared build command (no shell) and, when the
// build is not prepare-only, verifies the entrypoint artifact. It does NOT run
// the install phase; callers that need install-before-build should use
// EnsureSourceBuildOutput, which is the canonical install-then-build entry.
func RunSourceBuild(manifestPath string, manifest *providermanifestv1.Manifest, opts SourceBuildOptions) error {
	build := EffectiveSourceBuild(manifest)
	if build == nil {
		return nil
	}
	if len(build.Command) == 0 {
		return fmt.Errorf("%s.command is required", build.Label)
	}

	rootDir := filepath.Dir(manifestPath)
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

	if err := runSourcePhase(manifestPath, build.Label, build.Workdir, build.Command, nil, opts); err != nil {
		return err
	}
	if build.PrepareOnly {
		return nil
	}
	return verifySourceBuildOutput(outputPath, outputRel, outputKind, opts)
}

// runSourcePhase execs a declared phase command (no shell) from the source
// root, resolving the optional relative workdir and merging phase env over the
// target-platform env. Shared by install and build.
func runSourcePhase(manifestPath, label, workdir string, command, envOverrides []string, opts SourceBuildOptions) error {
	if len(command) == 0 {
		return fmt.Errorf("%s.command is required", label)
	}

	rootDir := filepath.Dir(manifestPath)
	phaseWorkdir := rootDir
	if workdir != "" && workdir != "." {
		phaseWorkdir = filepath.Join(rootDir, filepath.FromSlash(workdir))
	}

	info, err := os.Stat(phaseWorkdir)
	if err != nil {
		return fmt.Errorf("stat %s.workdir %q: %w", label, workdir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s.workdir %q is not a directory", label, workdir)
	}

	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = phaseWorkdir
	cmd.Env = mergePhaseEnv(sourceBuildEnv(os.Environ(), opts), envOverrides)
	cmd.Stdout = commandStdout(opts.Output)
	cmd.Stderr = commandStderr(opts.Output)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run %s.command: %w", label, err)
	}
	return nil
}

// mergePhaseEnv folds a phase's env overrides (as "KEY=VALUE" strings) over the
// target-platform env; phase values win.
func mergePhaseEnv(base, overrides []string) []string {
	if len(overrides) == 0 {
		return base
	}
	overridden := make(map[string]struct{}, len(overrides))
	merged := make([]string, 0, len(base)+len(overrides))
	for _, kv := range base {
		key, _, ok := strings.Cut(kv, "=")
		if ok {
			if _, hit := overridden[key]; hit {
				continue
			}
			if override, hit := lookupEnv(overrides, key); hit {
				overridden[key] = struct{}{}
				merged = append(merged, key+"="+override)
				continue
			}
		}
		merged = append(merged, kv)
	}
	for _, kv := range overrides {
		key, value, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if _, hit := overridden[key]; hit {
			continue
		}
		overridden[key] = struct{}{}
		merged = append(merged, key+"="+value)
	}
	return merged
}

func lookupEnv(env []string, key string) (string, bool) {
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if ok && k == key {
			return v, true
		}
	}
	return "", false
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
	goos, _ := SourceBuildTarget(SourceBuildOptions{})
	outputRel, err := SourceBuildOutputPath(manifest, goos)
	if err != nil {
		return "", "", err
	}
	return outputRel, "executable", nil
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

// EnsureSourceBuildOutput is the canonical install-then-build entry; callers
// that need a build output should use this rather than RunSourceBuild.
func EnsureSourceBuildOutput(manifestPath string, manifest *providermanifestv1.Manifest, opts SourceBuildOptions) error {
	if err := RunSourceInstall(manifestPath, manifest, opts); err != nil {
		return err
	}
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

func SourceInstallInputs(manifest *providermanifestv1.Manifest) []string {
	install := EffectiveSourceInstall(manifest)
	if install == nil {
		return nil
	}
	return append([]string(nil), install.Inputs...)
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
		if err := RunSourceInstall(manifestPath, manifest, opts); err != nil {
			return SourceExecution{}, err
		}
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
	if _, err := sourceExecutionKind(manifest, kind); err != nil {
		return ResolvedSourceExecution{}, err
	}
	exec := SourceExecution{Workdir: rootDir}
	goos, _ := SourceBuildTarget(opts)
	outputRel, err := SourceBuildOutputPath(manifest, goos)
	if err != nil {
		return ResolvedSourceExecution{}, err
	}
	exec.Command = filepath.Join(rootDir, filepath.FromSlash(outputRel))
	return ResolvedSourceExecution{
		SourceExecution: exec,
		Intent:          SourceExecutionIntentPackagedEntrypoint,
	}, nil
}

func explicitRunExecution(rootDir string, manifest *providermanifestv1.Manifest) ResolvedSourceExecution {
	run := EffectiveSourceRun(manifest)
	workdir := rootDir
	if run != nil && run.Workdir != "" && run.Workdir != "." {
		workdir = filepath.Join(rootDir, filepath.FromSlash(run.Workdir))
	}
	command := ""
	var args []string
	if run != nil && len(run.Command) > 0 {
		command = run.Command[0]
		args = append([]string(nil), run.Command[1:]...)
	}
	return ResolvedSourceExecution{
		SourceExecution: SourceExecution{
			Command: command,
			Args:    args,
			Workdir: workdir,
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
