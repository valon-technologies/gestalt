package daemon

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestE2EAgentLaunchDryRunUsesLocalHarnessOnly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	workDir := filepath.Join(dir, "work")
	if err := os.Mkdir(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workDir: %v", err)
	}
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `apiVersion: gestaltd.config/v5
providers:
  agent:
    local:
      default: true
      localHarness:
        command: /bin/sh
        args: ["-c", "echo hi"]
        workingDirectory: work
        env:
          OPENAI_API_KEY: secret
          PLAIN: visible
    deployment_only:
      execution:
        runtime:
          provider: missing
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "agent", "launch", "--config", cfgPath, "--dry-run").CombinedOutput()
	if err != nil {
		t.Fatalf("agent launch --dry-run failed: %v\n%s", err, out)
	}
	var payload struct {
		Provider         string            `json:"provider"`
		Harness          string            `json:"harness"`
		Command          string            `json:"command"`
		ResolvedCommand  string            `json:"resolvedCommand"`
		Args             []string          `json:"args"`
		WorkingDirectory string            `json:"workingDirectory"`
		Env              map[string]string `json:"env"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("parse dry-run json: %v\n%s", err, out)
	}
	if payload.Provider != "local" {
		t.Fatalf("provider = %q, want local", payload.Provider)
	}
	if payload.Harness != "default" {
		t.Fatalf("harness = %q, want default", payload.Harness)
	}
	if payload.Command != "/bin/sh" || payload.ResolvedCommand != "/bin/sh" {
		t.Fatalf("command = (%q, %q), want /bin/sh", payload.Command, payload.ResolvedCommand)
	}
	if got := strings.Join(payload.Args, "\x00"); got != "-c\x00echo hi" {
		t.Fatalf("args = %#v", payload.Args)
	}
	if payload.WorkingDirectory != workDir {
		t.Fatalf("workingDirectory = %q, want %q", payload.WorkingDirectory, workDir)
	}
	if payload.Env["OPENAI_API_KEY"] != "***" {
		t.Fatalf("OPENAI_API_KEY dry-run value = %q, want redacted", payload.Env["OPENAI_API_KEY"])
	}
	if payload.Env["PLAIN"] != "visible" {
		t.Fatalf("PLAIN dry-run value = %q, want visible", payload.Env["PLAIN"])
	}
	if _, ok := payload.Env["PATH"]; ok {
		t.Fatalf("dry-run env included inherited PATH: %#v", payload.Env)
	}
}

func TestE2EAgentLaunchRunsHarnessWithInheritedStdioAndExitCode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	workDir := filepath.Join(dir, "work")
	if err := os.Mkdir(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workDir: %v", err)
	}
	harnessPath := filepath.Join(workDir, "harness.sh")
	writeExecutable(t, harnessPath, `#!/bin/sh
printf 'harness stdout\n'
printf 'harness stderr\n' >&2
printf 'cwd=%s\n' "$(pwd)"
printf 'overlay=%s\n' "$HARNESS_VALUE"
exit 7
`)
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `apiVersion: gestaltd.config/v5
providers:
  agent:
    local:
      default: true
      localHarness:
        command: ./harness.sh
        workingDirectory: work
        env:
          HARNESS_VALUE: from-config
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "agent", "launch", "--config", cfgPath).CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("agent launch err = %v, want exit status 7\n%s", err, out)
	}
	if exitErr.ExitCode() != 7 {
		t.Fatalf("exit code = %d, want 7\n%s", exitErr.ExitCode(), out)
	}
	physicalWorkDir, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		t.Fatalf("eval workDir: %v", err)
	}
	for _, want := range []string{
		"harness stdout",
		"harness stderr",
		"cwd=" + physicalWorkDir,
		"overlay=from-config",
	} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestE2EAgentDoctorReportsMissingRequiredCommand(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `apiVersion: gestaltd.config/v5
providers:
  agent:
    local:
      default: true
      localHarness:
        command: /bin/sh
        requiredCommands:
          - definitely-missing-gestalt-agent-command
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "agent", "doctor", "--config", cfgPath).CombinedOutput()
	if err == nil {
		t.Fatalf("agent doctor unexpectedly succeeded:\n%s", out)
	}
	if !strings.Contains(string(out), "harnesses.default.requiredCommands") ||
		!strings.Contains(string(out), "definitely-missing-gestalt-agent-command") {
		t.Fatalf("doctor output missing required command failure:\n%s", out)
	}
}

func TestE2EAgentLaunchFailsWhenSelectedHarnessEnvIsMissing(t *testing.T) {
	t.Parallel()

	const missingEnv = "GESTALT_TEST_MISSING_SELECTED_HARNESS_ENV"

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `apiVersion: gestaltd.config/v5
providers:
  agent:
    local:
      default: true
      localHarness:
        command: /bin/sh
        env:
          OPENAI_API_KEY: ${GESTALT_TEST_MISSING_SELECTED_HARNESS_ENV}
    unselected:
      localHarness:
        command: /bin/sh
        env:
          OTHER: ${GESTALT_TEST_MISSING_UNSELECTED_HARNESS_ENV}
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := exec.Command(gestaltdBin, "agent", "launch", "--config", cfgPath, "--dry-run")
	cmd.Env = withoutEnv(os.Environ(), missingEnv, "GESTALT_TEST_MISSING_UNSELECTED_HARNESS_ENV")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("agent launch unexpectedly succeeded:\n%s", out)
	}
	got := string(out)
	if !strings.Contains(got, "providers.agent.local harness") ||
		!strings.Contains(got, missingEnv) ||
		strings.Contains(got, "GESTALT_TEST_MISSING_UNSELECTED_HARNESS_ENV") {
		t.Fatalf("missing env output = %s", out)
	}
}

func withoutEnv(env []string, keys ...string) []string {
	drop := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		drop[key] = struct{}{}
	}
	out := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if _, ok := drop[key]; ok {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func writeExecutable(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}
}
