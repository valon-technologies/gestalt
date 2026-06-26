package providerdev

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

const (
	defaultReadyTimeout   = 90 * time.Second
	devShutdownGrace      = 5 * time.Second
	devRestartBackoffMin  = 500 * time.Millisecond
	devRestartBackoffMax  = 8 * time.Second
	devReadinessProbeTick = 250 * time.Millisecond
)

type Target struct {
	Name         string
	Kind         string
	BasePath     string
	Workdir      string
	Command      []string
	Env          map[string]string
	ReadyTimeout time.Duration
}

type Supervisor struct {
	ctx    context.Context
	cancel context.CancelFunc
	logger *slog.Logger
	procs  map[string]*managedProc
	wg     sync.WaitGroup
}

type managedProc struct {
	target    Target
	port      int
	proxy     *httputil.ReverseProxy
	proxyPort int
	ready     atomic.Bool
	mu        sync.Mutex
	cmd       *exec.Cmd
	done      chan struct{}
	waitErr   error
}

func TargetsFromConfig(cfg *config.Config) ([]Target, error) {
	if cfg == nil {
		return nil, nil
	}
	names := make([]string, 0, len(cfg.Providers.UI))
	for name := range cfg.Providers.UI {
		names = append(names, name)
	}
	slices.Sort(names)

	var targets []Target
	for _, name := range names {
		entry := cfg.Providers.UI[name]
		if entry == nil || !entry.DevActive {
			continue
		}
		run := providerpkg.EffectiveSourceRun(entry.ResolvedManifest)
		if run == nil {
			return nil, fmt.Errorf("ui %q: dev-active without run manifest", name)
		}
		workdir := entry.ResolvedDevWorkdir
		if w := strings.TrimSpace(run.Workdir); w != "" && w != "." {
			root := filepath.Dir(entry.ResolvedManifestPath)
			workdir = filepath.Join(root, filepath.FromSlash(w))
		}
		readyTimeout := defaultReadyTimeout
		if run.ReadyTimeout > 0 {
			readyTimeout = run.ReadyTimeout
		}
		targets = append(targets, Target{
			Name:         name,
			Kind:         "ui",
			BasePath:     entry.Path,
			Workdir:      workdir,
			Command:      append([]string(nil), run.Command...),
			Env:          run.Env,
			ReadyTimeout: readyTimeout,
		})
	}
	return targets, nil
}

func Start(ctx context.Context, logger *slog.Logger, targets []Target) (*Supervisor, error) {
	if len(targets) == 0 {
		return &Supervisor{procs: map[string]*managedProc{}}, nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	runCtx, cancel := context.WithCancel(ctx)
	s := &Supervisor{
		ctx:    runCtx,
		cancel: cancel,
		logger: logger,
		procs:  make(map[string]*managedProc, len(targets)),
	}
	for _, target := range targets {
		target := target
		if len(target.Command) == 0 {
			cancel()
			return nil, fmt.Errorf("dev target %q: command is required", target.Name)
		}
		proc := &managedProc{target: target}
		s.procs[target.Name] = proc
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.runProc(runCtx, proc)
		}()
	}
	return s, nil
}

func (s *Supervisor) Handlers() map[string]http.Handler {
	handlers := make(map[string]http.Handler, len(s.procs))
	for name, proc := range s.procs {
		handlers[name] = proc.handler()
	}
	return handlers
}

func (s *Supervisor) Stop() {
	if s == nil {
		return
	}
	s.cancel()
	for _, proc := range s.procs {
		proc.stop()
	}
	s.wg.Wait()
}

func (s *Supervisor) runProc(ctx context.Context, proc *managedProc) {
	backoff := devRestartBackoffMin
	for {
		if ctx.Err() != nil {
			return
		}
		if err := proc.start(ctx, s.logger); err != nil {
			s.logger.Warn("dev process failed to start", "provider", proc.target.Name, "error", err)
		} else {
			waitErr := proc.wait()
			if ctx.Err() != nil {
				return
			}
			if waitErr != nil {
				s.logger.Warn("dev process exited", "provider", proc.target.Name, "error", waitErr)
			}
		}
		proc.ready.Store(false)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if backoff < devRestartBackoffMax {
			backoff *= 2
			if backoff > devRestartBackoffMax {
				backoff = devRestartBackoffMax
			}
		}
	}
}

func (p *managedProc) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !p.ready.Load() {
			serveNotReady(w)
			return
		}
		proxy, err := p.reverseProxy()
		if err != nil {
			http.Error(w, "dev server unavailable", http.StatusBadGateway)
			return
		}
		proxy.ServeHTTP(w, r)
	})
}

func (p *managedProc) reverseProxy() (*httputil.ReverseProxy, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.proxy != nil && p.proxyPort == p.port {
		return p.proxy, nil
	}
	upstream, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", p.port))
	if err != nil {
		return nil, err
	}
	p.proxy = newReverseProxy(upstream)
	p.proxyPort = p.port
	return p.proxy, nil
}

func (p *managedProc) start(ctx context.Context, logger *slog.Logger) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	port, err := reserveLocalPort()
	if err != nil {
		return err
	}
	p.port = port
	p.ready.Store(false)

	cmd := exec.CommandContext(ctx, p.target.Command[0], p.target.Command[1:]...)
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = devShutdownGrace
	cmd.Dir = p.target.Workdir
	cmd.Env = devCommandEnv(port, p.target)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout := newPrefixedLineWriter(logger, p.target.Name, "stdout")
	stderr := newPrefixedLineWriter(logger, p.target.Name, "stderr")
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return err
	}
	p.cmd = cmd
	done := make(chan struct{})
	p.done = done
	go func() {
		p.waitErr = cmd.Wait()
		p.ready.Store(false)
		close(done)
	}()
	go p.probeReadiness(ctx, logger, port, done)
	return nil
}

func (p *managedProc) probeReadiness(ctx context.Context, logger *slog.Logger, port int, done chan struct{}) {
	deadline := time.Now().Add(p.target.ReadyTimeout)
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	warned := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		default:
		}
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			_ = conn.Close()
			p.ready.Store(true)
			logger.Info("dev process ready", "provider", p.target.Name, "port", port)
			return
		}
		if !warned && time.Now().After(deadline) {
			warned = true
			logger.Warn("dev process readiness timeout; continuing probe", "provider", p.target.Name, "timeout", p.target.ReadyTimeout)
		}
		timer := time.NewTimer(devReadinessProbeTick)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-done:
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (p *managedProc) wait() error {
	p.mu.Lock()
	done := p.done
	p.mu.Unlock()
	if done == nil {
		return nil
	}
	<-done
	return p.waitErr
}

func (p *managedProc) stop() {
	p.mu.Lock()
	cmd := p.cmd
	done := p.done
	p.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(devShutdownGrace):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
	}
}

// devCommandEnv builds the child process environment. GESTALT_DEV,
// GESTALT_DEV_PORT, and GESTALT_DEV_BASE_PATH are reserved contract
// variables and are appended last so manifest dev.env cannot override them.
func devCommandEnv(port int, target Target) []string {
	env := append([]string(nil), os.Environ()...)
	for key, value := range target.Env {
		env = append(env, key+"="+value)
	}
	env = append(env,
		"GESTALT_DEV=1",
		"GESTALT_DEV_PORT="+strconv.Itoa(port),
		"GESTALT_DEV_BASE_PATH="+target.BasePath,
	)
	return env
}

func reserveLocalPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("reserve local dev port: %w", err)
	}
	defer func() { _ = listener.Close() }()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("reserve local dev port: unexpected listener address type")
	}
	return addr.Port, nil
}

type prefixedLineWriter struct {
	logger   *slog.Logger
	provider string
	stream   string
	buf      []byte
}

func newPrefixedLineWriter(logger *slog.Logger, provider, stream string) *prefixedLineWriter {
	return &prefixedLineWriter{logger: logger, provider: provider, stream: stream}
}

func (w *prefixedLineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		idx := bytes.IndexByte(w.buf, '\n')
		if idx < 0 {
			break
		}
		line := strings.TrimRight(string(w.buf[:idx]), "\r")
		w.buf = w.buf[idx+1:]
		if line != "" {
			w.logger.Info(line, "provider", w.provider, "stream", w.stream)
		}
	}
	return len(p), nil
}
