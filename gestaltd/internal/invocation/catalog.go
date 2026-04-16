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

type CatalogResolutionMetadata struct {
	SessionAttempted bool
	SessionFailed    bool
}

func ResolveCatalogWithMetadata(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, defaultConnection, instance string, strictSession bool) (*catalog.Catalog, CatalogResolutionMetadata, error) {
	meta := CatalogResolutionMetadata{}
	staticCat := prov.Catalog()
	sessionCat, attempted, err := resolveSessionCatalog(ctx, prov, provName, resolver, p, defaultConnection, instance)
	meta.SessionAttempted = attempted
	if err != nil {
		if errors.Is(err, core.ErrSessionCatalogUnavailable) && staticCat != nil {
			err = nil
		}
	}
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

func ResolveOperation(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, operation string, sessionConnections []string, instance string) (catalog.CatalogOperation, string, string, error) {
	staticOp, staticOK := CatalogOperation(providerCatalog(prov), operation)
	if op, ok := catalog.OperationFromContext(ctx, provName, operation); ok {
		if staticOK {
			op = MergeCatalogOperation(staticOp, op)
		}
		return op, OperationTransport(op), "", nil
	}
	if !core.SupportsSessionCatalog(prov) {
		if staticOK {
			return staticOp, OperationTransport(staticOp), "", nil
		}
		return catalog.CatalogOperation{}, "", "", fmt.Errorf("%w: %q on provider %q", ErrOperationNotFound, operation, provName)
	}
	sessionOp, sessionConnection, sessionFound, err := resolveSessionOperation(ctx, prov, provName, resolver, p, operation, sessionConnections, instance)
	if err != nil {
		if staticOK && errors.Is(err, core.ErrSessionCatalogUnavailable) {
			return staticOp, OperationTransport(staticOp), "", nil
		}
		return catalog.CatalogOperation{}, "", "", err
	}
	if sessionFound {
		if staticOK {
			sessionOp = MergeCatalogOperation(staticOp, sessionOp)
		}
		return sessionOp, OperationTransport(sessionOp), sessionConnection, nil
	}
	if instance == "" && staticOK {
		return staticOp, OperationTransport(staticOp), "", nil
	}
	return catalog.CatalogOperation{}, "", "", fmt.Errorf("%w: %q on provider %q", ErrOperationNotFound, operation, provName)
}

func resolveSessionCatalog(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, connection, instance string) (*catalog.Catalog, bool, error) {
	if !core.SupportsSessionCatalog(prov) || (prov.ConnectionMode() != core.ConnectionModeNone && (resolver == nil || p == nil)) {
		return nil, false, nil
	}
	token := ""
	if resolver != nil && p != nil {
		var err error
		ctx, token, err = resolver.ResolveToken(ctx, p, provName, connection, instance)
		if err != nil {
			return nil, true, err
		}
	} else {
		ctx = WithCredentialContext(ctx, CredentialContext{Mode: core.ConnectionModeNone})
	}
	cat, _, err := core.CatalogForRequest(ctx, prov, token)
	return cat, true, err
}

func resolveSessionOperation(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, operation string, connections []string, instance string) (catalog.CatalogOperation, string, bool, error) {
	if len(connections) == 0 {
		connections = []string{""}
	}
	firstErr, resolved := error(nil), false
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
	merged := staticCat
	if merged == nil {
		merged = sessionCat
	}
	if merged == nil {
		return nil, fmt.Errorf("provider %q does not expose a catalog", provName)
	}
	merged = merged.Clone()
	if staticCat != nil && sessionCat != nil {
		staticIndexes := make(map[string]int, len(merged.Operations))
		for i := range merged.Operations {
			staticIndexes[merged.Operations[i].ID] = i
		}
		for i := range sessionCat.Operations {
			op := sessionCat.Operations[i]
			if idx, exists := staticIndexes[op.ID]; exists {
				merged.Operations[idx] = MergeCatalogOperation(merged.Operations[idx], op)
				continue
			}
			merged.Operations = append(merged.Operations, op)
		}
	}
	integration.CompileSchemas(merged)
	return merged, nil
}

func MergeCatalogOperation(staticOp, sessionOp catalog.CatalogOperation) catalog.CatalogOperation {
	sessionMethod := sessionOp.Method
	sessionPath := sessionOp.Path
	if sessionOp.Transport == "" && sessionMethod == "" && sessionPath == "" {
		sessionOp.Transport = staticOp.Transport
	}
	sessionOp.Method = firstNonEmpty(sessionMethod, staticOp.Method)
	sessionOp.Path = firstNonEmpty(sessionPath, staticOp.Path)
	sessionOp.Query = firstNonEmpty(sessionOp.Query, staticOp.Query)
	if len(staticOp.Parameters) == 0 {
		return sessionOp
	}
	if len(sessionOp.Parameters) == 0 {
		sessionOp.Parameters = append([]catalog.CatalogParameter(nil), staticOp.Parameters...)
		return sessionOp
	}
	staticParams := make(map[string]catalog.CatalogParameter, len(staticOp.Parameters))
	for _, param := range staticOp.Parameters {
		staticParams[param.Name] = param
	}
	for i := range sessionOp.Parameters {
		if staticParam, ok := staticParams[sessionOp.Parameters[i].Name]; ok {
			delete(staticParams, sessionOp.Parameters[i].Name)
			sessionOp.Parameters[i].WireName = firstNonEmpty(sessionOp.Parameters[i].WireName, staticParam.WireName)
			sessionOp.Parameters[i].Location = firstNonEmpty(sessionOp.Parameters[i].Location, staticParam.Location)
		}
	}
	for _, staticParam := range staticOp.Parameters {
		if _, ok := staticParams[staticParam.Name]; ok {
			sessionOp.Parameters = append(sessionOp.Parameters, staticParam)
		}
	}
	return sessionOp
}

func firstNonEmpty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func FilterCatalogForPrincipal(cat *catalog.Catalog, provName string, p *principal.Principal, authorizer *authorization.Authorizer) *catalog.Catalog {
	if cat == nil || authorizer == nil {
		return cat
	}
	filtered := cat.Clone()
	ops := filtered.Operations[:0]
	for i := range filtered.Operations {
		if authorizer.AllowCatalogOperation(p, provName, filtered.Operations[i]) {
			ops = append(ops, filtered.Operations[i])
		}
	}
	filtered.Operations = ops
	return filtered
}
