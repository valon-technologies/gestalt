package invocation

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

const maxInvocationDepth = 5

var (
	_ Invoker          = (*AuditedInvoker)(nil)
	_ CapabilityLister = (*AuditedInvoker)(nil)
)

type AuditedInvoker struct {
	broker *Broker
	source string
	audit  core.AuditSink
}

func NewAuditedInvoker(broker *Broker, source string, audit core.AuditSink) *AuditedInvoker {
	return &AuditedInvoker{broker: broker, source: source, audit: audit}
}

func (a *AuditedInvoker) Invoke(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error) {
	ctx, meta := ensureMeta(ctx)

	if p == nil {
		p = principal.FromContext(ctx)
	}
	if p == nil {
		p = &principal.Principal{}
	}

	entry := buildAuditEntry(ctx, p, a.source, providerName, operation, meta)

	if err := checkDepthAndRecursion(meta, providerName, instance, operation); err != nil {
		entry.Allowed = false
		entry.Error = err.Error()
		a.logAudit(ctx, entry)
		return nil, err
	}

	entry.Allowed = true
	a.logAudit(ctx, entry)

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

	return a.broker.Invoke(ctx, p, providerName, instance, operation, params)
}

func (a *AuditedInvoker) ListCapabilities() []core.Capability {
	return a.broker.ListCapabilities()
}

func (a *AuditedInvoker) ResolveToken(ctx context.Context, p *principal.Principal, providerName, connection, instance string) (string, error) {
	return a.broker.ResolveToken(ctx, p, providerName, connection, instance)
}

func (a *AuditedInvoker) logAudit(ctx context.Context, entry core.AuditEntry) {
	if a.audit != nil {
		a.audit.Log(ctx, entry)
	}
}

func checkDepthAndRecursion(meta *InvocationMeta, providerName, instance, operation string) error {
	if meta.Depth >= maxInvocationDepth {
		return &MaxDepthError{Depth: meta.Depth, Max: maxInvocationDepth}
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

	return nil
}

type MaxDepthError struct {
	Depth int
	Max   int
}

func (e *MaxDepthError) Error() string {
	return fmt.Sprintf("invocation depth %d exceeds maximum %d", e.Depth, e.Max)
}

type RecursionError struct {
	Provider  string
	Operation string
}

func (e *RecursionError) Error() string {
	return fmt.Sprintf("recursive call detected: %s/%s already in call chain", e.Provider, e.Operation)
}
