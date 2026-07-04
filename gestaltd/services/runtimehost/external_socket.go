package runtimehost

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
)

var ErrProviderSocketNotServed = errors.New("provider socket not served")

type PreparedProviderSockets struct {
	PluginSocket string
	Env          map[string]string
	cleanup      func()
}

func (p *PreparedProviderSockets) Cleanup() {
	if p == nil || p.cleanup == nil {
		return
	}
	p.cleanup()
	p.cleanup = nil
}

func PrepareExternalProviderSockets(cfg ProcessConfig) (*PreparedProviderSockets, error) {
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
	pluginSocket := filepath.Join(dir, "app.sock")
	execEnv := map[string]string{
		proto.EnvProviderSocket:    pluginSocket,
		proto.EnvProviderParentPID: strconv.Itoa(os.Getpid()),
	}
	if providerName := strings.TrimSpace(cfg.ProviderName); providerName != "" {
		execEnv[proto.EnvProviderName] = providerName
	}

	prepared := &PreparedProviderSockets{
		PluginSocket: pluginSocket,
		Env:          maps.Clone(execEnv),
	}
	activeHostServices := activeHostServices(cfg.HostServices)
	if len(activeHostServices) == 0 {
		prepared.cleanup = func() {
			if cfg.SocketDir == "" {
				_ = os.RemoveAll(dir)
			}
		}
		return prepared, nil
	}

	hostSocket := filepath.Join(dir, "host.sock")
	lis, err := net.Listen("unix", hostSocket)
	if err != nil {
		if cfg.SocketDir == "" {
			_ = os.RemoveAll(dir)
		}
		return nil, fmt.Errorf("listen on host socket: %w", err)
	}
	srv := grpc.NewServer(grpc.StatsHandler(otelgrpc.NewServerHandler(hostServiceServerGRPCOptions(cfg.ProviderName, unifiedHostService(), cfg.Telemetry)...)))
	for _, hostService := range activeHostServices {
		hostService.Register(srv)
	}
	go func() {
		_ = srv.Serve(lis)
	}()
	prepared.Env[HostServiceSocketEnv] = hostSocket
	prepared.cleanup = func() {
		srv.Stop()
		_ = lis.Close()
		if cfg.SocketDir == "" {
			_ = os.RemoveAll(dir)
		}
	}
	return prepared, nil
}

func DialExternalProviderSocket(ctx context.Context, socketPath string, cfg ProcessConfig) (*grpc.ClientConn, error) {
	conn, err := waitForPluginConn(ctx, socketPath, nil, cfg)
	if err == nil {
		return conn, nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return nil, ErrProviderSocketNotServed
	}
	if strings.Contains(err.Error(), "waiting for app socket") {
		return nil, ErrProviderSocketNotServed
	}
	return nil, err
}
