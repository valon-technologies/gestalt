package daemon

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseGlobalCLIOutputFlags(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		args      []string
		wantFlags CLIOutputFlags
		wantArgs  string
		wantErr   string
	}{
		{
			name:      "basic global flags before sync",
			args:      []string{"--verbose", "--quiet", "sync", "-v", "--no-progress", "--config", "config.yaml"},
			wantFlags: CLIOutputFlags{Verbose: true, Quiet: true, NoProgress: true},
			wantArgs:  "sync -v --config config.yaml",
		},
		{
			name:     "does not claim version short flags",
			args:     []string{"-v", "--v", "version"},
			wantArgs: "-v --v version",
		},
		{
			name:      "double dash boolean values",
			args:      []string{"--quiet=false", "--verbose=true", "--no-progress=false", "version"},
			wantFlags: CLIOutputFlags{Verbose: true},
			wantArgs:  "version",
		},
		{
			name:      "single dash boolean values",
			args:      []string{"-quiet=true", "-verbose=false", "-no-progress=true", "version"},
			wantFlags: CLIOutputFlags{Quiet: true, NoProgress: true},
			wantArgs:  "version",
		},
		{
			name:     "value before command",
			args:     []string{"--config", "--no-progress", "sync"},
			wantArgs: "--config --no-progress sync",
		},
		{
			name:     "value after command",
			args:     []string{"sync", "--config", "--quiet"},
			wantArgs: "sync --config --quiet",
		},
		{
			name:      "global flag after value",
			args:      []string{"--config", "config.yaml", "--quiet", "sync"},
			wantFlags: CLIOutputFlags{Quiet: true},
			wantArgs:  "--config config.yaml sync",
		},
		{
			name:      "equals form",
			args:      []string{"--config=--no-progress", "--quiet", "sync"},
			wantFlags: CLIOutputFlags{Quiet: true},
			wantArgs:  "--config=--no-progress sync",
		},
		{
			name:     "single dash before command",
			args:     []string{"-config", "--no-progress", "sync"},
			wantArgs: "-config --no-progress sync",
		},
		{
			name:     "single dash after command",
			args:     []string{"sync", "-config", "--quiet"},
			wantArgs: "sync -config --quiet",
		},
		{
			name:      "single dash equals form",
			args:      []string{"-config=--no-progress", "--quiet", "sync"},
			wantFlags: CLIOutputFlags{Quiet: true},
			wantArgs:  "-config=--no-progress sync",
		},
		{
			name:    "reject invalid quiet",
			args:    []string{"--quiet=maybe", "version"},
			wantErr: "expected true or false",
		},
		{
			name:    "reject invalid verbose",
			args:    []string{"--verbose=1", "version"},
			wantErr: "expected true or false",
		},
		{
			name:    "reject empty no-progress value",
			args:    []string{"--no-progress=", "version"},
			wantErr: "expected true or false",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotFlags, gotArgs, err := parseGlobalCLIOutputFlags(tc.args)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("parseGlobalCLIOutputFlags() error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseGlobalCLIOutputFlags() error = %v", err)
			}
			if gotFlags != tc.wantFlags {
				t.Fatalf("flags = %#v, want %#v", gotFlags, tc.wantFlags)
			}
			if got, want := strings.Join(gotArgs, " "), tc.wantArgs; got != want {
				t.Fatalf("args = %q, want %q", got, want)
			}
		})
	}
}

func TestCLIReporterInstall(t *testing.T) {
	t.Parallel()

	cliRunMu.Lock()
	defer cliRunMu.Unlock()

	var output bytes.Buffer
	reporter := NewTerminalReporter(&output, CLIOutputPolicy{
		CLIOutputFlags: CLIOutputFlags{Quiet: true},
	})
	scope := installCLIReporter(reporter)
	defer scope.Close()

	if currentCLIReporter() != reporter {
		t.Fatal("currentCLIReporter() did not return the installed reporter")
	}
	if got := currentCLIOutputFlags(); got != reporter.policy.CLIOutputFlags {
		t.Fatalf("currentCLIOutputFlags() = %#v, want %#v", got, reporter.policy.CLIOutputFlags)
	}
}

func TestMainUsageIncludesSharedOutputFlags(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	printMainUsage(&output)
	for _, want := range []string{"--verbose", "--quiet", "--no-progress", "-v", "-V", "version"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("main usage does not contain %q:\n%s", want, output.String())
		}
	}
	if strings.Contains(output.String(), "\n  --v ") {
		t.Fatalf("main usage contains unsupported --v flag:\n%s", output.String())
	}
}

func TestRunSyncHonorsGlobalQuietFlag(t *testing.T) {
	t.Parallel()

	cliRunMu.Lock()
	defer cliRunMu.Unlock()

	scope := installCLIReporter(NewTerminalReporter(io.Discard, CLIOutputPolicy{
		CLIOutputFlags: CLIOutputFlags{Verbose: true, Quiet: true},
	}))
	defer scope.Close()

	err := runSync([]string{"--verbose", "--config", filepath.Join(t.TempDir(), "missing.yaml")})
	if err == nil || !strings.Contains(err.Error(), "loading config") {
		t.Fatalf("runSync() error = %v, want config load failure", err)
	}
}

func TestRunSyncParallelismValidation(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		args      []string
		wantError string
	}{
		{
			name:      "zero",
			args:      []string{"--locked", "--parallelism", "0", "--config", filepath.Join(t.TempDir(), "missing.yaml")},
			wantError: "invalid --parallelism 0: must be at least 1",
		},
		{
			name:      "negative",
			args:      []string{"--locked", "--parallelism", "-1", "--config", filepath.Join(t.TempDir(), "missing.yaml")},
			wantError: "invalid --parallelism -1: must be at least 1",
		},
		{
			name:      "one accepted",
			args:      []string{"--parallelism", "1", "--config", filepath.Join(t.TempDir(), "missing.yaml")},
			wantError: "loading config",
		},
		{
			name:      "two accepted",
			args:      []string{"--parallelism", "2", "--config", filepath.Join(t.TempDir(), "missing.yaml")},
			wantError: "loading config",
		},
		{
			name:      "cache dir accepted",
			args:      []string{"--cache-dir", filepath.Join(t.TempDir(), "cache"), "--config", filepath.Join(t.TempDir(), "missing.yaml")},
			wantError: "loading config",
		},
		{
			name:      "verbose long accepted",
			args:      []string{"--verbose", "--config", filepath.Join(t.TempDir(), "missing.yaml")},
			wantError: "loading config",
		},
		{
			name:      "verbose short accepted",
			args:      []string{"-v", "--config", filepath.Join(t.TempDir(), "missing.yaml")},
			wantError: "loading config",
		},
		{
			name:      "repeated verbose short accepted",
			args:      []string{"-v", "-v", "--config", filepath.Join(t.TempDir(), "missing.yaml")},
			wantError: "loading config",
		},
		{
			name:      "condensed verbose short rejected",
			args:      []string{"-vv"},
			wantError: "flag provided but not defined: -vv",
		},
		{
			name:      "output format text accepted",
			args:      []string{"--output-format", "text", "--config", filepath.Join(t.TempDir(), "missing.yaml")},
			wantError: "loading config",
		},
		{
			name:      "output format json accepted",
			args:      []string{"--output-format=json", "--config", filepath.Join(t.TempDir(), "missing.yaml")},
			wantError: "loading config",
		},
		{
			name:      "output format invalid",
			args:      []string{"--output-format", "yaml"},
			wantError: `invalid --output-format "yaml"; expected "text" or "json"`,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := runSync(tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("runSync(%v) error = %v, want %q", tc.args, err, tc.wantError)
			}
		})
	}
}
