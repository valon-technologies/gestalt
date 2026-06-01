package authorization

import (
	"fmt"
	"time"

	sdkauthorization "github.com/valon-technologies/gestalt/sdk/go/authorization"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type (
	SourceLayer                          = sdkauthorization.SourceLayer
	RelationshipTargetType               = sdkauthorization.RelationshipTargetType
	AuthorizationSubject                 = sdkauthorization.AuthorizationSubject
	AuthorizationResource                = sdkauthorization.AuthorizationResource
	AuthorizationSubjectSet              = sdkauthorization.AuthorizationSubjectSet
	AuthorizationRelationshipTarget      = sdkauthorization.AuthorizationRelationshipTarget
	AuthorizationAction                  = sdkauthorization.AuthorizationAction
	CheckAccessRequest                   = sdkauthorization.CheckAccessRequest
	CheckAccessResponse                  = sdkauthorization.CheckAccessResponse
	CheckAccessManyRequest               = sdkauthorization.CheckAccessManyRequest
	CheckAccessManyResponse              = sdkauthorization.CheckAccessManyResponse
	Relationship                         = sdkauthorization.Relationship
	RelationshipTuple                    = sdkauthorization.RelationshipTuple
	RelationshipFilter                   = sdkauthorization.RelationshipFilter
	ListRelationshipsRequest             = sdkauthorization.ListRelationshipsRequest
	ListRelationshipsResponse            = sdkauthorization.ListRelationshipsResponse
	AddRelationshipRequest               = sdkauthorization.AddRelationshipRequest
	AddRelationshipResponse              = sdkauthorization.AddRelationshipResponse
	DeleteRelationshipRequest            = sdkauthorization.DeleteRelationshipRequest
	DeleteRelationshipResponse           = sdkauthorization.DeleteRelationshipResponse
	SetRelationshipsRequest              = sdkauthorization.SetRelationshipsRequest
	SetRelationshipsResponse             = sdkauthorization.SetRelationshipsResponse
	AuthorizationModel                   = sdkauthorization.AuthorizationModel
	AuthorizationModelRef                = sdkauthorization.AuthorizationModelRef
	AuthorizationModelResourceType       = sdkauthorization.AuthorizationModelResourceType
	AuthorizationModelRelation           = sdkauthorization.AuthorizationModelRelation
	AuthorizationModelAction             = sdkauthorization.AuthorizationModelAction
	AuthorizationModelAllowedTarget      = sdkauthorization.AuthorizationModelAllowedTarget
	SubjectSetType                       = sdkauthorization.SubjectSetType
	GetActiveModelRefResponse            = sdkauthorization.GetActiveModelRefResponse
	SetActiveModelRequest                = sdkauthorization.SetActiveModelRequest
	SetActiveModelResponse               = sdkauthorization.SetActiveModelResponse
	AuthorizationModelResourceTypeFilter = sdkauthorization.AuthorizationModelResourceTypeFilter
	ListActiveModelResourceTypesRequest  = sdkauthorization.ListActiveModelResourceTypesRequest
	ListActiveModelResourceTypesResponse = sdkauthorization.ListActiveModelResourceTypesResponse
)

func structFromAny(value map[string]any) (*structpb.Struct, error) {
	if value == nil {
		return nil, nil
	}
	return structpb.NewStruct(value)
}

func mapFromStruct(value *structpb.Struct) map[string]any {
	if value == nil {
		return nil
	}
	return value.AsMap()
}

func timestampFromNonZeroTime(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value)
}

func timeFromTimestamp(value *timestamppb.Timestamp) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.AsTime()
}

func protoAuthorizationSubject(value *AuthorizationSubject) (*proto.Subject, error) {
	if value == nil {
		return nil, nil
	}
	properties, err := structFromAny(value.Properties)
	if err != nil {
		return nil, fmt.Errorf("subject properties: %w", err)
	}
	return &proto.Subject{Type: value.Type, Id: value.Id, Properties: properties}, nil
}

func authorizationSubjectFromProto(value *proto.Subject) *AuthorizationSubject {
	if value == nil {
		return nil
	}
	return &AuthorizationSubject{Type: value.GetType(), Id: value.GetId(), Properties: mapFromStruct(value.GetProperties())}
}

func protoAuthorizationResource(value *AuthorizationResource) (*proto.Resource, error) {
	if value == nil {
		return nil, nil
	}
	properties, err := structFromAny(value.Properties)
	if err != nil {
		return nil, fmt.Errorf("resource properties: %w", err)
	}
	return &proto.Resource{Type: value.Type, Id: value.Id, Properties: properties}, nil
}

func authorizationResourceFromProto(value *proto.Resource) *AuthorizationResource {
	if value == nil {
		return nil
	}
	return &AuthorizationResource{Type: value.GetType(), Id: value.GetId(), Properties: mapFromStruct(value.GetProperties())}
}

func protoAuthorizationAction(value *AuthorizationAction) (*proto.Action, error) {
	if value == nil {
		return nil, nil
	}
	properties, err := structFromAny(value.Properties)
	if err != nil {
		return nil, fmt.Errorf("action properties: %w", err)
	}
	return &proto.Action{Name: value.Name, Properties: properties}, nil
}

func authorizationActionFromProto(value *proto.Action) *AuthorizationAction {
	if value == nil {
		return nil
	}
	return &AuthorizationAction{Name: value.GetName(), Properties: mapFromStruct(value.GetProperties())}
}

func protoAuthorizationSubjectSet(value *AuthorizationSubjectSet) (*proto.SubjectSet, error) {
	if value == nil {
		return nil, nil
	}
	resource, err := protoAuthorizationResource(value.Resource)
	if err != nil {
		return nil, err
	}
	return &proto.SubjectSet{Resource: resource, Relation: value.Relation}, nil
}

func authorizationSubjectSetFromProto(value *proto.SubjectSet) *AuthorizationSubjectSet {
	if value == nil {
		return nil
	}
	return &AuthorizationSubjectSet{Resource: authorizationResourceFromProto(value.GetResource()), Relation: value.GetRelation()}
}

func protoAuthorizationRelationshipTarget(value *AuthorizationRelationshipTarget) (*proto.RelationshipTarget, error) {
	if value == nil {
		return nil, nil
	}
	switch {
	case value.Subject != nil:
		subject, err := protoAuthorizationSubject(value.Subject)
		if err != nil {
			return nil, err
		}
		return &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_Subject{Subject: subject}}, nil
	case value.Resource != nil:
		resource, err := protoAuthorizationResource(value.Resource)
		if err != nil {
			return nil, err
		}
		return &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_Resource{Resource: resource}}, nil
	case value.SubjectSet != nil:
		subjectSet, err := protoAuthorizationSubjectSet(value.SubjectSet)
		if err != nil {
			return nil, err
		}
		return &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_SubjectSet{SubjectSet: subjectSet}}, nil
	default:
		return &proto.RelationshipTarget{}, nil
	}
}

func authorizationRelationshipTargetFromProto(value *proto.RelationshipTarget) *AuthorizationRelationshipTarget {
	if value == nil {
		return nil
	}
	switch kind := value.GetKind().(type) {
	case *proto.RelationshipTarget_Subject:
		return &AuthorizationRelationshipTarget{Subject: authorizationSubjectFromProto(kind.Subject)}
	case *proto.RelationshipTarget_Resource:
		return &AuthorizationRelationshipTarget{Resource: authorizationResourceFromProto(kind.Resource)}
	case *proto.RelationshipTarget_SubjectSet:
		return &AuthorizationRelationshipTarget{SubjectSet: authorizationSubjectSetFromProto(kind.SubjectSet)}
	default:
		return &AuthorizationRelationshipTarget{}
	}
}

func protoCheckAccessRequest(value *CheckAccessRequest) (*proto.CheckAccessRequest, error) {
	if value == nil {
		return nil, nil
	}
	subject, err := protoAuthorizationSubject(value.Subject)
	if err != nil {
		return nil, err
	}
	action, err := protoAuthorizationAction(value.Action)
	if err != nil {
		return nil, err
	}
	resource, err := protoAuthorizationResource(value.Resource)
	if err != nil {
		return nil, err
	}
	return &proto.CheckAccessRequest{Subject: subject, Action: action, Resource: resource}, nil
}

func checkAccessRequestFromProto(value *proto.CheckAccessRequest) *CheckAccessRequest {
	if value == nil {
		return nil
	}
	return &CheckAccessRequest{
		Subject:  authorizationSubjectFromProto(value.GetSubject()),
		Action:   authorizationActionFromProto(value.GetAction()),
		Resource: authorizationResourceFromProto(value.GetResource()),
	}
}

func protoCheckAccessResponse(value *CheckAccessResponse) *proto.CheckAccessResponse {
	if value == nil {
		return nil
	}
	return &proto.CheckAccessResponse{Allowed: value.Allowed, ModelId: value.ModelId}
}

func checkAccessResponseFromProto(value *proto.CheckAccessResponse) *CheckAccessResponse {
	if value == nil {
		return nil
	}
	return &CheckAccessResponse{Allowed: value.GetAllowed(), ModelId: value.GetModelId()}
}

func protoCheckAccessManyRequest(value *CheckAccessManyRequest) (*proto.CheckAccessManyRequest, error) {
	if value == nil {
		return nil, nil
	}
	requests := make([]*proto.CheckAccessRequest, 0, len(value.Requests))
	for i, request := range value.Requests {
		out, err := protoCheckAccessRequest(request)
		if err != nil {
			return nil, fmt.Errorf("requests[%d]: %w", i, err)
		}
		if out != nil {
			requests = append(requests, out)
		}
	}
	return &proto.CheckAccessManyRequest{Requests: requests}, nil
}

func checkAccessManyRequestFromProto(value *proto.CheckAccessManyRequest) *CheckAccessManyRequest {
	if value == nil {
		return nil
	}
	requests := make([]*CheckAccessRequest, 0, len(value.GetRequests()))
	for _, request := range value.GetRequests() {
		requests = append(requests, checkAccessRequestFromProto(request))
	}
	return &CheckAccessManyRequest{Requests: requests}
}

func protoCheckAccessManyResponse(value *CheckAccessManyResponse) *proto.CheckAccessManyResponse {
	if value == nil {
		return nil
	}
	decisions := make([]*proto.CheckAccessResponse, 0, len(value.Decisions))
	for _, decision := range value.Decisions {
		if out := protoCheckAccessResponse(decision); out != nil {
			decisions = append(decisions, out)
		}
	}
	return &proto.CheckAccessManyResponse{Decisions: decisions}
}

func checkAccessManyResponseFromProto(value *proto.CheckAccessManyResponse) *CheckAccessManyResponse {
	if value == nil {
		return nil
	}
	decisions := make([]*CheckAccessResponse, 0, len(value.GetDecisions()))
	for _, decision := range value.GetDecisions() {
		decisions = append(decisions, checkAccessResponseFromProto(decision))
	}
	return &CheckAccessManyResponse{Decisions: decisions}
}

func protoRelationship(value *Relationship) (*proto.Relationship, error) {
	if value == nil {
		return nil, nil
	}
	tuple, err := protoRelationshipTuple(value.Tuple)
	if err != nil {
		return nil, err
	}
	properties, err := structFromAny(value.Properties)
	if err != nil {
		return nil, fmt.Errorf("relationship properties: %w", err)
	}
	return &proto.Relationship{
		Tuple:       tuple,
		Properties:  properties,
		SourceLayer: proto.SourceLayer(value.SourceLayer),
	}, nil
}

func relationshipFromProto(value *proto.Relationship) *Relationship {
	if value == nil {
		return nil
	}
	return &Relationship{
		Tuple:       relationshipTupleFromProto(value.GetTuple()),
		Properties:  mapFromStruct(value.GetProperties()),
		SourceLayer: SourceLayer(value.GetSourceLayer()),
	}
}

func protoRelationshipTuple(value *RelationshipTuple) (*proto.RelationshipTuple, error) {
	if value == nil {
		return nil, nil
	}
	target, err := protoAuthorizationRelationshipTarget(value.Target)
	if err != nil {
		return nil, err
	}
	resource, err := protoAuthorizationResource(value.Resource)
	if err != nil {
		return nil, err
	}
	return &proto.RelationshipTuple{Target: target, Relation: value.Relation, Resource: resource}, nil
}

func relationshipTupleFromProto(value *proto.RelationshipTuple) *RelationshipTuple {
	if value == nil {
		return nil
	}
	return &RelationshipTuple{
		Target:   authorizationRelationshipTargetFromProto(value.GetTarget()),
		Relation: value.GetRelation(),
		Resource: authorizationResourceFromProto(value.GetResource()),
	}
}

func protoRelationshipFilter(value *RelationshipFilter) (*proto.RelationshipFilter, error) {
	if value == nil {
		return nil, nil
	}
	target, err := protoAuthorizationRelationshipTarget(value.Target)
	if err != nil {
		return nil, err
	}
	resource, err := protoAuthorizationResource(value.Resource)
	if err != nil {
		return nil, err
	}
	return &proto.RelationshipFilter{
		Target:           target,
		Relation:         value.Relation,
		Resource:         resource,
		TargetType:       proto.RelationshipTargetType(value.TargetType),
		TargetEntityType: value.TargetEntityType,
		ResourceType:     value.ResourceType,
		SourceLayer:      proto.SourceLayer(value.SourceLayer),
	}, nil
}

func relationshipFilterFromProto(value *proto.RelationshipFilter) *RelationshipFilter {
	if value == nil {
		return nil
	}
	return &RelationshipFilter{
		Target:           authorizationRelationshipTargetFromProto(value.GetTarget()),
		Relation:         value.GetRelation(),
		Resource:         authorizationResourceFromProto(value.GetResource()),
		TargetType:       RelationshipTargetType(value.GetTargetType()),
		TargetEntityType: value.GetTargetEntityType(),
		ResourceType:     value.GetResourceType(),
		SourceLayer:      SourceLayer(value.GetSourceLayer()),
	}
}

func protoListRelationshipsRequest(value *ListRelationshipsRequest) (*proto.ListRelationshipsRequest, error) {
	if value == nil {
		return nil, nil
	}
	filter, err := protoRelationshipFilter(value.Filter)
	if err != nil {
		return nil, err
	}
	return &proto.ListRelationshipsRequest{Filter: filter, PageSize: value.PageSize, PageToken: value.PageToken}, nil
}

func listRelationshipsRequestFromProto(value *proto.ListRelationshipsRequest) *ListRelationshipsRequest {
	if value == nil {
		return nil
	}
	return &ListRelationshipsRequest{
		Filter:    relationshipFilterFromProto(value.GetFilter()),
		PageSize:  value.GetPageSize(),
		PageToken: value.GetPageToken(),
	}
}

func protoListRelationshipsResponse(value *ListRelationshipsResponse) (*proto.ListRelationshipsResponse, error) {
	if value == nil {
		return nil, nil
	}
	relationships := make([]*proto.Relationship, 0, len(value.Relationships))
	for i, relationship := range value.Relationships {
		out, err := protoRelationship(relationship)
		if err != nil {
			return nil, fmt.Errorf("relationships[%d]: %w", i, err)
		}
		if out != nil {
			relationships = append(relationships, out)
		}
	}
	return &proto.ListRelationshipsResponse{Relationships: relationships, NextPageToken: value.NextPageToken}, nil
}

func listRelationshipsResponseFromProto(value *proto.ListRelationshipsResponse) *ListRelationshipsResponse {
	if value == nil {
		return nil
	}
	relationships := make([]*Relationship, 0, len(value.GetRelationships()))
	for _, relationship := range value.GetRelationships() {
		relationships = append(relationships, relationshipFromProto(relationship))
	}
	return &ListRelationshipsResponse{Relationships: relationships, NextPageToken: value.GetNextPageToken()}
}

func protoAddRelationshipRequest(value *AddRelationshipRequest) (*proto.AddRelationshipRequest, error) {
	if value == nil {
		return nil, nil
	}
	relationship, err := protoRelationship(value.Relationship)
	if err != nil {
		return nil, err
	}
	return &proto.AddRelationshipRequest{Relationship: relationship}, nil
}

func addRelationshipRequestFromProto(value *proto.AddRelationshipRequest) *AddRelationshipRequest {
	if value == nil {
		return nil
	}
	return &AddRelationshipRequest{Relationship: relationshipFromProto(value.GetRelationship())}
}

func protoAddRelationshipResponse(value *AddRelationshipResponse) (*proto.AddRelationshipResponse, error) {
	if value == nil {
		return nil, nil
	}
	relationship, err := protoRelationship(value.Relationship)
	if err != nil {
		return nil, err
	}
	return &proto.AddRelationshipResponse{Relationship: relationship}, nil
}

func addRelationshipResponseFromProto(value *proto.AddRelationshipResponse) *AddRelationshipResponse {
	if value == nil {
		return nil
	}
	return &AddRelationshipResponse{Relationship: relationshipFromProto(value.GetRelationship())}
}

func protoDeleteRelationshipRequest(value *DeleteRelationshipRequest) (*proto.DeleteRelationshipRequest, error) {
	if value == nil {
		return nil, nil
	}
	tuple, err := protoRelationshipTuple(value.RelationshipTuple)
	if err != nil {
		return nil, err
	}
	return &proto.DeleteRelationshipRequest{RelationshipTuple: tuple}, nil
}

func deleteRelationshipRequestFromProto(value *proto.DeleteRelationshipRequest) *DeleteRelationshipRequest {
	if value == nil {
		return nil
	}
	return &DeleteRelationshipRequest{RelationshipTuple: relationshipTupleFromProto(value.GetRelationshipTuple())}
}

func protoSetRelationshipsRequest(value *SetRelationshipsRequest) (*proto.SetRelationshipsRequest, error) {
	if value == nil {
		return nil, nil
	}
	relationships := make([]*proto.Relationship, 0, len(value.Relationships))
	for i, relationship := range value.Relationships {
		out, err := protoRelationship(relationship)
		if err != nil {
			return nil, fmt.Errorf("relationships[%d]: %w", i, err)
		}
		if out != nil {
			relationships = append(relationships, out)
		}
	}
	return &proto.SetRelationshipsRequest{Relationships: relationships}, nil
}

func setRelationshipsRequestFromProto(value *proto.SetRelationshipsRequest) *SetRelationshipsRequest {
	if value == nil {
		return nil
	}
	relationships := make([]*Relationship, 0, len(value.GetRelationships()))
	for _, relationship := range value.GetRelationships() {
		relationships = append(relationships, relationshipFromProto(relationship))
	}
	return &SetRelationshipsRequest{Relationships: relationships}
}

func protoSetRelationshipsResponse(value *SetRelationshipsResponse) (*proto.SetRelationshipsResponse, error) {
	if value == nil {
		return nil, nil
	}
	relationships := make([]*proto.Relationship, 0, len(value.Relationships))
	for i, relationship := range value.Relationships {
		out, err := protoRelationship(relationship)
		if err != nil {
			return nil, fmt.Errorf("relationships[%d]: %w", i, err)
		}
		if out != nil {
			relationships = append(relationships, out)
		}
	}
	return &proto.SetRelationshipsResponse{Relationships: relationships}, nil
}

func setRelationshipsResponseFromProto(value *proto.SetRelationshipsResponse) *SetRelationshipsResponse {
	if value == nil {
		return nil
	}
	relationships := make([]*Relationship, 0, len(value.GetRelationships()))
	for _, relationship := range value.GetRelationships() {
		relationships = append(relationships, relationshipFromProto(relationship))
	}
	return &SetRelationshipsResponse{Relationships: relationships}
}

func protoAuthorizationModel(value *AuthorizationModel) *proto.AuthorizationModel {
	if value == nil {
		return nil
	}
	resourceTypes := make([]*proto.AuthorizationModelResourceType, 0, len(value.ResourceTypes))
	for _, resourceType := range value.ResourceTypes {
		if out := protoAuthorizationModelResourceType(resourceType); out != nil {
			resourceTypes = append(resourceTypes, out)
		}
	}
	return &proto.AuthorizationModel{Id: value.Id, Version: value.Version, ResourceTypes: resourceTypes}
}

func authorizationModelFromProto(value *proto.AuthorizationModel) *AuthorizationModel {
	if value == nil {
		return nil
	}
	resourceTypes := make([]*AuthorizationModelResourceType, 0, len(value.GetResourceTypes()))
	for _, resourceType := range value.GetResourceTypes() {
		resourceTypes = append(resourceTypes, authorizationModelResourceTypeFromProto(resourceType))
	}
	return &AuthorizationModel{Id: value.GetId(), Version: value.GetVersion(), ResourceTypes: resourceTypes}
}

func protoAuthorizationModelRef(value *AuthorizationModelRef) *proto.AuthorizationModelRef {
	if value == nil {
		return nil
	}
	return &proto.AuthorizationModelRef{
		Id:        value.Id,
		Version:   value.Version,
		CreatedAt: timestampFromNonZeroTime(value.CreatedAt),
	}
}

func authorizationModelRefFromProto(value *proto.AuthorizationModelRef) *AuthorizationModelRef {
	if value == nil {
		return nil
	}
	return &AuthorizationModelRef{Id: value.GetId(), Version: value.GetVersion(), CreatedAt: timeFromTimestamp(value.GetCreatedAt())}
}

func protoAuthorizationModelResourceType(value *AuthorizationModelResourceType) *proto.AuthorizationModelResourceType {
	if value == nil {
		return nil
	}
	relations := make([]*proto.AuthorizationModelRelation, 0, len(value.Relations))
	for _, relation := range value.Relations {
		if out := protoAuthorizationModelRelation(relation); out != nil {
			relations = append(relations, out)
		}
	}
	actions := make([]*proto.AuthorizationModelAction, 0, len(value.Actions))
	for _, action := range value.Actions {
		if out := protoAuthorizationModelAction(action); out != nil {
			actions = append(actions, out)
		}
	}
	return &proto.AuthorizationModelResourceType{
		Name:        value.Name,
		Relations:   relations,
		Actions:     actions,
		SourceLayer: proto.SourceLayer(value.SourceLayer),
	}
}

func authorizationModelResourceTypeFromProto(value *proto.AuthorizationModelResourceType) *AuthorizationModelResourceType {
	if value == nil {
		return nil
	}
	relations := make([]*AuthorizationModelRelation, 0, len(value.GetRelations()))
	for _, relation := range value.GetRelations() {
		relations = append(relations, authorizationModelRelationFromProto(relation))
	}
	actions := make([]*AuthorizationModelAction, 0, len(value.GetActions()))
	for _, action := range value.GetActions() {
		actions = append(actions, authorizationModelActionFromProto(action))
	}
	return &AuthorizationModelResourceType{
		Name:        value.GetName(),
		Relations:   relations,
		Actions:     actions,
		SourceLayer: SourceLayer(value.GetSourceLayer()),
	}
}

func protoAuthorizationModelRelation(value *AuthorizationModelRelation) *proto.AuthorizationModelRelation {
	if value == nil {
		return nil
	}
	allowedTargets := make([]*proto.AuthorizationModelAllowedTarget, 0, len(value.AllowedTargets))
	for _, target := range value.AllowedTargets {
		if out := protoAuthorizationModelAllowedTarget(target); out != nil {
			allowedTargets = append(allowedTargets, out)
		}
	}
	return &proto.AuthorizationModelRelation{Name: value.Name, AllowedTargets: allowedTargets}
}

func authorizationModelRelationFromProto(value *proto.AuthorizationModelRelation) *AuthorizationModelRelation {
	if value == nil {
		return nil
	}
	allowedTargets := make([]*AuthorizationModelAllowedTarget, 0, len(value.GetAllowedTargets()))
	for _, target := range value.GetAllowedTargets() {
		allowedTargets = append(allowedTargets, authorizationModelAllowedTargetFromProto(target))
	}
	return &AuthorizationModelRelation{Name: value.GetName(), AllowedTargets: allowedTargets}
}

func protoAuthorizationModelAction(value *AuthorizationModelAction) *proto.AuthorizationModelAction {
	if value == nil {
		return nil
	}
	return &proto.AuthorizationModelAction{Name: value.Name, Relations: append([]string(nil), value.Relations...)}
}

func authorizationModelActionFromProto(value *proto.AuthorizationModelAction) *AuthorizationModelAction {
	if value == nil {
		return nil
	}
	return &AuthorizationModelAction{Name: value.GetName(), Relations: append([]string(nil), value.GetRelations()...)}
}

func protoAuthorizationModelAllowedTarget(value *AuthorizationModelAllowedTarget) *proto.AuthorizationModelAllowedTarget {
	if value == nil {
		return nil
	}
	switch {
	case value.SubjectType != "":
		return &proto.AuthorizationModelAllowedTarget{Kind: &proto.AuthorizationModelAllowedTarget_SubjectType{SubjectType: value.SubjectType}}
	case value.ResourceType != "":
		return &proto.AuthorizationModelAllowedTarget{Kind: &proto.AuthorizationModelAllowedTarget_ResourceType{ResourceType: value.ResourceType}}
	case value.SubjectSetType != nil:
		return &proto.AuthorizationModelAllowedTarget{Kind: &proto.AuthorizationModelAllowedTarget_SubjectSetType{SubjectSetType: &proto.SubjectSetType{
			ResourceType: value.SubjectSetType.ResourceType,
			Relation:     value.SubjectSetType.Relation,
		}}}
	default:
		return &proto.AuthorizationModelAllowedTarget{}
	}
}

func authorizationModelAllowedTargetFromProto(value *proto.AuthorizationModelAllowedTarget) *AuthorizationModelAllowedTarget {
	if value == nil {
		return nil
	}
	switch kind := value.GetKind().(type) {
	case *proto.AuthorizationModelAllowedTarget_SubjectType:
		return &AuthorizationModelAllowedTarget{SubjectType: kind.SubjectType}
	case *proto.AuthorizationModelAllowedTarget_ResourceType:
		return &AuthorizationModelAllowedTarget{ResourceType: kind.ResourceType}
	case *proto.AuthorizationModelAllowedTarget_SubjectSetType:
		return &AuthorizationModelAllowedTarget{SubjectSetType: &SubjectSetType{
			ResourceType: kind.SubjectSetType.GetResourceType(),
			Relation:     kind.SubjectSetType.GetRelation(),
		}}
	default:
		return &AuthorizationModelAllowedTarget{}
	}
}

func protoGetActiveModelRefResponse(value *GetActiveModelRefResponse) *proto.GetActiveModelRefResponse {
	if value == nil {
		return nil
	}
	return &proto.GetActiveModelRefResponse{Model: protoAuthorizationModelRef(value.Model)}
}

func getActiveModelRefResponseFromProto(value *proto.GetActiveModelRefResponse) *GetActiveModelRefResponse {
	if value == nil {
		return nil
	}
	return &GetActiveModelRefResponse{Model: authorizationModelRefFromProto(value.GetModel())}
}

func protoSetActiveModelRequest(value *SetActiveModelRequest) *proto.SetActiveModelRequest {
	if value == nil {
		return nil
	}
	return &proto.SetActiveModelRequest{Model: protoAuthorizationModel(value.Model)}
}

func setActiveModelRequestFromProto(value *proto.SetActiveModelRequest) *SetActiveModelRequest {
	if value == nil {
		return nil
	}
	return &SetActiveModelRequest{Model: authorizationModelFromProto(value.GetModel())}
}

func protoSetActiveModelResponse(value *SetActiveModelResponse) *proto.SetActiveModelResponse {
	if value == nil {
		return nil
	}
	return &proto.SetActiveModelResponse{Model: protoAuthorizationModelRef(value.Model)}
}

func setActiveModelResponseFromProto(value *proto.SetActiveModelResponse) *SetActiveModelResponse {
	if value == nil {
		return nil
	}
	return &SetActiveModelResponse{Model: authorizationModelRefFromProto(value.GetModel())}
}

func protoAuthorizationModelResourceTypeFilter(value *AuthorizationModelResourceTypeFilter) *proto.AuthorizationModelResourceTypeFilter {
	if value == nil {
		return nil
	}
	return &proto.AuthorizationModelResourceTypeFilter{Name: value.Name, SourceLayer: proto.SourceLayer(value.SourceLayer)}
}

func authorizationModelResourceTypeFilterFromProto(value *proto.AuthorizationModelResourceTypeFilter) *AuthorizationModelResourceTypeFilter {
	if value == nil {
		return nil
	}
	return &AuthorizationModelResourceTypeFilter{Name: value.GetName(), SourceLayer: SourceLayer(value.GetSourceLayer())}
}

func protoListActiveModelResourceTypesRequest(value *ListActiveModelResourceTypesRequest) *proto.ListActiveModelResourceTypesRequest {
	if value == nil {
		return nil
	}
	return &proto.ListActiveModelResourceTypesRequest{
		ModelId: value.ModelId,
		Filter:  protoAuthorizationModelResourceTypeFilter(value.Filter),
	}
}

func listActiveModelResourceTypesRequestFromProto(value *proto.ListActiveModelResourceTypesRequest) *ListActiveModelResourceTypesRequest {
	if value == nil {
		return nil
	}
	return &ListActiveModelResourceTypesRequest{
		ModelId: value.GetModelId(),
		Filter:  authorizationModelResourceTypeFilterFromProto(value.GetFilter()),
	}
}

func protoListActiveModelResourceTypesResponse(value *ListActiveModelResourceTypesResponse) *proto.ListActiveModelResourceTypesResponse {
	if value == nil {
		return nil
	}
	resourceTypes := make([]*proto.AuthorizationModelResourceType, 0, len(value.ResourceTypes))
	for _, resourceType := range value.ResourceTypes {
		if out := protoAuthorizationModelResourceType(resourceType); out != nil {
			resourceTypes = append(resourceTypes, out)
		}
	}
	return &proto.ListActiveModelResourceTypesResponse{ResourceTypes: resourceTypes}
}

func listActiveModelResourceTypesResponseFromProto(value *proto.ListActiveModelResourceTypesResponse) *ListActiveModelResourceTypesResponse {
	if value == nil {
		return nil
	}
	resourceTypes := make([]*AuthorizationModelResourceType, 0, len(value.GetResourceTypes()))
	for _, resourceType := range value.GetResourceTypes() {
		resourceTypes = append(resourceTypes, authorizationModelResourceTypeFromProto(resourceType))
	}
	return &ListActiveModelResourceTypesResponse{ResourceTypes: resourceTypes}
}
