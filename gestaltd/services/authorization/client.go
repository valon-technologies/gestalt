package authorization

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/valon-technologies/gestalt/server/core"
	rpcauthorization "github.com/valon-technologies/gestalt/server/rpc/authorization"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ExecConfig struct {
	Command                string
	Args                   []string
	Workdir                string
	Env                    map[string]string
	Config                 map[string]any
	Egress                 egress.Policy
	HostBinary             string
	Cleanup                func()
	HostServices           []runtimehost.HostService
	Name                   string
	HostServiceGRPCOptions []grpc.ServerOption
}

type remoteAuthorizationProvider struct {
	*rpcauthorization.Client
	runtime proto.ProviderLifecycleClient
	closer  io.Closer
}

type Executable struct {
	conn    grpc.ClientConnInterface
	runtime proto.ProviderLifecycleClient
	closer  io.Closer

	once sync.Once
	err  error
}

func StartExecutable(ctx context.Context, cfg ExecConfig) (*Executable, error) {
	proc, err := runtimehost.StartAppProcess(ctx, runtimehost.ProcessConfig{
		Command:           cfg.Command,
		Args:              cfg.Args,
		Workdir:           cfg.Workdir,
		Env:               cfg.Env,
		Egress:            cfg.Egress,
		HostBinary:        cfg.HostBinary,
		Cleanup:           cfg.Cleanup,
		HostServices:      cfg.HostServices,
		ProviderName:      cfg.Name,
		GRPCServerOptions: cfg.HostServiceGRPCOptions,
	})
	if err != nil {
		return nil, err
	}

	runtimeClient := proc.Lifecycle()
	_, err = runtimehost.ConfigureRuntimeProvider(ctx, runtimeClient, proto.ProviderKind_PROVIDER_KIND_AUTHORIZATION, cfg.Name, cfg.Config)
	if err != nil {
		_ = proc.Close()
		return nil, err
	}

	return &Executable{
		conn:    proc.Conn(),
		runtime: runtimeClient,
		closer:  proc,
	}, nil
}

func (e *Executable) Conn() grpc.ClientConnInterface {
	if e == nil {
		return nil
	}
	return e.conn
}

func (e *Executable) Runtime() proto.ProviderLifecycleClient {
	if e == nil {
		return nil
	}
	return e.runtime
}

func (e *Executable) Close() error {
	if e == nil {
		return nil
	}
	e.once.Do(func() {
		if e.closer != nil {
			e.err = e.closer.Close()
		}
	})
	return e.err
}

func NewFromExecutable(exec *Executable, cfg ExecConfig) (core.AuthorizationProvider, error) {
	if exec == nil {
		return nil, fmt.Errorf("authorization executable is required")
	}
	client := rpcauthorization.NewConn(exec.Conn(), rpcauthorization.Options{
		UnaryTimeout: runtimehost.ProviderRPCTimeout,
		ProviderID:   cfg.Name,
	})
	return &remoteAuthorizationProvider{Client: client, runtime: exec.Runtime(), closer: exec}, nil
}

func NewExecutable(ctx context.Context, cfg ExecConfig) (core.AuthorizationProvider, error) {
	exec, err := StartExecutable(ctx, cfg)
	if err != nil {
		return nil, err
	}
	provider, err := NewFromExecutable(exec, cfg)
	if err != nil {
		_ = exec.Close()
		return nil, err
	}
	return provider, nil
}

// NewRemote returns an authorization provider backed by a public gRPC client.
func NewRemote(client proto.AuthorizationClient, name string) (core.AuthorizationProvider, error) {
	if client == nil {
		return nil, fmt.Errorf("authorization provider client is required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("authorization provider name is required")
	}
	return rpcauthorization.NewClient(client, rpcauthorization.Options{
		UnaryTimeout: runtimehost.ProviderRPCTimeout,
		ProviderID:   name,
	}), nil
}

func (r *remoteAuthorizationProvider) Ping(ctx context.Context) error {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	_, err := r.runtime.HealthCheck(ctx, &emptypb.Empty{})
	return err
}

func (r *remoteAuthorizationProvider) Close() error {
	if r == nil || r.closer == nil {
		return nil
	}
	return r.closer.Close()
}
