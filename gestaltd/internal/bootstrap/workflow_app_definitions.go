package bootstrap

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/workflowwire"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

var appWorkflowLocalIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// appWorkflowDeclarations keeps reported workflows with the configuration used
// to build their app. Registry installs resolve manifests without mutating the
// base config, so reconciliation must snapshot both together.
type appWorkflowDeclarations struct {
	mu    sync.Mutex
	byApp map[string]appWorkflowDeclaration
}

type appWorkflowDeclaration struct {
	entry *config.ProviderEntry
	specs []*proto.WorkflowDefinitionSpec
}

func newAppWorkflowDeclarations() *appWorkflowDeclarations {
	return &appWorkflowDeclarations{byApp: make(map[string]appWorkflowDeclaration)}
}

func (r *appWorkflowDeclarations) Set(appName string, entry *config.ProviderEntry, specs []*proto.WorkflowDefinitionSpec) {
	if r == nil {
		return
	}
	appName = strings.TrimSpace(appName)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byApp == nil {
		r.byApp = make(map[string]appWorkflowDeclaration)
	}
	r.byApp[appName] = appWorkflowDeclaration{entry: entry, specs: slices.Clone(specs)}
}

func (r *appWorkflowDeclarations) Snapshot(cfg *config.Config) (*config.Config, map[string][]*proto.WorkflowDefinitionSpec) {
	if r == nil || cfg == nil {
		return cfg, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	resolved := *cfg
	resolved.Apps = maps.Clone(cfg.Apps)
	out := make(map[string][]*proto.WorkflowDefinitionSpec, len(r.byApp))
	for appName, declaration := range r.byApp {
		if declaration.entry != nil && cfg.Apps[appName] != nil {
			resolved.Apps[appName] = declaration.entry
		}
		out[appName] = slices.Clone(declaration.specs)
	}
	return &resolved, out
}

func appWorkflowDefinitionID(appName, localID string) string {
	return coreworkflow.AppDefinitionID(appName, localID)
}

func validateAppWorkflowLocalID(localID string) error {
	localID = strings.TrimSpace(localID)
	if localID == "" {
		return fmt.Errorf("workflow definition id is required")
	}
	if strings.HasPrefix(localID, coreworkflow.ConfigManagedDefinitionPrefix) {
		return fmt.Errorf("workflow definition id must not use reserved %q prefix", coreworkflow.ConfigManagedDefinitionPrefix)
	}
	if strings.HasPrefix(localID, "app_") {
		return fmt.Errorf("workflow definition id must not use reserved %q prefix", "app_")
	}
	if !appWorkflowLocalIDPattern.MatchString(localID) {
		return fmt.Errorf("workflow definition id %q must match ^[a-z0-9][a-z0-9_-]{0,63}$", localID)
	}
	return nil
}

func isAppWorkflowOwnedDefinition(existing *coreworkflow.Definition, definitionID string) bool {
	if existing == nil {
		return false
	}
	return existing.ID == definitionID && strings.HasPrefix(existing.ID, "app_")
}

func appWorkflowProtectedPrefixes(cfg *config.Config, reported map[string][]*proto.WorkflowDefinitionSpec) []string {
	if cfg == nil {
		return nil
	}
	prefixes := make([]string, 0, len(cfg.Apps))
	for appName := range cfg.Apps {
		appName = strings.TrimSpace(appName)
		if appName == "" {
			continue
		}
		if _, ok := reported[appName]; ok {
			continue
		}
		prefixes = append(prefixes, "app_"+appName+"_")
	}
	return prefixes
}

func desiredAppWorkflowDefinitions(cfg *config.Config, decls map[string][]*proto.WorkflowDefinitionSpec) (map[string]desiredWorkflowConfigDefinition, error) {
	desired := map[string]desiredWorkflowConfigDefinition{}
	if cfg == nil || len(decls) == 0 {
		return desired, nil
	}

	var defaultProvider string
	needsProvider := false
	for _, specs := range decls {
		if len(specs) > 0 {
			needsProvider = true
			break
		}
	}
	if needsProvider {
		providerName, _, err := cfg.EffectiveWorkflowProvider("")
		if err != nil {
			return nil, err
		}
		defaultProvider = strings.TrimSpace(providerName)
		if defaultProvider == "" {
			for appName, specs := range decls {
				if len(specs) > 0 {
					return nil, fmt.Errorf("bootstrap: app %q workflow definitions require a configured workflow provider", appName)
				}
			}
			return nil, fmt.Errorf("bootstrap: workflow definitions are declared by apps but no workflow provider is configured")
		}
	}

	knownApps := slices.Sorted(maps.Keys(cfg.Apps))
	for _, appName := range slices.Sorted(maps.Keys(decls)) {
		specs := decls[appName]
		for i, specProto := range specs {
			if specProto == nil {
				return nil, fmt.Errorf("bootstrap: app %q workflow definition[%d] is required", appName, i)
			}
			localID := strings.TrimSpace(specProto.GetId())
			if err := validateAppWorkflowLocalID(localID); err != nil {
				return nil, fmt.Errorf("bootstrap: app %q workflow definition %q: %w", appName, localID, err)
			}
			runAs := strings.TrimSpace(specProto.GetRunAs())
			if runAs == "" {
				return nil, fmt.Errorf("bootstrap: app %q workflow definition %q: workflow run_as is required", appName, localID)
			}
			kind, subjectID, ok := core.ParseSubjectID(runAs)
			if !ok || kind != "service_account" || subjectID == "" {
				return nil, fmt.Errorf("bootstrap: app %q workflow definition %q: workflow run_as must be service_account:<id>", appName, localID)
			}
			spec, err := workflowwire.DefinitionSpecFromProto(specProto)
			if err != nil {
				return nil, fmt.Errorf("bootstrap: app %q workflow definition %q: %w", appName, localID, err)
			}
			if spec == nil {
				return nil, fmt.Errorf("bootstrap: app %q workflow definition %q: spec is required", appName, localID)
			}
			spec.ID = appWorkflowDefinitionID(appName, localID)
			storedID := spec.ID
			if owner := coreworkflow.DefinitionOwnerApp(storedID, knownApps); owner != appName {
				return nil, fmt.Errorf(
					"bootstrap: app %q workflow definition %q has ambiguous id %q owned by configured app %q",
					appName,
					localID,
					storedID,
					owner,
				)
			}
			if _, exists := desired[storedID]; exists {
				return nil, fmt.Errorf(
					"bootstrap: workflow definition id %q is declared by both app %q and app %q",
					storedID,
					desired[storedID].FromApp,
					appName,
				)
			}
			desired[storedID] = desiredWorkflowConfigDefinition{
				DefinitionKey: localID,
				ProviderName:  defaultProvider,
				DefinitionID:  storedID,
				FromApp:       appName,
				Spec:          *spec,
			}
		}
	}
	return desired, nil
}

func mergeDesiredWorkflowDefinitions(cfgDesired, appDesired map[string]desiredWorkflowConfigDefinition) (map[string]desiredWorkflowConfigDefinition, error) {
	merged := make(map[string]desiredWorkflowConfigDefinition, len(cfgDesired)+len(appDesired))
	for definitionID := range cfgDesired {
		entry := cfgDesired[definitionID] //nolint:gocritic // map values not addressable
		if existing, ok := merged[definitionID]; ok {
			return nil, duplicateWorkflowDefinitionIDError(existing, entry)
		}
		merged[definitionID] = entry
	}
	for definitionID := range appDesired {
		entry := appDesired[definitionID] //nolint:gocritic // map values not addressable
		if existing, ok := merged[definitionID]; ok {
			return nil, duplicateWorkflowDefinitionIDError(existing, entry)
		}
		merged[definitionID] = entry
	}
	return merged, nil
}

func duplicateWorkflowDefinitionIDError(existing, entry desiredWorkflowConfigDefinition) error {
	return fmt.Errorf(
		"bootstrap: workflow definition id %q is declared by both %s and %s",
		entry.DefinitionID,
		desiredWorkflowDefinitionSource(existing),
		desiredWorkflowDefinitionSource(entry),
	)
}

func desiredWorkflowDefinitionSource(entry desiredWorkflowConfigDefinition) string {
	if appName := strings.TrimSpace(entry.FromApp); appName != "" {
		return fmt.Sprintf("app %q", appName)
	}
	return fmt.Sprintf("config key %q", entry.DefinitionKey)
}

func definitionMatchesProtectedPrefix(definitionID string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(definitionID, prefix) {
			return true
		}
	}
	return false
}
