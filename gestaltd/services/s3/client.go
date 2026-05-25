package s3

import (
	"context"
	"io"

	s3sdk "github.com/valon-technologies/gestalt/sdk/go/s3"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	rpcs3 "github.com/valon-technologies/gestalt/server/rpc/s3"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ExecConfig configures a provider-backed S3 executable.
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

type remoteS3 struct {
	s3sdk.S3
	runtime proto.ProviderLifecycleClient
	closer  io.Closer
}

func NewExecutable(ctx context.Context, cfg ExecConfig) (s3sdk.S3, error) {
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
	_, err = runtimehost.ConfigureRuntimeProvider(ctx, runtimeClient, proto.ProviderKind_PROVIDER_KIND_S3, cfg.Name, cfg.Config)
	if err != nil {
		_ = proc.Close()
		return nil, err
	}

	client := rpcs3.NewConn(proc.Conn(), rpcs3.Options{
		UnaryTimeout: runtimehost.ProviderRPCTimeout,
	})
	return &remoteS3{
		S3:      client,
		runtime: runtimeClient,
		closer:  proc,
	}, nil
}

func (r *remoteS3) Ping(ctx context.Context) error {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	_, err := r.runtime.HealthCheck(ctx, &emptypb.Empty{})
	return err
}

func (r *remoteS3) Close() error {
	if r == nil || r.closer == nil {
		return nil
	}
	return r.closer.Close()
}
