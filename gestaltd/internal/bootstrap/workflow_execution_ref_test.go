package bootstrap

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
)

func workflowExecutionRefPermissionsForTarget(target coreworkflow.Target) []core.AccessPermission {
	if target.Agent != nil {
		out := make([]core.AccessPermission, 0, len(target.Agent.ToolRefs))
		for i := range target.Agent.ToolRefs {
			tool := target.Agent.ToolRefs[i]
			pluginName := strings.TrimSpace(tool.Plugin)
			operation := strings.TrimSpace(tool.Operation)
			if pluginName == "" || operation == "" {
				continue
			}
			out = append(out, core.AccessPermission{
				Plugin:     pluginName,
				Operations: []string{operation},
			})
		}
		return out
	}
	if target.Plugin == nil {
		return nil
	}
	pluginName := strings.TrimSpace(target.Plugin.PluginName)
	operation := strings.TrimSpace(target.Plugin.Operation)
	if pluginName == "" || operation == "" {
		return nil
	}
	return []core.AccessPermission{{
		Plugin:     pluginName,
		Operations: []string{operation},
	}}
}
