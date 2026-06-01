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

type observedAuthorizationProviderCarrier interface {
	observedAuthorizationBase() *observedAuthorizationProvider
}

func InstrumentAuthorizationProvider(name string, provider core.AuthorizationProvider) core.AuthorizationProvider {
	if provider == nil {
		return nil
	}
	if _, ok := provider.(observedAuthorizationProviderCarrier); ok {
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
	if observed, ok := provider.(observedAuthorizationProviderCarrier); ok {
		return observed.observedAuthorizationBase().metricName()
	}
	return strings.TrimSpace(provider.Name())
}

func (p *observedAuthorizationProvider) observedAuthorizationBase() *observedAuthorizationProvider {
	return p
}

func (p *observedAuthorizationProvider) Name() string {
	return p.delegate.Name()
}

func (p *observedAuthorizationProvider) CheckAccess(ctx context.Context, req *core.CheckAccessRequest) (resp *core.CheckAccessResponse, err error) {
	ctx, end := p.start(ctx, "check_access")
	defer func() { end(err) }()
	return p.delegate.CheckAccess(ctx, req)
}

func (p *observedAuthorizationProvider) CheckAccessMany(ctx context.Context, req *core.CheckAccessManyRequest) (resp *core.CheckAccessManyResponse, err error) {
	ctx, end := p.start(ctx, "check_access_many")
	defer func() { end(err) }()
	return p.delegate.CheckAccessMany(ctx, req)
}

func (p *observedAuthorizationProvider) ListRelationships(ctx context.Context, req *core.ListRelationshipsRequest) (resp *core.ListRelationshipsResponse, err error) {
	ctx, end := p.start(ctx, "list_relationships")
	defer func() { end(err) }()
	return p.delegate.ListRelationships(ctx, req)
}

func (p *observedAuthorizationProvider) AddRelationship(ctx context.Context, req *core.AddRelationshipRequest) (resp *core.AddRelationshipResponse, err error) {
	ctx, end := p.start(ctx, "add_relationship")
	defer func() { end(err) }()
	return p.delegate.AddRelationship(ctx, req)
}

func (p *observedAuthorizationProvider) DeleteRelationship(ctx context.Context, req *core.DeleteRelationshipRequest) (resp *core.DeleteRelationshipResponse, err error) {
	ctx, end := p.start(ctx, "delete_relationship")
	defer func() { end(err) }()
	return p.delegate.DeleteRelationship(ctx, req)
}

func (p *observedAuthorizationProvider) SetRelationships(ctx context.Context, req *core.SetRelationshipsRequest) (resp *core.SetRelationshipsResponse, err error) {
	ctx, end := p.start(ctx, "set_relationships")
	defer func() { end(err) }()
	return p.delegate.SetRelationships(ctx, req)
}

func (p *observedAuthorizationProvider) GetActiveModelRef(ctx context.Context) (resp *core.GetActiveModelRefResponse, err error) {
	ctx, end := p.start(ctx, "get_active_model_ref")
	defer func() { end(err) }()
	return p.delegate.GetActiveModelRef(ctx)
}

func (p *observedAuthorizationProvider) SetActiveModel(ctx context.Context, req *core.SetActiveModelRequest) (resp *core.SetActiveModelResponse, err error) {
	ctx, end := p.start(ctx, "set_active_model")
	defer func() { end(err) }()
	return p.delegate.SetActiveModel(ctx, req)
}

func (p *observedAuthorizationProvider) ListActiveModelResourceTypes(ctx context.Context, req *core.ListActiveModelResourceTypesRequest) (resp *core.ListActiveModelResourceTypesResponse, err error) {
	ctx, end := p.start(ctx, "list_active_model_resource_types")
	defer func() { end(err) }()
	return p.delegate.ListActiveModelResourceTypes(ctx, req)
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
