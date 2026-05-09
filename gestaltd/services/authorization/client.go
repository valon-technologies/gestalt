package authorization

import (
	"context"
	"io"
	"slices"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ExecConfig struct {
	Command      string
	Args         []string
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

type remoteAuthorizationProviderWithEffectiveSearch struct {
	*remoteAuthorizationProvider
}

type remoteAuthorizationProviderWithExpansion struct {
	*remoteAuthorizationProvider
}

type remoteAuthorizationProviderWithEffectiveSearchAndExpansion struct {
	*remoteAuthorizationProvider
}

func NewExecutable(ctx context.Context, cfg ExecConfig) (core.AuthorizationProvider, error) {
	proc, err := runtimehost.StartPluginProcess(ctx, runtimehost.ProcessConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
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

	capabilities := remoteAuthorizationCapabilities(ctx, authzClient)
	return newRemoteAuthorizationProvider(authzClient, runtimeClient, proc, name, capabilities), nil
}

func newRemoteAuthorizationProvider(client proto.AuthorizationProviderClient, runtime proto.ProviderLifecycleClient, closer io.Closer, name string, capabilities []string) core.AuthorizationProvider {
	base := &remoteAuthorizationProvider{
		client:  client,
		runtime: runtime,
		closer:  closer,
		name:    name,
	}
	hasEffectiveSearch := remoteAuthorizationCapabilitySet(capabilities).has(capabilityEffectiveSearchResources, capabilityEffectiveSearchSubjects)
	hasExpansion := remoteAuthorizationCapabilitySet(capabilities).has(capabilityExpand)
	switch {
	case hasEffectiveSearch && hasExpansion:
		return &remoteAuthorizationProviderWithEffectiveSearchAndExpansion{remoteAuthorizationProvider: base}
	case hasEffectiveSearch:
		return &remoteAuthorizationProviderWithEffectiveSearch{remoteAuthorizationProvider: base}
	case hasExpansion:
		return &remoteAuthorizationProviderWithExpansion{remoteAuthorizationProvider: base}
	default:
		return base
	}
}

func remoteAuthorizationCapabilities(ctx context.Context, client proto.AuthorizationProviderClient) []string {
	if client == nil {
		return nil
	}
	callCtx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	metadata, err := client.GetMetadata(callCtx, &emptypb.Empty{})
	if err != nil {
		return nil
	}
	return append([]string(nil), metadata.GetCapabilities()...)
}

type remoteAuthorizationCapabilitySet []string

func (s remoteAuthorizationCapabilitySet) has(capabilities ...string) bool {
	for _, capability := range capabilities {
		if !slices.Contains(s, capability) {
			return false
		}
	}
	return true
}

func (r *remoteAuthorizationProvider) Name() string {
	return r.name
}

func (r *remoteAuthorizationProvider) Evaluate(ctx context.Context, req *core.AccessEvaluationRequest) (*core.AccessDecision, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	return r.client.Evaluate(ctx, req)
}

func (r *remoteAuthorizationProvider) EvaluateMany(ctx context.Context, req *core.AccessEvaluationsRequest) (*core.AccessEvaluationsResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	return r.client.EvaluateMany(ctx, req)
}

func (r *remoteAuthorizationProvider) SearchResources(ctx context.Context, req *core.ResourceSearchRequest) (*core.ResourceSearchResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	return r.client.SearchResources(ctx, req)
}

func (r *remoteAuthorizationProvider) SearchSubjects(ctx context.Context, req *core.SubjectSearchRequest) (*core.SubjectSearchResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	return r.client.SearchSubjects(ctx, req)
}

func (r *remoteAuthorizationProviderWithEffectiveSearch) EffectiveSearchResources(ctx context.Context, req *core.ResourceSearchRequest) (*core.ResourceSearchResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	return r.client.EffectiveSearchResources(ctx, req)
}

func (r *remoteAuthorizationProviderWithEffectiveSearch) EffectiveSearchSubjects(ctx context.Context, req *core.EffectiveSubjectSearchRequest) (*core.EffectiveSubjectSearchResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	return r.client.EffectiveSearchSubjects(ctx, req)
}

func (r *remoteAuthorizationProviderWithEffectiveSearchAndExpansion) EffectiveSearchResources(ctx context.Context, req *core.ResourceSearchRequest) (*core.ResourceSearchResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	return r.client.EffectiveSearchResources(ctx, req)
}

func (r *remoteAuthorizationProviderWithEffectiveSearchAndExpansion) EffectiveSearchSubjects(ctx context.Context, req *core.EffectiveSubjectSearchRequest) (*core.EffectiveSubjectSearchResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	return r.client.EffectiveSearchSubjects(ctx, req)
}

func (r *remoteAuthorizationProvider) SearchActions(ctx context.Context, req *core.ActionSearchRequest) (*core.ActionSearchResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	return r.client.SearchActions(ctx, req)
}

func (r *remoteAuthorizationProvider) GetMetadata(ctx context.Context) (*core.AuthorizationMetadata, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	return r.client.GetMetadata(ctx, &emptypb.Empty{})
}

func (r *remoteAuthorizationProvider) ReadRelationships(ctx context.Context, req *core.ReadRelationshipsRequest) (*core.ReadRelationshipsResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	return r.client.ReadRelationships(ctx, req)
}

func (r *remoteAuthorizationProvider) WriteRelationships(ctx context.Context, req *core.WriteRelationshipsRequest) error {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	_, err := r.client.WriteRelationships(ctx, req)
	return err
}

func (r *remoteAuthorizationProviderWithExpansion) Expand(ctx context.Context, req *core.ExpandRequest) (*core.ExpandResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	return r.client.Expand(ctx, req)
}

func (r *remoteAuthorizationProviderWithEffectiveSearchAndExpansion) Expand(ctx context.Context, req *core.ExpandRequest) (*core.ExpandResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	return r.client.Expand(ctx, req)
}

func (r *remoteAuthorizationProvider) GetActiveModel(ctx context.Context) (*core.GetActiveModelResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	return r.client.GetActiveModel(ctx, &emptypb.Empty{})
}

func (r *remoteAuthorizationProvider) ListModels(ctx context.Context, req *core.ListModelsRequest) (*core.ListModelsResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	return r.client.ListModels(ctx, req)
}

func (r *remoteAuthorizationProvider) WriteModel(ctx context.Context, req *core.WriteModelRequest) (*core.AuthorizationModelRef, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	return r.client.WriteModel(ctx, req)
}

func (r *remoteAuthorizationProvider) Close() error {
	if r == nil || r.closer == nil {
		return nil
	}
	return r.closer.Close()
}
