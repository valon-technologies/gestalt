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

// sourceBuildProducesOutputOrImplicitGo reports whether a build output will be
// produced for the provider at root: either a declared build phase that
// produces output, or a Go provider with an entrypoint (built implicitly by
// gestaltd via a synthesized wrapper main).
func sourceBuildProducesOutputOrImplicitGo(root string, manifest *providermanifestv1.Manifest) bool {
	if SourceBuildProducesOutput(manifest) {
		return true
	}
	if !HasGoProviderPackage(root) {
		return false
	}
	kind, err := ManifestKind(manifest)
	if err != nil {
		return false
	}
	entry := EntrypointForKind(manifest, kind)
	return entry != nil && strings.TrimSpace(entry.ArtifactPath) != ""
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
		return runImplicitGoBuild(manifestPath, manifest, opts)
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

// runImplicitGoBuild is the fallback build path for a Go provider that declares
// an entrypoint but no build.command: gestaltd synthesizes a wrapper main that
// serves the provider via the SDK Serve<Kind>Provider call and compiles it with
// `go build` to the entrypoint.artifactPath. Non-Go providers with no declared
// build are a no-op (the caller enforces that a declared build is required for
// release packaging elsewhere).
func runImplicitGoBuild(manifestPath string, manifest *providermanifestv1.Manifest, opts SourceBuildOptions) error {
	rootDir := filepath.Dir(manifestPath)
	if !HasGoProviderPackage(rootDir) {
		return nil
	}
	kind, err := ManifestKind(manifest)
	if err != nil {
		return err
	}
	entry := EntrypointForKind(manifest, kind)
	if entry == nil || strings.TrimSpace(entry.ArtifactPath) == "" {
		return fmt.Errorf("entrypoint.artifactPath is required to build a Go provider without a declared build.command")
	}
	outputRel := entry.ArtifactPath
	outputPath := filepath.Join(rootDir, filepath.FromSlash(outputRel))
	goos, goarch := SourceBuildTarget(opts)
	// Build to a sibling temp path first so a failed build does not delete a
	// pre-existing entrypoint artifact; only replace the entrypoint on success.
	tmpOutput := outputPath + ".build"
	if err := os.RemoveAll(tmpOutput); err != nil {
		return fmt.Errorf("remove staged build output %q: %w", outputRel, err)
	}
	if err := BuildGoProviderBinary(rootDir, tmpOutput, kind, goos, goarch); err != nil {
		_ = os.RemoveAll(tmpOutput)
		return err
	}
	if err := os.Rename(tmpOutput, outputPath); err != nil {
		_ = os.RemoveAll(tmpOutput)
		return fmt.Errorf("install build output %q: %w", outputRel, err)
	}
	return verifySourceBuildOutput(outputPath, outputRel, "executable", opts)
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

// EnsureSourceBuildOutput is the canonical install-then-build entry for a
// source provider: it runs the install phase (if declared) before the build,
// then verifies the entrypoint artifact. Callers that need a build output
// should use this rather than RunSourceBuild, which is build-only.
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
	}
	if HasExplicitSourceRun(manifest) {
		return ResolvedSourceExecution{}, fmt.Errorf("manifest defines run but no %s entrypoint.artifactPath", kind)
	}
	return ResolvedSourceExecution{}, missingDeclaredSourceBuildError(manifest, kind)
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
