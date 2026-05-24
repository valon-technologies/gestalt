package s3

import (
	"context"
	"io"

	s3sdk "github.com/valon-technologies/gestalt/sdk/go/s3"
	s3host "github.com/valon-technologies/gestalt/sdk/go/s3/host"
	proto "github.com/valon-technologies/gestalt/sdk/go/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ExecConfig struct {
	Command    string
	Args       []string
	Workdir    string
	Env        map[string]string
	Config     map[string]any
	Egress     egress.Policy
	HostBinary string
	Cleanup    func()
	Name       string
}

type executableS3 struct {
	s3sdk.Client
	runtime proto.ProviderLifecycleClient
	closer  io.Closer
}

func NewExecutable(ctx context.Context, cfg ExecConfig) (s3sdk.Client, error) {
	proc, err := runtimehost.StartAppProcess(ctx, runtimehost.ProcessConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Workdir:      cfg.Workdir,
		Env:          cfg.Env,
		Egress:       cfg.Egress,
		HostBinary:   cfg.HostBinary,
		Cleanup:      cfg.Cleanup,
		ProviderName: cfg.Name,
	})
	if err != nil {
		return nil, err
	}

	runtimeClient := proc.Lifecycle()
	if _, err := runtimehost.ConfigureRuntimeProvider(ctx, runtimeClient, proto.ProviderKind_PROVIDER_KIND_S3, cfg.Name, cfg.Config); err != nil {
		_ = proc.Close()
		return nil, err
	}

	return &executableS3{
		Client:  s3host.NewProviderConn(proc.Conn(), runtimehost.ProviderRPCTimeout),
		runtime: runtimeClient,
		closer:  proc,
	}, nil
}

func (e *executableS3) Ping(ctx context.Context) error {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	_, err := e.runtime.HealthCheck(ctx, &emptypb.Empty{})
	return err
}

func (e *executableS3) Close() error {
	if e == nil || e.closer == nil {
		return nil
	}
	return e.closer.Close()
}
