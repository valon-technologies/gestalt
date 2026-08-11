package providerdev

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
)

const frontendOnlyGrace = 5 * time.Second

var ErrFrontendOnlyDevApp = errors.New("frontend-only dev app")

type AppCommand struct {
	Workdir      string
	Command      []string
	Env          map[string]string
	ReadyTimeout time.Duration
}

type AppTarget struct {
	Name       string
	BasePath   string
	SocketPath string
	BaseEnv    map[string]string
	Commands   []AppCommand
}

type AppHandle struct {
	name string
	app  *appManaged
}

func (h *AppHandle) FrontendHandler() http.Handler {
	return h.app.frontendHandler()
}

func (h *AppHandle) FrontendReady() <-chan struct{} {
	return h.app.frontendReady
}

func (h *AppHandle) AllExited() <-chan struct{} {
	return h.app.allExited
}

func (h *AppHandle) SocketPath() string {
	return h.app.target.SocketPath
}

func (h *AppHandle) FrontendURL() string {
	if h == nil || h.app == nil {
		return ""
	}
	basePath := "/" + strings.Trim(h.app.target.BasePath, "/")
	if basePath != "/" {
		basePath += "/"
	}
	return (&url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort("127.0.0.1", strconv.Itoa(h.app.port)),
		Path:   basePath,
	}).String()
}

type appManaged struct {
	target AppTarget
	port   int
	cancel context.CancelFunc
	logger *slog.Logger

	frontendReady     chan struct{}
	frontendReadyOnce sync.Once
	allExited         chan struct{}

	proxy     *httputil.ReverseProxy
	proxyPort int
	ready     atomic.Bool

	mu    sync.Mutex
	procs []*appCommandProc
	wg    sync.WaitGroup
}

type appCommandProc struct {
	parent  *appManaged
	index   int
	cmd     *exec.Cmd
	done    chan struct{}
	waitErr error
	output  *childOutput
}

func AppTargetForEntry(name string, entry *config.ProviderEntry) (AppTarget, error) {
	if entry == nil {
		return AppTarget{}, fmt.Errorf("app %q: entry is required", name)
	}
	if !entry.DevActive {
		return AppTarget{}, fmt.Errorf("app %q: dev-active required", name)
	}
	commands, err := providerpkg.SourceRunCommands(entry.ResolvedManifestPath)
	if err != nil {
		return AppTarget{}, fmt.Errorf("app %q: %w", name, err)
	}
	basePath := ""
	if entry.Static != nil {
		basePath = strings.TrimSpace(entry.Static.Mount)
	}
	cmds := make([]AppCommand, 0, len(commands))
	for _, command := range commands {
		readyTimeout := defaultReadyTimeout
		if command.ReadyTimeout > 0 {
			readyTimeout = command.ReadyTimeout
		}
		workdir := command.Workdir
		if workdir == "" {
			workdir = entry.ResolvedDevWorkdir
		}
		cmds = append(cmds, AppCommand{
			Workdir:      workdir,
			Command:      append([]string(nil), command.Command...),
			Env:          command.Env,
			ReadyTimeout: readyTimeout,
		})
	}
	return AppTarget{
		Name:     name,
		BasePath: basePath,
		Commands: cmds,
	}, nil
}

func (s *Supervisor) StartApp(ctx context.Context, target AppTarget) (*AppHandle, error) {
	if s == nil {
		return nil, fmt.Errorf("dev supervisor is not configured")
	}
	if len(target.Commands) == 0 {
		return nil, fmt.Errorf("dev app %q: at least one run command is required", target.Name)
	}
	for i, command := range target.Commands {
		if len(command.Command) == 0 {
			return nil, fmt.Errorf("dev app %q command %d: command is required", target.Name, i)
		}
	}
	if s.procByName(target.Name) != nil {
		return nil, fmt.Errorf("dev app %q is already running", target.Name)
	}
	port, err := reserveLocalPort()
	if err != nil {
		return nil, err
	}
	runCtx := ctx
	if s.ctx != nil {
		runCtx = s.ctx
	}
	appCtx, appCancel := context.WithCancel(runCtx)
	app := &appManaged{
		target:        target,
		port:          port,
		cancel:        appCancel,
		logger:        s.logger,
		frontendReady: make(chan struct{}),
		allExited:     make(chan struct{}),
	}
	s.appsMu.Lock()
	if _, exists := s.apps[target.Name]; exists {
		s.appsMu.Unlock()
		appCancel()
		return nil, fmt.Errorf("dev app %q already started", target.Name)
	}
	s.apps[target.Name] = app
	s.appsMu.Unlock()
	for i, command := range target.Commands {
		proc := &appCommandProc{parent: app, index: i}
		app.procs = append(app.procs, proc)
		app.wg.Add(1)
		go func(proc *appCommandProc, command AppCommand) {
			defer app.wg.Done()
			s.runAppCommand(appCtx, proc, command)
		}(proc, command)
	}
	go func() {
		app.wg.Wait()
		close(app.allExited)
	}()
	go app.probeFrontendReadiness(appCtx, s.logger)
	return &AppHandle{name: target.Name, app: app}, nil
}

func (s *Supervisor) AppHandle(name string) (*AppHandle, bool) {
	if s == nil {
		return nil, false
	}
	s.appsMu.RLock()
	app, ok := s.apps[name]
	s.appsMu.RUnlock()
	if !ok {
		return nil, false
	}
	return &AppHandle{name: name, app: app}, true
}

func (s *Supervisor) StopApp(name string) {
	if s == nil {
		return
	}
	s.appsMu.Lock()
	app, ok := s.apps[name]
	if ok {
		delete(s.apps, name)
	}
	s.appsMu.Unlock()
	if !ok {
		return
	}
	if app.cancel != nil {
		app.cancel()
	}
	app.stop()
}

func (s *Supervisor) runAppCommand(ctx context.Context, proc *appCommandProc, command AppCommand) {
	backoff := devRestartBackoffMin
	for {
		if ctx.Err() != nil {
			return
		}
		if err := proc.start(ctx, command); err != nil {
			if s.logger != nil {
				args := []any{"app", proc.parent.target.Name, "command", proc.index, "error", err}
				args = append(args, childFailureLogArgs(proc.currentOutput())...)
				s.logger.Warn("dev app command failed to start", args...)
			}
		} else {
			waitErr := proc.wait()
			if ctx.Err() != nil {
				return
			}
			if waitErr != nil && s.logger != nil {
				args := []any{"app", proc.parent.target.Name, "command", proc.index, "error", waitErr}
				args = append(args, childFailureLogArgs(proc.currentOutput())...)
				s.logger.Warn("dev app command exited", args...)
			}
		}
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

func (p *appCommandProc) start(ctx context.Context, command AppCommand) error {
	p.parent.mu.Lock()
	defer p.parent.mu.Unlock()

	cmd := exec.CommandContext(ctx, command.Command[0], command.Command[1:]...)
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = devShutdownGrace
	cmd.Dir = command.Workdir
	cmd.Env = devAppCommandEnv(p.parent.port, p.parent.target, command.Env)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	output := newChildOutput(p.parent.logger, "app", p.parent.target.Name, strconv.Itoa(p.index))
	cmd.Stdout, cmd.Stderr = output.writers()
	p.output = output
	if err := cmd.Start(); err != nil {
		return err
	}
	p.cmd = cmd
	done := make(chan struct{})
	p.done = done
	go func() {
		p.waitErr = cmd.Wait()
		close(done)
	}()
	return nil
}

func (p *appCommandProc) wait() error {
	if p.done == nil {
		return nil
	}
	<-p.done
	return p.waitErr
}

func (a *appManaged) stop() {
	a.mu.Lock()
	procs := append([]*appCommandProc(nil), a.procs...)
	a.mu.Unlock()
	for _, proc := range procs {
		proc.stop()
	}
}

func (p *appCommandProc) stop() {
	p.parent.mu.Lock()
	cmd := p.cmd
	done := p.done
	p.parent.mu.Unlock()
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

func (a *appManaged) probeFrontendReadiness(ctx context.Context, logger *slog.Logger) {
	if strings.TrimSpace(a.target.BasePath) == "" {
		return
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(a.port))
	a.probeFrontendReadinessWith(ctx, logger, func() (net.Conn, error) {
		return net.DialTimeout("tcp", addr, time.Second)
	})
}

func (a *appManaged) probeFrontendReadinessWith(ctx context.Context, logger *slog.Logger, probe func() (net.Conn, error)) {
	a.probeFrontendReadinessUntil(ctx, logger, time.Now().Add(maxReadyTimeout(a.target.Commands)), probe)
}

func (a *appManaged) probeFrontendReadinessUntil(ctx context.Context, logger *slog.Logger, deadline time.Time, probe func() (net.Conn, error)) {
	warned := false
	loggedReady := false
	readyBeforeDeadline := false
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		conn, err := probe()
		if err == nil {
			if conn != nil {
				_ = conn.Close()
			}
			now := time.Now()
			a.ready.Store(true)
			if now.Before(deadline) {
				readyBeforeDeadline = true
			}
			a.frontendReadyOnce.Do(func() { close(a.frontendReady) })
			if logger != nil && !loggedReady {
				logger.Debug("dev app frontend ready", "app", a.target.Name, "port", a.port)
				loggedReady = true
			}
		} else {
			a.ready.Store(false)
			loggedReady = false
		}
		if !warned && time.Now().After(deadline) {
			warned = true
			if !readyBeforeDeadline && logger != nil {
				args := []any{"app", a.target.Name}
				for i, proc := range a.commandProcs() {
					if output := proc.currentOutput(); output != nil {
						if tail := output.tail(); tail != "" {
							args = append(args, fmt.Sprintf("command_%d_output_tail", i), "\n"+tail)
						}
					}
				}
				logger.Warn("dev app frontend readiness timeout; continuing probe", args...)
			}
		}
		timer := time.NewTimer(devReadinessProbeTick)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (a *appManaged) commandProcs() []*appCommandProc {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]*appCommandProc(nil), a.procs...)
}

func (p *appCommandProc) currentOutput() *childOutput {
	p.parent.mu.Lock()
	defer p.parent.mu.Unlock()
	return p.output
}

func maxReadyTimeout(commands []AppCommand) time.Duration {
	timeout := defaultReadyTimeout
	for _, command := range commands {
		if command.ReadyTimeout > timeout {
			timeout = command.ReadyTimeout
		}
	}
	return timeout
}

func (a *appManaged) frontendHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(a.target.BasePath) == "" {
			http.NotFound(w, r)
			return
		}
		if !a.ready.Load() {
			serveNotReady(w)
			return
		}
		proxy, err := a.reverseProxy()
		if err != nil {
			http.Error(w, "dev server unavailable", http.StatusBadGateway)
			return
		}
		ensureBasePathPrefix(r, a.target.BasePath)
		proxy.ServeHTTP(w, r)
	})
}

// ensureBasePathPrefix roots the request path at basePath, the prefix the dev
// server is configured to serve under. Already-prefixed paths are unchanged.
func ensureBasePathPrefix(r *http.Request, basePath string) {
	base := "/" + strings.Trim(strings.TrimSpace(basePath), "/")
	if base == "/" {
		return
	}
	p := r.URL.Path
	if p == base || strings.HasPrefix(p, base+"/") {
		return
	}
	rest := strings.TrimPrefix(p, "/")
	if rest == "" {
		r.URL.Path = base + "/"
		return
	}
	r.URL.Path = base + "/" + rest
}

func (a *appManaged) reverseProxy() (*httputil.ReverseProxy, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.proxy != nil && a.proxyPort == a.port {
		return a.proxy, nil
	}
	upstream, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", a.port))
	if err != nil {
		return nil, err
	}
	a.proxy = newReverseProxy(upstream)
	a.proxyPort = a.port
	return a.proxy, nil
}

func devAppCommandEnv(port int, target AppTarget, manifestEnv map[string]string) []string {
	env := append([]string(nil), os.Environ()...)
	for key, value := range manifestEnv {
		env = append(env, key+"="+value)
	}
	for key, value := range target.BaseEnv {
		env = append(env, key+"="+value)
	}
	env = append(env,
		"GESTALT_DEV=1",
		"GESTALT_DEV_PORT="+strconv.Itoa(port),
		"GESTALT_DEV_BASE_PATH="+target.BasePath,
	)
	if socket := strings.TrimSpace(target.SocketPath); socket != "" {
		env = append(env, "GESTALT_PROVIDER_SOCKET="+socket)
	}
	return env
}

// ClassifyFrontendOnlyDevApp returns ErrFrontendOnlyDevApp when the socket dial
// timed out but the frontend port became ready.
func ClassifyFrontendOnlyDevApp(ctx context.Context, dialErr error, handle *AppHandle) error {
	if dialErr == nil {
		return nil
	}
	if !errors.Is(dialErr, runtimehost.ErrProviderSocketNotServed) {
		return dialErr
	}
	if handle == nil {
		return dialErr
	}
	select {
	case <-handle.AllExited():
		return fmt.Errorf("dev app %q exited before classification", handle.name)
	case <-handle.FrontendReady():
		timer := time.NewTimer(frontendOnlyGrace)
		defer timer.Stop()
		select {
		case <-handle.AllExited():
			return fmt.Errorf("dev app %q exited during frontend-only grace", handle.name)
		case <-timer.C:
			return ErrFrontendOnlyDevApp
		case <-ctx.Done():
			return ctx.Err()
		}
	case <-ctx.Done():
		return dialErr
	}
}
