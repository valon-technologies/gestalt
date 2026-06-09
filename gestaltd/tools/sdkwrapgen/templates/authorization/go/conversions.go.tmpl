package authorization

import (
	"fmt"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func CheckAccessRequestFromProto(in *proto.CheckAccessRequest) *CheckAccessRequest {
	return checkAccessRequestFromProto(in)
}

func CheckAccessRequestToProto(in *CheckAccessRequest) (*proto.CheckAccessRequest, error) {
	if in == nil {
		return nil, nil
	}
	subject, err := protoSubject(in.Subject)
	if err != nil {
		return nil, err
	}
	action, err := protoAction(in.Action)
	if err != nil {
		return nil, err
	}
	resource, err := protoResource(in.Resource)
	if err != nil {
		return nil, err
	}
	return &proto.CheckAccessRequest{Subject: subject, Action: action, Resource: resource}, nil
}

func CheckAccessResponseFromProto(in *proto.CheckAccessResponse) *CheckAccessResponse {
	if in == nil {
		return nil
	}
	return &CheckAccessResponse{Allowed: in.GetAllowed(), ModelID: in.GetModelId()}
}

func CheckAccessResponseToProto(in *CheckAccessResponse) *proto.CheckAccessResponse {
	return protoCheckAccessResponse(in)
}

func CheckAccessManyRequestFromProto(in *proto.CheckAccessManyRequest) *CheckAccessManyRequest {
	return checkAccessManyRequestFromProto(in)
}

func CheckAccessManyRequestToProto(in *CheckAccessManyRequest) (*proto.CheckAccessManyRequest, error) {
	if in == nil {
		return nil, nil
	}
	out := &proto.CheckAccessManyRequest{Requests: make([]*proto.CheckAccessRequest, 0, len(in.Requests))}
	for i, item := range in.Requests {
		req, err := CheckAccessRequestToProto(item)
		if err != nil {
			return nil, fmt.Errorf("requests[%d]: %w", i, err)
		}
		out.Requests = append(out.Requests, req)
	}
	return out, nil
}

func CheckAccessManyResponseFromProto(in *proto.CheckAccessManyResponse) *CheckAccessManyResponse {
	if in == nil {
		return nil
	}
	out := &CheckAccessManyResponse{Decisions: make([]*CheckAccessResponse, 0, len(in.GetDecisions()))}
	for _, item := range in.GetDecisions() {
		out.Decisions = append(out.Decisions, CheckAccessResponseFromProto(item))
	}
	return out
}

func CheckAccessManyResponseToProto(in *CheckAccessManyResponse) *proto.CheckAccessManyResponse {
	return protoCheckAccessManyResponse(in)
}

func authorizationSubjectFromProto(in *proto.Subject) *Subject {
	if in == nil {
		return nil
	}
	return &Subject{Type: in.GetType(), ID: in.GetId(), Properties: mapFromStruct(in.GetProperties())}
}

func protoSubject(in *Subject) (*proto.Subject, error) {
	if in == nil {
		return nil, nil
	}
	properties, err := structFromMap(in.Properties)
	if err != nil {
		return nil, fmt.Errorf("subject properties: %w", err)
	}
	return &proto.Subject{Type: in.Type, Id: in.ID, Properties: properties}, nil
}

func authorizationActionFromProto(in *proto.Action) *Action {
	if in == nil {
		return nil
	}
	return &Action{Name: in.GetName(), Properties: mapFromStruct(in.GetProperties())}
}

func protoAction(in *Action) (*proto.Action, error) {
	if in == nil {
		return nil, nil
	}
	properties, err := structFromMap(in.Properties)
	if err != nil {
		return nil, fmt.Errorf("action properties: %w", err)
	}
	return &proto.Action{Name: in.Name, Properties: properties}, nil
}

func authorizationResourceFromProto(in *proto.Resource) *Resource {
	if in == nil {
		return nil
	}
	return &Resource{Type: in.GetType(), ID: in.GetId(), Properties: mapFromStruct(in.GetProperties())}
}

func protoResource(in *Resource) (*proto.Resource, error) {
	if in == nil {
		return nil, nil
	}
	properties, err := structFromMap(in.Properties)
	if err != nil {
		return nil, fmt.Errorf("resource properties: %w", err)
	}
	return &proto.Resource{Type: in.Type, Id: in.ID, Properties: properties}, nil
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
	return &proto.CheckAccessResponse{Allowed: in.Allowed, ModelId: in.ModelID}
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

func relationshipTargetFromProto(in *proto.RelationshipTarget) RelationshipTarget {
	if in == nil {
		return nil
	}
	switch kind := in.GetKind().(type) {
	case *proto.RelationshipTarget_Subject:
		subject := authorizationSubjectFromProto(kind.Subject)
		if subject == nil {
			return RelationshipTargetSubject{}
		}
		return RelationshipTargetSubject{Subject: *subject}
	case *proto.RelationshipTarget_Resource:
		resource := authorizationResourceFromProto(kind.Resource)
		if resource == nil {
			return RelationshipTargetResource{}
		}
		return RelationshipTargetResource{Resource: *resource}
	case *proto.RelationshipTarget_SubjectSet:
		subjectSet := subjectSetFromProto(kind.SubjectSet)
		if subjectSet == nil {
			return RelationshipTargetSubjectSet{}
		}
		return RelationshipTargetSubjectSet{SubjectSet: *subjectSet}
	default:
		return RelationshipTargetUnset{}
	}
}

func protoRelationshipTarget(in RelationshipTarget) (*proto.RelationshipTarget, error) {
	if in == nil {
		return nil, nil
	}
	switch target := in.(type) {
	case RelationshipTargetSubject:
		subject, err := protoSubject(&target.Subject)
		if err != nil {
			return nil, err
		}
		return &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_Subject{Subject: subject}}, nil
	case RelationshipTargetResource:
		resource, err := protoResource(&target.Resource)
		if err != nil {
			return nil, err
		}
		return &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_Resource{Resource: resource}}, nil
	case RelationshipTargetSubjectSet:
		subjectSet, err := protoSubjectSet(&target.SubjectSet)
		if err != nil {
			return nil, err
		}
		return &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_SubjectSet{SubjectSet: subjectSet}}, nil
	case RelationshipTargetUnset:
		return &proto.RelationshipTarget{}, nil
	default:
		return nil, fmt.Errorf("unsupported relationship target %T", in)
	}
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
	resource, err := protoResource(in.Resource)
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
	resource, err := protoResource(in.Resource)
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
	resource, err := protoResource(in.Resource)
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
	return &proto.SetAuthorizationStateResponse{ActiveModel: protoModelRef(in.ActiveModel)}
}

func authorizationModelFromProto(in *proto.AuthorizationModel) *Model {
	if in == nil {
		return nil
	}
	return &Model{
		ID:            in.GetId(),
		Version:       in.GetVersion(),
		ResourceTypes: authorizationModelResourceTypesFromProto(in.GetResourceTypes()),
	}
}

func protoModel(in *Model) (*proto.AuthorizationModel, error) {
	if in == nil {
		return nil, nil
	}
	resourceTypes, err := protoModelResourceTypes(in.ResourceTypes)
	if err != nil {
		return nil, err
	}
	return &proto.AuthorizationModel{Id: in.ID, Version: in.Version, ResourceTypes: resourceTypes}, nil
}

func authorizationModelRefFromProto(in *proto.AuthorizationModelRef) *ModelRef {
	if in == nil {
		return nil
	}
	var createdAt *time.Time
	if in.GetCreatedAt() != nil {
		value := in.GetCreatedAt().AsTime()
		createdAt = &value
	}
	return &ModelRef{ID: in.GetId(), Version: in.GetVersion(), CreatedAt: createdAt}
}

func protoModelRef(in *ModelRef) *proto.AuthorizationModelRef {
	if in == nil {
		return nil
	}
	out := &proto.AuthorizationModelRef{Id: in.ID, Version: in.Version}
	if in.CreatedAt != nil {
		out.CreatedAt = timestamppb.New(*in.CreatedAt)
	}
	return out
}

func protoGetActiveModelRefResponse(in *GetActiveModelRefResponse) *proto.GetActiveModelRefResponse {
	if in == nil {
		return nil
	}
	return &proto.GetActiveModelRefResponse{Model: protoModelRef(in.Model)}
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
	return &proto.SetActiveModelResponse{Model: protoModelRef(in.Model)}
}

func authorizationModelResourceTypeFromProto(in *proto.AuthorizationModelResourceType) *ModelResourceType {
	if in == nil {
		return nil
	}
	return &ModelResourceType{
		Name:                in.GetName(),
		Relations:           modelRelationsFromProto(in.GetRelations()),
		Actions:             modelActionsFromProto(in.GetActions()),
		SourceLayer:         SourceLayer(in.GetSourceLayer()),
		DefaultAccessPolicy: DefaultAccessPolicy(in.GetDefaultAccessPolicy()),
	}
}

func protoModelResourceType(in *ModelResourceType) (*proto.AuthorizationModelResourceType, error) {
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
	}, nil
}

func authorizationModelResourceTypesFromProto(in []*proto.AuthorizationModelResourceType) []*ModelResourceType {
	out := make([]*ModelResourceType, 0, len(in))
	for _, item := range in {
		out = append(out, authorizationModelResourceTypeFromProto(item))
	}
	return out
}

func protoModelResourceTypes(in []*ModelResourceType) ([]*proto.AuthorizationModelResourceType, error) {
	out := make([]*proto.AuthorizationModelResourceType, 0, len(in))
	for i, item := range in {
		resourceType, err := protoModelResourceType(item)
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

func modelAllowedTargetsFromProto(in []*proto.ModelAllowedTarget) []ModelAllowedTarget {
	out := make([]ModelAllowedTarget, 0, len(in))
	for _, item := range in {
		if item == nil {
			out = append(out, nil)
			continue
		}
		switch kind := item.GetKind().(type) {
		case *proto.ModelAllowedTarget_SubjectType:
			out = append(out, ModelAllowedTargetSubjectType{SubjectType: kind.SubjectType})
		case *proto.ModelAllowedTarget_ResourceType:
			out = append(out, ModelAllowedTargetResourceType{ResourceType: kind.ResourceType})
		case *proto.ModelAllowedTarget_SubjectSetType:
			subjectSetType := subjectSetTypeFromProto(kind.SubjectSetType)
			if subjectSetType == nil {
				out = append(out, ModelAllowedTargetSubjectSetType{})
				continue
			}
			out = append(out, ModelAllowedTargetSubjectSetType{SubjectSetType: *subjectSetType})
		default:
			out = append(out, ModelAllowedTargetUnset{})
		}
	}
	return out
}

func protoModelAllowedTargets(in []ModelAllowedTarget) ([]*proto.ModelAllowedTarget, error) {
	out := make([]*proto.ModelAllowedTarget, 0, len(in))
	for _, item := range in {
		out = append(out, protoModelAllowedTarget(item))
	}
	return out, nil
}

func protoModelAllowedTarget(in ModelAllowedTarget) *proto.ModelAllowedTarget {
	if in == nil {
		return nil
	}
	switch target := in.(type) {
	case ModelAllowedTargetSubjectType:
		return &proto.ModelAllowedTarget{Kind: &proto.ModelAllowedTarget_SubjectType{SubjectType: target.SubjectType}}
	case ModelAllowedTargetResourceType:
		return &proto.ModelAllowedTarget{Kind: &proto.ModelAllowedTarget_ResourceType{ResourceType: target.ResourceType}}
	case ModelAllowedTargetSubjectSetType:
		return &proto.ModelAllowedTarget{Kind: &proto.ModelAllowedTarget_SubjectSetType{SubjectSetType: protoSubjectSetType(&target.SubjectSetType)}}
	case ModelAllowedTargetUnset:
		return &proto.ModelAllowedTarget{}
	default:
		return &proto.ModelAllowedTarget{}
	}
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

func authorizationModelResourceTypeFilterFromProto(in *proto.AuthorizationModelResourceTypeFilter) *ModelResourceTypeFilter {
	if in == nil {
		return nil
	}
	return &ModelResourceTypeFilter{Name: in.GetName(), SourceLayer: SourceLayer(in.GetSourceLayer())}
}

func protoListActiveModelResourceTypesResponse(in *ListActiveModelResourceTypesResponse) (*proto.ListActiveModelResourceTypesResponse, error) {
	if in == nil {
		return nil, nil
	}
	resourceTypes, err := protoModelResourceTypes(in.ResourceTypes)
	if err != nil {
		return nil, err
	}
	return &proto.ListActiveModelResourceTypesResponse{
		ResourceTypes: resourceTypes,
		NextPageToken: in.NextPageToken,
		ModelId:       in.ModelID,
	}, nil
}

func ListRelationshipsRequestFromProto(in *proto.ListRelationshipsRequest) *ListRelationshipsRequest {
	return listRelationshipsRequestFromProto(in)
}

func ListRelationshipsRequestToProto(in *ListRelationshipsRequest) (*proto.ListRelationshipsRequest, error) {
	if in == nil {
		return nil, nil
	}
	filter, err := protoRelationshipFilter(in.Filter)
	if err != nil {
		return nil, err
	}
	return &proto.ListRelationshipsRequest{Filter: filter, PageSize: in.PageSize, PageToken: in.PageToken}, nil
}

func ListRelationshipsResponseFromProto(in *proto.ListRelationshipsResponse) *ListRelationshipsResponse {
	if in == nil {
		return nil
	}
	return &ListRelationshipsResponse{Relationships: relationshipsFromProto(in.GetRelationships()), NextPageToken: in.GetNextPageToken()}
}

func ListRelationshipsResponseToProto(in *ListRelationshipsResponse) (*proto.ListRelationshipsResponse, error) {
	return protoListRelationshipsResponse(in)
}

func AddRelationshipRequestFromProto(in *proto.AddRelationshipRequest) *AddRelationshipRequest {
	return addRelationshipRequestFromProto(in)
}

func AddRelationshipRequestToProto(in *AddRelationshipRequest) (*proto.AddRelationshipRequest, error) {
	if in == nil {
		return nil, nil
	}
	relationship, err := protoRelationship(in.Relationship)
	if err != nil {
		return nil, err
	}
	return &proto.AddRelationshipRequest{Relationship: relationship}, nil
}

func AddRelationshipResponseFromProto(in *proto.AddRelationshipResponse) *AddRelationshipResponse {
	if in == nil {
		return nil
	}
	return &AddRelationshipResponse{Relationship: relationshipFromProto(in.GetRelationship())}
}

func AddRelationshipResponseToProto(in *AddRelationshipResponse) (*proto.AddRelationshipResponse, error) {
	return protoAddRelationshipResponse(in)
}

func DeleteRelationshipRequestFromProto(in *proto.DeleteRelationshipRequest) *DeleteRelationshipRequest {
	return deleteRelationshipRequestFromProto(in)
}

func DeleteRelationshipRequestToProto(in *DeleteRelationshipRequest) (*proto.DeleteRelationshipRequest, error) {
	if in == nil {
		return nil, nil
	}
	tuple, err := protoRelationshipTuple(in.RelationshipTuple)
	if err != nil {
		return nil, err
	}
	return &proto.DeleteRelationshipRequest{RelationshipTuple: tuple}, nil
}

func DeleteRelationshipResponseFromProto(in *proto.DeleteRelationshipResponse) *DeleteRelationshipResponse {
	if in == nil {
		return nil
	}
	return &DeleteRelationshipResponse{}
}

func DeleteRelationshipResponseToProto(in *DeleteRelationshipResponse) *proto.DeleteRelationshipResponse {
	return protoDeleteRelationshipResponse(in)
}

func SetAuthorizationStateRequestFromProto(in *proto.SetAuthorizationStateRequest) *SetAuthorizationStateRequest {
	return setAuthorizationStateRequestFromProto(in)
}

func SetAuthorizationStateRequestToProto(in *SetAuthorizationStateRequest) (*proto.SetAuthorizationStateRequest, error) {
	if in == nil {
		return nil, nil
	}
	model, err := protoModel(in.Model)
	if err != nil {
		return nil, err
	}
	relationships, err := protoRelationships(in.Relationships)
	if err != nil {
		return nil, err
	}
	return &proto.SetAuthorizationStateRequest{Model: model, Relationships: relationships}, nil
}

func SetAuthorizationStateResponseFromProto(in *proto.SetAuthorizationStateResponse) *SetAuthorizationStateResponse {
	if in == nil {
		return nil
	}
	return &SetAuthorizationStateResponse{ActiveModel: authorizationModelRefFromProto(in.GetActiveModel())}
}

func SetAuthorizationStateResponseToProto(in *SetAuthorizationStateResponse) *proto.SetAuthorizationStateResponse {
	return protoSetAuthorizationStateResponse(in)
}

func GetActiveModelRefResponseFromProto(in *proto.GetActiveModelRefResponse) *GetActiveModelRefResponse {
	if in == nil {
		return nil
	}
	return &GetActiveModelRefResponse{Model: authorizationModelRefFromProto(in.GetModel())}
}

func GetActiveModelRefResponseToProto(in *GetActiveModelRefResponse) *proto.GetActiveModelRefResponse {
	return protoGetActiveModelRefResponse(in)
}

func SetActiveModelRequestFromProto(in *proto.SetActiveModelRequest) *SetActiveModelRequest {
	return setActiveModelRequestFromProto(in)
}

func SetActiveModelRequestToProto(in *SetActiveModelRequest) (*proto.SetActiveModelRequest, error) {
	if in == nil {
		return nil, nil
	}
	model, err := protoModel(in.Model)
	if err != nil {
		return nil, err
	}
	return &proto.SetActiveModelRequest{Model: model}, nil
}

func SetActiveModelResponseFromProto(in *proto.SetActiveModelResponse) *SetActiveModelResponse {
	if in == nil {
		return nil
	}
	return &SetActiveModelResponse{Model: authorizationModelRefFromProto(in.GetModel())}
}

func SetActiveModelResponseToProto(in *SetActiveModelResponse) *proto.SetActiveModelResponse {
	return protoSetActiveModelResponse(in)
}

func ListActiveModelResourceTypesRequestFromProto(in *proto.ListActiveModelResourceTypesRequest) *ListActiveModelResourceTypesRequest {
	return listActiveModelResourceTypesRequestFromProto(in)
}

func ListActiveModelResourceTypesRequestToProto(in *ListActiveModelResourceTypesRequest) *proto.ListActiveModelResourceTypesRequest {
	if in == nil {
		return nil
	}
	var filter *proto.AuthorizationModelResourceTypeFilter
	if in.Filter != nil {
		filter = &proto.AuthorizationModelResourceTypeFilter{
			Name:        in.Filter.Name,
			SourceLayer: proto.SourceLayer(in.Filter.SourceLayer),
		}
	}
	return &proto.ListActiveModelResourceTypesRequest{
		Filter:    filter,
		PageSize:  in.PageSize,
		PageToken: in.PageToken,
	}
}

func ListActiveModelResourceTypesResponseFromProto(in *proto.ListActiveModelResourceTypesResponse) *ListActiveModelResourceTypesResponse {
	if in == nil {
		return nil
	}
	return &ListActiveModelResourceTypesResponse{
		ResourceTypes: authorizationModelResourceTypesFromProto(in.GetResourceTypes()),
		NextPageToken: in.GetNextPageToken(),
		ModelID:       in.GetModelId(),
	}
}

func ListActiveModelResourceTypesResponseToProto(in *ListActiveModelResourceTypesResponse) (*proto.ListActiveModelResourceTypesResponse, error) {
	return protoListActiveModelResourceTypesResponse(in)
}
