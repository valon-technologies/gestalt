package daemon

import (
	"path/filepath"
	"strings"
	"testing"
)

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
