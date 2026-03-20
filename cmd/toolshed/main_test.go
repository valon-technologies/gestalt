package main

import (
	"testing"
)

func TestRun_UnknownCommand(t *testing.T) {
	t.Parallel()

	err := run([]string{"bogus"})
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
	if err.Error() != "unknown command: bogus" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRun_MCPSubcommand_MissingConfig(t *testing.T) {
	t.Parallel()

	err := run([]string{"mcp", "-config", "/nonexistent/config.yaml"})
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}
