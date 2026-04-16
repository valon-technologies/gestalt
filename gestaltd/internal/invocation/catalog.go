package invocation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/integration"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

type TokenResolver interface {
	ResolveToken(ctx context.Context, p *principal.Principal, providerName, connection, instance string) (context.Context, string, error)
}

func ResolveCatalogWithMetadata(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, defaultConnection, instance string, strictSession bool) (*catalog.Catalog, bool, error) {
	staticCat := prov.Catalog()
	sessionCat, attempted, err := resolveSessionCatalog(ctx, prov, provName, resolver, p, defaultConnection, instance)
	sessionFailed := attempted && err != nil
	if err != nil {
		if errors.Is(err, core.ErrSessionCatalogUnavailable) && staticCat != nil {
			err = nil
			sessionFailed = false
		}
	}
	if err != nil {
		if strictSession || staticCat == nil {
			return nil, sessionFailed, err
		}
		slog.WarnContext(ctx, "catalog session resolution failed", "provider", provName, "error", err)
	}
	merged, err := mergeCatalogs(provName, staticCat, sessionCat)
	return merged, sessionFailed, err
}

func ResolveOperation(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, operation string, sessionConnections []string, instance string) (catalog.CatalogOperation, string, string, error) {
	staticOp, staticOK := CatalogOperation(providerCatalog(prov), operation)
	if op, ok := catalog.OperationFromContext(ctx, provName, operation); ok {
		return op, OperationTransport(op), "", nil
	}
	sessionOp, sessionConnection, sessionFound, err := resolveSessionOperation(ctx, prov, provName, resolver, p, operation, sessionConnections, instance)
	if err != nil {
		if staticOK && errors.Is(err, core.ErrSessionCatalogUnavailable) {
			return staticOp, OperationTransport(staticOp), "", nil
		}
		return catalog.CatalogOperation{}, "", "", err
	}
	if sessionFound {
		return sessionOp, OperationTransport(sessionOp), sessionConnection, nil
	}
	if staticOK && (!core.SupportsSessionCatalog(prov) || instance == "") {
		return staticOp, OperationTransport(staticOp), "", nil
	}
	return catalog.CatalogOperation{}, "", "", fmt.Errorf("%w: %q on provider %q", ErrOperationNotFound, operation, provName)
}

func resolveSessionCatalog(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, connection, instance string) (*catalog.Catalog, bool, error) {
	scp, ok := prov.(core.SessionCatalogProvider)
	if !ok || (prov.ConnectionMode() != core.ConnectionModeNone && (resolver == nil || p == nil)) {
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
	cat, err := scp.CatalogForRequest(ctx, token)
	return HydrateSessionCatalog(prov.Catalog(), cat), true, err
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
	if sessionCat == nil && staticCat == nil {
		return nil, fmt.Errorf("provider %q does not expose a catalog", provName)
	}
	if staticCat == nil {
		merged := sessionCat.Clone()
		integration.CompileSchemas(merged)
		return merged, nil
	}
	merged := staticCat.Clone()
	if sessionCat != nil {
		staticIndexes := make(map[string]int, len(merged.Operations))
		for i := range merged.Operations {
			staticIndexes[merged.Operations[i].ID] = i
		}
		for i := range sessionCat.Operations {
			op := sessionCat.Operations[i]
			if idx, exists := staticIndexes[op.ID]; exists {
				merged.Operations[idx] = op
				continue
			}
			merged.Operations = append(merged.Operations, op)
		}
	}
	integration.CompileSchemas(merged)
	return merged, nil
}
func HydrateSessionCatalog(staticCat, sessionCat *catalog.Catalog) *catalog.Catalog {
	if sessionCat == nil {
		return nil
	}
	merged := sessionCat.Clone()
	if staticCat == nil {
		return merged
	}
	merged.Name = firstNonEmpty(merged.Name, staticCat.Name)
	merged.DisplayName = firstNonEmpty(merged.DisplayName, staticCat.DisplayName)
	merged.Description = firstNonEmpty(merged.Description, staticCat.Description)
	merged.IconSVG = firstNonEmpty(merged.IconSVG, staticCat.IconSVG)
	merged.BaseURL = firstNonEmpty(merged.BaseURL, staticCat.BaseURL)
	merged.AuthStyle = firstNonEmpty(merged.AuthStyle, staticCat.AuthStyle)
	if len(staticCat.Headers) > 0 {
		headers := maps.Clone(staticCat.Headers)
		maps.Copy(headers, merged.Headers)
		merged.Headers = headers
	}
	for i := range merged.Operations {
		if staticOp, ok := CatalogOperation(staticCat, merged.Operations[i].ID); ok {
			merged.Operations[i] = MergeCatalogOperation(staticOp, merged.Operations[i])
		}
	}
	return merged
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
