package bootstrap

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowauth"
)

func appendWorkflowAuthorizationResourceTypes(existing []*proto.AuthorizationModelResourceType, generated ...*proto.AuthorizationModelResourceType) ([]*proto.AuthorizationModelResourceType, error) {
	if len(generated) == 0 {
		return existing, nil
	}
	seen := make(map[string]struct{}, len(existing))
	for _, resourceType := range existing {
		name := strings.TrimSpace(resourceType.GetName())
		if name == "" {
			continue
		}
		seen[name] = struct{}{}
	}
	out := existing
	for _, resourceType := range generated {
		name := strings.TrimSpace(resourceType.GetName())
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("generated workflow authorization resource type %q conflicts with static authorization model", name)
		}
		seen[name] = struct{}{}
		out = append(out, resourceType)
	}
	return out, nil
}

func workflowAuthorizationResourceTypes(cfg *config.Config) []*proto.AuthorizationModelResourceType {
	if cfg == nil {
		return nil
	}
	for _, appName := range slices.Sorted(maps.Keys(cfg.Apps)) {
		entry := cfg.Apps[appName]
		if entry == nil || entry.Capabilities == nil || entry.Capabilities.Workflow == nil {
			continue
		}
		for _, operation := range entry.Capabilities.Workflow.Operations {
			if strings.TrimSpace(operation) != "" {
				return []*proto.AuthorizationModelResourceType{workflowauth.OperationResourceType()}
			}
		}
	}
	return nil
}

func workflowAuthorizationRelationships(cfg *config.Config) ([]*proto.Relationship, error) {
	if cfg == nil {
		return nil, nil
	}
	var out []*proto.Relationship
	seen := map[string]struct{}{}
	for _, appName := range slices.Sorted(maps.Keys(cfg.Apps)) {
		entry := cfg.Apps[appName]
		if entry == nil || entry.Capabilities == nil || entry.Capabilities.Workflow == nil {
			continue
		}
		appName = strings.TrimSpace(appName)
		for i, operation := range entry.Capabilities.Workflow.Operations {
			def, err := workflowAuthorizationRelationshipDef(appName, operation)
			if err != nil {
				return nil, fmt.Errorf("apps.%s.capabilities.workflow.operations[%d]: %w", appName, i, err)
			}
			key := workflowAuthorizationRelationshipKey(def)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			relationship, err := staticAuthorizationRelationship(def)
			if err != nil {
				return nil, fmt.Errorf("apps.%s.capabilities.workflow.operations[%d]: relationship: %w", appName, i, err)
			}
			out = append(out, relationship)
		}
	}
	return out, nil
}

func workflowAuthorizationRelationshipDef(appName, operation string) (config.AuthorizationRelationshipDef, error) {
	appName = strings.TrimSpace(appName)
	operation = strings.TrimSpace(operation)
	switch {
	case appName == "":
		return config.AuthorizationRelationshipDef{}, fmt.Errorf("app is required")
	case operation == "":
		return config.AuthorizationRelationshipDef{}, fmt.Errorf("operation is required")
	case !workflowauth.IsSupportedOperation(operation):
		return config.AuthorizationRelationshipDef{}, fmt.Errorf("operation %q is not supported", operation)
	}
	return config.AuthorizationRelationshipDef{
		Subject: config.AuthorizationSubjectDef{
			Type: workflowauth.SubjectTypeApp,
			ID:   appName,
		},
		Relation: workflowauth.RelationInvoker,
		Resource: config.AuthorizationResourceDef{
			Type: workflowauth.ResourceTypeOperation,
			ID:   workflowauth.OperationResourceID(appName, operation),
		},
		Properties: map[string]string{
			"app":       appName,
			"operation": operation,
		},
	}, nil
}

func workflowAuthorizationRelationshipKey(def config.AuthorizationRelationshipDef) string {
	return strings.Join([]string{
		def.Subject.Type,
		def.Subject.ID,
		def.Relation,
		def.Resource.Type,
		def.Resource.ID,
	}, "\x00")
}
