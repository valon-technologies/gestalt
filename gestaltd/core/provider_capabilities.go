package core

import (
	"cmp"
	"context"
	"maps"

	"github.com/valon-technologies/gestalt/server/core/catalog"
)

func SupportsSessionCatalog(prov Provider) bool {
	_, ok := prov.(SessionCatalogProvider)
	return ok
}

func CatalogForRequest(ctx context.Context, prov Provider, token string) (*catalog.Catalog, error) {
	scp, ok := prov.(SessionCatalogProvider)
	if !ok {
		return nil, nil
	}
	cat, err := scp.CatalogForRequest(ctx, token)
	return HydrateSessionCatalog(prov.Catalog(), cat), err
}

func HydrateSessionCatalog(staticCat, sessionCat *catalog.Catalog) *catalog.Catalog {
	if sessionCat == nil {
		return nil
	}
	merged := sessionCat.Clone()
	if staticCat == nil {
		return merged
	}
	merged.Name = cmp.Or(merged.Name, staticCat.Name)
	merged.DisplayName = cmp.Or(merged.DisplayName, staticCat.DisplayName)
	merged.Description = cmp.Or(merged.Description, staticCat.Description)
	merged.IconSVG = cmp.Or(merged.IconSVG, staticCat.IconSVG)
	merged.BaseURL = cmp.Or(merged.BaseURL, staticCat.BaseURL)
	merged.AuthStyle = cmp.Or(merged.AuthStyle, staticCat.AuthStyle)
	if len(staticCat.Headers) > 0 {
		headers := maps.Clone(staticCat.Headers)
		maps.Copy(headers, merged.Headers)
		merged.Headers = headers
	}
	for i := range merged.Operations {
		for j := range staticCat.Operations {
			if staticCat.Operations[j].ID == merged.Operations[i].ID {
				merged.Operations[i] = hydrateSessionOperation(staticCat.Operations[j], merged.Operations[i])
				break
			}
		}
	}
	return merged
}

func hydrateSessionOperation(staticOp, sessionOp catalog.CatalogOperation) catalog.CatalogOperation {
	if sessionOp.Transport == "" && sessionOp.Path == "" && sessionOp.Query == "" {
		sessionOp.Transport = staticOp.Transport
	}
	sessionOp.Method = cmp.Or(sessionOp.Method, staticOp.Method)
	sessionOp.Path = cmp.Or(sessionOp.Path, staticOp.Path)
	sessionOp.Query = cmp.Or(sessionOp.Query, staticOp.Query)
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
			sessionOp.Parameters[i].WireName = cmp.Or(sessionOp.Parameters[i].WireName, staticParam.WireName)
			sessionOp.Parameters[i].Location = cmp.Or(sessionOp.Parameters[i].Location, staticParam.Location)
		}
	}
	for _, staticParam := range staticOp.Parameters {
		if _, ok := staticParams[staticParam.Name]; ok {
			sessionOp.Parameters = append(sessionOp.Parameters, staticParam)
		}
	}
	return sessionOp
}
