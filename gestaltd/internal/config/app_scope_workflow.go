package config

import (
	"maps"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

func workflowNodeInAppClosure(workflowNode *yaml.Node, keepApps map[string]struct{}) bool {
	for _, stepNode := range workflowStepNodesFromWorkflowNode(workflowNode) {
		appName := scalarStringNode(mappingValueNode(mappingValueNode(stepNode, "app"), "name"))
		if appName == "" {
			continue
		}
		if _, ok := keepApps[appName]; ok {
			return true
		}
	}
	return false
}

func workflowRefsFromNode(workflowNode *yaml.Node) workflowAppRefs {
	refs := workflowAppRefs{}
	for _, stepNode := range workflowStepNodesFromWorkflowNode(workflowNode) {
		refs.Add(scalarStringNode(mappingValueNode(mappingValueNode(stepNode, "app"), "name")))
		agentNode := mappingValueNode(stepNode, "agent")
		if toolsNode := mappingValueNode(agentNode, "tools"); toolsNode != nil && toolsNode.Kind == yaml.SequenceNode {
			for _, tool := range toolsNode.Content {
				refs.Add(scalarStringNode(mappingValueNode(tool, "app")))
			}
		}
	}
	return refs
}

func workflowStepNodesFromWorkflowNode(workflowNode *yaml.Node) []*yaml.Node {
	stepsNode := mappingValueNode(workflowNode, "steps")
	if stepsNode == nil || stepsNode.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]*yaml.Node, 0, len(stepsNode.Content))
	for _, stepNode := range stepsNode.Content {
		if stepNode != nil && stepNode.Kind == yaml.MappingNode {
			out = append(out, stepNode)
		}
	}
	return out
}

type workflowAppRefs map[string]struct{}

func (r workflowAppRefs) Add(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	r[name] = struct{}{}
}

func (r workflowAppRefs) Names() []string {
	names := slices.Sorted(maps.Keys(r))
	return names
}

func workflowsTargetingAppClosure(cfg *Config, keepApps map[string]struct{}) map[string]workflowAppRefs {
	if cfg == nil {
		return nil
	}
	refsByName := map[string]workflowAppRefs{}
	for name := range cfg.Workflows.Definitions {
		definition := cfg.Workflows.Definitions[name]
		if workflowStepsInAppClosure(definition.Steps, keepApps) {
			refsByName["definition:"+name] = workflowDefinitionAppRefs(definition)
		}
	}
	return refsByName
}

func workflowStepsInAppClosure(steps []WorkflowStepConfig, keepApps map[string]struct{}) bool {
	for i := range steps {
		if steps[i].App == nil {
			continue
		}
		if _, ok := keepApps[strings.TrimSpace(steps[i].App.Name)]; ok {
			return true
		}
	}
	return false
}

func filterWorkflowConfig(workflows *WorkflowsConfig, keep map[string]workflowAppRefs) {
	if workflows == nil {
		return
	}
	if len(workflows.Definitions) > 0 {
		filtered := make(map[string]WorkflowDefinitionConfig)
		for name := range workflows.Definitions {
			if _, ok := keep["definition:"+name]; ok {
				filtered[name] = workflows.Definitions[name]
			}
		}
		workflows.Definitions = filtered
	}
}

func workflowDefinitionAppRefs(definition WorkflowDefinitionConfig) workflowAppRefs {
	refs := workflowAppRefs{}
	addWorkflowStepRefs(refs, definition.Steps)
	return refs
}

func addWorkflowStepRefs(refs workflowAppRefs, steps []WorkflowStepConfig) {
	for i := range steps {
		step := &steps[i]
		if step.App != nil {
			refs.Add(step.App.Name)
		}
		if step.Agent != nil {
			for _, tool := range step.Agent.Tools {
				refs.Add(tool.App)
			}
		}
	}
}
