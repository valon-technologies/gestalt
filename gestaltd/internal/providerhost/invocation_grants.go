package providerhost

import (
	"strings"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/internal/config"
)

func InvocationDependencyGrants(deps []config.PluginInvocationDependency) map[string]map[string]struct{} {
	if len(deps) == 0 {
		return nil
	}
	grants := make(map[string]map[string]struct{}, len(deps))
	for _, dep := range deps {
		plugin := strings.TrimSpace(dep.Plugin)
		operation := strings.TrimSpace(dep.Operation)
		if plugin == "" || operation == "" {
			continue
		}
		ops := grants[plugin]
		if ops == nil {
			ops = make(map[string]struct{})
			grants[plugin] = ops
		}
		ops[operation] = struct{}{}
	}
	if len(grants) == 0 {
		return nil
	}
	return grants
}

func decodePluginInvocationGrantProto(grants []*proto.PluginInvocationGrant) map[string]map[string]struct{} {
	if len(grants) == 0 {
		return nil
	}
	decoded := make(map[string]map[string]struct{}, len(grants))
	for _, grant := range grants {
		if grant == nil {
			continue
		}
		plugin := strings.TrimSpace(grant.GetPlugin())
		if plugin == "" {
			continue
		}
		ops := decoded[plugin]
		if ops == nil {
			ops = make(map[string]struct{}, len(grant.GetOperations()))
			decoded[plugin] = ops
		}
		for _, operation := range grant.GetOperations() {
			operation = strings.TrimSpace(operation)
			if operation == "" {
				continue
			}
			ops[operation] = struct{}{}
		}
	}
	if len(decoded) == 0 {
		return nil
	}
	return decoded
}

func cloneOperationMap(src map[string]map[string]struct{}) map[string]map[string]struct{} {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]map[string]struct{}, len(src))
	for plugin, ops := range src {
		if len(ops) == 0 {
			out[plugin] = map[string]struct{}{}
			continue
		}
		cloned := make(map[string]struct{}, len(ops))
		for operation := range ops {
			cloned[operation] = struct{}{}
		}
		out[plugin] = cloned
	}
	return out
}
