package providerhost

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/egress"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"github.com/valon-technologies/gestalt/server/internal/sandbox"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	processStartupTimeout  = 45 * time.Second
	processShutdownTimeout = 2 * time.Second
	processStartRetryCount = 5
	processStartRetryDelay = 100 * time.Millisecond
)

type ProcessConfig struct {
	Command       string
	Args          []string
	Env           map[string]string
	AllowedHosts  []string
	DefaultAction egress.PolicyAction
	HostBinary    string
	Cleanup       func()
	HostServices  []HostService
	SocketDir     string
	ProviderName  string
	Telemetry     metricutil.TelemetryProviders
	Stdout        io.Writer
	Stderr        io.Writer
}

type ExecConfig struct {
	Command          string
	Args             []string
	Env              map[string]string
	StaticSpec       StaticProviderSpec
	Config           map[string]any
	AllowedHosts     []string
	DefaultAction    egress.PolicyAction
	HostBinary       string
	Cleanup          func()
	HostServices     []HostService
	InvocationTokens *InvocationTokenManager
	InvocationGrants invocationGrants
	ProviderName     string
	Telemetry        metricutil.TelemetryProviders
}

type HostService struct {
	Register func(*grpc.Server)
	EnvVar   string
	Name     string
}

type providerProcess struct {
	cmd            *exec.Cmd
	dir            string
	sandboxTmp     string
	conn           *grpc.ClientConn
	waitCh         chan error
	hostSrvs       []*grpc.Server
	hostLiss       []net.Listener
	proxy          *sandbox.ProxyServer
	sandboxCleanup func()
	cleanup        func()
	closeOnce      sync.Once
	closeErr       error
}

type PluginProcess struct {
	proc *providerProcess
}

func NewExecutableProvider(ctx context.Context, cfg ExecConfig) (core.Provider, error) {
	proc, err := startProviderProcess(ctx, cfg.processConfig())
	if err != nil {
		return nil, err
	}

	conn := &PluginProcess{proc: proc}
	opts := []RemoteProviderOption{WithCloser(conn)}
	if cfg.InvocationTokens != nil {
		opts = append(opts,
			WithInvocationTokens(cfg.InvocationTokens),
			WithInvocationTokenSubject(cfg.StaticSpec.Name, cfg.InvocationGrants),
		)
	}
	prov, err := NewRemoteProvider(
		ctx,
		conn.Integration(),
		cfg.StaticSpec,
		cfg.Config,
		opts...,
	)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return prov, nil
}

func (c ExecConfig) processConfig() ProcessConfig {
	return ProcessConfig{
		Command:       c.Command,
		Args:          c.Args,
		Env:           c.Env,
		AllowedHosts:  c.AllowedHosts,
		DefaultAction: c.DefaultAction,
		HostBinary:    c.HostBinary,
		Cleanup:       c.Cleanup,
		HostServices:  c.HostServices,
		ProviderName:  firstNonBlank(c.ProviderName, c.StaticSpec.Name),
		Telemetry:     c.Telemetry,
	}
}

func StartPluginProcess(ctx context.Context, cfg ProcessConfig) (*PluginProcess, error) {
	proc, err := startProviderProcess(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &PluginProcess{proc: proc}, nil
}

func (p *PluginProcess) Lifecycle() proto.ProviderLifecycleClient {
	if p == nil || p.proc == nil {
		return nil
	}
	return proto.NewProviderLifecycleClient(p.proc.conn)
}

func (p *PluginProcess) Integration() proto.IntegrationProviderClient {
	if p == nil || p.proc == nil {
		return nil
	}
	return proto.NewIntegrationProviderClient(p.proc.conn)
}

func (p *PluginProcess) Conn() *grpc.ClientConn {
	if p == nil || p.proc == nil {
		return nil
	}
	return p.proc.conn
}

func (p *PluginProcess) Close() error {
	if p == nil || p.proc == nil {
		return nil
	}
	return p.proc.Close()
}

func ServeProvider(ctx context.Context, provider core.Provider) error {
	return serveProvider(ctx, func(srv *grpc.Server) {
		proto.RegisterIntegrationProviderServer(srv, NewProviderServer(provider))
	})
}

func serveProvider(ctx context.Context, register func(*grpc.Server)) error {
	socket := os.Getenv(proto.EnvProviderSocket)
	if socket == "" {
		return fmt.Errorf("%s is required", proto.EnvProviderSocket)
	}
	if err := os.Remove(socket); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale socket %q: %w", socket, err)
	}

	lis, err := net.Listen("unix", socket)
	if err != nil {
		return fmt.Errorf("listen on plugin socket %q: %w", socket, err)
	}
	defer func() {
		_ = lis.Close()
		_ = os.Remove(socket)
	}()

	providerName := strings.TrimSpace(os.Getenv(proto.EnvProviderName))
	srv := grpc.NewServer(grpc.StatsHandler(otelgrpc.NewServerHandler(providerServerGRPCOptions(providerName, nil)...)))
	register(srv)

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-ctx.Done()
		srv.GracefulStop()
	}()

	err = srv.Serve(lis)
	if ctx.Err() != nil {
		<-stopped
		return nil
	}
	return err
}

func startProviderProcess(ctx context.Context, cfg ProcessConfig) (*providerProcess, error) {
	if cfg.Command == "" {
		return nil, fmt.Errorf("plugin command is required")
	}

	sandboxActive := len(cfg.AllowedHosts) > 0 || cfg.DefaultAction == egress.PolicyDeny

	dir := strings.TrimSpace(cfg.SocketDir)
	if dir == "" {
		var err error
		dir, err = newSocketDir()
		if err != nil {
			return nil, err
		}
	} else if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create socket dir: %w", err)
	}
	pluginSocket := filepath.Join(dir, "plugin.sock")
	execEnv := map[string]string{
		proto.EnvProviderSocket:    pluginSocket,
		proto.EnvProviderParentPID: strconv.Itoa(os.Getpid()),
	}
	if providerName := strings.TrimSpace(cfg.ProviderName); providerName != "" {
		execEnv[proto.EnvProviderName] = providerName
	}
	env := mergeExecEnv(cfg.Env, execEnv)
	stdout := cfg.Stdout
	if stdout == nil {
		stdout = os.Stderr
	}
	stderr := cfg.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	proc := &providerProcess{dir: dir}
	proc.cleanup = cfg.Cleanup
	for i, hostService := range cfg.HostServices {
		if hostService.Register == nil || hostService.EnvVar == "" {
			continue
		}
		hostSocket := filepath.Join(dir, fmt.Sprintf("host-%d.sock", i))
		lis, err := net.Listen("unix", hostSocket)
		if err != nil {
			cleanupStartupHostServices(proc)
			if cleanupErr := os.Remove(hostSocket); cleanupErr != nil && !os.IsNotExist(cleanupErr) {
				return nil, errors.Join(
					fmt.Errorf("listen on host socket: %w", err),
					fmt.Errorf("cleanup failed host socket %q: %w", hostSocket, cleanupErr),
				)
			}
			if cfg.SocketDir == "" {
				_ = os.RemoveAll(dir)
			}
			return nil, fmt.Errorf("listen on host socket: %w", err)
		}
		srv := grpc.NewServer(grpc.StatsHandler(otelgrpc.NewServerHandler(hostServiceServerGRPCOptions(cfg.ProviderName, hostService, cfg.Telemetry)...)))
		hostService.Register(srv)
		proc.hostLiss = append(proc.hostLiss, lis)
		proc.hostSrvs = append(proc.hostSrvs, srv)
		go func() {
			_ = srv.Serve(lis)
		}()
		env[hostService.EnvVar] = hostSocket
	}

	if sandboxActive {
		sandboxTmp, err := newPluginTempDir("gstp-sandbox-tmp-")
		if err != nil {
			_ = proc.Close()
			return nil, fmt.Errorf("create sandbox tmpdir: %w", err)
		}
		proc.sandboxTmp = sandboxTmp
		env["TMPDIR"] = sandboxTmp

		policy := &sandbox.Policy{
			ReadOnlyPaths:  append(sandbox.DefaultReadOnlyPaths(), filepath.Dir(cfg.Command)),
			ReadWritePaths: []string{dir, sandboxTmp},
			AllowedHosts:   cfg.AllowedHosts,
			HostBinary:     cfg.HostBinary,
		}

		defaultAction := cfg.DefaultAction
		checkHost := func(host string) error {
			return egress.CheckHost(policy.AllowedHosts, host, defaultAction)
		}
		proxy := sandbox.NewProxyServer(checkHost)
		port, err := proxy.Start()
		if err != nil {
			_ = proc.Close()
			return nil, fmt.Errorf("start sandbox proxy: %w", err)
		}
		proc.proxy = proxy
		policy.ProxyPort = port
		proxyAddr := fmt.Sprintf("http://127.0.0.1:%d", port)
		env["HTTP_PROXY"] = proxyAddr
		env["HTTPS_PROXY"] = proxyAddr

		cmd, cleanup, err := startCommandWithRetry(ctx, func() (*exec.Cmd, func(), error) {
			cmd := exec.Command(cfg.Command, cfg.Args...)
			cmd.Env = buildPluginEnv(env, sandboxActive)
			cmd.Stdout = stdout
			cmd.Stderr = stderr
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

			wrapped, cleanup, err := sandbox.Wrap(policy, cmd)
			if err != nil {
				return nil, nil, fmt.Errorf("sandbox wrap: %w", err)
			}
			return wrapped, cleanup, nil
		})
		if err != nil {
			_ = proc.Close()
			return nil, fmt.Errorf("start plugin process: %w", err)
		}
		proc.sandboxCleanup = cleanup
		proc.cmd = cmd
	} else {
		cmd, _, err := startCommandWithRetry(ctx, func() (*exec.Cmd, func(), error) {
			cmd := exec.Command(cfg.Command, cfg.Args...)
			cmd.Env = append(safeBaseEnv(), envSlice(env)...)
			cmd.Stdout = stdout
			cmd.Stderr = stderr
			return cmd, nil, nil
		})
		if err != nil {
			_ = proc.Close()
			return nil, fmt.Errorf("start plugin process: %w", err)
		}
		proc.cmd = cmd
	}

	proc.waitCh = make(chan error, 1)
	go func() {
		proc.waitCh <- proc.cmd.Wait()
		close(proc.waitCh)
	}()

	startCtx, cancel := context.WithTimeout(ctx, processStartupTimeout)
	defer cancel()

	conn, err := waitForPluginConn(startCtx, pluginSocket, proc.waitCh, cfg)
	if err != nil {
		_ = proc.Close()
		return nil, err
	}
	proc.conn = conn

	return proc, nil
}

func buildPluginEnv(env map[string]string, sandboxActive bool) []string {
	base := safeBaseEnv()
	if sandboxActive {
		var filtered []string
		for _, entry := range base {
			key := entry[:strings.IndexByte(entry, '=')]
			if _, overridden := env[key]; !overridden {
				filtered = append(filtered, entry)
			}
		}
		base = filtered
	}
	return append(base, envSlice(env)...)
}

func (p *providerProcess) Close() error {
	if p == nil {
		return nil
	}

	p.closeOnce.Do(func() {
		var errs []error
		if p.conn != nil {
			if err := p.conn.Close(); err != nil {
				errs = append(errs, fmt.Errorf("close plugin connection: %w", err))
			}
		}
		for _, hostSrv := range p.hostSrvs {
			stopGRPCServer(hostSrv, hostServiceShutdownTimeout)
		}
		for _, hostLis := range p.hostLiss {
			if err := hostLis.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				errs = append(errs, fmt.Errorf("close runtime host listener: %w", err))
			}
		}
		if p.cmd != nil && p.cmd.Process != nil {
			if p.proxy != nil {
				_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)
			} else {
				_ = p.cmd.Process.Signal(syscall.SIGTERM)
			}
			select {
			case err := <-p.waitCh:
				if err != nil && !errors.Is(err, context.Canceled) {
					var exitErr *exec.ExitError
					if !errors.As(err, &exitErr) || (exitErr.ExitCode() != 0 && exitErr.ExitCode() != -1) {
						errs = append(errs, fmt.Errorf("wait for plugin process: %w", err))
					}
				}
			case <-time.After(processShutdownTimeout):
				if p.proxy != nil {
					_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
				} else {
					if err := p.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
						errs = append(errs, fmt.Errorf("kill plugin process: %w", err))
					}
				}
				if err := <-p.waitCh; err != nil && !errors.Is(err, context.Canceled) {
					var exitErr *exec.ExitError
					if !errors.As(err, &exitErr) || exitErr.ExitCode() != -1 {
						errs = append(errs, fmt.Errorf("wait for killed plugin process: %w", err))
					}
				}
			}
		}
		if p.proxy != nil {
			if err := p.proxy.Close(); err != nil {
				errs = append(errs, fmt.Errorf("close sandbox proxy: %w", err))
			}
		}
		if p.sandboxCleanup != nil {
			p.sandboxCleanup()
		}
		if p.cleanup != nil {
			p.cleanup()
		}
		if p.sandboxTmp != "" {
			if err := os.RemoveAll(p.sandboxTmp); err != nil {
				errs = append(errs, fmt.Errorf("remove sandbox tmpdir: %w", err))
			}
		}
		if p.dir != "" {
			if err := os.RemoveAll(p.dir); err != nil {
				errs = append(errs, fmt.Errorf("remove plugin temp dir: %w", err))
			}
		}
		p.closeErr = errors.Join(errs...)
	})

	return p.closeErr
}

func cleanupStartupHostServices(proc *providerProcess) {
	if proc == nil {
		return
	}
	for _, hostSrv := range proc.hostSrvs {
		stopGRPCServer(hostSrv, hostServiceShutdownTimeout)
	}
	for _, hostLis := range proc.hostLiss {
		if hostLis == nil {
			continue
		}
		_ = hostLis.Close()
		socketPath := strings.TrimSpace(hostLis.Addr().String())
		if socketPath == "" {
			continue
		}
		_ = os.Remove(socketPath)
	}
}

func startCommandWithRetry(
	ctx context.Context,
	build func() (*exec.Cmd, func(), error),
) (*exec.Cmd, func(), error) {
	var lastErr error
	for attempt := 0; attempt < processStartRetryCount; attempt++ {
		cmd, cleanup, err := build()
		if err != nil {
			return nil, nil, err
		}
		if err := cmd.Start(); err == nil {
			return cmd, cleanup, nil
		} else {
			if cleanup != nil {
				cleanup()
			}
			if !errors.Is(err, syscall.ETXTBSY) {
				return nil, nil, err
			}
			lastErr = err
		}

		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * processStartRetryDelay):
		}
	}
	return nil, nil, lastErr
}

func NewPluginTempDir(pattern string) (string, error) {
	return newPluginTempDir(pattern)
}

func newSocketDir() (string, error) {
	return NewPluginTempDir("gstp-")
}

func newPluginTempDir(pattern string) (string, error) {
	base, err := resolvePluginTempBaseDir([]string{"/tmp", os.TempDir(), "/var/tmp", "/dev/shm", "."})
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp(base, pattern)
	if err != nil {
		return "", fmt.Errorf("create plugin temp dir: %w", err)
	}
	return dir, nil
}

func resolvePluginTempBaseDir(candidates []string) (string, error) {
	seen := make(map[string]struct{}, len(candidates))
	var errs []error
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if !filepath.IsAbs(candidate) {
			abs, err := filepath.Abs(candidate)
			if err == nil {
				candidate = abs
			}
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}

		info, err := os.Stat(candidate)
		switch {
		case err == nil && info.IsDir():
			return candidate, nil
		case err == nil:
			errs = append(errs, fmt.Errorf("%s exists but is not a directory", candidate))
		case os.IsNotExist(err):
			if mkErr := os.MkdirAll(candidate, 0o755); mkErr == nil {
				return candidate, nil
			} else {
				errs = append(errs, fmt.Errorf("mkdir %s: %w", candidate, mkErr))
			}
		default:
			errs = append(errs, fmt.Errorf("stat %s: %w", candidate, err))
		}
	}
	if len(errs) == 0 {
		return "", fmt.Errorf("resolve plugin temp dir base: no directory candidates")
	}
	return "", fmt.Errorf("resolve plugin temp dir base: %w", errors.Join(errs...))
}

func mergeExecEnv(base, extra map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func envSlice(values map[string]string) []string {
	out := make([]string, 0, len(values))
	for k, v := range values {
		out = append(out, k+"="+v)
	}
	return out
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

var safeEnvKeys = []string{
	"PATH", "HOME", "TMPDIR", "LANG", "TZ",
	"SSL_CERT_FILE", "SSL_CERT_DIR",
}

func safeBaseEnv() []string {
	var out []string
	for _, key := range safeEnvKeys {
		if val, ok := os.LookupEnv(key); ok {
			out = append(out, key+"="+val)
		}
	}
	return out
}

func waitForPluginConn(ctx context.Context, socket string, waitCh <-chan error, cfg ProcessConfig) (*grpc.ClientConn, error) {
	for {
		if _, err := os.Stat(socket); err == nil {
			conn, dialErr := dialReadyUnixSocket(ctx, socket, waitCh, cfg)
			if dialErr == nil {
				return conn, nil
			}
			var pathErr *os.PathError
			if !errors.As(dialErr, &pathErr) {
				return nil, fmt.Errorf("dial plugin socket: %w", dialErr)
			}
		}

		select {
		case err, ok := <-waitCh:
			if !ok || err == nil {
				return nil, fmt.Errorf("plugin process exited before serving gRPC")
			}
			return nil, fmt.Errorf("plugin process exited before serving gRPC: %w", err)
		case <-ctx.Done():
			return nil, fmt.Errorf("waiting for plugin socket: %w", ctx.Err())
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func dialReadyUnixSocket(ctx context.Context, socket string, waitCh <-chan error, cfg ProcessConfig) (*grpc.ClientConn, error) {
	conn, err := dialUnixSocket(ctx, socket, cfg)
	if err != nil {
		return nil, err
	}
	if err := waitForGRPCReady(ctx, conn, waitCh); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func dialUnixSocket(ctx context.Context, socket string, cfg ProcessConfig) (*grpc.ClientConn, error) {
	conn, err := grpc.NewClient(
		"passthrough:///localhost",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		// grpc-go uses the dial target as the HTTP/2 authority by default. Passing
		// the raw Unix socket path here works for Go plugins, but tonic rejects that
		// authority and resets the stream with PROTOCOL_ERROR before any RPC handler
		// runs. Dial the Unix socket explicitly and present a stable authority value.
		grpc.WithAuthority("localhost"),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		}),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler(providerClientGRPCOptions(cfg.ProviderName, cfg.Telemetry)...)),
	)
	if err != nil {
		return nil, err
	}
	conn.Connect()
	return conn, nil
}

func waitForGRPCReady(ctx context.Context, conn *grpc.ClientConn, waitCh <-chan error) error {
	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			return nil
		}
		if state == connectivity.Shutdown {
			return fmt.Errorf("plugin gRPC connection shut down before ready")
		}

		select {
		case err, ok := <-waitCh:
			if !ok || err == nil {
				return fmt.Errorf("plugin process exited before serving gRPC")
			}
			return fmt.Errorf("plugin process exited before serving gRPC: %w", err)
		default:
		}

		waitCtx, cancel := context.WithTimeout(ctx, 25*time.Millisecond)
		changed := conn.WaitForStateChange(waitCtx, state)
		cancel()
		if !changed && ctx.Err() != nil {
			return fmt.Errorf("waiting for plugin gRPC ready: %w", ctx.Err())
		}
	}
}
