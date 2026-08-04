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

// DefinitionOwnerApp returns the longest candidate app name that owns
// definitionID under the app_<app>_… convention. Prefer the full installed
// app set so shorter names cannot claim definitions owned by underscore
// supersets (foo vs foo_bar). With a single candidate this matches a plain
// prefix check.
func DefinitionOwnerApp(definitionID string, candidates []string) string {
	rest, ok := strings.CutPrefix(strings.TrimSpace(definitionID), "app_")
	if !ok || rest == "" {
		return ""
	}
	best := ""
	for _, app := range candidates {
		app = strings.TrimSpace(app)
		if app == "" {
			continue
		}
		if rest == app || strings.HasPrefix(rest, app+"_") {
			if len(app) >= len(best) {
				best = app
			}
		}
	}
	return best
}

// DefinitionBelongsToApp reports whether definitionID is owned by appName
// via the app_<appName>_… convention when appName is the only candidate.
// Prefer DefinitionOwnerApp with the full app set when disambiguating
// underscore-containing names.
func DefinitionBelongsToApp(definitionID, appName string) bool {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return false
	}
	return DefinitionOwnerApp(definitionID, []string{appName}) == appName
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
// knownApps should be the installed app names so underscore supersets win
// longest-match ownership; when empty, only targetApp is considered.
func RunMatchesTargetApp(run *Run, targetApp string, knownApps ...string) bool {
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
	candidates := make([]string, 0, len(knownApps)+1)
	candidates = append(candidates, app)
	for _, name := range knownApps {
		name = strings.TrimSpace(name)
		if name == "" || name == app {
			continue
		}
		candidates = append(candidates, name)
	}
	return DefinitionOwnerApp(run.DefinitionID, candidates) == app
}
