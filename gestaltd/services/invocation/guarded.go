package invocation

import (
	"context"
	"errors"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

const (
	DefaultMaxDepth  = 5
	DefaultRateLimit = 100
	DefaultRateBurst = 20
)

var (
	_ Invoker                   = (*GuardedInvoker)(nil)
	_ GraphQLInvoker            = (*GuardedInvoker)(nil)
	_ CapabilityLister          = (*GuardedInvoker)(nil)
	_ TokenResolver             = (*GuardedInvoker)(nil)
	_ RuntimeCredentialResolver = (*GuardedInvoker)(nil)
	_ subjectTokenResolver      = (*GuardedInvoker)(nil)
)

type GuardedInvoker struct {
	inner    Invoker
	lister   CapabilityLister
	allowed  map[string]struct{} // nil = allow all
	source   string
	maxDepth int
	audit    core.AuditSink
	limiter  *rateLimiter
}

type GuardedOption func(*GuardedInvoker)

func WithAllowedProviders(providers []string) GuardedOption {
	return func(g *GuardedInvoker) {
		g.allowed = make(map[string]struct{}, len(providers))
		for _, provider := range providers {
			g.allowed[provider] = struct{}{}
		}
	}
}

func WithMaxDepth(n int) GuardedOption {
	return func(g *GuardedInvoker) { g.maxDepth = n }
}

func WithRateLimit(rps, burst int) GuardedOption {
	return func(g *GuardedInvoker) { g.limiter = newRateLimiter(rps, burst) }
}

func WithoutRateLimit() GuardedOption {
	return func(g *GuardedInvoker) { g.limiter = nil }
}

func NewGuarded(inner Invoker, lister CapabilityLister, source string, audit core.AuditSink, opts ...GuardedOption) *GuardedInvoker {
	guarded := &GuardedInvoker{
		inner:    inner,
		lister:   lister,
		source:   source,
		maxDepth: DefaultMaxDepth,
		audit:    audit,
		limiter:  newRateLimiter(DefaultRateLimit, DefaultRateBurst),
	}
	for _, opt := range opts {
		opt(guarded)
	}
	return guarded
}

func (g *GuardedInvoker) Invoke(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error) {
	ctx, meta := ensureMeta(ctx)

	if p == nil {
		p = principal.FromContext(ctx)
	}
	if p == nil {
		p = &principal.Principal{}
	}

	entry := buildAuditEntry(ctx, p, g.source, providerName, operation, meta)
	ctx = withAuditEntry(ctx, &entry)

	if err := g.check(meta, providerName, instance, operation); err != nil {
		entry.Allowed = false
		entry.Error = err.Error()
		g.logAudit(ctx, entry)
		return nil, err
	}

	entry.Allowed = true
	defer func() {
		g.logAudit(ctx, entry)
	}()

	chainInstance := instance
	if chainInstance == "" {
		chainInstance = "default"
	}
	chainKey := providerName + "/" + chainInstance + "/" + operation
	next := &InvocationMeta{
		RequestID: meta.RequestID,
		Depth:     meta.Depth + 1,
		CallChain: append(append([]string(nil), meta.CallChain...), chainKey),
	}
	ctx = ContextWithMeta(ctx, next)

	result, err := g.inner.Invoke(ctx, p, providerName, instance, operation, params)
	if err != nil {
		entry.Error = err.Error()
		if errors.Is(err, ErrAuthorizationDenied) || errors.Is(err, ErrScopeDenied) || errors.Is(err, ErrNotAuthenticated) {
			entry.Allowed = false
		}
	}
	return result, err
}

func (g *GuardedInvoker) InvokeGraphQL(ctx context.Context, p *principal.Principal, providerName, instance string, request GraphQLRequest) (*core.OperationResult, error) {
	graphqlInvoker, ok := g.inner.(GraphQLInvoker)
	if !ok {
		return nil, fmt.Errorf("plugin invoker is not available")
	}

	ctx, meta := ensureMeta(ctx)

	if p == nil {
		p = principal.FromContext(ctx)
	}
	if p == nil {
		p = &principal.Principal{}
	}

	entry := buildAuditEntry(ctx, p, g.source, providerName, "graphql", meta)
	ctx = withAuditEntry(ctx, &entry)

	if err := g.check(meta, providerName, instance, "graphql"); err != nil {
		entry.Allowed = false
		entry.Error = err.Error()
		g.logAudit(ctx, entry)
		return nil, err
	}

	entry.Allowed = true
	defer func() {
		g.logAudit(ctx, entry)
	}()

	chainInstance := instance
	if chainInstance == "" {
		chainInstance = "default"
	}
	chainKey := providerName + "/" + chainInstance + "/graphql"
	next := &InvocationMeta{
		RequestID: meta.RequestID,
		Depth:     meta.Depth + 1,
		CallChain: append(append([]string(nil), meta.CallChain...), chainKey),
	}
	ctx = ContextWithMeta(ctx, next)

	result, err := graphqlInvoker.InvokeGraphQL(ctx, p, providerName, instance, request)
	if err != nil {
		entry.Error = err.Error()
		if errors.Is(err, ErrAuthorizationDenied) || errors.Is(err, ErrScopeDenied) || errors.Is(err, ErrNotAuthenticated) {
			entry.Allowed = false
		}
	}
	return result, err
}

func (g *GuardedInvoker) ListCapabilities() []core.Capability {
	if g.lister == nil {
		return nil
	}

	caps := g.lister.ListCapabilities()
	if g.allowed == nil {
		return caps
	}

	filtered := make([]core.Capability, 0, len(caps))
	for i := range caps {
		if _, ok := g.allowed[caps[i].Provider]; ok {
			filtered = append(filtered, caps[i])
		}
	}
	return filtered
}

func (g *GuardedInvoker) check(meta *InvocationMeta, providerName, instance, operation string) error {
	if meta.Depth >= g.maxDepth {
		return &MaxDepthError{Depth: meta.Depth, Max: g.maxDepth}
	}

	checkInstance := instance
	if checkInstance == "" {
		checkInstance = "default"
	}
	chainKey := providerName + "/" + checkInstance + "/" + operation
	for _, entry := range meta.CallChain {
		if entry == chainKey {
			return &RecursionError{Provider: providerName, Operation: operation}
		}
	}

	if g.allowed != nil {
		if _, ok := g.allowed[providerName]; !ok {
			return fmt.Errorf("provider %q is not available in this scope", providerName)
		}
	}

	if g.limiter != nil && !g.limiter.Allow(providerName) {
		return &RateLimitError{Provider: providerName}
	}

	return nil
}

func (g *GuardedInvoker) ResolveToken(ctx context.Context, p *principal.Principal, providerName, connection, instance string) (context.Context, string, error) {
	if r, ok := g.inner.(TokenResolver); ok {
		return r.ResolveToken(ctx, p, providerName, connection, instance)
	}
	return ctx, "", fmt.Errorf("token resolution not supported")
}

func (g *GuardedInvoker) ResolveRuntimeConnectionCredential(ctx context.Context, p *principal.Principal, providerName, connection, instance string) (context.Context, ConnectionRuntimeCredential, ConnectionRuntimeInfo, error) {
	if r, ok := g.inner.(RuntimeCredentialResolver); ok {
		return r.ResolveRuntimeConnectionCredential(ctx, p, providerName, connection, instance)
	}
	return ctx, ConnectionRuntimeCredential{}, ConnectionRuntimeInfo{}, fmt.Errorf("runtime connection credential resolution not supported")
}

func (g *GuardedInvoker) ResolveSubjectToken(ctx context.Context, prov core.Provider, subjectID, providerName, connection, instance string) (context.Context, string, error) {
	if r, ok := g.inner.(subjectTokenResolver); ok {
		return r.ResolveSubjectToken(ctx, prov, subjectID, providerName, connection, instance)
	}
	return ctx, "", fmt.Errorf("subject token resolution not supported")
}

func (g *GuardedInvoker) logAudit(ctx context.Context, entry core.AuditEntry) {
	if g.audit != nil {
		g.audit.Log(ctx, entry)
	}
}
