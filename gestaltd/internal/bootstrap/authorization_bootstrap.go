package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/protoutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

func bootstrapAuthorizationProviderState(ctx context.Context, cfg *config.Config, providers map[string]core.AuthorizationProvider) error {
	if len(providers) == 0 {
		return nil
	}
	name, provider, err := selectedAuthorizationProviderInstance(cfg, providers)
	if err != nil {
		return err
	}
	if provider == nil {
		return nil
	}
	model, err := staticAuthorizationModel(cfg)
	if err != nil {
		return fmt.Errorf("bootstrap: authorization provider %q: %w", name, err)
	}
	runtimeResourceTypes, err := listRuntimeAuthorizationModelResourceTypes(ctx, provider)
	if err != nil {
		return fmt.Errorf("bootstrap: authorization provider %q: %w", name, err)
	}
	model.ResourceTypes = mergedAuthorizationModelResourceTypes(model.GetResourceTypes(), runtimeResourceTypes)
	if err := stampAuthorizationModel(model, time.Now()); err != nil {
		return fmt.Errorf("bootstrap: authorization provider %q: %w", name, err)
	}
	staticRelationships, err := staticAuthorizationRelationships(cfg.Authorization)
	if err != nil {
		return fmt.Errorf("bootstrap: authorization provider %q: %w", name, err)
	}
	workflowRelationships, err := workflowAuthorizationRelationships(cfg)
	if err != nil {
		return fmt.Errorf("bootstrap: authorization provider %q: %w", name, err)
	}
	staticRelationships = append(staticRelationships, workflowRelationships...)
	runtimeRelationships, err := listRuntimeAuthorizationRelationships(ctx, provider)
	if err != nil {
		return fmt.Errorf("bootstrap: authorization provider %q: %w", name, err)
	}
	staticRelationships = append(staticRelationships, runtimeRelationships...)
	if _, err := provider.SetAuthorizationState(ctx, &proto.SetAuthorizationStateRequest{
		Model:         model,
		Relationships: staticRelationships,
	}); err != nil {
		return fmt.Errorf("bootstrap: authorization provider %q: set authorization state: %w", name, err)
	}
	return nil
}

func selectedAuthorizationProviderInstance(cfg *config.Config, providers map[string]core.AuthorizationProvider) (string, core.AuthorizationProvider, error) {
	name, entry, err := cfg.SelectedAuthorizationProvider()
	if err != nil {
		return "", nil, err
	}
	if entry == nil {
		return "", nil, nil
	}
	return name, providers[name], nil
}

func staticAuthorizationModel(cfg *config.Config) (*proto.AuthorizationModel, error) {
	model := &proto.AuthorizationModel{}
	for _, modelName := range slices.Sorted(maps.Keys(cfg.Authorization.Models)) {
		modelDef := cfg.Authorization.Models[modelName]
		for _, resourceTypeName := range slices.Sorted(maps.Keys(modelDef.ResourceTypes)) {
			resourceType, err := staticAuthorizationResourceType(resourceTypeName, modelDef.ResourceTypes[resourceTypeName])
			if err != nil {
				return nil, fmt.Errorf("model %q resource type %q: %w", modelName, resourceTypeName, err)
			}
			model.ResourceTypes = append(model.ResourceTypes, resourceType)
		}
	}
	var err error
	model.ResourceTypes, err = appendWorkflowAuthorizationResourceTypes(model.ResourceTypes, workflowAuthorizationResourceTypes(cfg)...)
	if err != nil {
		return nil, err
	}
	return model, nil
}

func stampAuthorizationModel(model *proto.AuthorizationModel, now time.Time) error {
	id, err := authorizationModelContentHash(model)
	if err != nil {
		return err
	}
	model.Id = id
	model.Version = strconv.FormatInt(now.Unix(), 10)
	return nil
}

func authorizationModelContentHash(model *proto.AuthorizationModel) (string, error) {
	content := gproto.Clone(model).(*proto.AuthorizationModel)
	content.Id = ""
	content.Version = ""
	data, err := gproto.MarshalOptions{Deterministic: true}.Marshal(content)
	if err != nil {
		return "", fmt.Errorf("hash authorization model content: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func mergedAuthorizationModelResourceTypes(staticResourceTypes, runtimeResourceTypes []*proto.AuthorizationModelResourceType) []*proto.AuthorizationModelResourceType {
	out := make([]*proto.AuthorizationModelResourceType, 0, len(staticResourceTypes)+len(runtimeResourceTypes))
	staticNames := make(map[string]struct{}, len(staticResourceTypes))
	for _, resourceType := range staticResourceTypes {
		name := strings.TrimSpace(resourceType.GetName())
		staticNames[name] = struct{}{}
		out = append(out, resourceType)
	}
	for _, resourceType := range runtimeResourceTypes {
		name := strings.TrimSpace(resourceType.GetName())
		if _, ok := staticNames[name]; ok {
			continue
		}
		out = append(out, resourceType)
	}
	return out
}

func defaultRole(def config.AuthorizationResourceTypeDef) string {
	return strings.TrimSpace(def.DefaultRole)
}

func staticAuthorizationResourceType(name string, def config.AuthorizationResourceTypeDef) (*proto.AuthorizationModelResourceType, error) {
	resourceType := &proto.AuthorizationModelResourceType{
		Name:        strings.TrimSpace(name),
		SourceLayer: proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG,
		DefaultRole: defaultRole(def),
	}
	for _, relationName := range slices.Sorted(maps.Keys(def.Relations)) {
		relation := def.Relations[relationName]
		allowedTargets, err := staticAuthorizationAllowedTargets(relation.SubjectTypes, relation.AllowedTargets)
		if err != nil {
			return nil, fmt.Errorf("relations.%s.allowedTargets: %w", relationName, err)
		}
		resourceType.Relations = append(resourceType.Relations, &proto.ModelRelation{
			Name:           strings.TrimSpace(relationName),
			AllowedTargets: allowedTargets,
		})
	}
	for _, actionName := range slices.Sorted(maps.Keys(def.Actions)) {
		action := def.Actions[actionName]
		resourceType.Actions = append(resourceType.Actions, &proto.ModelAction{
			Name:      strings.TrimSpace(actionName),
			Relations: append([]string(nil), action.Relations...),
		})
	}
	return resourceType, nil
}

func staticAuthorizationAllowedTargets(subjectTypes []string, targets []config.AuthorizationAllowedTargetDef) ([]*proto.ModelAllowedTarget, error) {
	out := make([]*proto.ModelAllowedTarget, 0, len(subjectTypes)+len(targets))
	for _, subjectType := range subjectTypes {
		subjectType = strings.TrimSpace(subjectType)
		if subjectType == "" {
			continue
		}
		out = append(out, &proto.ModelAllowedTarget{
			Kind: &proto.ModelAllowedTarget_SubjectType{SubjectType: subjectType},
		})
	}
	for i, target := range targets {
		switch {
		case strings.TrimSpace(target.SubjectType) != "":
			out = append(out, &proto.ModelAllowedTarget{
				Kind: &proto.ModelAllowedTarget_SubjectType{SubjectType: strings.TrimSpace(target.SubjectType)},
			})
		case strings.TrimSpace(target.ResourceType) != "":
			out = append(out, &proto.ModelAllowedTarget{
				Kind: &proto.ModelAllowedTarget_ResourceType{ResourceType: strings.TrimSpace(target.ResourceType)},
			})
		case target.SubjectSet != nil:
			out = append(out, &proto.ModelAllowedTarget{
				Kind: &proto.ModelAllowedTarget_SubjectSetType{SubjectSetType: &proto.SubjectSetType{
					ResourceType: strings.TrimSpace(target.SubjectSet.ResourceType),
					Relation:     strings.TrimSpace(target.SubjectSet.Relation),
				}},
			})
		default:
			return nil, fmt.Errorf("[%d]: target is required", i)
		}
	}
	return out, nil
}

func staticAuthorizationRelationships(cfg config.AuthorizationConfig) ([]*proto.Relationship, error) {
	out := make([]*proto.Relationship, 0, len(cfg.Relationships))
	for i := range cfg.Relationships {
		relationship, err := staticAuthorizationRelationship(cfg.Relationships[i])
		if err != nil {
			return nil, fmt.Errorf("relationships[%d]: %w", i, err)
		}
		out = append(out, relationship)
	}
	return out, nil
}

func staticAuthorizationRelationship(def config.AuthorizationRelationshipDef) (*proto.Relationship, error) {
	properties, err := stringPropertiesStruct(def.Properties)
	if err != nil {
		return nil, err
	}
	return &proto.Relationship{
		Tuple: &proto.RelationshipTuple{
			Target:   staticAuthorizationRelationshipTarget(def),
			Relation: strings.TrimSpace(def.Relation),
			Resource: staticAuthorizationResource(def.Resource),
		},
		Properties:  properties,
		SourceLayer: proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG,
	}, nil
}

func staticAuthorizationRelationshipTarget(def config.AuthorizationRelationshipDef) *proto.RelationshipTarget {
	switch {
	case def.Target.Subject != nil:
		return &proto.RelationshipTarget{
			Kind: &proto.RelationshipTarget_Subject{Subject: staticAuthorizationSubject(*def.Target.Subject)},
		}
	case def.Target.Resource != nil:
		return &proto.RelationshipTarget{
			Kind: &proto.RelationshipTarget_Resource{Resource: staticAuthorizationResource(*def.Target.Resource)},
		}
	case def.Target.SubjectSet != nil:
		return &proto.RelationshipTarget{
			Kind: &proto.RelationshipTarget_SubjectSet{SubjectSet: &proto.SubjectSet{
				Resource: staticAuthorizationResource(def.Target.SubjectSet.Resource),
				Relation: strings.TrimSpace(def.Target.SubjectSet.Relation),
			}},
		}
	default:
		return &proto.RelationshipTarget{
			Kind: &proto.RelationshipTarget_Subject{Subject: staticAuthorizationSubject(def.Subject)},
		}
	}
}

func staticAuthorizationSubject(def config.AuthorizationSubjectDef) *proto.Subject {
	if strings.TrimSpace(def.Type) == "" && strings.TrimSpace(def.ID) == "" {
		return nil
	}
	properties, _ := stringPropertiesStruct(def.Properties)
	return &proto.Subject{
		Type:       strings.TrimSpace(def.Type),
		Id:         strings.TrimSpace(def.ID),
		Properties: properties,
	}
}

func staticAuthorizationResource(def config.AuthorizationResourceDef) *proto.Resource {
	if strings.TrimSpace(def.Type) == "" && strings.TrimSpace(def.ID) == "" {
		return nil
	}
	properties, _ := stringPropertiesStruct(def.Properties)
	return &proto.Resource{
		Type:       strings.TrimSpace(def.Type),
		Id:         strings.TrimSpace(def.ID),
		Properties: properties,
	}
}

func stringPropertiesStruct(properties map[string]string) (*structpb.Struct, error) {
	if len(properties) == 0 {
		return nil, nil
	}
	values := make(map[string]any, len(properties))
	for key, value := range properties {
		values[key] = value
	}
	return protoutil.StructFromMap(values)
}

func listRuntimeAuthorizationRelationships(ctx context.Context, provider core.AuthorizationProvider) ([]*proto.Relationship, error) {
	var out []*proto.Relationship
	pageToken := ""
	for {
		resp, err := provider.ListRelationships(ctx, &proto.ListRelationshipsRequest{
			Filter: &proto.RelationshipFilter{
				SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME,
			},
			PageSize:  500,
			PageToken: pageToken,
		})
		if err != nil {
			return nil, fmt.Errorf("list runtime relationships: %w", err)
		}
		for _, relationship := range resp.GetRelationships() {
			if relationship.GetSourceLayer() != proto.SourceLayer_SOURCE_LAYER_RUNTIME {
				continue
			}
			out = append(out, gproto.Clone(relationship).(*proto.Relationship))
		}
		pageToken = strings.TrimSpace(resp.GetNextPageToken())
		if pageToken == "" {
			return out, nil
		}
	}
}

func listRuntimeAuthorizationModelResourceTypes(ctx context.Context, provider core.AuthorizationProvider) ([]*proto.AuthorizationModelResourceType, error) {
	var out []*proto.AuthorizationModelResourceType
	pageToken := ""
	for {
		resp, err := provider.ListActiveModelResourceTypes(ctx, &proto.ListActiveModelResourceTypesRequest{
			Filter: &proto.AuthorizationModelResourceTypeFilter{
				SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME,
			},
			PageSize:  500,
			PageToken: pageToken,
		})
		if err != nil {
			if status.Code(err) == codes.NotFound {
				return out, nil
			}
			return nil, fmt.Errorf("list runtime model resource types: %w", err)
		}
		for _, resourceType := range resp.GetResourceTypes() {
			if resourceType.GetSourceLayer() != proto.SourceLayer_SOURCE_LAYER_RUNTIME {
				continue
			}
			out = append(out, gproto.Clone(resourceType).(*proto.AuthorizationModelResourceType))
		}
		pageToken = strings.TrimSpace(resp.GetNextPageToken())
		if pageToken == "" {
			return out, nil
		}
	}
}
