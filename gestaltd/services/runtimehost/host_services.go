package runtimehost

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
)

type StartedHostService struct {
	Name       string
	EnvVar     string // always DefaultHostServiceSocketEnv; retained for callers expecting an env key
	SocketPath string
}

type StartedHostServices struct {
	dir          string
	socketPath   string
	serviceNames []string
	hostSrvs     []*grpc.Server
	hostLiss     []net.Listener
	closeOnce    sync.Once
	closeErr     error
}

type HostServicesOption func(*hostServicesConfig)

type hostServicesConfig struct {
	providerName string
	telemetry    metricutil.TelemetryProviders
}

func WithHostServicesProviderName(name string) HostServicesOption {
	return func(cfg *hostServicesConfig) {
		cfg.providerName = name
	}
}

func WithHostServicesTelemetry(telemetry metricutil.TelemetryProviders) HostServicesOption {
	return func(cfg *hostServicesConfig) {
		cfg.telemetry = telemetry
	}
}

func StartHostServices(services []HostService, opts ...HostServicesOption) (*StartedHostServices, error) {
	var cfg hostServicesConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	active := activeHostServices(services)
	if len(active) == 0 {
		return nil, nil
	}

	dir, err := newSocketDir()
	if err != nil {
		return nil, err
	}
	started := &StartedHostServices{dir: dir}
	hostSocket := filepath.Join(dir, "host.sock")
	lis, err := net.Listen("unix", hostSocket)
	if err != nil {
		_ = started.Close()
		if cleanupErr := os.Remove(hostSocket); cleanupErr != nil && !os.IsNotExist(cleanupErr) {
			return nil, errors.Join(
				fmt.Errorf("listen on host socket: %w", err),
				fmt.Errorf("cleanup failed host socket %q: %w", hostSocket, cleanupErr),
			)
		}
		return nil, fmt.Errorf("listen on host socket: %w", err)
	}
	srv := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler(hostServiceServerGRPCOptions(cfg.providerName, unifiedHostService(), cfg.telemetry)...)),
		grpc.UnaryInterceptor(telemetryUnaryServerInterceptor(cfg.telemetry)),
		grpc.StreamInterceptor(telemetryStreamServerInterceptor(cfg.telemetry)),
	)
	for _, service := range active {
		service.Register(srv)
		started.serviceNames = append(started.serviceNames, hostServiceMetricName(service))
	}
	started.socketPath = hostSocket
	started.hostLiss = append(started.hostLiss, lis)
	started.hostSrvs = append(started.hostSrvs, srv)
	go func() {
		_ = srv.Serve(lis)
	}()
	return started, nil
}

func activeHostServices(services []HostService) []HostService {
	active := make([]HostService, 0, len(services))
	for _, service := range services {
		if service.Register == nil {
			continue
		}
		active = append(active, service)
	}
	return active
}

func unifiedHostService() HostService {
	// Metrics label for the shared listener; per-service RPC metrics are not split out.
	return HostService{
		Name: "host_service",
	}
}

func (s *StartedHostServices) SocketBinding() StartedHostService {
	if s == nil || strings.TrimSpace(s.socketPath) == "" {
		return StartedHostService{}
	}
	return StartedHostService{
		EnvVar:     DefaultHostServiceSocketEnv,
		SocketPath: s.socketPath,
	}
}

func (s *StartedHostServices) RegisteredServiceNames() []string {
	if s == nil {
		return nil
	}
	return append([]string(nil), s.serviceNames...)
}

// Bindings returns the unified host-service socket binding as a single entry.
func (s *StartedHostServices) Bindings() []StartedHostService {
	socket := s.SocketBinding()
	if strings.TrimSpace(socket.SocketPath) == "" {
		return nil
	}
	return []StartedHostService{socket}
}

func (s *StartedHostServices) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		var errs []error
		for _, hostSrv := range s.hostSrvs {
			stopGRPCServer(hostSrv, hostServiceShutdownTimeout)
		}
		for _, hostLis := range s.hostLiss {
			if err := hostLis.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				errs = append(errs, fmt.Errorf("close runtime host listener: %w", err))
			}
			socketPath := hostLis.Addr().String()
			if socketPath != "" {
				if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
					errs = append(errs, fmt.Errorf("remove runtime host socket %q: %w", socketPath, err))
				}
			}
		}
		if s.dir != "" {
			if err := os.RemoveAll(s.dir); err != nil {
				errs = append(errs, fmt.Errorf("remove runtime host socket dir: %w", err))
			}
		}
		s.closeErr = errors.Join(errs...)
	})
	return s.closeErr
}
