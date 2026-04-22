package providerhost

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	"google.golang.org/grpc"
)

type StartedHostService struct {
	EnvVar     string
	SocketPath string
}

type StartedHostServices struct {
	dir       string
	services  []StartedHostService
	hostSrvs  []*grpc.Server
	hostLiss  []net.Listener
	closeOnce sync.Once
	closeErr  error
}

func StartHostServices(services []HostService) (*StartedHostServices, error) {
	active := make([]HostService, 0, len(services))
	for _, service := range services {
		if service.Register == nil || service.EnvVar == "" {
			continue
		}
		active = append(active, service)
	}
	if len(active) == 0 {
		return nil, nil
	}

	dir, err := newSocketDir()
	if err != nil {
		return nil, err
	}
	started := &StartedHostServices{dir: dir}
	for i, service := range active {
		hostSocket := filepath.Join(dir, fmt.Sprintf("host-%d.sock", i))
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
		srv := grpc.NewServer()
		service.Register(srv)
		started.hostLiss = append(started.hostLiss, lis)
		started.hostSrvs = append(started.hostSrvs, srv)
		started.services = append(started.services, StartedHostService{
			EnvVar:     service.EnvVar,
			SocketPath: hostSocket,
		})
		go func() {
			_ = srv.Serve(lis)
		}()
	}
	return started, nil
}

func (s *StartedHostServices) Bindings() []StartedHostService {
	if s == nil {
		return nil
	}
	return append([]StartedHostService(nil), s.services...)
}

func (s *StartedHostServices) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		var errs []error
		for _, hostSrv := range s.hostSrvs {
			hostSrv.Stop()
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
