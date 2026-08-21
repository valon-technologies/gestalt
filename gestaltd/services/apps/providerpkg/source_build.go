package providerpkg

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

type ResolvedCommand struct {
	Workdir      string
	Command      []string
	Env          map[string]string
	Inputs       []string
	ReadyTimeout time.Duration
	Role         string
}

type ResolvedSourceBuild struct {
	Commands    []ResolvedCommand
	PrepareOnly bool
}

type ResolvedSourceInstall struct {
	Commands []ResolvedCommand
}

type ResolvedSourceRun struct {
	Commands []ResolvedCommand
}

type SourceBuildOptions struct {
	GOOS   string
	GOARCH string
	LibC   string
	Output CommandOutput
}

type sourcePackagingBuildPlan struct {
	skipTargetCommands map[int]struct{}
}

type sourceBuildCommandObservation struct {
	executableChanged bool
	staticChanged     bool
	supportChanged    bool
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
	Env     map[string]string
	Cleanup func()
}

type ResolvedSourceExecution struct {
	SourceExecution
	Intent SourceExecutionIntent
}

func resolvePhaseCommands(label string, phases []providermanifestv1.SourcePhaseCommand) ([]ResolvedCommand, error) {
	if len(phases) == 0 {
		return nil, nil
	}
	out := make([]ResolvedCommand, 0, len(phases))
	for i, phase := range phases {
		if len(phase.Command) == 0 {
			return nil, fmt.Errorf("%s[%d].command is required", label, i)
		}
		readyTimeout, err := parseSourceRunReadyTimeout(phase.ReadyTimeout)
		if err != nil {
			return nil, fmt.Errorf("%s[%d].readyTimeout: %w", label, i, err)
		}
		out = append(out, ResolvedCommand{
			Workdir:      phase.Workdir,
			Command:      append([]string(nil), phase.Command...),
			Env:          maps.Clone(phase.Env),
			Inputs:       append([]string(nil), phase.Inputs...),
			ReadyTimeout: readyTimeout,
			Role:         strings.TrimSpace(phase.Role),
		})
	}
	return out, nil
}

func EffectiveSourceRun(manifest *providermanifestv1.Manifest) *ResolvedSourceRun {
	if manifest == nil || manifest.Run == nil {
		return nil
	}
	commands, err := resolvePhaseCommands("run", manifest.Run.PhaseCommands())
	if err != nil || len(commands) == 0 {
		return nil
	}
	return &ResolvedSourceRun{Commands: commands}
}

func EffectiveSourceRunCommand(manifest *providermanifestv1.Manifest) *ResolvedCommand {
	run := EffectiveSourceRun(manifest)
	if run == nil || len(run.Commands) == 0 {
		return nil
	}
	cmd := run.Commands[0]
	return &cmd
}

func parseSourceRunReadyTimeout(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	return time.ParseDuration(raw)
}

func BuildWorkdirAbsPaths(sourceDir string, commands []ResolvedCommand) []string {
	if len(commands) == 0 {
		return []string{filepath.Clean(sourceDir)}
	}
	seen := map[string]struct{}{}
	var paths []string
	for _, command := range commands {
		root := sourceDir
		if w := strings.TrimSpace(command.Workdir); w != "" && w != "." {
			root = filepath.Join(sourceDir, filepath.FromSlash(w))
		}
		root = filepath.Clean(root)
		if _, ok := seen[root]; ok {
			continue
		}
		seen[root] = struct{}{}
		paths = append(paths, root)
	}
	return paths
}

func EffectiveSourceBuild(manifest *providermanifestv1.Manifest) *ResolvedSourceBuild {
	if manifest == nil || manifest.Build == nil {
		return nil
	}
	commands, err := resolvePhaseCommands("build", manifest.Build.PhaseCommands())
	if err != nil || len(commands) == 0 {
		return nil
	}
	return &ResolvedSourceBuild{
		Commands:    commands,
		PrepareOnly: manifest.Build.PrepareOnly,
	}
}

func SourceBuildProducesOutput(manifest *providermanifestv1.Manifest) bool {
	build := EffectiveSourceBuild(manifest)
	return build != nil && !build.PrepareOnly
}

func EffectiveSourceInstall(manifest *providermanifestv1.Manifest) *ResolvedSourceInstall {
	if manifest == nil || manifest.Install == nil {
		return nil
	}
	commands, err := resolvePhaseCommands("install", manifest.Install.PhaseCommands())
	if err != nil || len(commands) == 0 {
		return nil
	}
	return &ResolvedSourceInstall{Commands: commands}
}

// RunSourceInstall execs declared install commands serially (no shell) from the
// source root. Side-effect only: no entrypoint artifact is verified.
func RunSourceInstall(manifestPath string, manifest *providermanifestv1.Manifest, opts SourceBuildOptions) error {
	install := EffectiveSourceInstall(manifest)
	if install == nil {
		return nil
	}
	for i, command := range install.Commands {
		if err := runSourcePhase(manifestPath, fmt.Sprintf("install[%d]", i), command.Workdir, command.Command, envMapToSlice(command.Env), opts); err != nil {
			return err
		}
	}
	return nil
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

// RunSourceBuild execs declared build commands serially (no shell) and verifies output.
func RunSourceBuild(manifestPath string, manifest *providermanifestv1.Manifest, opts SourceBuildOptions) error {
	_, err := runSourceBuild(manifestPath, manifest, opts, nil, false)
	return err
}

func runSourceBuildForPackaging(manifestPath string, manifest *providermanifestv1.Manifest, opts SourceBuildOptions) (*sourcePackagingBuildPlan, error) {
	observations, err := runSourceBuild(manifestPath, manifest, opts, nil, true)
	if err != nil {
		return nil, err
	}
	lastExecutableCommand := -1
	for i, observation := range observations {
		if observation.executableChanged {
			lastExecutableCommand = i
		}
	}
	plan := &sourcePackagingBuildPlan{skipTargetCommands: make(map[int]struct{})}
	if lastExecutableCommand < 0 {
		return plan, nil
	}
	for i, observation := range observations {
		if i > lastExecutableCommand && observation.staticChanged && !observation.supportChanged {
			plan.skipTargetCommands[i] = struct{}{}
		}
	}
	return plan, nil
}

func runSourceBuildWithPackagingPlan(manifestPath string, manifest *providermanifestv1.Manifest, opts SourceBuildOptions, plan *sourcePackagingBuildPlan) error {
	var skip map[int]struct{}
	if plan != nil {
		skip = plan.skipTargetCommands
	}
	_, err := runSourceBuild(manifestPath, manifest, opts, skip, false)
	return err
}

func runSourceBuild(manifestPath string, manifest *providermanifestv1.Manifest, opts SourceBuildOptions, skip map[int]struct{}, observe bool) ([]sourceBuildCommandObservation, error) {
	build := EffectiveSourceBuild(manifest)
	if build == nil {
		return nil, nil
	}
	if len(build.Commands) == 0 {
		return nil, fmt.Errorf("build.command is required")
	}

	rootDir := filepath.Dir(manifestPath)
	var outputRel, outputKind, outputPath string
	var staticBuildEnv string
	if !build.PrepareOnly {
		var err error
		outputRel, outputKind, err = sourceBuildOutput(manifest, opts)
		if err != nil {
			return nil, err
		}
		if outputRel != "" {
			outputPath = filepath.Join(rootDir, filepath.FromSlash(outputRel))
			if err := os.RemoveAll(outputPath); err != nil {
				return nil, fmt.Errorf("remove build output %q: %w", outputRel, err)
			}
		}
		staticBuildEnv, err = prepareSourceStaticBuildDir(manifestPath)
		if err != nil {
			return nil, err
		}
	}

	observations := make([]sourceBuildCommandObservation, len(build.Commands))
	for i, command := range build.Commands {
		if _, ok := skip[i]; ok {
			continue
		}
		var beforeExecutable, beforeStatic, beforeSupport string
		if observe {
			var err error
			beforeExecutable, err = digestBuildPath(outputPath)
			if err != nil {
				return nil, err
			}
			beforeStatic, err = digestBuildPath(SourceStaticBuildDir(manifestPath))
			if err != nil {
				return nil, err
			}
			beforeSupport, err = digestPackagedSupport(rootDir, manifest)
			if err != nil {
				return nil, err
			}
		}
		label := "build"
		if len(build.Commands) > 1 {
			label = fmt.Sprintf("build[%d]", i)
		}
		envOverrides := envMapToSlice(command.Env)
		if staticBuildEnv != "" {
			envOverrides = append(envOverrides, envGestaltBuildStatic+"="+staticBuildEnv)
		}
		if err := runSourcePhase(manifestPath, label, command.Workdir, command.Command, envOverrides, opts); err != nil {
			return nil, err
		}
		if observe {
			afterExecutable, err := digestBuildPath(outputPath)
			if err != nil {
				return nil, err
			}
			afterStatic, err := digestBuildPath(SourceStaticBuildDir(manifestPath))
			if err != nil {
				return nil, err
			}
			afterSupport, err := digestPackagedSupport(rootDir, manifest)
			if err != nil {
				return nil, err
			}
			observations[i] = sourceBuildCommandObservation{
				executableChanged: beforeExecutable != afterExecutable,
				staticChanged:     beforeStatic != afterStatic,
				supportChanged:    beforeSupport != afterSupport,
			}
		}
	}
	if build.PrepareOnly {
		return observations, nil
	}
	return observations, verifyAppBuildOutputs(manifestPath, manifest, outputPath, outputRel, outputKind, opts)
}

func digestPackagedSupport(root string, manifest *providermanifestv1.Manifest) (string, error) {
	if manifest == nil {
		return "", nil
	}
	paths := []string{manifest.IconFile}
	for _, ref := range LocalPackageReferences(manifest) {
		paths = append(paths, ref.Path)
	}
	if manifest.Spec != nil {
		paths = append(paths, manifest.Spec.ConfigSchemaPath, manifest.Spec.AssetRoot)
	}
	for _, artifact := range manifest.Artifacts {
		paths = append(paths, artifact.Path)
	}
	sort.Strings(paths)
	hash := sha256.New()
	for _, rel := range paths {
		if strings.TrimSpace(rel) == "" {
			continue
		}
		digest, err := digestBuildPath(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return "", err
		}
		_, _ = fmt.Fprintf(hash, "%s\x00%s\x00", rel, digest)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func digestBuildPath(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", nil
	}
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if !info.IsDir() {
		return FileSHA256(root)
	}
	var files []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		return "", err
	}
	sort.Strings(files)
	hash := sha256.New()
	for _, path := range files {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return "", err
		}
		digest, err := FileSHA256(path)
		if err != nil {
			return "", err
		}
		_, _ = fmt.Fprintf(hash, "%s\x00%s\x00", filepath.ToSlash(rel), digest)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

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
	stdout, stderr, finalize := phaseCommandWriters(opts.Output)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		finalize(err)
		return fmt.Errorf("run %s.command: %w", label, err)
	}
	finalize(nil)
	return nil
}

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
	return sourceBuildOutput(manifest, SourceBuildOptions{})
}

func sourceBuildOutput(manifest *providermanifestv1.Manifest, opts SourceBuildOptions) (rel string, kind string, err error) {
	if build := EffectiveSourceBuild(manifest); build != nil && build.PrepareOnly {
		return "", "", nil
	}
	if _, err := ManifestKind(manifest); err != nil {
		return "", "", err
	}
	goos, _ := SourceBuildTarget(opts)
	outputRel, err := SourceBuildOutputPath(manifest, goos)
	if err != nil {
		return "", "", err
	}
	return outputRel, "executable", nil
}

func verifySourceBuildOutput(outputPath, outputRel, outputKind string, opts SourceBuildOptions) error {
	if outputRel == "" {
		return nil
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		return fmt.Errorf("build output %q not found: %w", outputRel, err)
	}
	switch outputKind {
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

func HostLibC() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	if matches, _ := filepath.Glob("/lib/ld-musl-*.so.1"); len(matches) > 0 {
		return "musl"
	}
	return "glibc"
}

func effectiveTargetLibC(opts SourceBuildOptions) string {
	if opts.LibC != "" {
		return opts.LibC
	}
	if goos, _ := SourceBuildTarget(opts); goos == "linux" {
		return "musl"
	}
	return ""
}

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
	outputRel, outputKind, err := sourceBuildOutput(manifest, opts)
	if err != nil {
		if EffectiveSourceBuild(manifest) == nil {
			return nil
		}
		return err
	}
	outputPath := filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(outputRel))
	return verifyAppBuildOutputs(manifestPath, manifest, outputPath, outputRel, outputKind, opts)
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
	var inputs []string
	for _, command := range build.Commands {
		inputs = append(inputs, command.Inputs...)
	}
	return inputs
}

func SourceInstallInputs(manifest *providermanifestv1.Manifest) []string {
	install := EffectiveSourceInstall(manifest)
	if install == nil {
		return nil
	}
	var inputs []string
	for _, command := range install.Commands {
		inputs = append(inputs, command.Inputs...)
	}
	return inputs
}

func SourceRunCommand(manifestPath string) (SourceExecution, error) {
	absoluteManifestPath, err := filepath.Abs(manifestPath)
	if err != nil {
		return SourceExecution{}, fmt.Errorf("resolve manifest path: %w", err)
	}
	manifestPath = absoluteManifestPath
	_, manifest, err := ReadSourceManifestFile(manifestPath)
	if err != nil {
		return SourceExecution{}, err
	}
	if !HasExplicitSourceRun(manifest) {
		return SourceExecution{}, fmt.Errorf("manifest %s: run command required", manifestPath)
	}
	if err := RejectMultipleSourceRuns(manifest); err != nil {
		return SourceExecution{}, err
	}
	return explicitRunExecution(filepath.Dir(manifestPath), manifest).SourceExecution, nil
}

// SourceRunCommands returns every explicit run entry for dev-mode supervision.
func SourceRunCommands(manifestPath string) ([]ResolvedCommand, error) {
	absoluteManifestPath, err := filepath.Abs(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("resolve manifest path: %w", err)
	}
	manifestPath = absoluteManifestPath
	_, manifest, err := ReadSourceManifestFile(manifestPath)
	if err != nil {
		return nil, err
	}
	if !HasExplicitSourceRun(manifest) {
		return nil, fmt.Errorf("manifest %s: run command required", manifestPath)
	}
	commands, err := resolvePhaseCommands("run", manifest.Run.PhaseCommands())
	if err != nil {
		return nil, err
	}
	if len(commands) == 0 {
		return nil, fmt.Errorf("manifest %s: run command required", manifestPath)
	}
	rootDir := filepath.Dir(manifestPath)
	for i := range commands {
		if commands[i].Workdir != "" && commands[i].Workdir != "." {
			commands[i].Workdir = filepath.Join(rootDir, filepath.FromSlash(commands[i].Workdir))
		} else {
			commands[i].Workdir = rootDir
		}
	}
	return commands, nil
}

const errRemotePreviewUICommandCount = "remote-preview requires exactly one manifest run command with role: ui"

// SourceUIRunCommands returns manifest run entries tagged with role: ui.
func SourceUIRunCommands(manifestPath string) ([]ResolvedCommand, error) {
	commands, err := SourceRunCommands(manifestPath)
	if err != nil {
		return nil, err
	}
	var ui []ResolvedCommand
	for _, command := range commands {
		if command.Role == providermanifestv1.SourceRunRoleUI {
			ui = append(ui, command)
		}
	}
	return ui, nil
}

// ValidateRemotePreviewUIRunTarget ensures remote-preview serve has exactly one UI run command.
func ValidateRemotePreviewUIRunTarget(manifestPath string) error {
	ui, err := SourceUIRunCommands(manifestPath)
	if err != nil {
		return err
	}
	switch len(ui) {
	case 0:
		return fmt.Errorf("%s: no manifest run command with role: ui", manifestPath)
	case 1:
		return nil
	default:
		return fmt.Errorf("%s: %s (found %d)", manifestPath, errRemotePreviewUICommandCount, len(ui))
	}
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
		if err := RejectMultipleSourceRuns(manifest); err != nil {
			return SourceExecution{}, err
		}
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
		if err := RejectMultipleSourceRuns(manifest); err != nil {
			return ResolvedSourceExecution{}, err
		}
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
	run := EffectiveSourceRunCommand(manifest)
	workdir := rootDir
	command := ""
	var args []string
	var env map[string]string
	if run != nil {
		if run.Workdir != "" && run.Workdir != "." {
			workdir = filepath.Join(rootDir, filepath.FromSlash(run.Workdir))
		}
		if len(run.Command) > 0 {
			command = run.Command[0]
			args = append([]string(nil), run.Command[1:]...)
		}
		env = maps.Clone(run.Env)
	}
	return ResolvedSourceExecution{
		SourceExecution: SourceExecution{
			Command: command,
			Args:    args,
			Workdir: workdir,
			Env:     env,
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
