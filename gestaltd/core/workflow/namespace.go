package workflow

import (
	"fmt"
	"strings"
)

const (
	// AppManagedDefinitionPrefix marks workflow definitions owned by app migrations.
	AppManagedDefinitionPrefix = "app_"
)

// AppManagedDefinitionID returns the stored workflow definition id for an app
// migration local id.
func AppManagedDefinitionID(appName, localID string) string {
	appName = strings.TrimSpace(appName)
	localID = strings.TrimSpace(localID)
	if appName == "" || localID == "" {
		return ""
	}
	return AppManagedDefinitionPrefix + appName + "_" + localID
}

// AppManagedDefinitionNamespace returns the required prefix for definitions
// owned by the configuring app.
func AppManagedDefinitionNamespace(appName string) string {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return AppManagedDefinitionPrefix
	}
	return AppManagedDefinitionPrefix + appName + "_"
}

// ValidateAppManagedDefinitionID reports whether definitionID is owned by appName.
func ValidateAppManagedDefinitionID(appName, definitionID string) error {
	definitionID = strings.TrimSpace(definitionID)
	if definitionID == "" {
		return fmt.Errorf("workflow definition id is required")
	}
	prefix := AppManagedDefinitionNamespace(appName)
	if !strings.HasPrefix(definitionID, prefix) {
		return fmt.Errorf("workflow definition id %q must use prefix %q", definitionID, prefix)
	}
	if strings.TrimSpace(appName) != "" && definitionID == prefix {
		return fmt.Errorf("workflow definition id %q must include a local id after prefix %q", definitionID, prefix)
	}
	return nil
}
