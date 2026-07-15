package daemon

import (
	"io"
	"os"
	"testing"

	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

func TestSyncBuildOutputPolicy(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		quiet       bool
		verbose     bool
		format      string
		wantStream  bool
		wantCapture bool
		wantDiscard bool
	}{
		{
			name:        "verbose streams build output to stderr",
			format:      syncOutputFormatText,
			verbose:     true,
			wantStream:  true,
		},
		{
			name:       "json streams build output to stderr",
			format:     syncOutputFormatJSON,
			wantStream: true,
		},
		{
			name:        "quiet text captures build output for failures",
			quiet:       true,
			format:      syncOutputFormatText,
			wantCapture: true,
		},
		{
			name:        "quiet json captures build output for failures",
			quiet:       true,
			format:      syncOutputFormatJSON,
			wantCapture: true,
		},
		{
			name:        "quiet verbose captures build output for failures",
			quiet:       true,
			verbose:     true,
			format:      syncOutputFormatText,
			wantCapture: true,
		},
		{
			name:        "default text discards successful build chatter",
			format:      syncOutputFormatText,
			wantDiscard: true,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := syncBuildOutput(tc.quiet, tc.format, tc.verbose)
			switch {
			case tc.wantStream:
				if got.CaptureErrors != nil {
					t.Fatalf("CaptureErrors = %v, want nil", got.CaptureErrors)
				}
				if got.Stdout != os.Stderr || got.Stderr != os.Stderr {
					t.Fatalf("build output = %#v, want stderr streaming", got)
				}
			case tc.wantCapture:
				if got.CaptureErrors != os.Stderr {
					t.Fatalf("CaptureErrors = %v, want os.Stderr", got.CaptureErrors)
				}
				if got.Stdout != nil || got.Stderr != nil {
					t.Fatalf("build output = %#v, want capture-only routing", got)
				}
			case tc.wantDiscard:
				if got.CaptureErrors != nil {
					t.Fatalf("CaptureErrors = %v, want nil", got.CaptureErrors)
				}
				if got.Stdout != io.Discard || got.Stderr != io.Discard {
					t.Fatalf("build output = %#v, want io.Discard", got)
				}
			default:
				t.Fatal("test case missing expectation")
			}
		})
	}
}

func TestSyncBuildOutputQuietUsesProviderCapture(t *testing.T) {
	t.Parallel()

	got := syncBuildOutput(true, syncOutputFormatJSON, true)
	if got.CaptureErrors == nil {
		t.Fatal("quiet sync build output should capture failures")
	}
	if got.Stdout != nil || got.Stderr != nil {
		t.Fatalf("quiet sync build output = %#v, want capture routing only", got)
	}
	_ = providerpkg.CommandOutput(got)
}
