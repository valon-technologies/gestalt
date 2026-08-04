package workflow

import (
	"encoding/base64"
	"encoding/json"
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

// OwnerKeyFromRunID extracts a provider-encoded owner_key from a run ID when
// the ID is a base64url JSON handle (temporal list summaries). Returns "" when
// the ID is not that shape.
func OwnerKeyFromRunID(runID string) string {
	raw := decodeRunIDPayload(runID)
	if len(raw) == 0 || raw[0] != '{' {
		return ""
	}
	var handle struct {
		OwnerKey string `json:"owner_key"`
	}
	if err := json.Unmarshal(raw, &handle); err != nil {
		return ""
	}
	return strings.TrimSpace(handle.OwnerKey)
}

// RunMatchesTargetApp reports whether a run belongs to targetApp for list
// filtering. Prefer hydrated step targets; fall back to definition-id ownership
// and provider run-handle owner_key when list summaries omit Target.steps.
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
	if DefinitionBelongsToApp(run.DefinitionID, app) {
		return true
	}
	if OwnerKeyFromRunID(run.ID) == app {
		return true
	}
	return false
}

func decodeRunIDPayload(runID string) []byte {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil
	}
	if raw, err := base64.RawURLEncoding.DecodeString(runID); err == nil {
		return raw
	}
	padded := runID
	if m := len(padded) % 4; m != 0 {
		padded += strings.Repeat("=", 4-m)
	}
	if raw, err := base64.URLEncoding.DecodeString(padded); err == nil {
		return raw
	}
	return nil
}
