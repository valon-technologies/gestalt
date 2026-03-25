package pluginapi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/invocation"
	pluginapiv1 "github.com/valon-technologies/gestalt/sdk/pluginapi/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	processStartupTimeout  = 10 * time.Second
	processShutdownTimeout = 2 * time.Second
)

type ExecConfig struct {
	Command string
	Args    []string
	Env     map[string]string
	Name    string
	Config  map[string]any
	Mode    string
}

type managedRuntime struct {
	core.Runtime
	proc     *pluginProcess
	stopOnce sync.Once
	stopErr  error
}

func (r *managedRuntime) Stop(ctx context.Context) error {
	r.stopOnce.Do(func() {
		var errs []error
		if r.Runtime != nil {
			if err := r.Runtime.Stop(ctx); err != nil {
				errs = append(errs, err)
			}
		}
		if r.proc != nil {
			if err := r.proc.Close(); err != nil {
				errs = append(errs, err)
			}
		}
		r.stopErr = errors.Join(errs...)
	})
	return r.stopErr
}

type pluginProcess struct {
	cmd       *exec.Cmd
	dir       string
	conn      *grpc.ClientConn
	waitCh    chan error
	hostSrv   *grpc.Server
	hostLis   net.Listener
	closeOnce sync.Once
	closeErr  error
}

func NewExecutableProvider(ctx context.Context, cfg ExecConfig) (core.Provider, error) {
	proc, err := startPluginProcess(ctx, cfg, nil)
	if err != nil {
		return nil, err
	}

	client := pluginapiv1.NewProviderPluginClient(proc.conn)
	prov, err := NewRemoteProvider(ctx, client, cfg.Name, cfg.Config, cfg.Mode, WithCloser(proc))
	if err != nil {
		_ = proc.Close()
		return nil, err
	}
	return prov, nil
}

func NewExecutableRuntime(
	ctx context.Context,
	name string,
	cfg ExecConfig,
	config map[string]any,
	invoker invocation.Invoker,
	lister invocation.CapabilityLister,
) (core.Runtime, error) {
	proc, err := startPluginProcess(ctx, cfg, func(srv *grpc.Server) {
		pluginapiv1.RegisterRuntimeHostServer(srv, NewRuntimeHostServer(invoker, lister))
	})
	if err != nil {
		return nil, err
	}

	var initialCaps []core.Capability
	if lister != nil {
		initialCaps = lister.ListCapabilities()
	}

	rt, err := NewRemoteRuntime(name, pluginapiv1.NewRuntimePluginClient(proc.conn), config, initialCaps)
	if err != nil {
		_ = proc.Close()
		return nil, err
	}
	return &managedRuntime{Runtime: rt, proc: proc}, nil
}

func ServeProvider(ctx context.Context, provider core.Provider) error {
	return servePlugin(ctx, func(srv *grpc.Server) {
		pluginapiv1.RegisterProviderPluginServer(srv, NewProviderServer(provider))
	})
}

func ServeRuntime(ctx context.Context, server pluginapiv1.RuntimePluginServer) error {
	return servePlugin(ctx, func(srv *grpc.Server) {
		pluginapiv1.RegisterRuntimePluginServer(srv, server)
	})
}

func DialRuntimeHost(ctx context.Context) (*grpc.ClientConn, pluginapiv1.RuntimeHostClient, error) {
	socket := os.Getenv(pluginapiv1.EnvRuntimeHostSocket)
	if socket == "" {
		return nil, nil, fmt.Errorf("%s is required", pluginapiv1.EnvRuntimeHostSocket)
	}
	conn, err := dialUnixSocket(ctx, socket)
	if err != nil {
		return nil, nil, err
	}
	return conn, pluginapiv1.NewRuntimeHostClient(conn), nil
}

func servePlugin(ctx context.Context, register func(*grpc.Server)) error {
	socket := os.Getenv(pluginapiv1.EnvPluginSocket)
	if socket == "" {
		return fmt.Errorf("%s is required", pluginapiv1.EnvPluginSocket)
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

func startPluginProcess(ctx context.Context, cfg ExecConfig, registerHost func(*grpc.Server)) (*pluginProcess, error) {
	if cfg.Command == "" {
		return nil, fmt.Errorf("plugin command is required")
	}

	dir, err := newSocketDir()
	if err != nil {
		return nil, err
	}
	pluginSocket := filepath.Join(dir, "plugin.sock")
	env := mergeExecEnv(cfg.Env, map[string]string{
		pluginapiv1.EnvPluginSocket: pluginSocket,
	})

	proc := &pluginProcess{dir: dir}
	if registerHost != nil {
		hostSocket := filepath.Join(dir, "host.sock")
		lis, err := net.Listen("unix", hostSocket)
		if err != nil {
			_ = os.RemoveAll(dir)
			return nil, fmt.Errorf("listen on runtime host socket: %w", err)
		}
		srv := grpc.NewServer()
		registerHost(srv)
		proc.hostLis = lis
		proc.hostSrv = srv
		go func() {
			_ = srv.Serve(lis)
		}()
		env[pluginapiv1.EnvRuntimeHostSocket] = hostSocket
	}

	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Env = append(os.Environ(), envSlice(env)...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		_ = proc.Close()
		return nil, fmt.Errorf("start plugin process: %w", err)
	}
	proc.cmd = cmd
	proc.waitCh = make(chan error, 1)
	go func() {
		proc.waitCh <- cmd.Wait()
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
			_ = p.cmd.Process.Signal(syscall.SIGTERM)
			select {
			case err := <-p.waitCh:
				if err != nil && !errors.Is(err, context.Canceled) {
					var exitErr *exec.ExitError
					if !errors.As(err, &exitErr) || (exitErr.ExitCode() != 0 && exitErr.ExitCode() != -1) {
						errs = append(errs, fmt.Errorf("wait for plugin process: %w", err))
					}
				}
			case <-time.After(processShutdownTimeout):
				if err := p.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
					errs = append(errs, fmt.Errorf("kill plugin process: %w", err))
				}
				if err := <-p.waitCh; err != nil && !errors.Is(err, context.Canceled) {
					var exitErr *exec.ExitError
					if !errors.As(err, &exitErr) || exitErr.ExitCode() != -1 {
						errs = append(errs, fmt.Errorf("wait for killed plugin process: %w", err))
					}
				}
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
		"passthrough:///"+socket,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", addr)
		}),
	)
	if err != nil {
		return nil, err
	}
	conn.Connect()
	return conn, nil
}
