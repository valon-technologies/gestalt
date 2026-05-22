package config

import (
	"maps"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

func workflowTargetNodeInAppClosure(targetNode *yaml.Node, keepApps map[string]struct{}) bool {
	for _, stepNode := range workflowStepNodesFromTargetNode(targetNode) {
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
	targetNode := mappingValueNode(workflowNode, "target")
	for _, stepNode := range workflowStepNodesFromTargetNode(targetNode) {
		refs.Add(scalarStringNode(mappingValueNode(mappingValueNode(stepNode, "app"), "name")))
		agentNode := mappingValueNode(stepNode, "agent")
		if toolsNode := mappingValueNode(agentNode, "tools"); toolsNode != nil && toolsNode.Kind == yaml.SequenceNode {
			for _, tool := range toolsNode.Content {
				refs.Add(scalarStringNode(mappingValueNode(tool, "app")))
			}
		}
	}
	if invokesNode := mappingValueNode(workflowNode, "invokes"); invokesNode != nil && invokesNode.Kind == yaml.SequenceNode {
		for _, invoke := range invokesNode.Content {
			refs.Add(scalarStringNode(mappingValueNode(invoke, "app")))
		}
	}
	return refs
}

func workflowStepNodesFromTargetNode(targetNode *yaml.Node) []*yaml.Node {
	stepsNode := mappingValueNode(targetNode, "steps")
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
	for name := range cfg.Workflows.Schedules {
		schedule := cfg.Workflows.Schedules[name]
		if workflowTargetInAppClosure(schedule.Target, keepApps) {
			refsByName["schedule:"+name] = workflowScheduleAppRefs(schedule)
		}
	}
	for name := range cfg.Workflows.EventTriggers {
		trigger := cfg.Workflows.EventTriggers[name]
		if workflowTargetInAppClosure(trigger.Target, keepApps) {
			refsByName["trigger:"+name] = workflowEventTriggerAppRefs(trigger)
		}
	}
	return refsByName
}

func workflowTargetInAppClosure(target *WorkflowTargetConfig, keepApps map[string]struct{}) bool {
	if target == nil {
		return false
	}
	for i := range target.Steps {
		if target.Steps[i].App == nil {
			continue
		}
		if _, ok := keepApps[strings.TrimSpace(target.Steps[i].App.Name)]; ok {
			return true
		}
	}
	return false
}

func filterWorkflowConfig(workflows *WorkflowsConfig, keep map[string]workflowAppRefs) {
	if workflows == nil {
		return
	}
	if len(workflows.Schedules) > 0 {
		filtered := make(map[string]WorkflowScheduleConfig)
		for name := range workflows.Schedules {
			if _, ok := keep["schedule:"+name]; ok {
				filtered[name] = workflows.Schedules[name]
			}
		}
		workflows.Schedules = filtered
	}
	if len(workflows.EventTriggers) > 0 {
		filtered := make(map[string]WorkflowEventTriggerConfig)
		for name := range workflows.EventTriggers {
			if _, ok := keep["trigger:"+name]; ok {
				filtered[name] = workflows.EventTriggers[name]
			}
		}
		workflows.EventTriggers = filtered
	}
}

func workflowScheduleAppRefs(schedule WorkflowScheduleConfig) workflowAppRefs {
	refs := workflowAppRefs{}
	addWorkflowTargetRefs(refs, schedule.Target)
	addWorkflowInvokeRefs(refs, schedule.Invokes)
	return refs
}

func workflowEventTriggerAppRefs(trigger WorkflowEventTriggerConfig) workflowAppRefs {
	refs := workflowAppRefs{}
	addWorkflowTargetRefs(refs, trigger.Target)
	addWorkflowInvokeRefs(refs, trigger.Invokes)
	return refs
}

func addWorkflowTargetRefs(refs workflowAppRefs, target *WorkflowTargetConfig) {
	if target == nil {
		return
	}
	for i := range target.Steps {
		step := &target.Steps[i]
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

func addWorkflowInvokeRefs(refs workflowAppRefs, invokes []WorkflowInvokeConfig) {
	for _, invoke := range invokes {
		refs.Add(invoke.App)
	}
}
