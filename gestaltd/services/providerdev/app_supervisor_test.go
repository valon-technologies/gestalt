package providerdev

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProbeFrontendReadinessTimeoutBehavior(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name            string
		initiallyReady  bool
		readyOnAttempt  int32
		loseReadiness   bool
		wantWarnings    int
		wantReadyAtStop bool
	}{
		{name: "ready before deadline then disconnects", initiallyReady: true, loseReadiness: true, wantWarnings: 0, wantReadyAtStop: false},
		{name: "never ready", wantWarnings: 1, wantReadyAtStop: false},
		{name: "ready after deadline", readyOnAttempt: 2, wantWarnings: 1, wantReadyAtStop: true},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var available atomic.Bool
			available.Store(tc.initiallyReady)
			var attempts atomic.Int32
			app := &appManaged{
				target: AppTarget{
					Name:     tc.name,
					BasePath: "/" + strings.ReplaceAll(tc.name, " ", "-"),
					Commands: []AppCommand{{ReadyTimeout: 50 * time.Millisecond}},
				},
				frontendReady: make(chan struct{}),
			}
			var logs bytes.Buffer
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() {
				app.probeFrontendReadinessUntil(ctx, slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})), time.Now().Add(50*time.Millisecond), func() (net.Conn, error) {
					if tc.readyOnAttempt > 0 && attempts.Add(1) >= tc.readyOnAttempt {
						conn, _ := net.Pipe()
						return conn, nil
					}
					if available.Load() {
						conn, _ := net.Pipe()
						return conn, nil
					}
					return nil, errors.New("frontend unavailable")
				})
				close(done)
			}()

			if tc.initiallyReady || tc.readyOnAttempt > 0 {
				select {
				case <-app.frontendReady:
				case <-time.After(time.Second):
					t.Fatal("frontend did not become ready")
				}
			}
			if tc.loseReadiness {
				available.Store(false)
			}
			time.Sleep(350 * time.Millisecond)
			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("readiness probe did not stop")
			}
			if got := strings.Count(logs.String(), "dev app frontend readiness timeout"); got != tc.wantWarnings {
				t.Fatalf("startup timeout warnings = %d, want %d; logs=%s", got, tc.wantWarnings, logs.String())
			}
			if got := app.ready.Load(); got != tc.wantReadyAtStop {
				t.Fatalf("frontend ready = %v, want %v", got, tc.wantReadyAtStop)
			}
		})
	}
}
