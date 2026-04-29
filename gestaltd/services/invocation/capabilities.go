package invocation

import (
	"bytes"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	integration "github.com/valon-technologies/gestalt/server/services/plugins/declarative"
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

		method := strings.ToUpper(strings.TrimSpace(op.Method))
		transport := OperationTransport(op)

		caps = append(caps, core.Capability{
			Provider:    name,
			Operation:   op.ID,
			Title:       op.Title,
			Description: op.Description,
			Parameters:  integration.ConvertParameters(op.Parameters),
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

func CatalogOperation(cat *catalog.Catalog, operation string) (catalog.CatalogOperation, bool) {
	if cat == nil || strings.TrimSpace(operation) == "" {
		return catalog.CatalogOperation{}, false
	}
	for i := range cat.Operations {
		if cat.Operations[i].ID == operation {
			return cat.Operations[i], true
		}
	}
	return catalog.CatalogOperation{}, false
}

func CatalogOperationTransport(cat *catalog.Catalog, operation string) (string, bool) {
	op, ok := CatalogOperation(cat, operation)
	if !ok {
		return "", false
	}
	return OperationTransport(op), true
}

func OperationTransport(op catalog.CatalogOperation) string {
	transport := strings.TrimSpace(op.Transport)
	if transport == "" && strings.TrimSpace(op.Method) != "" {
		return catalog.TransportREST
	}
	return transport
}
