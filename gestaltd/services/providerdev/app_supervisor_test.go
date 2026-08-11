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

func TestAppHandleFrontendURL(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		basePath string
		want     string
	}{
		{name: "mounted app", basePath: "/data-platform-dashboard", want: "http://127.0.0.1:5173/data-platform-dashboard/"},
		{name: "root mount", basePath: "/", want: "http://127.0.0.1:5173/"},
		{name: "empty mount", want: "http://127.0.0.1:5173/"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			handle := &AppHandle{app: &appManaged{
				target: AppTarget{BasePath: tc.basePath},
				port:   5173,
			}}
			if got := handle.FrontendURL(); got != tc.want {
				t.Fatalf("FrontendURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSupervisorAppHandle(t *testing.T) {
	t.Parallel()

	app := &appManaged{target: AppTarget{Name: "dashboard"}, port: 5173}
	supervisor := &Supervisor{apps: map[string]*appManaged{"dashboard": app}}
	handle, ok := supervisor.AppHandle("dashboard")
	if !ok {
		t.Fatal("AppHandle(dashboard) not found")
	}
	if handle.app != app {
		t.Fatal("AppHandle(dashboard) returned a different managed app")
	}
	if _, ok := supervisor.AppHandle("missing"); ok {
		t.Fatal("AppHandle(missing) unexpectedly found")
	}
}

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
