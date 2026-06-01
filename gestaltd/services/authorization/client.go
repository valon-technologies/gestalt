package authorization

import (
	"context"
	"io"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ExecConfig struct {
	Command      string
	Args         []string
	Workdir      string
	Env          map[string]string
	Config       map[string]any
	Egress       egress.Policy
	HostBinary   string
	Cleanup      func()
	HostServices []runtimehost.HostService
	Name         string
}

type remoteAuthorizationProvider struct {
	client  proto.AuthorizationProviderClient
	runtime proto.ProviderLifecycleClient
	closer  io.Closer
	name    string
}

func NewExecutable(ctx context.Context, cfg ExecConfig) (core.AuthorizationProvider, error) {
	proc, err := runtimehost.StartAppProcess(ctx, runtimehost.ProcessConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Workdir:      cfg.Workdir,
		Env:          cfg.Env,
		Egress:       cfg.Egress,
		HostBinary:   cfg.HostBinary,
		Cleanup:      cfg.Cleanup,
		HostServices: cfg.HostServices,
		ProviderName: cfg.Name,
	})
	if err != nil {
		return nil, err
	}

	runtimeClient := proc.Lifecycle()
	authzClient := proto.NewAuthorizationProviderClient(proc.Conn())
	meta, err := runtimehost.ConfigureRuntimeProvider(ctx, runtimeClient, proto.ProviderKind_PROVIDER_KIND_AUTHORIZATION, cfg.Name, cfg.Config)
	if err != nil {
		_ = proc.Close()
		return nil, err
	}

	name := cfg.Name
	if meta != nil && meta.Name != "" {
		name = meta.Name
	}
	if name == "" {
		name = "authorization"
	}

	return &remoteAuthorizationProvider{
		client:  authzClient,
		runtime: runtimeClient,
		closer:  proc,
		name:    name,
	}, nil
}

func (r *remoteAuthorizationProvider) Name() string {
	return r.name
}

func (r *remoteAuthorizationProvider) CheckAccess(ctx context.Context, req *core.CheckAccessRequest) (*core.CheckAccessResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	return r.client.CheckAccess(ctx, req)
}

func (r *remoteAuthorizationProvider) CheckAccessMany(ctx context.Context, req *core.CheckAccessManyRequest) (*core.CheckAccessManyResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	return r.client.CheckAccessMany(ctx, req)
}

func (r *remoteAuthorizationProvider) ListRelationships(ctx context.Context, req *core.ListRelationshipsRequest) (*core.ListRelationshipsResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	return r.client.ListRelationships(ctx, req)
}

func (r *remoteAuthorizationProvider) AddRelationship(ctx context.Context, req *core.AddRelationshipRequest) (*core.AddRelationshipResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	return r.client.AddRelationship(ctx, req)
}

func (r *remoteAuthorizationProvider) DeleteRelationship(ctx context.Context, req *core.DeleteRelationshipRequest) (*core.DeleteRelationshipResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	return r.client.DeleteRelationship(ctx, req)
}

func (r *remoteAuthorizationProvider) SetRelationships(ctx context.Context, req *core.SetRelationshipsRequest) (*core.SetRelationshipsResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	return r.client.SetRelationships(ctx, req)
}

func (r *remoteAuthorizationProvider) GetActiveModelRef(ctx context.Context) (*core.GetActiveModelRefResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	return r.client.GetActiveModelRef(ctx, &emptypb.Empty{})
}

func (r *remoteAuthorizationProvider) SetActiveModel(ctx context.Context, req *core.SetActiveModelRequest) (*core.SetActiveModelResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	return r.client.SetActiveModel(ctx, req)
}

func (r *remoteAuthorizationProvider) ListActiveModelResourceTypes(ctx context.Context, req *core.ListActiveModelResourceTypesRequest) (*core.ListActiveModelResourceTypesResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	return r.client.ListActiveModelResourceTypes(ctx, req)
}

func (r *remoteAuthorizationProvider) Close() error {
	if r == nil || r.closer == nil {
		return nil
	}
	return r.closer.Close()
}
