package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestGestaltd_HelpExitsCleanly(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("go", "run", ".", "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected exit 0 for --help, got error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "gestaltd validate") {
		t.Fatalf("expected usage output containing 'gestaltd validate', got: %s", out)
	}
	if !strings.Contains(string(out), "gestaltd prepare") {
		t.Fatalf("expected usage output containing 'gestaltd prepare', got: %s", out)
	}
	if !strings.Contains(string(out), "gestaltd serve") {
		t.Fatalf("expected usage output containing 'gestaltd serve', got: %s", out)
	}
	if !strings.Contains(string(out), "gestaltd dev") {
		t.Fatalf("expected usage output containing 'gestaltd dev', got: %s", out)
	}
}

func TestGestaltdValidateHelpExitsCleanly(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("go", "run", ".", "validate", "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected exit 0 for 'validate --help', got error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "gestaltd validate") {
		t.Fatalf("expected usage output containing 'gestaltd validate', got: %s", out)
	}
}

func TestGestaltdPrepareHelpExitsCleanly(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("go", "run", ".", "prepare", "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected exit 0 for 'prepare --help', got error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "gestaltd prepare") {
		t.Fatalf("expected usage output containing 'gestaltd prepare', got: %s", out)
	}
}
