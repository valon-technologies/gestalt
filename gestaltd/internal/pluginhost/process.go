package pluginhost

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/valon-technologies/gestalt/server/internal/sandbox"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	processStartupTimeout  = 10 * time.Second
	processShutdownTimeout = 2 * time.Second
)

type ExecConfig struct {
	Command      string
	Args         []string
	Env          map[string]string
	StaticSpec   StaticProviderSpec
	Config       map[string]any
	AllowedHosts []string
	HostBinary   string
	Cleanup      func()
}

type pluginProcess struct {
	cmd            *exec.Cmd
	dir            string
	sandboxTmp     string
	conn           *grpc.ClientConn
	waitCh         chan error
	hostSrv        *grpc.Server
	hostLis        net.Listener
	proxy          *sandbox.ProxyServer
	sandboxCleanup func()
	cleanup        func()
	closeOnce      sync.Once
	closeErr       error
}

func NewExecutableProvider(ctx context.Context, cfg ExecConfig) (core.Provider, error) {
	proc, err := startPluginProcess(ctx, cfg, nil, "")
	if err != nil {
		return nil, err
	}

	client := proto.NewPluginProviderClient(proc.conn)
	prov, err := NewRemoteProvider(
		ctx,
		client,
		cfg.StaticSpec,
		cfg.Config,
		WithCloser(proc),
	)
	if err != nil {
		_ = proc.Close()
		return nil, err
	}
	return prov, nil
}

func ServeProvider(ctx context.Context, provider core.Provider) error {
	return servePlugin(ctx, func(srv *grpc.Server) {
		proto.RegisterPluginProviderServer(srv, NewProviderServer(provider))
	})
}

func servePlugin(ctx context.Context, register func(*grpc.Server)) error {
	socket := os.Getenv(proto.EnvPluginSocket)
	if socket == "" {
		return fmt.Errorf("%s is required", proto.EnvPluginSocket)
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

	srv := grpc.NewServer()
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

func startPluginProcess(ctx context.Context, cfg ExecConfig, registerHost func(*grpc.Server), hostSocketEnv string) (*pluginProcess, error) {
	if cfg.Command == "" {
		return nil, fmt.Errorf("plugin command is required")
	}

	sandboxActive := len(cfg.AllowedHosts) > 0

	dir, err := newSocketDir()
	if err != nil {
		return nil, err
	}
	pluginSocket := filepath.Join(dir, "plugin.sock")
	env := mergeExecEnv(cfg.Env, map[string]string{
		proto.EnvPluginSocket:    pluginSocket,
		proto.EnvPluginParentPID: strconv.Itoa(os.Getpid()),
	})

	proc := &pluginProcess{dir: dir}
	proc.cleanup = cfg.Cleanup
	if registerHost != nil {
		hostSocket := filepath.Join(dir, "host.sock")
		lis, err := net.Listen("unix", hostSocket)
		if err != nil {
			_ = os.RemoveAll(dir)
			return nil, fmt.Errorf("listen on host socket: %w", err)
		}
		srv := grpc.NewServer()
		registerHost(srv)
		proc.hostLis = lis
		proc.hostSrv = srv
		go func() {
			_ = srv.Serve(lis)
		}()
		env[hostSocketEnv] = hostSocket
	}

	if sandboxActive {
		sandboxTmp, err := os.MkdirTemp("", "gstp-sandbox-tmp-")
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

		proxy := sandbox.NewProxyServer(policy.AllowedHosts)
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

		cmd := exec.Command(cfg.Command, cfg.Args...)
		cmd.Env = buildPluginEnv(env, sandboxActive)
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		wrapped, cleanup, err := sandbox.Wrap(policy, cmd)
		if err != nil {
			_ = proc.Close()
			return nil, fmt.Errorf("sandbox wrap: %w", err)
		}
		proc.sandboxCleanup = cleanup
		cmd = wrapped

		if err := cmd.Start(); err != nil {
			if proc.sandboxCleanup != nil {
				proc.sandboxCleanup()
			}
			_ = proc.Close()
			return nil, fmt.Errorf("start plugin process: %w", err)
		}
		proc.cmd = cmd
	} else {
		cmd := exec.Command(cfg.Command, cfg.Args...)
		cmd.Env = append(safeBaseEnv(), envSlice(env)...)
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr

		if err := cmd.Start(); err != nil {
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

	conn, err := waitForPluginConn(startCtx, pluginSocket, proc.waitCh)
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

func (p *pluginProcess) Close() error {
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
		if p.hostSrv != nil {
			p.hostSrv.Stop()
		}
		if p.hostLis != nil {
			if err := p.hostLis.Close(); err != nil {
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

func newSocketDir() (string, error) {
	base := "/tmp"
	if info, err := os.Stat(base); err != nil || !info.IsDir() {
		base = os.TempDir()
	}
	dir, err := os.MkdirTemp(base, "gstp-")
	if err != nil {
		return "", fmt.Errorf("create plugin temp dir: %w", err)
	}
	return dir, nil
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

func waitForPluginConn(ctx context.Context, socket string, waitCh <-chan error) (*grpc.ClientConn, error) {
	for {
		if _, err := os.Stat(socket); err == nil {
			conn, dialErr := dialUnixSocket(ctx, socket)
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

func dialUnixSocket(ctx context.Context, socket string) (*grpc.ClientConn, error) {
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
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		return nil, err
	}
	conn.Connect()
	return conn, nil
}
