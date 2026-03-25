package invocation

import (
	"strings"

	"github.com/valon-technologies/gestalt/core"
	ci "github.com/valon-technologies/gestalt/core/integration"
)

func capabilitiesForProvider(name string, prov core.Provider) []core.Capability {
	if cp, ok := prov.(core.CatalogProvider); ok {
		if cat := cp.Catalog(); cat != nil {
			return capabilitiesFromOperations(name, ci.OperationsList(cat))
		}
	}
	return capabilitiesFromOperations(name, prov.ListOperations())
}

func capabilitiesFromOperations(name string, ops []core.Operation) []core.Capability {
	caps := make([]core.Capability, 0, len(ops))
	for _, op := range ops {
		if strings.TrimSpace(op.Method) == "" {
			continue
		}
		caps = append(caps, core.Capability{
			Provider:    name,
			Operation:   op.Name,
			Description: op.Description,
			Parameters:  op.Parameters,
		})
	}
	return caps
}
