package daemon

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/operator"
)

type exitCodeError struct {
	code int
}

func (e exitCodeError) Error() string {
	return fmt.Sprintf("process exited with status %d", e.code)
}

// ExitCode extracts a daemon command exit code that intentionally mirrors a
// child process status.
func ExitCode(err error) (int, bool) {
	var exitErr exitCodeError
	if errors.As(err, &exitErr) {
		return exitErr.code, true
	}
	return 0, false
}

type localAgentCommandOptions struct {
	ConfigPaths []string
	Provider    string
	DryRun      bool
}

type localAgentHarnessPlan struct {
	ProviderName    string
	Harness         config.ProviderEntryLocalHarnessConfig
	WorkingDir      string
	Env             map[string]string
	ResolvedCommand string
}

func runAgent(args []string) error {
	if len(args) == 0 {
		printAgentUsage(os.Stderr)
		return flag.ErrHelp
	}
	switch args[0] {
	case "-h", "--help", "help":
		printAgentUsage(os.Stderr)
		return flag.ErrHelp
	case "launch":
		return runAgentLaunch(args[1:])
	case "doctor":
		return runAgentDoctor(args[1:])
	default:
		return fmt.Errorf("unknown agent command %q", args[0])
	}
}

func runAgentLaunch(args []string) error {
	opts, err := parseLocalAgentOptions("gestaltd agent launch", printAgentLaunchUsage, args, true)
	if err != nil {
		return err
	}
	plan, err := resolveLocalAgentHarness(opts)
	if err != nil {
		return err
	}
	if opts.DryRun {
		return printLocalAgentDryRun(plan)
	}
	return launchLocalAgentHarness(plan)
}

func runAgentDoctor(args []string) error {
	opts, err := parseLocalAgentOptions("gestaltd agent doctor", printAgentDoctorUsage, args, false)
	if err != nil {
		return err
	}
	plan, err := resolveLocalAgentHarness(opts)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "local agent harness %q ok\n", plan.ProviderName)
	return nil
}

func parseLocalAgentOptions(name string, usage func(io.Writer), args []string, allowDryRun bool) (localAgentCommandOptions, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = func() { usage(fs.Output()) }
	var configPaths repeatedStringFlag
	fs.Var(&configPaths, "config", "path to config file (repeat to layer overrides)")
	provider := fs.String("provider", "", "agent provider name; defaults to the configured default")
	dryRun := false
	if allowDryRun {
		fs.BoolVar(&dryRun, "dry-run", false, "print the resolved local harness command without starting it")
	}
	if err := fs.Parse(args); err != nil {
		return localAgentCommandOptions{}, err
	}
	if fs.NArg() > 0 {
		return localAgentCommandOptions{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	return localAgentCommandOptions{
		ConfigPaths: []string(configPaths),
		Provider:    *provider,
		DryRun:      dryRun,
	}, nil
}

func resolveLocalAgentHarness(opts localAgentCommandOptions) (localAgentHarnessPlan, error) {
	configPaths := operator.ResolveConfigPaths(opts.ConfigPaths)
	cfg, err := config.LoadPartialAllowMissingEnvPaths(configPaths)
	if err != nil {
		return localAgentHarnessPlan{}, err
	}
	providerName, entry, err := cfg.EffectiveAgentProvider(opts.Provider)
	if err != nil {
		return localAgentHarnessPlan{}, err
	}
	if entry == nil {
		return localAgentHarnessPlan{}, fmt.Errorf("no agent provider configured")
	}
	if err := config.ValidateSelectedAgentLocalHarnessEnvPaths(configPaths, providerName); err != nil {
		return localAgentHarnessPlan{}, err
	}
	if entry.LocalHarness == nil {
		return localAgentHarnessPlan{}, fmt.Errorf("providers.agent.%s.localHarness is required for local agent launch", providerName)
	}
	harness := *entry.LocalHarness
	harness.Args = slices.Clone(entry.LocalHarness.Args)
	harness.Env = maps.Clone(entry.LocalHarness.Env)
	harness.RequiredCommands = slices.Clone(entry.LocalHarness.RequiredCommands)
	if harness.Command == "" {
		return localAgentHarnessPlan{}, fmt.Errorf("providers.agent.%s.localHarness.command is required", providerName)
	}

	workingDir := strings.TrimSpace(harness.WorkingDirectory)
	if workingDir != "" {
		info, err := os.Stat(workingDir)
		if err != nil {
			return localAgentHarnessPlan{}, fmt.Errorf("providers.agent.%s.localHarness.workingDirectory: %w", providerName, err)
		}
		if !info.IsDir() {
			return localAgentHarnessPlan{}, fmt.Errorf("providers.agent.%s.localHarness.workingDirectory %q is not a directory", providerName, workingDir)
		}
	}

	env := effectiveEnvMap(harness.Env)
	for _, required := range harness.RequiredCommands {
		if _, err := lookPathWithEnv(required, workingDir, env); err != nil {
			return localAgentHarnessPlan{}, fmt.Errorf("providers.agent.%s.localHarness.requiredCommands: %w", providerName, err)
		}
	}
	resolvedCommand, err := lookPathWithEnv(harness.Command, workingDir, env)
	if err != nil {
		return localAgentHarnessPlan{}, fmt.Errorf("providers.agent.%s.localHarness.command: %w", providerName, err)
	}

	return localAgentHarnessPlan{
		ProviderName:    providerName,
		Harness:         harness,
		WorkingDir:      workingDir,
		Env:             env,
		ResolvedCommand: resolvedCommand,
	}, nil
}

func launchLocalAgentHarness(plan localAgentHarnessPlan) error {
	cmd := exec.Command(plan.ResolvedCommand, plan.Harness.Args...)
	if plan.WorkingDir != "" {
		cmd.Dir = plan.WorkingDir
	}
	cmd.Env = envList(plan.Env)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitCodeError{code: processExitCode(exitErr)}
		}
		return fmt.Errorf("run local agent harness %q: %w", plan.ProviderName, err)
	}
	return nil
}

func printLocalAgentDryRun(plan localAgentHarnessPlan) error {
	payload := struct {
		Provider         string            `json:"provider"`
		Command          string            `json:"command"`
		ResolvedCommand  string            `json:"resolvedCommand"`
		Args             []string          `json:"args,omitempty"`
		WorkingDirectory string            `json:"workingDirectory,omitempty"`
		Env              map[string]string `json:"env,omitempty"`
	}{
		Provider:         plan.ProviderName,
		Command:          plan.Harness.Command,
		ResolvedCommand:  plan.ResolvedCommand,
		Args:             plan.Harness.Args,
		WorkingDirectory: plan.WorkingDir,
		Env:              redactedEnvOverlay(plan.Harness.Env),
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func effectiveEnvMap(overlay map[string]string) map[string]string {
	env := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		env[key] = value
	}
	for key, value := range overlay {
		env[key] = value
	}
	return env
}

func envList(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+env[key])
	}
	return out
}

func redactedEnvOverlay(env map[string]string) map[string]string {
	if len(env) == 0 {
		return nil
	}
	out := make(map[string]string, len(env))
	for key, value := range env {
		if isSecretEnvKey(key) {
			out[key] = "***"
			continue
		}
		out[key] = value
	}
	return out
}

func isSecretEnvKey(key string) bool {
	upper := strings.ToUpper(key)
	for _, marker := range []string{"SECRET", "TOKEN", "PASSWORD", "KEY", "CREDENTIAL", "AUTH"} {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return false
}

func lookPathWithEnv(command, workingDir string, env map[string]string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", fmt.Errorf("command is required")
	}
	if hasPathSeparator(command) {
		path := command
		if !filepath.IsAbs(path) {
			baseDir := workingDir
			if baseDir == "" {
				var err error
				baseDir, err = os.Getwd()
				if err != nil {
					return "", fmt.Errorf("get working directory: %w", err)
				}
			}
			path = filepath.Join(baseDir, path)
		}
		if err := checkExecutable(path); err != nil {
			return "", err
		}
		return path, nil
	}

	pathEnv := env["PATH"]
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			dir = "."
		}
		if !filepath.IsAbs(dir) && workingDir != "" {
			dir = filepath.Join(workingDir, dir)
		}
		for _, candidate := range executableCandidates(filepath.Join(dir, command), env) {
			if err := checkExecutable(candidate); err == nil {
				return candidate, nil
			}
		}
	}
	return "", fmt.Errorf("command %q not found in PATH", command)
}

func hasPathSeparator(path string) bool {
	return strings.ContainsRune(path, os.PathSeparator) || (os.PathSeparator != '/' && strings.ContainsRune(path, '/'))
}

func executableCandidates(path string, env map[string]string) []string {
	if runtime.GOOS != "windows" || filepath.Ext(path) != "" {
		return []string{path}
	}
	exts := filepath.SplitList(env["PATHEXT"])
	if len(exts) == 0 {
		exts = []string{".COM", ".EXE", ".BAT", ".CMD"}
	}
	out := make([]string, 0, len(exts)+1)
	out = append(out, path)
	for _, ext := range exts {
		out = append(out, path+ext)
	}
	return out
}

func checkExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%q is a directory", path)
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		return fmt.Errorf("%q is not executable", path)
	}
	return nil
}

func printAgentUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd agent launch [--config PATH]... [--provider NAME] [--dry-run]")
	writeUsageLine(w, "  gestaltd agent doctor [--config PATH]... [--provider NAME]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Commands:")
	writeUsageLine(w, "  launch      Start the selected provider's configured local agent harness")
	writeUsageLine(w, "  doctor      Check that the selected local agent harness can be started")
}

func printAgentLaunchUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd agent launch [--config PATH]... [--provider NAME] [--dry-run]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Start the selected provider's configured local agent harness.")
	writeUsageLine(w, "Only providers.agent.<name>.localHarness is used; no server, session,")
	writeUsageLine(w, "persistence, runtime, tool grant, or AgentProvider RPC stack is started.")
}

func printAgentDoctorUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd agent doctor [--config PATH]... [--provider NAME]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Check the selected provider's configured local agent harness command,")
	writeUsageLine(w, "working directory, environment overlay, and requiredCommands.")
}
