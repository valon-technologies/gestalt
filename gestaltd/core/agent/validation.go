package agent

import (
	"fmt"
	"strings"
)

func ValidateMCPCatalogToolRefs(refs []ToolRef, fieldName string) error {
	fieldName = strings.TrimSpace(fieldName)
	if fieldName == "" {
		fieldName = "toolRefs"
	}
	for i := range refs {
		ref := refs[i]
		system := strings.TrimSpace(ref.System)
		pluginName := strings.TrimSpace(ref.App)
		operation := strings.TrimSpace(ref.Operation)
		connection := strings.TrimSpace(ref.Connection)
		instance := strings.TrimSpace(ref.Instance)
		if system != "" && system != SystemToolWorkflow {
			return fmt.Errorf("mcp catalog %s[%d].system %q is not supported", fieldName, i, system)
		}
		if system != "" && pluginName != "" {
			return fmt.Errorf("mcp catalog %s[%d] must set exactly one of app or system", fieldName, i)
		}
		if system != "" {
			if operation == "" {
				return fmt.Errorf("mcp catalog %s[%d].operation is required for system refs", fieldName, i)
			}
			if operation == "*" {
				return fmt.Errorf("mcp catalog %s[%d].operation wildcard is not supported", fieldName, i)
			}
			if connection != "" || instance != "" || ref.CredentialMode != "" || ref.RunAs != nil || ref.RunAsExternalIdentity != nil || strings.TrimSpace(ref.Title) != "" || strings.TrimSpace(ref.Description) != "" {
				return fmt.Errorf("mcp catalog %s[%d] system refs cannot include connection, instance, credential mode, runAs, runAs external identity, title, or description", fieldName, i)
			}
			continue
		}
		if pluginName == "" {
			return fmt.Errorf("mcp catalog %s[%d].app is required", fieldName, i)
		}
		if operation == "*" || connection == "*" || instance == "*" {
			return fmt.Errorf("mcp catalog %s[%d] wildcard fields are not supported", fieldName, i)
		}
		if pluginName == "*" && (operation != "" || connection != "" || instance != "" || ref.CredentialMode != "" || ref.RunAs != nil || ref.RunAsExternalIdentity != nil || strings.TrimSpace(ref.Title) != "" || strings.TrimSpace(ref.Description) != "") {
			return fmt.Errorf("mcp catalog %s[%d] global ref cannot include operation, connection, instance, credential mode, runAs, runAs external identity, title, or description", fieldName, i)
		}
		if ref.RunAs != nil && operation == "" {
			return fmt.Errorf("mcp catalog %s[%d].runAs requires an exact operation", fieldName, i)
		}
		if ref.RunAsExternalIdentity != nil && ref.RunAs == nil {
			return fmt.Errorf("mcp catalog %s[%d].runAs.externalIdentity requires runAs.subject", fieldName, i)
		}
		if ref.RunAsExternalIdentity != nil && operation == "" {
			return fmt.Errorf("mcp catalog %s[%d].runAs.externalIdentity requires an exact operation", fieldName, i)
		}
	}
	return nil
}
