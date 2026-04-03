package invocation

import (
	"bytes"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
)

func capabilitiesForProvider(name string, prov core.Provider) []core.Capability {
	if cat := providerCatalog(prov); cat != nil {
		return capabilitiesFromCatalog(name, cat)
	}
	return nil
}

func capabilitiesFromCatalog(name string, cat *catalog.Catalog) []core.Capability {
	caps := make([]core.Capability, 0, len(cat.Operations))
	for i := range cat.Operations {
		op := cat.Operations[i]
		if strings.TrimSpace(op.ID) == "" {
			continue
		}

		params := make([]core.Parameter, 0, len(op.Parameters))
		for _, p := range op.Parameters {
			params = append(params, core.Parameter{
				Name:        p.Name,
				Type:        p.Type,
				Description: p.Description,
				Required:    p.Required,
				Default:     p.Default,
			})
		}

		method := strings.ToUpper(strings.TrimSpace(op.Method))
		transport := strings.TrimSpace(op.Transport)
		if transport == "" && method != "" {
			transport = catalog.TransportREST
		}

		caps = append(caps, core.Capability{
			Provider:    name,
			Operation:   op.ID,
			Title:       op.Title,
			Description: op.Description,
			Parameters:  params,
			InputSchema: bytes.Clone(op.InputSchema),
			Method:      method,
			Transport:   transport,
			Annotations: core.CapabilityAnnotations{
				ReadOnlyHint:    op.Annotations.ReadOnlyHint,
				IdempotentHint:  op.Annotations.IdempotentHint,
				DestructiveHint: op.Annotations.DestructiveHint,
				OpenWorldHint:   op.Annotations.OpenWorldHint,
			},
		})
	}
	return caps
}

func providerCatalog(prov core.Provider) *catalog.Catalog {
	return prov.Catalog()
}

func CatalogOperationTransport(cat *catalog.Catalog, operation string) (string, bool) {
	if cat == nil || strings.TrimSpace(operation) == "" {
		return "", false
	}
	for i := range cat.Operations {
		if cat.Operations[i].ID == operation {
			transport := strings.TrimSpace(cat.Operations[i].Transport)
			if transport == "" && strings.TrimSpace(cat.Operations[i].Method) != "" {
				transport = catalog.TransportREST
			}
			return transport, true
		}
	}
	return "", false
}
