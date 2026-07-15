package daemon

import (
	"bytes"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/operator"
)

func TestTerminalReporterNonTTYUsesStableLines(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	reporter := NewTerminalReporter(&output, CLIOutputPolicy{})
	activity := reporter.Start("preparing")
	if got := output.String(); got != "preparing\n" {
		t.Fatalf("start output = %q, want stable status line", got)
	}
	activity.Finish("prepared")
	if got := output.String(); got != "preparing\nprepared\n" {
		t.Fatalf("finished output = %q, want stable status and completion lines", got)
	}
	if strings.Contains(output.String(), "\r") {
		t.Fatalf("non-TTY output contains carriage return: %q", output.String())
	}
}

func TestTerminalReporterNoProgressKeepsStableCompletion(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	reporter := NewTerminalReporter(&output, CLIOutputPolicy{
		CLIOutputFlags: CLIOutputFlags{NoProgress: true},
		Interactive:    true,
	})
	activity := reporter.Start("working")
	activity.Finish("done")
	if got := output.String(); got != "working\ndone\n" {
		t.Fatalf("no-progress output = %q, want stable status and completion", got)
	}
	if strings.Contains(output.String(), "\r") {
		t.Fatalf("no-progress output contains animation control: %q", output.String())
	}
}

func TestTerminalReporterQuietPreservesDiagnostics(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	reporter := NewTerminalReporter(&output, CLIOutputPolicy{CLIOutputFlags: CLIOutputFlags{Quiet: true}})
	reporter.Status("status")
	reporter.Verbose("details")
	activity := reporter.Start("working")
	activity.Finish("done")
	reporter.Warning("careful")
	if got := output.String(); got != "warning: careful\n" {
		t.Fatalf("quiet output = %q, want warnings only", got)
	}
}

func TestLifecycleProgressReporterUsesStableCommandLines(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	reporter := NewTerminalReporter(&out, CLIOutputPolicy{})
	progress := newLifecycleProgressReporter(reporter)
	progress(operator.LifecycleProgressEvent{Operation: operator.LifecycleOperationSync, Phase: operator.LifecyclePhaseInstall, Status: operator.LifecycleProgressStarted, Subject: "sync"})
	progress(operator.LifecycleProgressEvent{Operation: operator.LifecycleOperationSync, Phase: operator.LifecyclePhaseInstall, Status: operator.LifecycleProgressCompleted, Subject: "sync"})

	got := out.String()
	if !strings.Contains(got, "Preparing artifacts\n") || !strings.Contains(got, "Artifacts prepared\n") {
		t.Fatalf("phase summary missing from output: %q", got)
	}
	if strings.Contains(got, "\r") {
		t.Fatalf("lifecycle output should not use spinner control: %q", got)
	}
}

func TestLifecycleProgressReporterLockNotRequiredMessage(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	reporter := NewTerminalReporter(&out, CLIOutputPolicy{})
	progress := newLifecycleProgressReporter(reporter)
	progress(operator.LifecycleProgressEvent{
		Operation: operator.LifecycleOperationLock,
		Phase:     operator.LifecyclePhaseLock,
		Status:    operator.LifecycleProgressNoop,
		Subject:   "lockfile",
		Reason:    "not_required",
	})

	if got := out.String(); got != "Lockfile not required\n" {
		t.Fatalf("lock noop output = %q, want %q", got, "Lockfile not required\n")
	}
}
