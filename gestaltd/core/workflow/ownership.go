package workflow

import (
	"strings"
)

// AppDefinitionIDPrefix is the stable ownership prefix for app-owned
// workflow definitions: app_<appName>_<localId>.
func AppDefinitionIDPrefix(appName string) string {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return ""
	}
	return "app_" + appName + "_"
}

// AppDefinitionID builds the canonical app-owned definition id
// app_<appName>_<localId>.
func AppDefinitionID(appName, localID string) string {
	return AppDefinitionIDPrefix(appName) + strings.TrimSpace(localID)
}

// DefinitionBelongsToApp reports whether definitionID is owned by appName
// via the app_<appName>_… convention.
func DefinitionBelongsToApp(definitionID, appName string) bool {
	prefix := AppDefinitionIDPrefix(appName)
	if prefix == "" {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(definitionID), prefix)
}

// TargetReferencesApp reports whether any step on target invokes appName.
func TargetReferencesApp(target Target, appName string) bool {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return false
	}
	for i := range target.Steps {
		if target.Steps[i].App != nil && strings.TrimSpace(target.Steps[i].App.Name) == appName {
			return true
		}
	}
	return false
}

// RunMatchesTargetApp reports whether a run belongs to targetApp for list
// filtering. Prefer hydrated step targets; when list summaries omit
// Target.steps, fall back to app-owned definition IDs (app_<app>_…).
func RunMatchesTargetApp(run *Run, targetApp string) bool {
	if run == nil {
		return false
	}
	app := strings.TrimSpace(targetApp)
	if app == "" {
		return true
	}
	if TargetReferencesApp(run.Target, app) {
		return true
	}
	return DefinitionBelongsToApp(run.DefinitionID, app)
}
