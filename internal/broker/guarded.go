package broker

import (
	"context"
	"fmt"
	"time"

	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/internal/principal"
)

const (
	DefaultMaxDepth  = 5
	DefaultRateLimit = 100
	DefaultRateBurst = 20
)

var _ core.Broker = (*GuardedBroker)(nil)

type GuardedBroker struct {
	inner    *Broker
	allowed  map[string]struct{} // nil = allow all
	source   string
	maxDepth int
	audit    core.AuditSink
	limiter  *rateLimiter
}

type GuardedOption func(*GuardedBroker)

func WithAllowedProviders(providers []string) GuardedOption {
	return func(g *GuardedBroker) {
		g.allowed = make(map[string]struct{}, len(providers))
		for _, p := range providers {
			g.allowed[p] = struct{}{}
		}
	}
}

func WithMaxDepth(n int) GuardedOption {
	return func(g *GuardedBroker) { g.maxDepth = n }
}

func WithRateLimit(rps, burst int) GuardedOption {
	return func(g *GuardedBroker) { g.limiter = newRateLimiter(rps, burst) }
}

func WithoutRateLimit() GuardedOption {
	return func(g *GuardedBroker) { g.limiter = nil }
}

func NewGuarded(inner *Broker, source string, audit core.AuditSink, opts ...GuardedOption) *GuardedBroker {
	g := &GuardedBroker{
		inner:    inner,
		source:   source,
		maxDepth: DefaultMaxDepth,
		audit:    audit,
		limiter:  newRateLimiter(DefaultRateLimit, DefaultRateBurst),
	}
	for _, o := range opts {
		o(g)
	}
	return g
}

func (g *GuardedBroker) Invoke(ctx context.Context, req core.InvocationRequest) (*core.OperationResult, error) {
	ctx, meta := ensureMeta(ctx)

	userID := req.UserID
	if userID == "" {
		if p := principal.FromContext(ctx); p != nil {
			userID = p.UserID
		}
	}

	entry := core.AuditEntry{
		Timestamp: time.Now(),
		RequestID: meta.RequestID,
		Source:    g.source,
		UserID:    userID,
		Provider:  req.Provider,
		Operation: req.Operation,
		Depth:     meta.Depth,
	}

	if err := g.check(meta, req); err != nil {
		entry.Allowed = false
		entry.Error = err.Error()
		g.logAudit(entry)
		return nil, err
	}

	entry.Allowed = true
	g.logAudit(entry)

	chainKey := req.Provider + "/" + req.Operation
	next := &InvocationMeta{
		RequestID: meta.RequestID,
		Depth:     meta.Depth + 1,
		CallChain: append(append([]string(nil), meta.CallChain...), chainKey),
	}
	ctx = ContextWithMeta(ctx, next)

	return g.inner.Invoke(ctx, req)
}

func (g *GuardedBroker) check(meta *InvocationMeta, req core.InvocationRequest) error {
	if meta.Depth >= g.maxDepth {
		return &MaxDepthError{Depth: meta.Depth, Max: g.maxDepth}
	}

	chainKey := req.Provider + "/" + req.Operation
	for _, entry := range meta.CallChain {
		if entry == chainKey {
			return &RecursionError{Provider: req.Provider, Operation: req.Operation}
		}
	}

	if g.allowed != nil {
		if _, ok := g.allowed[req.Provider]; !ok {
			return fmt.Errorf("provider %q is not available in this scope", req.Provider)
		}
	}

	if g.limiter != nil && !g.limiter.Allow(req.Provider) {
		return &RateLimitError{Provider: req.Provider}
	}

	return nil
}

func (g *GuardedBroker) ListCapabilities() []core.Capability {
	all := g.inner.ListCapabilities()
	if g.allowed == nil {
		return all
	}
	var filtered []core.Capability
	for _, cap := range all {
		if _, ok := g.allowed[cap.Provider]; ok {
			filtered = append(filtered, cap)
		}
	}
	return filtered
}

func (g *GuardedBroker) logAudit(entry core.AuditEntry) {
	if g.audit != nil {
		g.audit.Log(entry)
	}
}
