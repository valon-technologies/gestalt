package providerhost

import (
	"context"
	"io"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"google.golang.org/protobuf/types/known/emptypb"
)

type AuthorizationExecConfig struct {
	Command      string
	Args         []string
	Env          map[string]string
	Config       map[string]any
	AllowedHosts []string
	HostBinary   string
	Cleanup      func()
	HostServices []HostService
	Name         string
}

type remoteAuthorizationProvider struct {
	client  proto.AuthorizationProviderClient
	runtime proto.ProviderLifecycleClient
	closer  io.Closer
	name    string
}

func NewExecutableAuthorizationProvider(ctx context.Context, cfg AuthorizationExecConfig) (core.AuthorizationProvider, error) {
	execCfg := ExecConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Env:          cfg.Env,
		Config:       cfg.Config,
		AllowedHosts: cfg.AllowedHosts,
		HostBinary:   cfg.HostBinary,
		Cleanup:      cfg.Cleanup,
		HostServices: cfg.HostServices,
		ProviderName: cfg.Name,
	}
	proc, err := startProviderProcess(ctx, execCfg.processConfig())
	if err != nil {
		return nil, err
	}

	runtimeClient := proto.NewProviderLifecycleClient(proc.conn)
	authzClient := proto.NewAuthorizationProviderClient(proc.conn)
	meta, err := ConfigureRuntimeProvider(ctx, runtimeClient, proto.ProviderKind_PROVIDER_KIND_AUTHORIZATION, cfg.Name, cfg.Config)
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

func (r *remoteAuthorizationProvider) Evaluate(ctx context.Context, req *core.AccessEvaluationRequest) (*core.AccessDecision, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	return r.client.Evaluate(ctx, req)
}

func (r *remoteAuthorizationProvider) EvaluateMany(ctx context.Context, req *core.AccessEvaluationsRequest) (*core.AccessEvaluationsResponse, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	return r.client.EvaluateMany(ctx, req)
}

func (r *remoteAuthorizationProvider) SearchResources(ctx context.Context, req *core.ResourceSearchRequest) (*core.ResourceSearchResponse, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	return r.client.SearchResources(ctx, req)
}

func (r *remoteAuthorizationProvider) SearchSubjects(ctx context.Context, req *core.SubjectSearchRequest) (*core.SubjectSearchResponse, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	return r.client.SearchSubjects(ctx, req)
}

func (r *remoteAuthorizationProvider) SearchActions(ctx context.Context, req *core.ActionSearchRequest) (*core.ActionSearchResponse, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	return r.client.SearchActions(ctx, req)
}

func (r *remoteAuthorizationProvider) GetMetadata(ctx context.Context) (*core.AuthorizationMetadata, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	return r.client.GetMetadata(ctx, &emptypb.Empty{})
}

func (r *remoteAuthorizationProvider) ReadRelationships(ctx context.Context, req *core.ReadRelationshipsRequest) (*core.ReadRelationshipsResponse, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	return r.client.ReadRelationships(ctx, req)
}

func (r *remoteAuthorizationProvider) WriteRelationships(ctx context.Context, req *core.WriteRelationshipsRequest) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	_, err := r.client.WriteRelationships(ctx, req)
	return err
}

func (r *remoteAuthorizationProvider) GetActiveModel(ctx context.Context) (*core.GetActiveModelResponse, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	return r.client.GetActiveModel(ctx, &emptypb.Empty{})
}

func (r *remoteAuthorizationProvider) ListModels(ctx context.Context, req *core.ListModelsRequest) (*core.ListModelsResponse, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	return r.client.ListModels(ctx, req)
}

func (r *remoteAuthorizationProvider) WriteModel(ctx context.Context, req *core.WriteModelRequest) (*core.AuthorizationModelRef, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	return r.client.WriteModel(ctx, req)
}

func (r *remoteAuthorizationProvider) Close() error {
	if r == nil || r.closer == nil {
		return nil
	}
	return r.closer.Close()
}
