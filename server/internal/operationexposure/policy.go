package operationexposure

import (
	"fmt"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coreintegration "github.com/valon-technologies/gestalt/server/core/integration"
	"github.com/valon-technologies/gestalt/server/internal/config"
)

// Policy normalizes allowed_operations handling so every provider type uses the
// same validation, aliasing, and description override behavior.
type Policy struct {
	exposedToOriginal map[string]string
	originalToExposed map[string]string
	descriptions      map[string]string
}

func New(allowed map[string]*config.OperationOverride) (*Policy, error) {
	if allowed == nil {
		return nil, nil
	}
	if len(allowed) == 0 {
		return nil, fmt.Errorf("allowed_operations cannot be empty; omit the field to allow all")
	}

	policy := &Policy{
		exposedToOriginal: make(map[string]string, len(allowed)),
		originalToExposed: make(map[string]string, len(allowed)),
	}
	exposedNames := make(map[string]string, len(allowed))
	var collisions []string

	for original, override := range allowed {
		exposed := original
		if override != nil && override.Alias != "" {
			exposed = override.Alias
		}
		if existing, ok := exposedNames[exposed]; ok {
			collisions = append(collisions, fmt.Sprintf("%q and %q both resolve to %q", existing, original, exposed))
		}
		exposedNames[exposed] = original
		policy.exposedToOriginal[exposed] = original
		policy.originalToExposed[original] = exposed
		if override != nil && override.Description != "" {
			if policy.descriptions == nil {
				policy.descriptions = make(map[string]string)
			}
			policy.descriptions[exposed] = override.Description
		}
	}

	if len(collisions) > 0 {
		return nil, fmt.Errorf("alias collisions: %s", strings.Join(collisions, "; "))
	}

	return policy, nil
}

func (p *Policy) Validate(ops []core.Operation) error {
	if p == nil {
		return nil
	}

	opSet := make(map[string]struct{}, len(ops))
	for _, op := range ops {
		opSet[op.Name] = struct{}{}
	}
	for original := range p.originalToExposed {
		if _, ok := opSet[original]; !ok {
			return fmt.Errorf("allowed_operations contains unknown operation %q", original)
		}
	}

	return nil
}

func (p *Policy) Resolve(name string) (string, bool) {
	if p == nil {
		return name, true
	}
	original, ok := p.exposedToOriginal[name]
	return original, ok
}

func (p *Policy) Wrap(prov core.Provider) core.Provider {
	if p == nil {
		return prov
	}

	var opts []coreintegration.RestrictedOption
	if len(p.descriptions) > 0 {
		opts = append(opts, coreintegration.WithDescriptions(p.DescriptionOverrides()))
	}
	return coreintegration.NewRestricted(prov, p.RestrictedMap(), opts...)
}

func (p *Policy) RestrictedMap() map[string]string {
	if p == nil {
		return nil
	}

	restricted := make(map[string]string, len(p.exposedToOriginal))
	for exposed, original := range p.exposedToOriginal {
		if exposed == original {
			restricted[exposed] = ""
			continue
		}
		restricted[exposed] = original
	}
	return restricted
}

func (p *Policy) DescriptionOverrides() map[string]string {
	if len(p.descriptions) == 0 {
		return nil
	}

	descriptions := make(map[string]string, len(p.descriptions))
	for exposed, description := range p.descriptions {
		descriptions[exposed] = description
	}
	return descriptions
}

func (p *Policy) ApplyOperations(ops []core.Operation) []core.Operation {
	if p == nil {
		return slices.Clone(ops)
	}

	filtered := make([]core.Operation, 0, len(p.exposedToOriginal))
	for _, op := range ops {
		exposed, ok := p.originalToExposed[op.Name]
		if !ok {
			continue
		}
		op.Name = exposed
		if description, ok := p.descriptions[exposed]; ok {
			op.Description = description
		}
		filtered = append(filtered, op)
	}
	return filtered
}

func (p *Policy) ApplyCatalog(cat *catalog.Catalog) *catalog.Catalog {
	if cat == nil {
		return nil
	}
	if p == nil {
		return cat.Clone()
	}

	filtered := cat.Clone()
	filtered.Operations = make([]catalog.CatalogOperation, 0, len(p.exposedToOriginal))
	for i := range cat.Operations {
		op := cat.Operations[i]
		exposed, ok := p.originalToExposed[op.ID]
		if !ok {
			continue
		}
		op.ID = exposed
		if description, ok := p.descriptions[exposed]; ok {
			op.Description = description
		}
		filtered.Operations = append(filtered.Operations, op)
	}
	return filtered
}
