package daemon

import (
	"bytes"
	"strings"
	"testing"
)

func TestTerminalReporterNoProgressUsesStableOutput(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	reporter := NewTerminalReporter(&output, CLIOutputPolicy{
		Interactive:    true,
		CLIOutputFlags: CLIOutputFlags{NoProgress: true},
	})
	activity := reporter.Start("Syncing artifacts")
	activity.Finish("Sync complete")

	got := output.String()
	if strings.Contains(got, "\r") {
		t.Fatalf("no-progress output should not use spinner carriage returns:\n%s", got)
	}
	if !strings.Contains(got, "Syncing artifacts") {
		t.Fatalf("no-progress output missing stable status line:\n%s", got)
	}
	if !strings.Contains(got, "Sync complete") {
		t.Fatalf("no-progress output missing completion line:\n%s", got)
	}
}
