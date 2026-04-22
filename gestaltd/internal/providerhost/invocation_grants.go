package providerhost

import (
	"slices"
	"strings"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/internal/config"
)

type invocationGrant struct {
	AllOperations bool
	Operations    map[string]struct{}
	Surfaces      map[string]struct{}
}

type invocationGrants map[string]invocationGrant

type InvocationGrant = invocationGrant
type InvocationGrants = invocationGrants

type invocationGrantClaims struct {
	AllOperations bool     `json:"all_operations,omitempty"`
	Operations    []string `json:"operations,omitempty"`
	Surfaces      []string `json:"surfaces,omitempty"`
}

func InvocationDependencyGrants(deps []config.PluginInvocationDependency) InvocationGrants {
	if len(deps) == 0 {
		return nil
	}
	grants := make(invocationGrants, len(deps))
	for _, dep := range deps {
		plugin := strings.TrimSpace(dep.Plugin)
		operation := strings.TrimSpace(dep.Operation)
		surface := strings.ToLower(strings.TrimSpace(dep.Surface))
		if plugin == "" || (operation == "" && surface == "") {
			continue
		}
		grant := grants[plugin]
		if operation != "" {
			if grant.Operations == nil {
				grant.Operations = make(map[string]struct{})
			}
			grant.Operations[operation] = struct{}{}
		}
		if surface != "" {
			if grant.Surfaces == nil {
				grant.Surfaces = make(map[string]struct{})
			}
			grant.Surfaces[surface] = struct{}{}
		}
		grants[plugin] = grant
	}
	if len(grants) == 0 {
		return nil
	}
	return grants
}

func decodePluginInvocationGrantProto(grants []*proto.PluginInvocationGrant) invocationGrants {
	if len(grants) == 0 {
		return nil
	}
	decoded := make(invocationGrants, len(grants))
	for _, grant := range grants {
		if grant == nil {
			continue
		}
		plugin := strings.TrimSpace(grant.GetPlugin())
		if plugin == "" {
			continue
		}
		decodedGrant := decoded[plugin]
		if grant.GetAllOperations() {
			decodedGrant.AllOperations = true
		}
		for _, operation := range grant.GetOperations() {
			operation = strings.TrimSpace(operation)
			if operation == "" {
				continue
			}
			if decodedGrant.Operations == nil {
				decodedGrant.Operations = make(map[string]struct{})
			}
			decodedGrant.Operations[operation] = struct{}{}
		}
		for _, surface := range grant.GetSurfaces() {
			surface = strings.ToLower(strings.TrimSpace(surface))
			if surface == "" {
				continue
			}
			if decodedGrant.Surfaces == nil {
				decodedGrant.Surfaces = make(map[string]struct{})
			}
			decodedGrant.Surfaces[surface] = struct{}{}
		}
		if !decodedGrant.AllOperations && len(decodedGrant.Operations) == 0 && len(decodedGrant.Surfaces) == 0 {
			// Preserve existing behavior for older clients that represented
			// wildcard grants as a plugin entry with no explicit operations.
			decodedGrant.AllOperations = true
		}
		decoded[plugin] = decodedGrant
	}
	if len(decoded) == 0 {
		return nil
	}
	return decoded
}

func cloneInvocationGrants(src invocationGrants) invocationGrants {
	if len(src) == 0 {
		return nil
	}
	out := make(invocationGrants, len(src))
	for plugin, grant := range src {
		cloned := invocationGrant{
			AllOperations: grant.AllOperations,
		}
		if len(grant.Operations) > 0 {
			cloned.Operations = make(map[string]struct{}, len(grant.Operations))
			for operation := range grant.Operations {
				cloned.Operations[operation] = struct{}{}
			}
		}
		if len(grant.Surfaces) > 0 {
			cloned.Surfaces = make(map[string]struct{}, len(grant.Surfaces))
			for surface := range grant.Surfaces {
				cloned.Surfaces[surface] = struct{}{}
			}
		}
		out[plugin] = cloned
	}
	return out
}

func encodeInvocationGrantClaims(src invocationGrants) map[string]invocationGrantClaims {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]invocationGrantClaims, len(src))
	for plugin, grant := range src {
		out[plugin] = invocationGrantClaims{
			AllOperations: grant.AllOperations,
			Operations:    sortedGrantKeys(grant.Operations),
			Surfaces:      sortedGrantKeys(grant.Surfaces),
		}
	}
	return out
}

func decodeInvocationGrantClaims(src map[string]invocationGrantClaims) invocationGrants {
	if len(src) == 0 {
		return nil
	}
	out := make(invocationGrants, len(src))
	for plugin, grant := range src {
		decoded := invocationGrant{
			AllOperations: grant.AllOperations,
		}
		for _, operation := range grant.Operations {
			operation = strings.TrimSpace(operation)
			if operation == "" {
				continue
			}
			if decoded.Operations == nil {
				decoded.Operations = make(map[string]struct{})
			}
			decoded.Operations[operation] = struct{}{}
		}
		for _, surface := range grant.Surfaces {
			surface = strings.ToLower(strings.TrimSpace(surface))
			if surface == "" {
				continue
			}
			if decoded.Surfaces == nil {
				decoded.Surfaces = make(map[string]struct{})
			}
			decoded.Surfaces[surface] = struct{}{}
		}
		if !decoded.AllOperations && len(decoded.Operations) == 0 && len(decoded.Surfaces) == 0 {
			decoded.AllOperations = true
		}
		out[plugin] = decoded
	}
	return out
}

func invocationGrantSubset(candidate, allowed invocationGrants) bool {
	if len(candidate) == 0 {
		return true
	}
	for plugin, grant := range candidate {
		allowedGrant, ok := allowed[plugin]
		if !ok {
			return false
		}
		if grant.AllOperations && !allowedGrant.AllOperations {
			return false
		}
		if len(grant.Operations) > 0 && !allowedGrant.AllOperations {
			for operation := range grant.Operations {
				if _, ok := allowedGrant.Operations[operation]; !ok {
					return false
				}
			}
		}
		for surface := range grant.Surfaces {
			if _, ok := allowedGrant.Surfaces[surface]; !ok {
				return false
			}
		}
	}
	return true
}

func allowsOperation(grants invocationGrants, plugin, operation string) bool {
	if len(grants) == 0 {
		return false
	}
	grant, ok := grants[plugin]
	if !ok {
		return false
	}
	if grant.AllOperations {
		return true
	}
	_, ok = grant.Operations[operation]
	return ok
}

func allowsSurface(grants invocationGrants, plugin, surface string) bool {
	if len(grants) == 0 {
		return false
	}
	grant, ok := grants[plugin]
	if !ok {
		return false
	}
	_, ok = grant.Surfaces[strings.ToLower(strings.TrimSpace(surface))]
	return ok
}

func sortedGrantKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
