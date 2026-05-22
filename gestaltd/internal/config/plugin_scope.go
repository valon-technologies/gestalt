package config

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"gopkg.in/yaml.v3"
)

// ApplyPluginScope projects a loaded config down to the requested plugin set
// and the transitive plugin references needed to validate and run that set.
func ApplyPluginScope(cfg *Config, plugins []string) error {
	names := NormalizePluginScopeNames(plugins)
	if len(names) == 0 {
		return nil
	}
	if cfg == nil {
		return fmt.Errorf("plugin scope requires a config")
	}

	requestedPlugins := make(map[string]struct{}, len(names))
	for _, name := range names {
		requestedPlugins[name] = struct{}{}
	}
	keptWorkflows := workflowsTargetingPluginClosure(cfg, requestedPlugins)
	keepPlugins := make(map[string]struct{}, len(names))
	queue := append([]string(nil), names...)
	for _, refs := range keptWorkflows {
		queue = append(queue, refs.Names()...)
	}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if _, ok := keepPlugins[name]; ok {
			continue
		}
		entry := cfg.Plugins[name]
		if entry == nil {
			return fmt.Errorf("plugin scope references unknown plugin %q", name)
		}
		keepPlugins[name] = struct{}{}
		for _, dep := range entry.Invokes {
			depName := strings.TrimSpace(dep.Plugin)
			if depName != "" {
				queue = append(queue, depName)
			}
		}
	}

	providerScope, err := providerRefsForPluginScope(cfg, keepPlugins, keptWorkflows)
	if err != nil {
		return err
	}

	cfg.Plugins = filterProviderEntries(cfg.Plugins, keepPlugins)
	cfg.Providers.UI = filterUIEntries(cfg.Providers.UI, uiEntriesForPluginScope(cfg, keepPlugins))
	filterWorkflowConfig(&cfg.Workflows, keptWorkflows)
	filterProvidersForPluginScope(cfg, providerScope)
	return nil
}

// NormalizePluginScopeNames trims plugin scope names and removes duplicates
// while preserving the first-seen order.
func NormalizePluginScopeNames(plugins []string) []string {
	seen := make(map[string]struct{}, len(plugins))
	var names []string
	for _, plugin := range plugins {
		name := strings.TrimSpace(plugin)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

func applyPluginScopeNode(root *yaml.Node, plugins []string) error {
	names := NormalizePluginScopeNames(plugins)
	if len(names) == 0 {
		return nil
	}
	doc := documentValueNode(root)
	if doc == nil || doc.Kind == 0 {
		return fmt.Errorf("plugin scope requires a config")
	}
	if doc.Kind != yaml.MappingNode {
		return fmt.Errorf("parsing config YAML: expected mapping document")
	}

	pluginsNode := mappingValueNode(doc, "plugins")
	requestedPlugins := make(map[string]struct{}, len(names))
	for _, name := range names {
		requestedPlugins[name] = struct{}{}
	}
	keptWorkflows := workflowNodesTargetingPluginClosure(doc, requestedPlugins)
	keepPlugins := make(map[string]struct{}, len(names))
	queue := append([]string(nil), names...)
	for _, refs := range keptWorkflows {
		queue = append(queue, refs.Names()...)
	}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if _, ok := keepPlugins[name]; ok {
			continue
		}
		pluginNode := mappingValueNode(pluginsNode, name)
		if pluginNode == nil {
			return fmt.Errorf("plugin scope references unknown plugin %q", name)
		}
		keepPlugins[name] = struct{}{}
		queue = append(queue, pluginInvokeRefsFromNode(pluginNode)...)
	}

	providerScope, err := providerRefsForPluginScopeNode(doc, keepPlugins, keptWorkflows)
	if err != nil {
		return err
	}

	filterMappingNode(pluginsNode, keepPlugins)
	if providersNode := mappingValueNode(doc, "providers"); providersNode != nil {
		filterMappingNode(mappingValueNode(providersNode, "ui"), uiEntriesForPluginScopeNode(doc, keepPlugins))
	}
	filterWorkflowNodes(mappingValueNode(doc, "workflows"), keptWorkflows)
	filterProviderNodesForPluginScope(doc, providerScope)
	filterScopedTopLevelReferenceNodes(doc)
	return nil
}

func filterMappingNode(node *yaml.Node, keep map[string]struct{}) {
	node = documentValueNode(node)
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	filtered := make([]*yaml.Node, 0, len(node.Content))
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i]
		if key == nil {
			continue
		}
		if _, ok := keep[strings.TrimSpace(key.Value)]; !ok {
			continue
		}
		filtered = append(filtered, node.Content[i], node.Content[i+1])
	}
	node.Content = filtered
}

func scalarStringNode(node *yaml.Node) string {
	if node == nil || node.Kind != yaml.ScalarNode {
		return ""
	}
	return strings.TrimSpace(node.Value)
}

func pluginInvokeRefsFromNode(pluginNode *yaml.Node) []string {
	invokes := mappingValueNode(pluginNode, "invokes")
	if invokes == nil || invokes.Kind != yaml.SequenceNode {
		return nil
	}
	var refs []string
	for _, item := range invokes.Content {
		if ref := scalarStringNode(mappingValueNode(item, "plugin")); ref != "" {
			refs = append(refs, ref)
		}
	}
	return refs
}

func uiEntriesForPluginScopeNode(doc *yaml.Node, keepPlugins map[string]struct{}) map[string]struct{} {
	keepUIs := map[string]struct{}{}
	if adminUI := scalarStringNode(mappingValueNode(mappingValueNode(mappingValueNode(doc, "server"), "admin"), "ui")); adminUI != "" {
		keepUIs[adminUI] = struct{}{}
	}
	providersUI := mappingValueNode(mappingValueNode(doc, "providers"), "ui")
	pluginsNode := mappingValueNode(doc, "plugins")
	for pluginName := range keepPlugins {
		pluginNode := mappingValueNode(pluginsNode, pluginName)
		if uiName := pluginUIBundleFromNode(pluginNode); uiName != "" {
			keepUIs[uiName] = struct{}{}
		}
		if mappingValueNode(providersUI, pluginName) != nil {
			keepUIs[pluginName] = struct{}{}
		}
	}
	for uiName, entry := range mappingNodeEntries(providersUI) {
		if _, ok := keepPlugins[scalarStringNode(mappingValueNode(entry, "ownerPlugin"))]; ok {
			keepUIs[uiName] = struct{}{}
		}
	}
	return keepUIs
}

func pluginUIBundleFromNode(pluginNode *yaml.Node) string {
	uiNode := mappingValueNode(pluginNode, "ui")
	if uiNode == nil {
		return ""
	}
	if uiNode.Kind == yaml.ScalarNode {
		return scalarStringNode(uiNode)
	}
	return scalarStringNode(mappingValueNode(uiNode, "bundle"))
}

func workflowNodesTargetingPluginClosure(doc *yaml.Node, keepPlugins map[string]struct{}) map[string]workflowPluginRefs {
	refsByName := map[string]workflowPluginRefs{}
	workflowsNode := mappingValueNode(doc, "workflows")
	for name, schedule := range mappingNodeEntries(mappingValueNode(workflowsNode, "schedules")) {
		if workflowTargetNodeInPluginClosure(mappingValueNode(schedule, "target"), keepPlugins) {
			refsByName["schedule:"+name] = workflowRefsFromNode(schedule)
		}
	}
	for name, trigger := range mappingNodeEntries(mappingValueNode(workflowsNode, "eventTriggers")) {
		if workflowTargetNodeInPluginClosure(mappingValueNode(trigger, "target"), keepPlugins) {
			refsByName["trigger:"+name] = workflowRefsFromNode(trigger)
		}
	}
	return refsByName
}

func mappingNodeEntries(node *yaml.Node) map[string]*yaml.Node {
	node = documentValueNode(node)
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	out := make(map[string]*yaml.Node, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i]
		if key == nil {
			continue
		}
		out[strings.TrimSpace(key.Value)] = node.Content[i+1]
	}
	return out
}

func workflowTargetNodeInPluginClosure(targetNode *yaml.Node, keepPlugins map[string]struct{}) bool {
	for _, stepNode := range workflowStepNodesFromTargetNode(targetNode) {
		pluginName := scalarStringNode(mappingValueNode(mappingValueNode(stepNode, "plugin"), "name"))
		if pluginName == "" {
			continue
		}
		if _, ok := keepPlugins[pluginName]; ok {
			return true
		}
	}
	return false
}

func workflowRefsFromNode(workflowNode *yaml.Node) workflowPluginRefs {
	refs := workflowPluginRefs{}
	targetNode := mappingValueNode(workflowNode, "target")
	for _, stepNode := range workflowStepNodesFromTargetNode(targetNode) {
		refs.Add(scalarStringNode(mappingValueNode(mappingValueNode(stepNode, "plugin"), "name")))
		agentNode := mappingValueNode(stepNode, "agent")
		if toolsNode := mappingValueNode(agentNode, "tools"); toolsNode != nil && toolsNode.Kind == yaml.SequenceNode {
			for _, tool := range toolsNode.Content {
				refs.Add(scalarStringNode(mappingValueNode(tool, "plugin")))
			}
		}
	}
	if invokesNode := mappingValueNode(workflowNode, "invokes"); invokesNode != nil && invokesNode.Kind == yaml.SequenceNode {
		for _, invoke := range invokesNode.Content {
			refs.Add(scalarStringNode(mappingValueNode(invoke, "plugin")))
		}
	}
	if permissionsNode := mappingValueNode(workflowNode, "permissions"); permissionsNode != nil && permissionsNode.Kind == yaml.SequenceNode {
		for _, permission := range permissionsNode.Content {
			refs.Add(scalarStringNode(mappingValueNode(permission, "plugin")))
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

func filterWorkflowNodes(workflowsNode *yaml.Node, keep map[string]workflowPluginRefs) {
	if workflowsNode == nil {
		return
	}
	keepSchedules := map[string]struct{}{}
	keepTriggers := map[string]struct{}{}
	for key := range keep {
		switch {
		case strings.HasPrefix(key, "schedule:"):
			keepSchedules[strings.TrimPrefix(key, "schedule:")] = struct{}{}
		case strings.HasPrefix(key, "trigger:"):
			keepTriggers[strings.TrimPrefix(key, "trigger:")] = struct{}{}
		}
	}
	filterMappingNode(mappingValueNode(workflowsNode, "schedules"), keepSchedules)
	filterMappingNode(mappingValueNode(workflowsNode, "eventTriggers"), keepTriggers)
}

type pluginScopeProviderRefs struct {
	Authentication      map[string]struct{}
	Authorization       map[string]struct{}
	ExternalCredentials map[string]struct{}
	Secrets             map[string]struct{}
	Telemetry           map[string]struct{}
	Audit               map[string]struct{}
	IndexedDB           map[string]struct{}
	Cache               map[string]struct{}
	S3                  map[string]struct{}
	Workflow            map[string]struct{}
	Agent               map[string]struct{}
	Runtime             map[string]struct{}
}

func newPluginScopeProviderRefs() pluginScopeProviderRefs {
	return pluginScopeProviderRefs{
		Authentication:      map[string]struct{}{},
		Authorization:       map[string]struct{}{},
		ExternalCredentials: map[string]struct{}{},
		Secrets:             map[string]struct{}{},
		Telemetry:           map[string]struct{}{},
		Audit:               map[string]struct{}{},
		IndexedDB:           map[string]struct{}{},
		Cache:               map[string]struct{}{},
		S3:                  map[string]struct{}{},
		Workflow:            map[string]struct{}{},
		Agent:               map[string]struct{}{},
		Runtime:             map[string]struct{}{},
	}
}

func addProviderRef(keep map[string]struct{}, name string) {
	name = strings.TrimSpace(name)
	if name != "" {
		keep[name] = struct{}{}
	}
}

func providerRefsForPluginScopeNode(doc *yaml.Node, keepPlugins map[string]struct{}, keptWorkflows map[string]workflowPluginRefs) (pluginScopeProviderRefs, error) {
	refs := newPluginScopeProviderRefs()
	addSelectedHostProviderRefFromNode(doc, "authentication", refs.Authentication)
	addSelectedHostProviderRefFromNode(doc, "authorization", refs.Authorization)
	addSelectedHostProviderRefFromNode(doc, "externalCredentials", refs.ExternalCredentials)
	addSelectedHostProviderRefFromNode(doc, "secrets", refs.Secrets)
	addSelectedHostProviderRefFromNode(doc, "telemetry", refs.Telemetry)
	addSelectedHostProviderRefFromNode(doc, "audit", refs.Audit)
	addSelectedHostProviderRefFromNode(doc, "indexeddb", refs.IndexedDB)

	pluginsNode := mappingValueNode(doc, "plugins")
	for pluginName := range keepPlugins {
		pluginNode := mappingValueNode(pluginsNode, pluginName)
		if pluginNode == nil {
			continue
		}
		addPluginProviderRefsFromNode(doc, &refs, pluginNode)
	}
	for key := range keptWorkflows {
		switch {
		case strings.HasPrefix(key, "schedule:"):
			name := strings.TrimPrefix(key, "schedule:")
			schedule := mappingValueNode(mappingValueNode(mappingValueNode(doc, "workflows"), "schedules"), name)
			addWorkflowProviderRefsFromNode(doc, &refs, schedule)
		case strings.HasPrefix(key, "trigger:"):
			name := strings.TrimPrefix(key, "trigger:")
			trigger := mappingValueNode(mappingValueNode(mappingValueNode(doc, "workflows"), "eventTriggers"), name)
			addWorkflowProviderRefsFromNode(doc, &refs, trigger)
		}
	}
	addProviderMapDependenciesFromNode(doc, "workflow", refs.Workflow, &refs)
	addProviderMapDependenciesFromNode(doc, "agent", refs.Agent, &refs)
	if err := expandSecretProviderRefsFromNode(doc, keepPlugins, keptWorkflows, &refs); err != nil {
		return refs, err
	}
	return refs, nil
}

func addSelectedHostProviderRefFromNode(doc *yaml.Node, key string, keep map[string]struct{}) {
	explicit := scalarStringNode(mappingValueNode(mappingValueNode(mappingValueNode(doc, "server"), "providers"), key))
	for name := range selectedProviderNamesFromNode(mappingValueNode(mappingValueNode(doc, "providers"), key), explicit) {
		addProviderRef(keep, name)
	}
}

func selectedProviderNamesFromNode(entriesNode *yaml.Node, explicit string) map[string]struct{} {
	keep := map[string]struct{}{}
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		keep[explicit] = struct{}{}
		return keep
	}
	entries := mappingNodeEntries(entriesNode)
	if len(entries) == 0 {
		return keep
	}
	for name, entry := range entries {
		if scalarBoolNode(mappingValueNode(entry, "default")) {
			keep[name] = struct{}{}
		}
	}
	if len(keep) > 0 {
		return keep
	}
	if len(entries) == 1 {
		for name := range entries {
			keep[name] = struct{}{}
		}
	}
	return keep
}

func scalarBoolNode(node *yaml.Node) bool {
	if node == nil || node.Kind != yaml.ScalarNode {
		return false
	}
	value := strings.TrimSpace(node.Value)
	return strings.EqualFold(value, "true") || strings.Contains(value, "${")
}

func addPluginProviderRefsFromNode(doc *yaml.Node, refs *pluginScopeProviderRefs, pluginNode *yaml.Node) {
	authProvider := scalarStringNode(mappingValueNode(mappingValueNode(pluginNode, "auth"), "provider"))
	switch authProvider {
	case "":
	case "server":
		addSelectedHostProviderRefFromNode(doc, "authentication", refs.Authentication)
	default:
		addProviderRef(refs.Authentication, authProvider)
	}
	if indexedDBNode := mappingValueNode(pluginNode, "indexeddb"); indexedDBNode != nil {
		if provider := scalarStringNode(mappingValueNode(indexedDBNode, "provider")); provider != "" {
			addProviderRef(refs.IndexedDB, provider)
		} else {
			addSelectedHostProviderRefFromNode(doc, "indexeddb", refs.IndexedDB)
		}
	}
	addSequenceRefsFromNode(refs.Cache, mappingValueNode(pluginNode, "cache"))
	addSequenceRefsFromNode(refs.S3, mappingValueNode(pluginNode, "s3"))
	addRuntimeProviderRefFromNode(doc, pluginNode, refs.Runtime)
}

func addSequenceRefsFromNode(keep map[string]struct{}, seq *yaml.Node) {
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return
	}
	for _, item := range seq.Content {
		addProviderRef(keep, scalarStringNode(item))
	}
}

func addRuntimeProviderRefFromNode(doc *yaml.Node, entryNode *yaml.Node, keep map[string]struct{}) {
	runtimeNode := mappingValueNode(entryNode, "runtime")
	if runtimeNode == nil {
		return
	}
	if provider := scalarStringNode(mappingValueNode(runtimeNode, "provider")); provider != "" {
		addProviderRef(keep, provider)
		return
	}
	explicit := scalarStringNode(mappingValueNode(mappingValueNode(mappingValueNode(doc, "server"), "runtime"), "defaultProvider"))
	for name := range selectedProviderNamesFromNode(mappingValueNode(mappingValueNode(doc, "runtime"), "providers"), explicit) {
		addProviderRef(keep, name)
	}
}

func addWorkflowProviderRefsFromNode(doc *yaml.Node, refs *pluginScopeProviderRefs, workflowNode *yaml.Node) {
	if workflowNode == nil {
		return
	}
	if provider := scalarStringNode(mappingValueNode(workflowNode, "provider")); provider != "" {
		addProviderRef(refs.Workflow, provider)
	} else {
		for name := range selectedProviderNamesFromNode(mappingValueNode(mappingValueNode(doc, "providers"), "workflow"), "") {
			addProviderRef(refs.Workflow, name)
		}
	}
	for _, stepNode := range workflowStepNodesFromTargetNode(mappingValueNode(workflowNode, "target")) {
		agentNode := mappingValueNode(stepNode, "agent")
		if agentNode == nil {
			continue
		}
		if provider := scalarStringNode(mappingValueNode(agentNode, "provider")); provider != "" {
			addProviderRef(refs.Agent, provider)
		} else {
			for name := range selectedProviderNamesFromNode(mappingValueNode(mappingValueNode(doc, "providers"), "agent"), "") {
				addProviderRef(refs.Agent, name)
			}
		}
	}
}

func addProviderMapDependenciesFromNode(doc *yaml.Node, key string, keep map[string]struct{}, refs *pluginScopeProviderRefs) {
	entriesNode := mappingValueNode(mappingValueNode(doc, "providers"), key)
	for name := range keep {
		entry := mappingValueNode(entriesNode, name)
		if entry == nil {
			continue
		}
		if provider := scalarStringNode(mappingValueNode(mappingValueNode(entry, "indexeddb"), "provider")); provider != "" {
			addProviderRef(refs.IndexedDB, provider)
		}
		addRuntimeProviderRefFromNode(doc, entry, refs.Runtime)
	}
}

func filterProviderNodesForPluginScope(doc *yaml.Node, refs pluginScopeProviderRefs) {
	providersNode := mappingValueNode(doc, "providers")
	if providersNode != nil {
		filterMappingNode(mappingValueNode(providersNode, "authentication"), refs.Authentication)
		filterMappingNode(mappingValueNode(providersNode, "authorization"), refs.Authorization)
		filterMappingNode(mappingValueNode(providersNode, "externalCredentials"), refs.ExternalCredentials)
		filterMappingNode(mappingValueNode(providersNode, "secrets"), refs.Secrets)
		filterMappingNode(mappingValueNode(providersNode, "telemetry"), refs.Telemetry)
		filterMappingNode(mappingValueNode(providersNode, "audit"), refs.Audit)
		filterMappingNode(mappingValueNode(providersNode, "indexeddb"), refs.IndexedDB)
		filterMappingNode(mappingValueNode(providersNode, "cache"), refs.Cache)
		filterMappingNode(mappingValueNode(providersNode, "s3"), refs.S3)
		filterMappingNode(mappingValueNode(providersNode, "workflow"), refs.Workflow)
		filterMappingNode(mappingValueNode(providersNode, "agent"), refs.Agent)
	}
	filterMappingNode(mappingValueNode(mappingValueNode(doc, "runtime"), "providers"), refs.Runtime)
}

func expandSecretProviderRefsFromNode(doc *yaml.Node, keepPlugins map[string]struct{}, keptWorkflows map[string]workflowPluginRefs, refs *pluginScopeProviderRefs) error {
	for {
		before := len(refs.Secrets)
		for _, entry := range retainedProviderEntryNodes(doc, keepPlugins, keptWorkflows, *refs) {
			if err := collectSecretProviderRefsFromNode(refs.Secrets, entry, false); err != nil {
				return err
			}
		}
		secretsNode := mappingValueNode(mappingValueNode(doc, "providers"), "secrets")
		for name := range refs.Secrets {
			entry := mappingValueNode(secretsNode, name)
			if err := collectSecretProviderRefsFromNode(refs.Secrets, entry, true); err != nil {
				return err
			}
		}
		if len(refs.Secrets) == before {
			return nil
		}
	}
}

func retainedProviderEntryNodes(doc *yaml.Node, keepPlugins map[string]struct{}, keptWorkflows map[string]workflowPluginRefs, refs pluginScopeProviderRefs) []*yaml.Node {
	var entries []*yaml.Node
	pluginsNode := mappingValueNode(doc, "plugins")
	for pluginName := range keepPlugins {
		if entry := mappingValueNode(pluginsNode, pluginName); entry != nil {
			entries = append(entries, entry)
		}
	}
	if providersNode := mappingValueNode(doc, "providers"); providersNode != nil {
		entries = append(entries, retainedProviderMapNodes(mappingValueNode(providersNode, "authentication"), refs.Authentication)...)
		entries = append(entries, retainedProviderMapNodes(mappingValueNode(providersNode, "authorization"), refs.Authorization)...)
		entries = append(entries, retainedProviderMapNodes(mappingValueNode(providersNode, "externalCredentials"), refs.ExternalCredentials)...)
		entries = append(entries, retainedProviderMapNodes(mappingValueNode(providersNode, "telemetry"), refs.Telemetry)...)
		entries = append(entries, retainedProviderMapNodes(mappingValueNode(providersNode, "audit"), refs.Audit)...)
		entries = append(entries, retainedProviderMapNodes(mappingValueNode(providersNode, "indexeddb"), refs.IndexedDB)...)
		entries = append(entries, retainedProviderMapNodes(mappingValueNode(providersNode, "cache"), refs.Cache)...)
		entries = append(entries, retainedProviderMapNodes(mappingValueNode(providersNode, "s3"), refs.S3)...)
		entries = append(entries, retainedProviderMapNodes(mappingValueNode(providersNode, "workflow"), refs.Workflow)...)
		entries = append(entries, retainedProviderMapNodes(mappingValueNode(providersNode, "agent"), refs.Agent)...)
		entries = append(entries, retainedProviderMapNodes(mappingValueNode(providersNode, "ui"), uiEntriesForPluginScopeNode(doc, keepPlugins))...)
	}
	entries = append(entries, retainedProviderMapNodes(mappingValueNode(mappingValueNode(doc, "runtime"), "providers"), refs.Runtime)...)
	for key := range keptWorkflows {
		switch {
		case strings.HasPrefix(key, "schedule:"):
			name := strings.TrimPrefix(key, "schedule:")
			if entry := mappingValueNode(mappingValueNode(mappingValueNode(doc, "workflows"), "schedules"), name); entry != nil {
				entries = append(entries, entry)
			}
		case strings.HasPrefix(key, "trigger:"):
			name := strings.TrimPrefix(key, "trigger:")
			if entry := mappingValueNode(mappingValueNode(mappingValueNode(doc, "workflows"), "eventTriggers"), name); entry != nil {
				entries = append(entries, entry)
			}
		}
	}
	return entries
}

func retainedProviderMapNodes(entriesNode *yaml.Node, keep map[string]struct{}) []*yaml.Node {
	var entries []*yaml.Node
	for name := range keep {
		if entry := mappingValueNode(entriesNode, name); entry != nil {
			entries = append(entries, entry)
		}
	}
	return entries
}

func collectSecretProviderRefsFromNode(keep map[string]struct{}, node *yaml.Node, skipConfig bool) error {
	if node == nil {
		return nil
	}
	switch node.Kind {
	case yaml.ScalarNode:
		ref, ok, err := ParseSecretRefTransport(node.Value)
		if err != nil {
			return err
		}
		if ok {
			addProviderRef(keep, ref.Provider)
		}
	case yaml.SequenceNode, yaml.DocumentNode:
		for _, child := range node.Content {
			if err := collectSecretProviderRefsFromNode(keep, child, skipConfig); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i]
			if skipConfig && key != nil && key.Value == "config" {
				continue
			}
			if err := collectSecretProviderRefsFromNode(keep, node.Content[i+1], skipConfig); err != nil {
				return err
			}
		}
	}
	return nil
}

func filterScopedTopLevelReferenceNodes(doc *yaml.Node) {
	var entries []*yaml.Node
	entries = append(entries, mappingNodeValues(mappingValueNode(doc, "plugins"))...)
	if providersNode := mappingValueNode(doc, "providers"); providersNode != nil {
		for _, key := range []string{"authentication", "authorization", "externalCredentials", "secrets", "telemetry", "audit", "indexeddb", "cache", "s3", "workflow", "agent", "ui"} {
			entries = append(entries, mappingNodeValues(mappingValueNode(providersNode, key))...)
		}
	}
	entries = append(entries, mappingNodeValues(mappingValueNode(mappingValueNode(doc, "runtime"), "providers"))...)

	connectionRefs := map[string]struct{}{}
	providerRepos := map[string]struct{}{}
	providerSnapshotRepos := map[string]struct{}{}
	keepAllProviderRepos := false
	for _, entry := range entries {
		collectConnectionRefsFromNode(connectionRefs, entry)
		if collectSourceRepositoryRefsFromNode(providerRepos, providerSnapshotRepos, mappingValueNode(entry, "source")) {
			keepAllProviderRepos = true
		}
	}
	filterMappingNode(mappingValueNode(doc, "connections"), connectionRefs)
	if !keepAllProviderRepos {
		filterMappingNode(mappingValueNode(doc, "providerRepositories"), providerRepos)
	}
	filterMappingNode(mappingValueNode(doc, "providerSnapshotRepositories"), providerSnapshotRepos)
}

func mappingNodeValues(node *yaml.Node) []*yaml.Node {
	node = documentValueNode(node)
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	out := make([]*yaml.Node, 0, len(node.Content)/2)
	for i := 1; i < len(node.Content); i += 2 {
		out = append(out, node.Content[i])
	}
	return out
}

func collectConnectionRefsFromNode(refs map[string]struct{}, entry *yaml.Node) {
	connectionsNode := mappingValueNode(entry, "connections")
	if connectionsNode == nil || connectionsNode.Kind != yaml.MappingNode {
		return
	}
	for _, connNode := range mappingNodeValues(connectionsNode) {
		addProviderRef(refs, scalarStringNode(mappingValueNode(connNode, "ref")))
	}
}

func collectSourceRepositoryRefsFromNode(providerRepos, snapshotRepos map[string]struct{}, sourceNode *yaml.Node) bool {
	if sourceNode == nil || sourceNode.Kind != yaml.MappingNode {
		return false
	}
	repo := scalarStringNode(mappingValueNode(sourceNode, "repo"))
	addProviderRef(providerRepos, repo)
	addProviderRef(snapshotRepos, scalarStringNode(mappingValueNode(mappingValueNode(sourceNode, "git"), "artifactRepository")))
	return repo == "" && scalarStringNode(mappingValueNode(sourceNode, "package")) != ""
}

func filterProviderEntries(entries map[string]*ProviderEntry, keep map[string]struct{}) map[string]*ProviderEntry {
	if len(entries) == 0 {
		return entries
	}
	filtered := make(map[string]*ProviderEntry, len(keep))
	for _, name := range slices.Sorted(maps.Keys(keep)) {
		if entry := entries[name]; entry != nil {
			filtered[name] = entry
		}
	}
	return filtered
}

func uiEntriesForPluginScope(cfg *Config, keepPlugins map[string]struct{}) map[string]struct{} {
	keepUIs := map[string]struct{}{}
	if adminUI := strings.TrimSpace(cfg.Server.Admin.UI); adminUI != "" {
		keepUIs[adminUI] = struct{}{}
	}
	for pluginName := range keepPlugins {
		entry := cfg.Plugins[pluginName]
		if entry == nil {
			continue
		}
		if uiName := strings.TrimSpace(entry.UI); uiName != "" {
			keepUIs[uiName] = struct{}{}
		}
		if cfg.Providers.UI[pluginName] != nil {
			keepUIs[pluginName] = struct{}{}
		}
	}
	for uiName, entry := range cfg.Providers.UI {
		if entry == nil {
			continue
		}
		if _, ok := keepPlugins[strings.TrimSpace(entry.OwnerPlugin)]; ok {
			keepUIs[uiName] = struct{}{}
		}
	}
	return keepUIs
}

func filterUIEntries(entries map[string]*UIEntry, keep map[string]struct{}) map[string]*UIEntry {
	if len(entries) == 0 {
		return entries
	}
	filtered := make(map[string]*UIEntry, len(keep))
	for _, name := range slices.Sorted(maps.Keys(keep)) {
		if entry := entries[name]; entry != nil {
			filtered[name] = entry
		}
	}
	return filtered
}

func filterRuntimeProviderEntries(entries map[string]*RuntimeProviderEntry, keep map[string]struct{}) map[string]*RuntimeProviderEntry {
	if len(entries) == 0 {
		return entries
	}
	filtered := make(map[string]*RuntimeProviderEntry, len(keep))
	for _, name := range slices.Sorted(maps.Keys(keep)) {
		if entry := entries[name]; entry != nil {
			filtered[name] = entry
		}
	}
	return filtered
}

func providerRefsForPluginScope(cfg *Config, keepPlugins map[string]struct{}, keptWorkflows map[string]workflowPluginRefs) (pluginScopeProviderRefs, error) {
	refs := newPluginScopeProviderRefs()
	if err := addSelectedHostProviderRef(cfg, HostProviderKindAuthentication, refs.Authentication); err != nil {
		return refs, err
	}
	if err := addSelectedHostProviderRef(cfg, HostProviderKindAuthorization, refs.Authorization); err != nil {
		return refs, err
	}
	if err := addSelectedHostProviderRef(cfg, HostProviderKindExternalCredentials, refs.ExternalCredentials); err != nil {
		return refs, err
	}
	if err := addSelectedHostProviderRef(cfg, HostProviderKindSecrets, refs.Secrets); err != nil {
		return refs, err
	}
	if err := addSelectedHostProviderRef(cfg, HostProviderKindTelemetry, refs.Telemetry); err != nil {
		return refs, err
	}
	if err := addSelectedHostProviderRef(cfg, HostProviderKindAudit, refs.Audit); err != nil {
		return refs, err
	}
	if err := addSelectedHostProviderRef(cfg, HostProviderKindIndexedDB, refs.IndexedDB); err != nil {
		return refs, err
	}

	for pluginName := range keepPlugins {
		entry := cfg.Plugins[pluginName]
		if entry == nil {
			continue
		}
		if err := addPluginProviderRefs(cfg, &refs, entry); err != nil {
			return refs, err
		}
	}
	for key := range keptWorkflows {
		switch {
		case strings.HasPrefix(key, "schedule:"):
			name := strings.TrimPrefix(key, "schedule:")
			schedule := cfg.Workflows.Schedules[name]
			if err := addWorkflowProviderRefs(cfg, &refs, schedule.Provider, schedule.Target); err != nil {
				return refs, err
			}
		case strings.HasPrefix(key, "trigger:"):
			name := strings.TrimPrefix(key, "trigger:")
			trigger := cfg.Workflows.EventTriggers[name]
			if err := addWorkflowProviderRefs(cfg, &refs, trigger.Provider, trigger.Target); err != nil {
				return refs, err
			}
		}
	}
	if err := addProviderEntryDependencies(cfg, cfg.Providers.Workflow, refs.Workflow, &refs); err != nil {
		return refs, err
	}
	if err := addProviderEntryDependencies(cfg, cfg.Providers.Agent, refs.Agent, &refs); err != nil {
		return refs, err
	}
	if err := expandSecretProviderRefs(cfg, keepPlugins, keptWorkflows, &refs); err != nil {
		return refs, err
	}
	return refs, nil
}

func addSelectedHostProviderRef(cfg *Config, kind HostProviderKind, keep map[string]struct{}) error {
	name, _, err := cfg.SelectedHostProvider(kind)
	if err != nil {
		return err
	}
	addProviderRef(keep, name)
	return nil
}

func addSelectedWorkflowProviderRef(cfg *Config, keep map[string]struct{}) error {
	name, _, err := cfg.SelectedWorkflowProvider()
	if err != nil {
		return err
	}
	addProviderRef(keep, name)
	return nil
}

func addSelectedAgentProviderRef(cfg *Config, keep map[string]struct{}) error {
	name, _, err := cfg.SelectedAgentProvider()
	if err != nil {
		return err
	}
	addProviderRef(keep, name)
	return nil
}

func addSelectedRuntimeProviderRef(cfg *Config, keep map[string]struct{}) error {
	name, _, err := cfg.SelectedRuntimeProvider()
	if err != nil {
		return err
	}
	addProviderRef(keep, name)
	return nil
}

func addPluginProviderRefs(cfg *Config, refs *pluginScopeProviderRefs, entry *ProviderEntry) error {
	if entry == nil {
		return nil
	}
	if entry.RouteAuth != nil {
		switch provider := strings.TrimSpace(entry.RouteAuth.Provider); provider {
		case "":
		case "server":
			if err := addSelectedHostProviderRef(cfg, HostProviderKindAuthentication, refs.Authentication); err != nil {
				return err
			}
		default:
			addProviderRef(refs.Authentication, provider)
		}
	}
	if entry.IndexedDB != nil {
		if provider := strings.TrimSpace(entry.IndexedDB.Provider); provider != "" {
			addProviderRef(refs.IndexedDB, provider)
		} else if err := addSelectedHostProviderRef(cfg, HostProviderKindIndexedDB, refs.IndexedDB); err != nil {
			return err
		}
	}
	for _, binding := range entry.Cache {
		addProviderRef(refs.Cache, binding)
	}
	for _, binding := range entry.S3 {
		addProviderRef(refs.S3, binding)
	}
	return addRuntimeProviderRef(cfg, refs.Runtime, entry)
}

func addRuntimeProviderRef(cfg *Config, keep map[string]struct{}, entry *ProviderEntry) error {
	if entry == nil || entry.Runtime == nil {
		return nil
	}
	if provider := strings.TrimSpace(entry.Runtime.Provider); provider != "" {
		addProviderRef(keep, provider)
		return nil
	}
	return addSelectedRuntimeProviderRef(cfg, keep)
}

func addWorkflowProviderRefs(cfg *Config, refs *pluginScopeProviderRefs, workflowProvider string, target *WorkflowTargetConfig) error {
	if strings.TrimSpace(workflowProvider) != "" {
		addProviderRef(refs.Workflow, workflowProvider)
	} else if err := addSelectedWorkflowProviderRef(cfg, refs.Workflow); err != nil {
		return err
	}
	if target == nil {
		return nil
	}
	for i := range target.Steps {
		step := &target.Steps[i]
		if step.Agent == nil {
			continue
		}
		if strings.TrimSpace(step.Agent.Provider) != "" {
			addProviderRef(refs.Agent, step.Agent.Provider)
			continue
		}
		if err := addSelectedAgentProviderRef(cfg, refs.Agent); err != nil {
			return err
		}
	}
	return nil
}

func addProviderEntryDependencies(cfg *Config, entries map[string]*ProviderEntry, keep map[string]struct{}, refs *pluginScopeProviderRefs) error {
	for name := range keep {
		entry := entries[name]
		if entry == nil {
			continue
		}
		if entry.IndexedDB != nil {
			addProviderRef(refs.IndexedDB, entry.IndexedDB.Provider)
		}
		if err := addRuntimeProviderRef(cfg, refs.Runtime, entry); err != nil {
			return err
		}
	}
	return nil
}

func filterProvidersForPluginScope(cfg *Config, refs pluginScopeProviderRefs) {
	cfg.Providers.Authentication = filterProviderEntries(cfg.Providers.Authentication, refs.Authentication)
	cfg.Providers.Authorization = filterProviderEntries(cfg.Providers.Authorization, refs.Authorization)
	cfg.Providers.ExternalCredentials = filterProviderEntries(cfg.Providers.ExternalCredentials, refs.ExternalCredentials)
	cfg.Providers.Secrets = filterProviderEntries(cfg.Providers.Secrets, refs.Secrets)
	cfg.Providers.Telemetry = filterProviderEntries(cfg.Providers.Telemetry, refs.Telemetry)
	cfg.Providers.Audit = filterProviderEntries(cfg.Providers.Audit, refs.Audit)
	cfg.Providers.IndexedDB = filterProviderEntries(cfg.Providers.IndexedDB, refs.IndexedDB)
	cfg.Providers.Cache = filterProviderEntries(cfg.Providers.Cache, refs.Cache)
	cfg.Providers.S3 = filterProviderEntries(cfg.Providers.S3, refs.S3)
	cfg.Providers.Workflow = filterProviderEntries(cfg.Providers.Workflow, refs.Workflow)
	cfg.Providers.Agent = filterProviderEntries(cfg.Providers.Agent, refs.Agent)
	cfg.Runtime.Providers = filterRuntimeProviderEntries(cfg.Runtime.Providers, refs.Runtime)
}

func expandSecretProviderRefs(cfg *Config, keepPlugins map[string]struct{}, keptWorkflows map[string]workflowPluginRefs, refs *pluginScopeProviderRefs) error {
	for {
		before := len(refs.Secrets)
		scoped := scopedConfigForSecretRefCollection(cfg, keepPlugins, keptWorkflows, *refs)
		configRefs, err := ReferencedConfigSecretProviders(&scoped)
		if err != nil {
			return err
		}
		sourceAuthRefs, err := ReferencedSourceAuthSecretProviders(&scoped)
		if err != nil {
			return err
		}
		for name := range configRefs {
			addProviderRef(refs.Secrets, name)
		}
		for name := range sourceAuthRefs {
			addProviderRef(refs.Secrets, name)
		}
		if len(refs.Secrets) == before {
			return nil
		}
	}
}

func scopedConfigForSecretRefCollection(cfg *Config, keepPlugins map[string]struct{}, keptWorkflows map[string]workflowPluginRefs, refs pluginScopeProviderRefs) Config {
	scoped := *cfg
	scoped.Plugins = filterProviderEntries(cfg.Plugins, keepPlugins)
	scoped.Providers.Authentication = filterProviderEntries(cfg.Providers.Authentication, refs.Authentication)
	scoped.Providers.Authorization = filterProviderEntries(cfg.Providers.Authorization, refs.Authorization)
	scoped.Providers.ExternalCredentials = filterProviderEntries(cfg.Providers.ExternalCredentials, refs.ExternalCredentials)
	scoped.Providers.Secrets = filterProviderEntries(cfg.Providers.Secrets, refs.Secrets)
	scoped.Providers.Telemetry = filterProviderEntries(cfg.Providers.Telemetry, refs.Telemetry)
	scoped.Providers.Audit = filterProviderEntries(cfg.Providers.Audit, refs.Audit)
	scoped.Providers.IndexedDB = filterProviderEntries(cfg.Providers.IndexedDB, refs.IndexedDB)
	scoped.Providers.Cache = filterProviderEntries(cfg.Providers.Cache, refs.Cache)
	scoped.Providers.S3 = filterProviderEntries(cfg.Providers.S3, refs.S3)
	scoped.Providers.Workflow = filterProviderEntries(cfg.Providers.Workflow, refs.Workflow)
	scoped.Providers.Agent = filterProviderEntries(cfg.Providers.Agent, refs.Agent)
	scoped.Providers.UI = filterUIEntries(cfg.Providers.UI, uiEntriesForPluginScope(cfg, keepPlugins))
	scoped.Runtime.Providers = filterRuntimeProviderEntries(cfg.Runtime.Providers, refs.Runtime)
	filterWorkflowConfig(&scoped.Workflows, keptWorkflows)
	return scoped
}

type workflowPluginRefs map[string]struct{}

func (r workflowPluginRefs) Add(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	r[name] = struct{}{}
}

func (r workflowPluginRefs) Names() []string {
	names := slices.Sorted(maps.Keys(r))
	return names
}

func workflowsTargetingPluginClosure(cfg *Config, keepPlugins map[string]struct{}) map[string]workflowPluginRefs {
	if cfg == nil {
		return nil
	}
	refsByName := map[string]workflowPluginRefs{}
	for name := range cfg.Workflows.Schedules {
		schedule := cfg.Workflows.Schedules[name]
		if workflowTargetInPluginClosure(schedule.Target, keepPlugins) {
			refsByName["schedule:"+name] = workflowSchedulePluginRefs(schedule)
		}
	}
	for name := range cfg.Workflows.EventTriggers {
		trigger := cfg.Workflows.EventTriggers[name]
		if workflowTargetInPluginClosure(trigger.Target, keepPlugins) {
			refsByName["trigger:"+name] = workflowEventTriggerPluginRefs(trigger)
		}
	}
	return refsByName
}

func workflowTargetInPluginClosure(target *WorkflowTargetConfig, keepPlugins map[string]struct{}) bool {
	if target == nil {
		return false
	}
	for i := range target.Steps {
		if target.Steps[i].Plugin == nil {
			continue
		}
		if _, ok := keepPlugins[strings.TrimSpace(target.Steps[i].Plugin.Name)]; ok {
			return true
		}
	}
	return false
}

func filterWorkflowConfig(workflows *WorkflowsConfig, keep map[string]workflowPluginRefs) {
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

func workflowSchedulePluginRefs(schedule WorkflowScheduleConfig) workflowPluginRefs {
	refs := workflowPluginRefs{}
	addWorkflowTargetRefs(refs, schedule.Target)
	addWorkflowInvokeRefs(refs, schedule.Invokes)
	addPermissionRefs(refs, schedule.Permissions)
	return refs
}

func workflowEventTriggerPluginRefs(trigger WorkflowEventTriggerConfig) workflowPluginRefs {
	refs := workflowPluginRefs{}
	addWorkflowTargetRefs(refs, trigger.Target)
	addWorkflowInvokeRefs(refs, trigger.Invokes)
	addPermissionRefs(refs, trigger.Permissions)
	return refs
}

func addWorkflowTargetRefs(refs workflowPluginRefs, target *WorkflowTargetConfig) {
	if target == nil {
		return
	}
	for i := range target.Steps {
		step := &target.Steps[i]
		if step.Plugin != nil {
			refs.Add(step.Plugin.Name)
		}
		if step.Agent != nil {
			for _, tool := range step.Agent.Tools {
				refs.Add(tool.Plugin)
			}
		}
	}
}

func addWorkflowInvokeRefs(refs workflowPluginRefs, invokes []WorkflowInvokeConfig) {
	for _, invoke := range invokes {
		refs.Add(invoke.Plugin)
	}
}

func addPermissionRefs(refs workflowPluginRefs, permissions []core.AccessPermission) {
	for _, permission := range permissions {
		refs.Add(permission.Plugin)
	}
}
