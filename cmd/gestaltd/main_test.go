package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestRun_CheckWithMissingConfig(t *testing.T) {
	t.Parallel()

	err := run([]string{"--check", "--config", "nonexistent.yaml"})
	if err == nil {
		t.Fatal("expected error for missing config file")
	}
}

func TestRun_UnknownFlag(t *testing.T) {
	t.Parallel()

	err := run([]string{"--bogus"})
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

func TestGestaltd_HelpExitsCleanly(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("go", "run", ".", "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected exit 0 for --help, got error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "-config") {
		t.Fatalf("expected usage output containing '-config', got: %s", out)
	}
}

func TestRun_RejectsPositionalArgs(t *testing.T) {
	t.Parallel()
	err := run([]string{"serve", "--config", "foo.yaml"})
	if err == nil {
		t.Fatal("expected error for positional arguments")
	}
}

func TestRun_RejectsTrailingArgs(t *testing.T) {
	t.Parallel()
	err := run([]string{"--config", "foo.yaml", "extra"})
	if err == nil {
		t.Fatal("expected error for trailing arguments")
	}
}
