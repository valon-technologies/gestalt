package observability

import (
	"context"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"go.opentelemetry.io/otel/attribute"
)

type observedAuthorizationProvider struct {
	name     string
	delegate core.AuthorizationProvider
}

func InstrumentAuthorizationProvider(name string, provider core.AuthorizationProvider) core.AuthorizationProvider {
	if provider == nil {
		return nil
	}
	if _, ok := provider.(*observedAuthorizationProvider); ok {
		return provider
	}
	return &observedAuthorizationProvider{
		name:     strings.TrimSpace(name),
		delegate: provider,
	}
}

func AuthorizationProviderMetricName(provider core.AuthorizationProvider) string {
	if provider == nil {
		return ""
	}
	if observed, ok := provider.(*observedAuthorizationProvider); ok {
		return observed.metricName()
	}
	return strings.TrimSpace(provider.Name())
}

func (p *observedAuthorizationProvider) Name() string {
	return p.delegate.Name()
}

func (p *observedAuthorizationProvider) Evaluate(ctx context.Context, req *core.AccessEvaluationRequest) (decision *core.AccessDecision, err error) {
	ctx, end := p.start(ctx, "evaluate")
	defer func() { end(err) }()
	return p.delegate.Evaluate(ctx, req)
}

func (p *observedAuthorizationProvider) EvaluateMany(ctx context.Context, req *core.AccessEvaluationsRequest) (resp *core.AccessEvaluationsResponse, err error) {
	ctx, end := p.start(ctx, "evaluate_many")
	defer func() { end(err) }()
	return p.delegate.EvaluateMany(ctx, req)
}

func (p *observedAuthorizationProvider) SearchResources(ctx context.Context, req *core.ResourceSearchRequest) (resp *core.ResourceSearchResponse, err error) {
	ctx, end := p.start(ctx, "search_resources")
	defer func() { end(err) }()
	return p.delegate.SearchResources(ctx, req)
}

func (p *observedAuthorizationProvider) SearchSubjects(ctx context.Context, req *core.SubjectSearchRequest) (resp *core.SubjectSearchResponse, err error) {
	ctx, end := p.start(ctx, "search_subjects")
	defer func() { end(err) }()
	return p.delegate.SearchSubjects(ctx, req)
}

func (p *observedAuthorizationProvider) SearchActions(ctx context.Context, req *core.ActionSearchRequest) (resp *core.ActionSearchResponse, err error) {
	ctx, end := p.start(ctx, "search_actions")
	defer func() { end(err) }()
	return p.delegate.SearchActions(ctx, req)
}

func (p *observedAuthorizationProvider) GetMetadata(ctx context.Context) (metadata *core.AuthorizationMetadata, err error) {
	ctx, end := p.start(ctx, "get_metadata")
	defer func() { end(err) }()
	return p.delegate.GetMetadata(ctx)
}

func (p *observedAuthorizationProvider) ReadRelationships(ctx context.Context, req *core.ReadRelationshipsRequest) (resp *core.ReadRelationshipsResponse, err error) {
	ctx, end := p.start(ctx, "read_relationships")
	defer func() { end(err) }()
	return p.delegate.ReadRelationships(ctx, req)
}

func (p *observedAuthorizationProvider) WriteRelationships(ctx context.Context, req *core.WriteRelationshipsRequest) (err error) {
	ctx, end := p.start(ctx, "write_relationships")
	defer func() { end(err) }()
	return p.delegate.WriteRelationships(ctx, req)
}

func (p *observedAuthorizationProvider) GetActiveModel(ctx context.Context) (resp *core.GetActiveModelResponse, err error) {
	ctx, end := p.start(ctx, "get_active_model")
	defer func() { end(err) }()
	return p.delegate.GetActiveModel(ctx)
}

func (p *observedAuthorizationProvider) ListModels(ctx context.Context, req *core.ListModelsRequest) (resp *core.ListModelsResponse, err error) {
	ctx, end := p.start(ctx, "list_models")
	defer func() { end(err) }()
	return p.delegate.ListModels(ctx, req)
}

func (p *observedAuthorizationProvider) WriteModel(ctx context.Context, req *core.WriteModelRequest) (ref *core.AuthorizationModelRef, err error) {
	ctx, end := p.start(ctx, "write_model")
	defer func() { end(err) }()
	return p.delegate.WriteModel(ctx, req)
}

func (p *observedAuthorizationProvider) Close() error {
	closer, ok := p.delegate.(interface{ Close() error })
	if !ok {
		return nil
	}
	return closer.Close()
}

func (p *observedAuthorizationProvider) start(ctx context.Context, operation string) (context.Context, func(error)) {
	startedAt := time.Now()
	attrs := []attribute.KeyValue{
		AttrAuthorizationProvider.String(p.metricName()),
		AttrAuthorizationOperation.String(operation),
	}
	ctx, span := StartSpan(ctx, "authorization.provider.operation", attrs...)
	return ctx, func(err error) {
		EndSpan(span, err)
		RecordAuthorizationProviderOperation(ctx, startedAt, err != nil, attrs...)
	}
}

func (p *observedAuthorizationProvider) metricName() string {
	if p == nil {
		return ""
	}
	if name := strings.TrimSpace(p.name); name != "" {
		return name
	}
	if p.delegate == nil {
		return ""
	}
	return strings.TrimSpace(p.delegate.Name())
}
