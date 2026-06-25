package gestalt

import (
	"fmt"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func authorizationSubjectFromProto(in *proto.Subject) *AuthorizationSubject {
	if in == nil {
		return nil
	}
	return &AuthorizationSubject{Type: in.GetType(), Id: in.GetId(), Properties: mapFromStruct(in.GetProperties())}
}

func protoAuthorizationSubject(in *AuthorizationSubject) (*proto.Subject, error) {
	if in == nil {
		return nil, nil
	}
	properties, err := structFromMap(in.Properties)
	if err != nil {
		return nil, fmt.Errorf("subject properties: %w", err)
	}
	return &proto.Subject{Type: in.Type, Id: in.Id, Properties: properties}, nil
}

func authorizationActionFromProto(in *proto.Action) *AuthorizationAction {
	if in == nil {
		return nil
	}
	return &AuthorizationAction{Name: in.GetName(), Properties: mapFromStruct(in.GetProperties())}
}

func protoAuthorizationAction(in *AuthorizationAction) (*proto.Action, error) {
	if in == nil {
		return nil, nil
	}
	properties, err := structFromMap(in.Properties)
	if err != nil {
		return nil, fmt.Errorf("action properties: %w", err)
	}
	return &proto.Action{Name: in.Name, Properties: properties}, nil
}

func authorizationResourceFromProto(in *proto.Resource) *AuthorizationResource {
	if in == nil {
		return nil
	}
	return &AuthorizationResource{Type: in.GetType(), Id: in.GetId(), Properties: mapFromStruct(in.GetProperties())}
}

func protoAuthorizationResource(in *AuthorizationResource) (*proto.Resource, error) {
	if in == nil {
		return nil, nil
	}
	properties, err := structFromMap(in.Properties)
	if err != nil {
		return nil, fmt.Errorf("resource properties: %w", err)
	}
	return &proto.Resource{Type: in.Type, Id: in.Id, Properties: properties}, nil
}

func checkAccessRequestFromProto(in *proto.CheckAccessRequest) *CheckAccessRequest {
	if in == nil {
		return nil
	}
	return &CheckAccessRequest{
		Subject:  authorizationSubjectFromProto(in.GetSubject()),
		Action:   authorizationActionFromProto(in.GetAction()),
		Resource: authorizationResourceFromProto(in.GetResource()),
	}
}

func protoCheckAccessResponse(in *CheckAccessResponse) *proto.CheckAccessResponse {
	if in == nil {
		return nil
	}
	return &proto.CheckAccessResponse{Allowed: in.Allowed, ModelId: in.ModelId}
}

func checkAccessManyRequestFromProto(in *proto.CheckAccessManyRequest) *CheckAccessManyRequest {
	if in == nil {
		return nil
	}
	out := &CheckAccessManyRequest{Requests: make([]*CheckAccessRequest, 0, len(in.GetRequests()))}
	for _, req := range in.GetRequests() {
		out.Requests = append(out.Requests, checkAccessRequestFromProto(req))
	}
	return out
}

func protoCheckAccessManyResponse(in *CheckAccessManyResponse) *proto.CheckAccessManyResponse {
	if in == nil {
		return nil
	}
	out := &proto.CheckAccessManyResponse{Decisions: make([]*proto.CheckAccessResponse, 0, len(in.Decisions))}
	for _, decision := range in.Decisions {
		out.Decisions = append(out.Decisions, protoCheckAccessResponse(decision))
	}
	return out
}

func relationshipTargetFromProto(in *proto.RelationshipTarget) *RelationshipTarget {
	if in == nil {
		return nil
	}
	switch kind := in.GetKind().(type) {
	case *proto.RelationshipTarget_Subject:
		return &RelationshipTarget{Subject: authorizationSubjectFromProto(kind.Subject)}
	case *proto.RelationshipTarget_Resource:
		return &RelationshipTarget{Resource: authorizationResourceFromProto(kind.Resource)}
	case *proto.RelationshipTarget_SubjectSet:
		return &RelationshipTarget{SubjectSet: subjectSetFromProto(kind.SubjectSet)}
	default:
		return &RelationshipTarget{}
	}
}

func protoRelationshipTarget(in *RelationshipTarget) (*proto.RelationshipTarget, error) {
	if in == nil {
		return nil, nil
	}
	if in.Subject != nil {
		subject, err := protoAuthorizationSubject(in.Subject)
		if err != nil {
			return nil, err
		}
		return &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_Subject{Subject: subject}}, nil
	}
	if in.Resource != nil {
		resource, err := protoAuthorizationResource(in.Resource)
		if err != nil {
			return nil, err
		}
		return &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_Resource{Resource: resource}}, nil
	}
	if in.SubjectSet != nil {
		subjectSet, err := protoSubjectSet(in.SubjectSet)
		if err != nil {
			return nil, err
		}
		return &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_SubjectSet{SubjectSet: subjectSet}}, nil
	}
	return &proto.RelationshipTarget{}, nil
}

func subjectSetFromProto(in *proto.SubjectSet) *SubjectSet {
	if in == nil {
		return nil
	}
	return &SubjectSet{Resource: authorizationResourceFromProto(in.GetResource()), Relation: in.GetRelation()}
}

func protoSubjectSet(in *SubjectSet) (*proto.SubjectSet, error) {
	if in == nil {
		return nil, nil
	}
	resource, err := protoAuthorizationResource(in.Resource)
	if err != nil {
		return nil, err
	}
	return &proto.SubjectSet{Resource: resource, Relation: in.Relation}, nil
}

func relationshipFilterFromProto(in *proto.RelationshipFilter) *RelationshipFilter {
	if in == nil {
		return nil
	}
	return &RelationshipFilter{
		Target:           relationshipTargetFromProto(in.GetTarget()),
		Relation:         in.GetRelation(),
		Resource:         authorizationResourceFromProto(in.GetResource()),
		TargetType:       RelationshipTargetType(in.GetTargetType()),
		TargetEntityType: in.GetTargetEntityType(),
		ResourceType:     in.GetResourceType(),
		SourceLayer:      SourceLayer(in.GetSourceLayer()),
	}
}

func protoRelationshipFilter(in *RelationshipFilter) (*proto.RelationshipFilter, error) {
	if in == nil {
		return nil, nil
	}
	target, err := protoRelationshipTarget(in.Target)
	if err != nil {
		return nil, err
	}
	resource, err := protoAuthorizationResource(in.Resource)
	if err != nil {
		return nil, err
	}
	return &proto.RelationshipFilter{
		Target:           target,
		Relation:         in.Relation,
		Resource:         resource,
		TargetType:       proto.RelationshipTargetType(in.TargetType),
		TargetEntityType: in.TargetEntityType,
		ResourceType:     in.ResourceType,
		SourceLayer:      proto.SourceLayer(in.SourceLayer),
	}, nil
}

func listRelationshipsRequestFromProto(in *proto.ListRelationshipsRequest) *ListRelationshipsRequest {
	if in == nil {
		return nil
	}
	return &ListRelationshipsRequest{
		Filter:    relationshipFilterFromProto(in.GetFilter()),
		PageSize:  in.GetPageSize(),
		PageToken: in.GetPageToken(),
	}
}

func protoListRelationshipsResponse(in *ListRelationshipsResponse) (*proto.ListRelationshipsResponse, error) {
	if in == nil {
		return nil, nil
	}
	relationships, err := protoRelationships(in.Relationships)
	if err != nil {
		return nil, err
	}
	return &proto.ListRelationshipsResponse{Relationships: relationships, NextPageToken: in.NextPageToken}, nil
}

func relationshipFromProto(in *proto.Relationship) *Relationship {
	if in == nil {
		return nil
	}
	return &Relationship{
		Tuple:       relationshipTupleFromProto(in.GetTuple()),
		Properties:  mapFromStruct(in.GetProperties()),
		SourceLayer: SourceLayer(in.GetSourceLayer()),
	}
}

func protoRelationship(in *Relationship) (*proto.Relationship, error) {
	if in == nil {
		return nil, nil
	}
	tuple, err := protoRelationshipTuple(in.Tuple)
	if err != nil {
		return nil, err
	}
	properties, err := structFromMap(in.Properties)
	if err != nil {
		return nil, fmt.Errorf("relationship properties: %w", err)
	}
	return &proto.Relationship{Tuple: tuple, Properties: properties, SourceLayer: proto.SourceLayer(in.SourceLayer)}, nil
}

func relationshipsFromProto(in []*proto.Relationship) []*Relationship {
	out := make([]*Relationship, 0, len(in))
	for _, item := range in {
		out = append(out, relationshipFromProto(item))
	}
	return out
}

func protoRelationships(in []*Relationship) ([]*proto.Relationship, error) {
	out := make([]*proto.Relationship, 0, len(in))
	for i, item := range in {
		relationship, err := protoRelationship(item)
		if err != nil {
			return nil, fmt.Errorf("relationships[%d]: %w", i, err)
		}
		out = append(out, relationship)
	}
	return out, nil
}

func relationshipTupleFromProto(in *proto.RelationshipTuple) *RelationshipTuple {
	if in == nil {
		return nil
	}
	return &RelationshipTuple{
		Target:   relationshipTargetFromProto(in.GetTarget()),
		Relation: in.GetRelation(),
		Resource: authorizationResourceFromProto(in.GetResource()),
	}
}

func protoRelationshipTuple(in *RelationshipTuple) (*proto.RelationshipTuple, error) {
	if in == nil {
		return nil, nil
	}
	target, err := protoRelationshipTarget(in.Target)
	if err != nil {
		return nil, err
	}
	resource, err := protoAuthorizationResource(in.Resource)
	if err != nil {
		return nil, err
	}
	return &proto.RelationshipTuple{Target: target, Relation: in.Relation, Resource: resource}, nil
}

func addRelationshipRequestFromProto(in *proto.AddRelationshipRequest) *AddRelationshipRequest {
	if in == nil {
		return nil
	}
	return &AddRelationshipRequest{Relationship: relationshipFromProto(in.GetRelationship())}
}

func protoAddRelationshipResponse(in *AddRelationshipResponse) (*proto.AddRelationshipResponse, error) {
	if in == nil {
		return nil, nil
	}
	relationship, err := protoRelationship(in.Relationship)
	if err != nil {
		return nil, err
	}
	return &proto.AddRelationshipResponse{Relationship: relationship}, nil
}

func deleteRelationshipRequestFromProto(in *proto.DeleteRelationshipRequest) *DeleteRelationshipRequest {
	if in == nil {
		return nil
	}
	return &DeleteRelationshipRequest{RelationshipTuple: relationshipTupleFromProto(in.GetRelationshipTuple())}
}

func protoDeleteRelationshipResponse(in *DeleteRelationshipResponse) *proto.DeleteRelationshipResponse {
	if in == nil {
		return nil
	}
	return &proto.DeleteRelationshipResponse{}
}

func setAuthorizationStateRequestFromProto(in *proto.SetAuthorizationStateRequest) *SetAuthorizationStateRequest {
	if in == nil {
		return nil
	}
	return &SetAuthorizationStateRequest{
		Model:         authorizationModelFromProto(in.GetModel()),
		Relationships: relationshipsFromProto(in.GetRelationships()),
	}
}

func protoSetAuthorizationStateResponse(in *SetAuthorizationStateResponse) *proto.SetAuthorizationStateResponse {
	if in == nil {
		return nil
	}
	return &proto.SetAuthorizationStateResponse{ActiveModel: protoAuthorizationModelRef(in.ActiveModel)}
}

func authorizationModelFromProto(in *proto.AuthorizationModel) *AuthorizationModel {
	if in == nil {
		return nil
	}
	return &AuthorizationModel{
		Id:            in.GetId(),
		Version:       in.GetVersion(),
		ResourceTypes: authorizationModelResourceTypesFromProto(in.GetResourceTypes()),
	}
}

func protoAuthorizationModel(in *AuthorizationModel) (*proto.AuthorizationModel, error) {
	if in == nil {
		return nil, nil
	}
	resourceTypes, err := protoAuthorizationModelResourceTypes(in.ResourceTypes)
	if err != nil {
		return nil, err
	}
	return &proto.AuthorizationModel{Id: in.Id, Version: in.Version, ResourceTypes: resourceTypes}, nil
}

func authorizationModelRefFromProto(in *proto.AuthorizationModelRef) *AuthorizationModelRef {
	if in == nil {
		return nil
	}
	var createdAt time.Time
	if in.GetCreatedAt() != nil {
		createdAt = in.GetCreatedAt().AsTime()
	}
	return &AuthorizationModelRef{Id: in.GetId(), Version: in.GetVersion(), CreatedAt: createdAt}
}

func protoAuthorizationModelRef(in *AuthorizationModelRef) *proto.AuthorizationModelRef {
	if in == nil {
		return nil
	}
	out := &proto.AuthorizationModelRef{Id: in.Id, Version: in.Version}
	if !in.CreatedAt.IsZero() {
		out.CreatedAt = timestamppb.New(in.CreatedAt)
	}
	return out
}

func protoGetActiveModelRefResponse(in *GetActiveModelRefResponse) *proto.GetActiveModelRefResponse {
	if in == nil {
		return nil
	}
	return &proto.GetActiveModelRefResponse{Model: protoAuthorizationModelRef(in.Model)}
}

func setActiveModelRequestFromProto(in *proto.SetActiveModelRequest) *SetActiveModelRequest {
	if in == nil {
		return nil
	}
	return &SetActiveModelRequest{Model: authorizationModelFromProto(in.GetModel())}
}

func protoSetActiveModelResponse(in *SetActiveModelResponse) *proto.SetActiveModelResponse {
	if in == nil {
		return nil
	}
	return &proto.SetActiveModelResponse{Model: protoAuthorizationModelRef(in.Model)}
}

func authorizationModelResourceTypeFromProto(in *proto.AuthorizationModelResourceType) *AuthorizationModelResourceType {
	if in == nil {
		return nil
	}
	return &AuthorizationModelResourceType{
		Name:                in.GetName(),
		Relations:           modelRelationsFromProto(in.GetRelations()),
		Actions:             modelActionsFromProto(in.GetActions()),
		SourceLayer:         SourceLayer(in.GetSourceLayer()),
		DefaultAccessPolicy: DefaultAccessPolicy(in.GetDefaultAccessPolicy()),
		DefaultRole:         in.GetDefaultRole(),
	}
}

func protoAuthorizationModelResourceType(in *AuthorizationModelResourceType) (*proto.AuthorizationModelResourceType, error) {
	if in == nil {
		return nil, nil
	}
	allowedRelations, err := protoModelRelations(in.Relations)
	if err != nil {
		return nil, err
	}
	return &proto.AuthorizationModelResourceType{
		Name:                in.Name,
		Relations:           allowedRelations,
		Actions:             protoModelActions(in.Actions),
		SourceLayer:         proto.SourceLayer(in.SourceLayer),
		DefaultAccessPolicy: proto.DefaultAccessPolicy(in.DefaultAccessPolicy),
		DefaultRole:         in.DefaultRole,
	}, nil
}

func authorizationModelResourceTypesFromProto(in []*proto.AuthorizationModelResourceType) []*AuthorizationModelResourceType {
	out := make([]*AuthorizationModelResourceType, 0, len(in))
	for _, item := range in {
		out = append(out, authorizationModelResourceTypeFromProto(item))
	}
	return out
}

func protoAuthorizationModelResourceTypes(in []*AuthorizationModelResourceType) ([]*proto.AuthorizationModelResourceType, error) {
	out := make([]*proto.AuthorizationModelResourceType, 0, len(in))
	for i, item := range in {
		resourceType, err := protoAuthorizationModelResourceType(item)
		if err != nil {
			return nil, fmt.Errorf("resource_types[%d]: %w", i, err)
		}
		out = append(out, resourceType)
	}
	return out, nil
}

func modelRelationsFromProto(in []*proto.ModelRelation) []*ModelRelation {
	out := make([]*ModelRelation, 0, len(in))
	for _, item := range in {
		if item == nil {
			out = append(out, nil)
			continue
		}
		out = append(out, &ModelRelation{Name: item.GetName(), AllowedTargets: modelAllowedTargetsFromProto(item.GetAllowedTargets())})
	}
	return out
}

func protoModelRelations(in []*ModelRelation) ([]*proto.ModelRelation, error) {
	out := make([]*proto.ModelRelation, 0, len(in))
	for i, item := range in {
		if item == nil {
			out = append(out, nil)
			continue
		}
		allowedTargets, err := protoModelAllowedTargets(item.AllowedTargets)
		if err != nil {
			return nil, fmt.Errorf("relations[%d].allowed_targets: %w", i, err)
		}
		out = append(out, &proto.ModelRelation{Name: item.Name, AllowedTargets: allowedTargets})
	}
	return out, nil
}

func modelActionsFromProto(in []*proto.ModelAction) []*ModelAction {
	out := make([]*ModelAction, 0, len(in))
	for _, item := range in {
		if item == nil {
			out = append(out, nil)
			continue
		}
		out = append(out, &ModelAction{Name: item.GetName(), Relations: append([]string(nil), item.GetRelations()...)})
	}
	return out
}

func protoModelActions(in []*ModelAction) []*proto.ModelAction {
	out := make([]*proto.ModelAction, 0, len(in))
	for _, item := range in {
		if item == nil {
			out = append(out, nil)
			continue
		}
		out = append(out, &proto.ModelAction{Name: item.Name, Relations: append([]string(nil), item.Relations...)})
	}
	return out
}

func modelAllowedTargetsFromProto(in []*proto.ModelAllowedTarget) []*ModelAllowedTarget {
	out := make([]*ModelAllowedTarget, 0, len(in))
	for _, item := range in {
		if item == nil {
			out = append(out, nil)
			continue
		}
		switch kind := item.GetKind().(type) {
		case *proto.ModelAllowedTarget_SubjectType:
			out = append(out, &ModelAllowedTarget{SubjectType: kind.SubjectType})
		case *proto.ModelAllowedTarget_ResourceType:
			out = append(out, &ModelAllowedTarget{ResourceType: kind.ResourceType})
		case *proto.ModelAllowedTarget_SubjectSetType:
			out = append(out, &ModelAllowedTarget{SubjectSetType: subjectSetTypeFromProto(kind.SubjectSetType)})
		default:
			out = append(out, &ModelAllowedTarget{})
		}
	}
	return out
}

func protoModelAllowedTargets(in []*ModelAllowedTarget) ([]*proto.ModelAllowedTarget, error) {
	out := make([]*proto.ModelAllowedTarget, 0, len(in))
	for _, item := range in {
		out = append(out, protoModelAllowedTarget(item))
	}
	return out, nil
}

func protoModelAllowedTarget(in *ModelAllowedTarget) *proto.ModelAllowedTarget {
	if in == nil {
		return nil
	}
	if in.SubjectType != "" {
		return &proto.ModelAllowedTarget{Kind: &proto.ModelAllowedTarget_SubjectType{SubjectType: in.SubjectType}}
	}
	if in.ResourceType != "" {
		return &proto.ModelAllowedTarget{Kind: &proto.ModelAllowedTarget_ResourceType{ResourceType: in.ResourceType}}
	}
	if in.SubjectSetType != nil {
		return &proto.ModelAllowedTarget{Kind: &proto.ModelAllowedTarget_SubjectSetType{SubjectSetType: protoSubjectSetType(in.SubjectSetType)}}
	}
	return &proto.ModelAllowedTarget{}
}

func subjectSetTypeFromProto(in *proto.SubjectSetType) *SubjectSetType {
	if in == nil {
		return nil
	}
	return &SubjectSetType{ResourceType: in.GetResourceType(), Relation: in.GetRelation()}
}

func protoSubjectSetType(in *SubjectSetType) *proto.SubjectSetType {
	if in == nil {
		return nil
	}
	return &proto.SubjectSetType{ResourceType: in.ResourceType, Relation: in.Relation}
}

func listActiveModelResourceTypesRequestFromProto(in *proto.ListActiveModelResourceTypesRequest) *ListActiveModelResourceTypesRequest {
	if in == nil {
		return nil
	}
	return &ListActiveModelResourceTypesRequest{
		Filter:    authorizationModelResourceTypeFilterFromProto(in.GetFilter()),
		PageSize:  in.GetPageSize(),
		PageToken: in.GetPageToken(),
	}
}

func authorizationModelResourceTypeFilterFromProto(in *proto.AuthorizationModelResourceTypeFilter) *AuthorizationModelResourceTypeFilter {
	if in == nil {
		return nil
	}
	return &AuthorizationModelResourceTypeFilter{Name: in.GetName(), SourceLayer: SourceLayer(in.GetSourceLayer())}
}

func protoListActiveModelResourceTypesResponse(in *ListActiveModelResourceTypesResponse) (*proto.ListActiveModelResourceTypesResponse, error) {
	if in == nil {
		return nil, nil
	}
	resourceTypes, err := protoAuthorizationModelResourceTypes(in.ResourceTypes)
	if err != nil {
		return nil, err
	}
	return &proto.ListActiveModelResourceTypesResponse{
		ResourceTypes: resourceTypes,
		NextPageToken: in.NextPageToken,
		ModelId:       in.ModelId,
	}, nil
}
