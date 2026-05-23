package daemon

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestMainExitPolicy(t *testing.T) { //nolint:paralleltest // mutates slog.Default
	for _, tc := range []struct { //nolint:paralleltest // subtests mutate slog.Default
		name     string
		err      error
		wantCode int
		wantLog  bool
	}{
		{
			name:     "success",
			err:      nil,
			wantCode: 0,
		},
		{
			name:     "help",
			err:      flag.ErrHelp,
			wantCode: 0,
		},
		{
			name:     "child exit",
			err:      exitCodeError{code: 7},
			wantCode: 7,
		},
		{
			name:     "failure",
			err:      fmt.Errorf("boom"),
			wantCode: 1,
			wantLog:  true,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var logs []string
			origDefault := slog.Default()
			t.Cleanup(func() { slog.SetDefault(origDefault) })
			slog.SetDefault(slog.New(&testLogHandler{logs: &logs}))

			if got := mainExitCode(tc.err); got != tc.wantCode {
				t.Fatalf("mainExitCode() = %d, want %d", got, tc.wantCode)
			}
			if tc.wantLog {
				if len(logs) != 1 {
					t.Fatalf("log count = %d, want 1", len(logs))
				}
				if logs[0] != "gestaltd exited error=boom" {
					t.Fatalf("log = %q, want gestaltd exited error=boom", logs[0])
				}
				return
			}
			if len(logs) != 0 {
				t.Fatalf("unexpected logs: %v", logs)
			}
		})
	}
}

func TestExitCode(t *testing.T) {
	t.Parallel()

	if code, ok := exitCode(exitCodeError{code: 7}); !ok || code != 7 {
		t.Fatalf("exitCode(exitCodeError{7}) = (%d, %v), want (7, true)", code, ok)
	}
	if code, ok := exitCode(fmt.Errorf("other")); ok {
		t.Fatalf("exitCode(generic) = (%d, true), want (_, false)", code)
	}
}

type testLogHandler struct {
	logs *[]string
}

func (h *testLogHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return true
}

func (h *testLogHandler) Handle(_ context.Context, r slog.Record) error {
	var buf strings.Builder
	r.Attrs(func(a slog.Attr) bool {
		if buf.Len() > 0 {
			buf.WriteByte(' ')
		}
		buf.WriteString(a.Key)
		buf.WriteByte('=')
		buf.WriteString(a.Value.String())
		return true
	})
	*h.logs = append(*h.logs, r.Message+" "+buf.String())
	return nil
}

func (h *testLogHandler) WithAttrs(_ []slog.Attr) slog.Handler {
	return h
}

func (h *testLogHandler) WithGroup(_ string) slog.Handler {
	return h
}
