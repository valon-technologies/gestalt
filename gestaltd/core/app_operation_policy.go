package core

import (
	"context"
	"slices"

	"github.com/valon-technologies/gestalt/server/core/catalog"
)

// AppOperationPolicy contains permission changes, never operation definitions.
// An absent key inherits the catalog's roles; an empty role list disables the
// operation. A single map cannot both enable and remove the same operation.
type AppOperationPolicy map[string][]string

type AppOperationPolicyStore interface {
	// GetAppOperationPolicy returns an empty policy when no changes exist.
	GetAppOperationPolicy(context.Context, string) (AppOperationPolicy, error)
}

func (p AppOperationPolicy) Resolve(id string, baseline []string) ([]string, bool) {
	roles, changed := p[id]
	if !changed {
		return baseline, true
	}
	return roles, len(roles) > 0
}

// Catalog preserves provider metadata and never mutates the provider's catalog.
func (p AppOperationPolicy) Catalog(baseline *catalog.Catalog) *catalog.Catalog {
	if baseline == nil || len(p) == 0 {
		return baseline
	}
	result := baseline.Clone()
	operations := result.Operations
	result.Operations = operations[:0]
	for i := range operations {
		operation := operations[i]
		roles, allowed := p.Resolve(operation.ID, operation.AllowedRoles)
		if allowed {
			operation.AllowedRoles = slices.Clone(roles)
			result.Operations = append(result.Operations, operation)
		}
	}
	return result
}
