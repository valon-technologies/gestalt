package invocation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/integration"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

type TokenResolver interface {
	ResolveToken(ctx context.Context, p *principal.Principal, providerName, connection, instance string) (context.Context, string, error)
}

type requestStaticOperationResolver interface {
	ResolveStaticOperationForRequest(ctx context.Context, operation string) (catalog.CatalogOperation, bool)
}

type CatalogResolutionMetadata struct {
	SessionAttempted bool
	SessionFailed    bool
}

type CatalogResolutionTarget struct {
	Connection string
	Instance   string
}

func ResolveCatalog(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, defaultConnection, instance string) (*catalog.Catalog, error) {
	cat, _, err := ResolveCatalogWithMetadata(ctx, prov, provName, resolver, p, defaultConnection, instance)
	return cat, err
}

func ResolveCatalogWithMetadata(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, defaultConnection, instance string) (*catalog.Catalog, CatalogResolutionMetadata, error) {
	return resolveCatalog(ctx, prov, provName, resolver, p, defaultConnection, instance, false)
}

func ResolveCatalogStrictWithMetadata(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, defaultConnection, instance string) (*catalog.Catalog, CatalogResolutionMetadata, error) {
	return resolveCatalog(ctx, prov, provName, resolver, p, defaultConnection, instance, true)
}

func ResolveCatalogForTargetsWithMetadata(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, targets []CatalogResolutionTarget, strictSession bool) (*catalog.Catalog, CatalogResolutionMetadata, error) {
	if len(targets) == 0 {
		targets = []CatalogResolutionTarget{{}}
	}

	var (
		firstErr              error
		sessionAttempted      bool
		sawSessionUnavailable bool
	)
	for _, target := range targets {
		cat, meta, err := resolveCatalog(ctx, prov, provName, resolver, p, target.Connection, target.Instance, strictSession)
		if err == nil {
			return cat, meta, nil
		}
		if firstErr == nil {
			firstErr = err
		}
		sessionAttempted = sessionAttempted || meta.SessionAttempted
		sawSessionUnavailable = sawSessionUnavailable || errors.Is(err, core.ErrSessionCatalogUnavailable)
	}

	if firstErr != nil && sawSessionUnavailable {
		if staticCat := prov.Catalog(); staticCat != nil {
			slog.WarnContext(ctx, "catalog resolution falling back to static catalog", "provider", provName, "error", firstErr)
			return staticCat.Clone(), CatalogResolutionMetadata{
				SessionAttempted: sessionAttempted,
				SessionFailed:    true,
			}, nil
		}
	}

	return nil, CatalogResolutionMetadata{
		SessionAttempted: sessionAttempted,
		SessionFailed:    firstErr != nil,
	}, firstErr
}

func ResolveOperation(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, operation string, sessionConnections []string, instance string) (catalog.CatalogOperation, string, string, error) {
	if op, ok := CatalogOperationFromContext(ctx, provName, operation); ok {
		return op, OperationTransport(op), "", nil
	}

	staticOp, staticOK := CatalogOperation(providerCatalog(prov), operation)
	if staticOK {
		if resolver, ok := prov.(requestStaticOperationResolver); ok {
			if op, ok := resolver.ResolveStaticOperationForRequest(ctx, operation); ok {
				return op, OperationTransport(op), "", nil
			}
		}
	}
	if !core.SupportsSessionCatalog(prov) {
		if staticOK {
			return staticOp, OperationTransport(staticOp), "", nil
		}
		return catalog.CatalogOperation{}, "", "", fmt.Errorf("%w: %q on provider %q", ErrOperationNotFound, operation, provName)
	}

	sessionOp, sessionConnection, sessionFound, err := resolveSessionOperation(ctx, prov, provName, resolver, p, operation, sessionConnections, instance)
	if err != nil {
		if staticOK && sessionCatalogUnsupported(err) {
			return staticOp, OperationTransport(staticOp), "", nil
		}
		return catalog.CatalogOperation{}, "", "", err
	}
	if sessionFound {
		return sessionOp, OperationTransport(sessionOp), sessionConnection, nil
	}
	if instance == "" && staticOK {
		return staticOp, OperationTransport(staticOp), "", nil
	}
	return catalog.CatalogOperation{}, "", "", fmt.Errorf("%w: %q on provider %q", ErrOperationNotFound, operation, provName)
}

func sessionCatalogUnsupported(err error) bool {
	return err != nil && errors.Is(err, core.ErrSessionCatalogUnsupported)
}

func resolveCatalog(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, defaultConnection, instance string, strictSession bool) (*catalog.Catalog, CatalogResolutionMetadata, error) {
	meta := CatalogResolutionMetadata{}
	staticCat := prov.Catalog()

	sessionCat, attempted, err := resolveSessionCatalog(ctx, prov, provName, resolver, p, defaultConnection, instance)
	meta.SessionAttempted = attempted
	if err != nil {
		meta.SessionFailed = true
		if strictSession || staticCat == nil {
			return nil, meta, err
		}
		slog.WarnContext(ctx, "catalog session resolution failed", "provider", provName, "error", err)
	}

	merged, err := mergeCatalogs(provName, staticCat, sessionCat)
	return merged, meta, err
}

func resolveSessionCatalog(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, connection, instance string) (*catalog.Catalog, bool, error) {
	if !core.SupportsSessionCatalog(prov) {
		return nil, false, nil
	}
	boundCredential := CredentialBindingResolution{}
	if bindingResolver, ok := resolver.(EffectiveCredentialBindingResolver); ok && p != nil {
		resolvedBinding, err := bindingResolver.ResolveEffectiveCredentialBinding(p, provName, connection, instance)
		if err != nil {
			return nil, true, err
		}
		if resolvedBinding.HasBinding {
			boundCredential = resolvedBinding
			connection = boundCredential.Connection
			instance = boundCredential.Instance
		}
	}
	if effectiveConnectionMode(ctx, prov) == core.ConnectionModeNone {
		if resolver != nil && p != nil {
			enrichedCtx, token, err := ResolveTokenForBinding(ctx, resolver, p, provName, connection, instance, boundCredential)
			if err != nil {
				return nil, true, err
			}
			cat, _, err := core.CatalogForRequest(enrichedCtx, prov, token)
			return cat, true, err
		}
		ctx = WithCredentialContext(ctx, CredentialContext{Mode: core.ConnectionModeNone})
		cat, _, err := core.CatalogForRequest(ctx, prov, "")
		return cat, true, err
	}
	if resolver == nil || p == nil {
		return nil, false, nil
	}

	ctx, token, err := ResolveTokenForBinding(ctx, resolver, p, provName, connection, instance, boundCredential)
	if err != nil {
		return nil, true, err
	}
	cat, _, err := core.CatalogForRequest(ctx, prov, token)
	return cat, true, err
}

func resolveSessionOperation(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, operation string, connections []string, instance string) (catalog.CatalogOperation, string, bool, error) {
	if len(connections) == 0 {
		connections = []string{""}
	}

	var (
		firstErr error
		resolved bool
	)
	for _, connection := range connections {
		cat, attempted, err := resolveSessionCatalog(ctx, prov, provName, resolver, p, connection, instance)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if !attempted || cat == nil {
			continue
		}
		resolved = true
		if op, ok := CatalogOperation(cat, operation); ok {
			return op, connection, true, nil
		}
	}
	if firstErr != nil && !resolved {
		return catalog.CatalogOperation{}, "", false, firstErr
	}
	return catalog.CatalogOperation{}, "", false, nil
}

func mergeCatalogs(provName string, staticCat, sessionCat *catalog.Catalog) (*catalog.Catalog, error) {
	var merged *catalog.Catalog
	switch {
	case staticCat == nil && sessionCat == nil:
		return nil, fmt.Errorf("provider %q does not expose a catalog", provName)
	case staticCat != nil && sessionCat == nil:
		merged = staticCat.Clone()
	case staticCat == nil && sessionCat != nil:
		merged = sessionCat.Clone()
	default:
		merged = staticCat.Clone()
		staticIndexes := make(map[string]int, len(merged.Operations))
		for i := range merged.Operations {
			staticIndexes[merged.Operations[i].ID] = i
		}
		for i := range sessionCat.Operations {
			if idx, exists := staticIndexes[sessionCat.Operations[i].ID]; exists {
				merged.Operations[idx] = sessionCat.Operations[i]
				continue
			}
			merged.Operations = append(merged.Operations, sessionCat.Operations[i])
		}
	}

	integration.CompileSchemas(merged)
	return merged, nil
}

func FilterCatalogForPrincipal(ctx context.Context, cat *catalog.Catalog, provName string, p *principal.Principal, authorizer authorization.RuntimeAuthorizer) *catalog.Catalog {
	if cat == nil || authorizer == nil {
		return cat
	}

	filtered := cat.Clone()
	ops := filtered.Operations[:0]
	for i := range filtered.Operations {
		if authorizer.AllowCatalogOperation(ctx, p, provName, filtered.Operations[i]) {
			ops = append(ops, filtered.Operations[i])
		}
	}
	filtered.Operations = ops
	return filtered
}
