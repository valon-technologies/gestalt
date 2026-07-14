package workflow

import (
	"encoding/base64"
	"fmt"
	"strings"
)

const (
	// AppManagedDefinitionPrefix marks workflow definitions owned by app migrations.
	AppManagedDefinitionPrefix = "app_"
	appManagedDefinitionSep    = "."
)

// AppManagedDefinitionID returns the collision-safe stored workflow definition id
// for an app migration local id. App and local segments are base64url-encoded and
// separated by a literal dot so underscore collisions cannot alias two pairs.
func AppManagedDefinitionID(appName, localID string) string {
	appName = strings.TrimSpace(appName)
	localID = strings.TrimSpace(localID)
	if appName == "" || localID == "" {
		return ""
	}
	return AppManagedDefinitionPrefix +
		encodeAppManagedSegment(appName) +
		appManagedDefinitionSep +
		encodeAppManagedSegment(localID)
}

// ValidateLocalDefinitionID reports whether localID is an app-authored local id.
// Callers must not supply reserved stored-id prefixes.
func ValidateLocalDefinitionID(localID string) error {
	localID = strings.TrimSpace(localID)
	if localID == "" {
		return fmt.Errorf("workflow definition id is required")
	}
	if strings.HasPrefix(localID, AppManagedDefinitionPrefix) {
		return fmt.Errorf("workflow definition id must be a local id, not a reserved %q prefix", AppManagedDefinitionPrefix)
	}
	if strings.HasPrefix(localID, ConfigManagedDefinitionPrefix) {
		return fmt.Errorf("workflow definition id must be a local id, not a reserved %q prefix", ConfigManagedDefinitionPrefix)
	}
	return nil
}

// ParseAppManagedDefinitionID decodes a stored app-managed definition id.
func ParseAppManagedDefinitionID(storedID string) (appName, localID string, err error) {
	storedID = strings.TrimSpace(storedID)
	if !strings.HasPrefix(storedID, AppManagedDefinitionPrefix) {
		return "", "", fmt.Errorf("workflow definition id %q is not app-managed", storedID)
	}
	body := strings.TrimPrefix(storedID, AppManagedDefinitionPrefix)
	appEncoded, localEncoded, ok := strings.Cut(body, appManagedDefinitionSep)
	if !ok || appEncoded == "" || localEncoded == "" {
		return "", "", fmt.Errorf("workflow definition id %q is not a valid app-managed id", storedID)
	}
	appName, err = decodeAppManagedSegment(appEncoded)
	if err != nil {
		return "", "", fmt.Errorf("workflow definition id %q: %w", storedID, err)
	}
	localID, err = decodeAppManagedSegment(localEncoded)
	if err != nil {
		return "", "", fmt.Errorf("workflow definition id %q: %w", storedID, err)
	}
	return appName, localID, nil
}

func encodeAppManagedSegment(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func decodeAppManagedSegment(value string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", fmt.Errorf("decode app-managed segment: %w", err)
	}
	return string(raw), nil
}
