package main

import (
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
