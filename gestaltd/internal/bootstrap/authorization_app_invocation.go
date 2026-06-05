package bootstrap

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

const (
	appInvocationAuthorizationSubjectTypeApp        = "app"
	appInvocationAuthorizationResourceTypeOperation = "gestalt.app.operation"
	appInvocationAuthorizationResourceTypeSurface   = "gestalt.app.surface"
	appInvocationAuthorizationRelationInvoker       = "invoker"
	appInvocationAuthorizationActionInvoke          = "invoke"
)

func appendAppInvocationAuthorizationResourceTypes(existing []*proto.AuthorizationModelResourceType, generated ...*proto.AuthorizationModelResourceType) ([]*proto.AuthorizationModelResourceType, error) {
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
			return nil, fmt.Errorf("generated app invocation resource type %q conflicts with static authorization model", name)
		}
		seen[name] = struct{}{}
		out = append(out, resourceType)
	}
	return out, nil
}

func appInvocationAuthorizationResourceTypes(cfg *config.Config) []*proto.AuthorizationModelResourceType {
	if cfg == nil {
		return nil
	}
	needsOperationResource := false
	needsSurfaceResource := false
	for _, appName := range slices.Sorted(maps.Keys(cfg.Apps)) {
		entry := cfg.Apps[appName]
		if entry == nil {
			continue
		}
		for _, invoke := range entry.Invokes {
			if strings.TrimSpace(invoke.Operation) != "" {
				needsOperationResource = true
			}
			if strings.TrimSpace(invoke.Surface) != "" {
				needsSurfaceResource = true
			}
		}
	}
	var out []*proto.AuthorizationModelResourceType
	if needsOperationResource {
		out = append(out, appInvocationAuthorizationResourceType(appInvocationAuthorizationResourceTypeOperation))
	}
	if needsSurfaceResource {
		out = append(out, appInvocationAuthorizationResourceType(appInvocationAuthorizationResourceTypeSurface))
	}
	return out
}

func appInvocationAuthorizationResourceType(name string) *proto.AuthorizationModelResourceType {
	return &proto.AuthorizationModelResourceType{
		Name:                name,
		SourceLayer:         proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG,
		DefaultAccessPolicy: proto.DefaultAccessPolicy_DEFAULT_ACCESS_POLICY_DENY,
		Relations: []*proto.ModelRelation{{
			Name: appInvocationAuthorizationRelationInvoker,
			AllowedTargets: []*proto.ModelAllowedTarget{{
				Kind: &proto.ModelAllowedTarget_SubjectType{SubjectType: appInvocationAuthorizationSubjectTypeApp},
			}},
		}},
		Actions: []*proto.ModelAction{{
			Name:      appInvocationAuthorizationActionInvoke,
			Relations: []string{appInvocationAuthorizationRelationInvoker},
		}},
	}
}

func appInvocationAuthorizationRelationships(cfg *config.Config) ([]*proto.Relationship, error) {
	if cfg == nil {
		return nil, nil
	}
	var out []*proto.Relationship
	seen := map[string]struct{}{}
	for _, callerApp := range slices.Sorted(maps.Keys(cfg.Apps)) {
		entry := cfg.Apps[callerApp]
		if entry == nil {
			continue
		}
		callerApp = strings.TrimSpace(callerApp)
		for i, invoke := range entry.Invokes {
			def, err := appInvocationAuthorizationRelationshipDef(callerApp, invoke)
			if err != nil {
				return nil, fmt.Errorf("apps.%s.invokes[%d]: %w", callerApp, i, err)
			}
			key := appInvocationAuthorizationRelationshipKey(def)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			relationship, err := staticAuthorizationRelationship(def)
			if err != nil {
				return nil, fmt.Errorf("apps.%s.invokes[%d]: relationship: %w", callerApp, i, err)
			}
			out = append(out, relationship)
		}
	}
	return out, nil
}

func appInvocationAuthorizationRelationshipDef(callerApp string, invoke config.AppInvocationDependency) (config.AuthorizationRelationshipDef, error) {
	callerApp = strings.TrimSpace(callerApp)
	targetApp := strings.TrimSpace(invoke.App)
	operation := strings.TrimSpace(invoke.Operation)
	surface := strings.ToLower(strings.TrimSpace(invoke.Surface))
	switch {
	case callerApp == "":
		return config.AuthorizationRelationshipDef{}, fmt.Errorf("caller app is required")
	case targetApp == "":
		return config.AuthorizationRelationshipDef{}, fmt.Errorf("app is required")
	case operation == "" && surface == "":
		return config.AuthorizationRelationshipDef{}, fmt.Errorf("operation or surface is required")
	case operation != "" && surface != "":
		return config.AuthorizationRelationshipDef{}, fmt.Errorf("only one of operation or surface may be set")
	case surface != "" && surface != string(config.SpecSurfaceGraphQL):
		return config.AuthorizationRelationshipDef{}, fmt.Errorf("surface %q is not supported", surface)
	}
	credentialMode := core.NormalizeOptionalConnectionMode(core.ConnectionMode(invoke.CredentialMode))
	if credentialMode != "" && credentialMode != core.ConnectionModeNone && credentialMode != core.ConnectionModeSubject {
		return config.AuthorizationRelationshipDef{}, fmt.Errorf("credentialMode %q is not supported", invoke.CredentialMode)
	}

	var resource config.AuthorizationResourceDef
	properties := map[string]string{
		"caller_app": callerApp,
		"target_app": targetApp,
	}
	if credentialMode != "" {
		properties["credential_mode"] = string(credentialMode)
	}
	if operation != "" {
		properties["operation"] = operation
		resource = config.AuthorizationResourceDef{
			Type: appInvocationAuthorizationResourceTypeOperation,
			ID:   appInvocationOperationResourceID(targetApp, operation),
		}
	} else {
		properties["surface"] = surface
		resource = config.AuthorizationResourceDef{
			Type: appInvocationAuthorizationResourceTypeSurface,
			ID:   appInvocationSurfaceResourceID(targetApp, surface),
		}
	}
	if runAs := invoke.RunAsSubject(); operation != "" && runAs != nil {
		properties["run_as_subject_id"] = runAs.SubjectID
		if runAs.CredentialSubjectID != "" {
			properties["run_as_credential_subject_id"] = runAs.CredentialSubjectID
		}
		if invoke.RunAs != nil {
			properties["run_as_apply_by_default"] = strconv.FormatBool(invoke.RunAsAppliesByDefault())
		}
	}

	return config.AuthorizationRelationshipDef{
		Subject: config.AuthorizationSubjectDef{
			Type: appInvocationAuthorizationSubjectTypeApp,
			ID:   callerApp,
		},
		Relation:   appInvocationAuthorizationRelationInvoker,
		Resource:   resource,
		Properties: properties,
	}, nil
}

func appInvocationAuthorizationRelationshipKey(def config.AuthorizationRelationshipDef) string {
	return strings.Join([]string{
		def.Subject.Type,
		def.Subject.ID,
		def.Relation,
		def.Resource.Type,
		def.Resource.ID,
	}, "\x00")
}

func appInvocationOperationResourceID(appName, operation string) string {
	return strings.TrimSpace(appName) + "/operations/" + strings.TrimSpace(operation)
}

func appInvocationSurfaceResourceID(appName, surface string) string {
	return strings.TrimSpace(appName) + "/surfaces/" + strings.ToLower(strings.TrimSpace(surface))
}
