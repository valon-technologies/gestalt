package gestalt

import (
	"fmt"

	proto "github.com/valon-technologies/gestalt/sdk/go/protov1/v1"
)

func protoAuthorizationSubject(value *AuthorizationSubject) (*proto.Subject, error) {
	if value == nil {
		return nil, nil
	}
	properties, err := structFromAny(value.Properties)
	if err != nil {
		return nil, fmt.Errorf("subject properties: %w", err)
	}
	return &proto.Subject{
		Type:       value.Type,
		Id:         value.Id,
		Properties: properties,
	}, nil
}

func authorizationSubjectFromProto(value *proto.Subject) *AuthorizationSubject {
	if value == nil {
		return nil
	}
	return &AuthorizationSubject{
		Type:       value.GetType(),
		Id:         value.GetId(),
		Properties: mapFromStruct(value.GetProperties()),
	}
}

func protoAuthorizationResource(value *AuthorizationResource) (*proto.Resource, error) {
	if value == nil {
		return nil, nil
	}
	properties, err := structFromAny(value.Properties)
	if err != nil {
		return nil, fmt.Errorf("resource properties: %w", err)
	}
	return &proto.Resource{
		Type:       value.Type,
		Id:         value.Id,
		Properties: properties,
	}, nil
}

func authorizationResourceFromProto(value *proto.Resource) *AuthorizationResource {
	if value == nil {
		return nil
	}
	return &AuthorizationResource{
		Type:       value.GetType(),
		Id:         value.GetId(),
		Properties: mapFromStruct(value.GetProperties()),
	}
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
	return &AuthorizationSubjectSet{
		Resource: authorizationResourceFromProto(value.GetResource()),
		Relation: value.GetRelation(),
	}
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
		return &proto.RelationshipTarget{
			Kind: &proto.RelationshipTarget_Subject{Subject: subject},
		}, nil
	case value.Resource != nil:
		resource, err := protoAuthorizationResource(value.Resource)
		if err != nil {
			return nil, err
		}
		return &proto.RelationshipTarget{
			Kind: &proto.RelationshipTarget_Resource{Resource: resource},
		}, nil
	case value.SubjectSet != nil:
		subjectSet, err := protoAuthorizationSubjectSet(value.SubjectSet)
		if err != nil {
			return nil, err
		}
		return &proto.RelationshipTarget{
			Kind: &proto.RelationshipTarget_SubjectSet{SubjectSet: subjectSet},
		}, nil
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
	return &AuthorizationAction{
		Name:       value.GetName(),
		Properties: mapFromStruct(value.GetProperties()),
	}
}

func protoAccessEvaluationRequest(value *AccessEvaluationRequest) (*proto.AccessEvaluationRequest, error) {
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
	context, err := structFromAny(value.Context)
	if err != nil {
		return nil, fmt.Errorf("context: %w", err)
	}
	return &proto.AccessEvaluationRequest{
		Subject:  subject,
		Action:   action,
		Resource: resource,
		Context:  context,
	}, nil
}

func accessEvaluationRequestFromProto(value *proto.AccessEvaluationRequest) *AccessEvaluationRequest {
	if value == nil {
		return nil
	}
	return &AccessEvaluationRequest{
		Subject:  authorizationSubjectFromProto(value.GetSubject()),
		Action:   authorizationActionFromProto(value.GetAction()),
		Resource: authorizationResourceFromProto(value.GetResource()),
		Context:  mapFromStruct(value.GetContext()),
	}
}

func protoAccessDecision(value *AccessDecision) (*proto.AccessDecision, error) {
	if value == nil {
		return nil, nil
	}
	context, err := structFromAny(value.Context)
	if err != nil {
		return nil, fmt.Errorf("decision context: %w", err)
	}
	return &proto.AccessDecision{
		Allowed: value.Allowed,
		Context: context,
		ModelId: value.ModelId,
	}, nil
}

func accessDecisionFromProto(value *proto.AccessDecision) *AccessDecision {
	if value == nil {
		return nil
	}
	return &AccessDecision{
		Allowed: value.GetAllowed(),
		Context: mapFromStruct(value.GetContext()),
		ModelId: value.GetModelId(),
	}
}

func protoAccessEvaluationsRequest(value *AccessEvaluationsRequest) (*proto.AccessEvaluationsRequest, error) {
	if value == nil {
		return nil, nil
	}
	requests := make([]*proto.AccessEvaluationRequest, 0, len(value.Requests))
	for i, request := range value.Requests {
		out, err := protoAccessEvaluationRequest(request)
		if err != nil {
			return nil, fmt.Errorf("requests[%d]: %w", i, err)
		}
		if out != nil {
			requests = append(requests, out)
		}
	}
	return &proto.AccessEvaluationsRequest{Requests: requests}, nil
}

func accessEvaluationsRequestFromProto(value *proto.AccessEvaluationsRequest) *AccessEvaluationsRequest {
	if value == nil {
		return nil
	}
	requests := make([]*AccessEvaluationRequest, 0, len(value.GetRequests()))
	for _, request := range value.GetRequests() {
		requests = append(requests, accessEvaluationRequestFromProto(request))
	}
	return &AccessEvaluationsRequest{Requests: requests}
}

func protoAccessEvaluationsResponse(value *AccessEvaluationsResponse) (*proto.AccessEvaluationsResponse, error) {
	if value == nil {
		return nil, nil
	}
	decisions := make([]*proto.AccessDecision, 0, len(value.Decisions))
	for i, decision := range value.Decisions {
		out, err := protoAccessDecision(decision)
		if err != nil {
			return nil, fmt.Errorf("decisions[%d]: %w", i, err)
		}
		if out != nil {
			decisions = append(decisions, out)
		}
	}
	return &proto.AccessEvaluationsResponse{Decisions: decisions}, nil
}

func accessEvaluationsResponseFromProto(value *proto.AccessEvaluationsResponse) *AccessEvaluationsResponse {
	if value == nil {
		return nil
	}
	decisions := make([]*AccessDecision, 0, len(value.GetDecisions()))
	for _, decision := range value.GetDecisions() {
		decisions = append(decisions, accessDecisionFromProto(decision))
	}
	return &AccessEvaluationsResponse{Decisions: decisions}
}

func protoResourceSearchRequest(value *ResourceSearchRequest) (*proto.ResourceSearchRequest, error) {
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
	context, err := structFromAny(value.Context)
	if err != nil {
		return nil, fmt.Errorf("context: %w", err)
	}
	return &proto.ResourceSearchRequest{
		Subject:      subject,
		Action:       action,
		ResourceType: value.ResourceType,
		Context:      context,
		PageSize:     value.PageSize,
		PageToken:    value.PageToken,
	}, nil
}

func resourceSearchRequestFromProto(value *proto.ResourceSearchRequest) *ResourceSearchRequest {
	if value == nil {
		return nil
	}
	return &ResourceSearchRequest{
		Subject:      authorizationSubjectFromProto(value.GetSubject()),
		Action:       authorizationActionFromProto(value.GetAction()),
		ResourceType: value.GetResourceType(),
		Context:      mapFromStruct(value.GetContext()),
		PageSize:     value.GetPageSize(),
		PageToken:    value.GetPageToken(),
	}
}

func protoResourceSearchResponse(value *ResourceSearchResponse) (*proto.ResourceSearchResponse, error) {
	if value == nil {
		return nil, nil
	}
	resources := make([]*proto.Resource, 0, len(value.Resources))
	for i, resource := range value.Resources {
		out, err := protoAuthorizationResource(resource)
		if err != nil {
			return nil, fmt.Errorf("resources[%d]: %w", i, err)
		}
		if out != nil {
			resources = append(resources, out)
		}
	}
	return &proto.ResourceSearchResponse{
		Resources:     resources,
		NextPageToken: value.NextPageToken,
		ModelId:       value.ModelId,
	}, nil
}

func resourceSearchResponseFromProto(value *proto.ResourceSearchResponse) *ResourceSearchResponse {
	if value == nil {
		return nil
	}
	resources := make([]*AuthorizationResource, 0, len(value.GetResources()))
	for _, resource := range value.GetResources() {
		resources = append(resources, authorizationResourceFromProto(resource))
	}
	return &ResourceSearchResponse{
		Resources:     resources,
		NextPageToken: value.GetNextPageToken(),
		ModelId:       value.GetModelId(),
	}
}

func protoSubjectSearchRequest(value *SubjectSearchRequest) (*proto.SubjectSearchRequest, error) {
	if value == nil {
		return nil, nil
	}
	resource, err := protoAuthorizationResource(value.Resource)
	if err != nil {
		return nil, err
	}
	action, err := protoAuthorizationAction(value.Action)
	if err != nil {
		return nil, err
	}
	context, err := structFromAny(value.Context)
	if err != nil {
		return nil, fmt.Errorf("context: %w", err)
	}
	return &proto.SubjectSearchRequest{
		Resource:    resource,
		Action:      action,
		SubjectType: value.SubjectType,
		Context:     context,
		PageSize:    value.PageSize,
		PageToken:   value.PageToken,
	}, nil
}

func subjectSearchRequestFromProto(value *proto.SubjectSearchRequest) *SubjectSearchRequest {
	if value == nil {
		return nil
	}
	return &SubjectSearchRequest{
		Resource:    authorizationResourceFromProto(value.GetResource()),
		Action:      authorizationActionFromProto(value.GetAction()),
		SubjectType: value.GetSubjectType(),
		Context:     mapFromStruct(value.GetContext()),
		PageSize:    value.GetPageSize(),
		PageToken:   value.GetPageToken(),
	}
}

func protoSubjectSearchResponse(value *SubjectSearchResponse) (*proto.SubjectSearchResponse, error) {
	if value == nil {
		return nil, nil
	}
	subjects := make([]*proto.Subject, 0, len(value.Subjects))
	for i, subject := range value.Subjects {
		out, err := protoAuthorizationSubject(subject)
		if err != nil {
			return nil, fmt.Errorf("subjects[%d]: %w", i, err)
		}
		if out != nil {
			subjects = append(subjects, out)
		}
	}
	return &proto.SubjectSearchResponse{
		Subjects:      subjects,
		NextPageToken: value.NextPageToken,
		ModelId:       value.ModelId,
	}, nil
}

func subjectSearchResponseFromProto(value *proto.SubjectSearchResponse) *SubjectSearchResponse {
	if value == nil {
		return nil
	}
	subjects := make([]*AuthorizationSubject, 0, len(value.GetSubjects()))
	for _, subject := range value.GetSubjects() {
		subjects = append(subjects, authorizationSubjectFromProto(subject))
	}
	return &SubjectSearchResponse{
		Subjects:      subjects,
		NextPageToken: value.GetNextPageToken(),
		ModelId:       value.GetModelId(),
	}
}

func protoEffectiveSubjectSearchRequest(value *EffectiveSubjectSearchRequest) (*proto.EffectiveSubjectSearchRequest, error) {
	if value == nil {
		return nil, nil
	}
	resource, err := protoAuthorizationResource(value.Resource)
	if err != nil {
		return nil, err
	}
	action, err := protoAuthorizationAction(value.Action)
	if err != nil {
		return nil, err
	}
	context, err := structFromAny(value.Context)
	if err != nil {
		return nil, fmt.Errorf("context: %w", err)
	}
	return &proto.EffectiveSubjectSearchRequest{
		Resource:  resource,
		Action:    action,
		Context:   context,
		PageSize:  value.PageSize,
		PageToken: value.PageToken,
	}, nil
}

func effectiveSubjectSearchRequestFromProto(value *proto.EffectiveSubjectSearchRequest) *EffectiveSubjectSearchRequest {
	if value == nil {
		return nil
	}
	return &EffectiveSubjectSearchRequest{
		Resource:  authorizationResourceFromProto(value.GetResource()),
		Action:    authorizationActionFromProto(value.GetAction()),
		Context:   mapFromStruct(value.GetContext()),
		PageSize:  value.GetPageSize(),
		PageToken: value.GetPageToken(),
	}
}

func protoEffectiveSubjectSearchResponse(value *EffectiveSubjectSearchResponse) (*proto.EffectiveSubjectSearchResponse, error) {
	if value == nil {
		return nil, nil
	}
	targets := make([]*proto.RelationshipTarget, 0, len(value.Targets))
	for i, target := range value.Targets {
		out, err := protoAuthorizationRelationshipTarget(target)
		if err != nil {
			return nil, fmt.Errorf("targets[%d]: %w", i, err)
		}
		if out != nil {
			targets = append(targets, out)
		}
	}
	return &proto.EffectiveSubjectSearchResponse{
		Targets:       targets,
		NextPageToken: value.NextPageToken,
		ModelId:       value.ModelId,
		Truncated:     value.Truncated,
	}, nil
}

func effectiveSubjectSearchResponseFromProto(value *proto.EffectiveSubjectSearchResponse) *EffectiveSubjectSearchResponse {
	if value == nil {
		return nil
	}
	targets := make([]*AuthorizationRelationshipTarget, 0, len(value.GetTargets()))
	for _, target := range value.GetTargets() {
		targets = append(targets, authorizationRelationshipTargetFromProto(target))
	}
	return &EffectiveSubjectSearchResponse{
		Targets:       targets,
		NextPageToken: value.GetNextPageToken(),
		ModelId:       value.GetModelId(),
		Truncated:     value.GetTruncated(),
	}
}

func protoActionSearchRequest(value *ActionSearchRequest) (*proto.ActionSearchRequest, error) {
	if value == nil {
		return nil, nil
	}
	subject, err := protoAuthorizationSubject(value.Subject)
	if err != nil {
		return nil, err
	}
	resource, err := protoAuthorizationResource(value.Resource)
	if err != nil {
		return nil, err
	}
	context, err := structFromAny(value.Context)
	if err != nil {
		return nil, fmt.Errorf("context: %w", err)
	}
	return &proto.ActionSearchRequest{
		Subject:   subject,
		Resource:  resource,
		Context:   context,
		PageSize:  value.PageSize,
		PageToken: value.PageToken,
	}, nil
}

func actionSearchRequestFromProto(value *proto.ActionSearchRequest) *ActionSearchRequest {
	if value == nil {
		return nil
	}
	return &ActionSearchRequest{
		Subject:   authorizationSubjectFromProto(value.GetSubject()),
		Resource:  authorizationResourceFromProto(value.GetResource()),
		Context:   mapFromStruct(value.GetContext()),
		PageSize:  value.GetPageSize(),
		PageToken: value.GetPageToken(),
	}
}

func protoActionSearchResponse(value *ActionSearchResponse) (*proto.ActionSearchResponse, error) {
	if value == nil {
		return nil, nil
	}
	actions := make([]*proto.Action, 0, len(value.Actions))
	for i, action := range value.Actions {
		out, err := protoAuthorizationAction(action)
		if err != nil {
			return nil, fmt.Errorf("actions[%d]: %w", i, err)
		}
		if out != nil {
			actions = append(actions, out)
		}
	}
	return &proto.ActionSearchResponse{
		Actions:       actions,
		NextPageToken: value.NextPageToken,
		ModelId:       value.ModelId,
	}, nil
}

func actionSearchResponseFromProto(value *proto.ActionSearchResponse) *ActionSearchResponse {
	if value == nil {
		return nil
	}
	actions := make([]*AuthorizationAction, 0, len(value.GetActions()))
	for _, action := range value.GetActions() {
		actions = append(actions, authorizationActionFromProto(action))
	}
	return &ActionSearchResponse{
		Actions:       actions,
		NextPageToken: value.GetNextPageToken(),
		ModelId:       value.GetModelId(),
	}
}

func protoAuthorizationMetadata(value *AuthorizationMetadata) *proto.AuthorizationMetadata {
	if value == nil {
		return nil
	}
	return &proto.AuthorizationMetadata{
		Capabilities:  append([]string(nil), value.Capabilities...),
		ActiveModelId: value.ActiveModelId,
	}
}

func authorizationMetadataFromProto(value *proto.AuthorizationMetadata) *AuthorizationMetadata {
	if value == nil {
		return nil
	}
	return &AuthorizationMetadata{
		Capabilities:  append([]string(nil), value.GetCapabilities()...),
		ActiveModelId: value.GetActiveModelId(),
	}
}

func protoRelationship(value *Relationship) (*proto.Relationship, error) {
	if value == nil {
		return nil, nil
	}
	subject, err := protoAuthorizationSubject(value.Subject)
	if err != nil {
		return nil, err
	}
	resource, err := protoAuthorizationResource(value.Resource)
	if err != nil {
		return nil, err
	}
	properties, err := structFromAny(value.Properties)
	if err != nil {
		return nil, fmt.Errorf("relationship properties: %w", err)
	}
	target, err := protoAuthorizationRelationshipTarget(value.Target)
	if err != nil {
		return nil, err
	}
	return &proto.Relationship{
		Subject:    subject,
		Relation:   value.Relation,
		Resource:   resource,
		Properties: properties,
		Target:     target,
	}, nil
}

func relationshipFromProto(value *proto.Relationship) *Relationship {
	if value == nil {
		return nil
	}
	return &Relationship{
		Subject:    authorizationSubjectFromProto(value.GetSubject()),
		Relation:   value.GetRelation(),
		Resource:   authorizationResourceFromProto(value.GetResource()),
		Properties: mapFromStruct(value.GetProperties()),
		Target:     authorizationRelationshipTargetFromProto(value.GetTarget()),
	}
}

func protoRelationshipKey(value *RelationshipKey) (*proto.RelationshipKey, error) {
	if value == nil {
		return nil, nil
	}
	subject, err := protoAuthorizationSubject(value.Subject)
	if err != nil {
		return nil, err
	}
	resource, err := protoAuthorizationResource(value.Resource)
	if err != nil {
		return nil, err
	}
	target, err := protoAuthorizationRelationshipTarget(value.Target)
	if err != nil {
		return nil, err
	}
	return &proto.RelationshipKey{
		Subject:  subject,
		Relation: value.Relation,
		Resource: resource,
		Target:   target,
	}, nil
}

func relationshipKeyFromProto(value *proto.RelationshipKey) *RelationshipKey {
	if value == nil {
		return nil
	}
	return &RelationshipKey{
		Subject:  authorizationSubjectFromProto(value.GetSubject()),
		Relation: value.GetRelation(),
		Resource: authorizationResourceFromProto(value.GetResource()),
		Target:   authorizationRelationshipTargetFromProto(value.GetTarget()),
	}
}

func protoReadRelationshipsRequest(value *ReadRelationshipsRequest) (*proto.ReadRelationshipsRequest, error) {
	if value == nil {
		return nil, nil
	}
	subject, err := protoAuthorizationSubject(value.Subject)
	if err != nil {
		return nil, err
	}
	resource, err := protoAuthorizationResource(value.Resource)
	if err != nil {
		return nil, err
	}
	target, err := protoAuthorizationRelationshipTarget(value.Target)
	if err != nil {
		return nil, err
	}
	return &proto.ReadRelationshipsRequest{
		Subject:   subject,
		Relation:  value.Relation,
		Resource:  resource,
		PageSize:  value.PageSize,
		PageToken: value.PageToken,
		ModelId:   value.ModelId,
		Target:    target,
	}, nil
}

func readRelationshipsRequestFromProto(value *proto.ReadRelationshipsRequest) *ReadRelationshipsRequest {
	if value == nil {
		return nil
	}
	return &ReadRelationshipsRequest{
		Subject:   authorizationSubjectFromProto(value.GetSubject()),
		Relation:  value.GetRelation(),
		Resource:  authorizationResourceFromProto(value.GetResource()),
		PageSize:  value.GetPageSize(),
		PageToken: value.GetPageToken(),
		ModelId:   value.GetModelId(),
		Target:    authorizationRelationshipTargetFromProto(value.GetTarget()),
	}
}

func protoReadRelationshipsResponse(value *ReadRelationshipsResponse) (*proto.ReadRelationshipsResponse, error) {
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
	return &proto.ReadRelationshipsResponse{
		Relationships: relationships,
		NextPageToken: value.NextPageToken,
		ModelId:       value.ModelId,
	}, nil
}

func readRelationshipsResponseFromProto(value *proto.ReadRelationshipsResponse) *ReadRelationshipsResponse {
	if value == nil {
		return nil
	}
	relationships := make([]*Relationship, 0, len(value.GetRelationships()))
	for _, relationship := range value.GetRelationships() {
		relationships = append(relationships, relationshipFromProto(relationship))
	}
	return &ReadRelationshipsResponse{
		Relationships: relationships,
		NextPageToken: value.GetNextPageToken(),
		ModelId:       value.GetModelId(),
	}
}

func protoWriteRelationshipsRequest(value *WriteRelationshipsRequest) (*proto.WriteRelationshipsRequest, error) {
	if value == nil {
		return nil, nil
	}
	writes := make([]*proto.Relationship, 0, len(value.Writes))
	for i, write := range value.Writes {
		out, err := protoRelationship(write)
		if err != nil {
			return nil, fmt.Errorf("writes[%d]: %w", i, err)
		}
		if out != nil {
			writes = append(writes, out)
		}
	}
	deletes := make([]*proto.RelationshipKey, 0, len(value.Deletes))
	for i, deleteKey := range value.Deletes {
		out, err := protoRelationshipKey(deleteKey)
		if err != nil {
			return nil, fmt.Errorf("deletes[%d]: %w", i, err)
		}
		if out != nil {
			deletes = append(deletes, out)
		}
	}
	return &proto.WriteRelationshipsRequest{
		Writes:  writes,
		Deletes: deletes,
		ModelId: value.ModelId,
	}, nil
}

func writeRelationshipsRequestFromProto(value *proto.WriteRelationshipsRequest) *WriteRelationshipsRequest {
	if value == nil {
		return nil
	}
	writes := make([]*Relationship, 0, len(value.GetWrites()))
	for _, write := range value.GetWrites() {
		writes = append(writes, relationshipFromProto(write))
	}
	deletes := make([]*RelationshipKey, 0, len(value.GetDeletes()))
	for _, deleteKey := range value.GetDeletes() {
		deletes = append(deletes, relationshipKeyFromProto(deleteKey))
	}
	return &WriteRelationshipsRequest{
		Writes:  writes,
		Deletes: deletes,
		ModelId: value.GetModelId(),
	}
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
	return &proto.AuthorizationModel{
		Version:       value.Version,
		ResourceTypes: resourceTypes,
	}
}

func authorizationModelFromProto(value *proto.AuthorizationModel) *AuthorizationModel {
	if value == nil {
		return nil
	}
	resourceTypes := make([]*AuthorizationModelResourceType, 0, len(value.GetResourceTypes()))
	for _, resourceType := range value.GetResourceTypes() {
		resourceTypes = append(resourceTypes, authorizationModelResourceTypeFromProto(resourceType))
	}
	return &AuthorizationModel{
		Version:       value.GetVersion(),
		ResourceTypes: resourceTypes,
	}
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
		Name:      value.Name,
		Relations: relations,
		Actions:   actions,
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
		Name:      value.GetName(),
		Relations: relations,
		Actions:   actions,
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
	return &proto.AuthorizationModelRelation{
		Name:           value.Name,
		SubjectTypes:   append([]string(nil), value.SubjectTypes...),
		AllowedTargets: allowedTargets,
		Rewrite:        protoAuthorizationModelRewrite(value.Rewrite),
	}
}

func authorizationModelRelationFromProto(value *proto.AuthorizationModelRelation) *AuthorizationModelRelation {
	if value == nil {
		return nil
	}
	allowedTargets := make([]*AuthorizationModelAllowedTarget, 0, len(value.GetAllowedTargets()))
	for _, target := range value.GetAllowedTargets() {
		allowedTargets = append(allowedTargets, authorizationModelAllowedTargetFromProto(target))
	}
	return &AuthorizationModelRelation{
		Name:           value.GetName(),
		SubjectTypes:   append([]string(nil), value.GetSubjectTypes()...),
		AllowedTargets: allowedTargets,
		Rewrite:        authorizationModelRewriteFromProto(value.GetRewrite()),
	}
}

func protoAuthorizationModelAction(value *AuthorizationModelAction) *proto.AuthorizationModelAction {
	if value == nil {
		return nil
	}
	return &proto.AuthorizationModelAction{
		Name:      value.Name,
		Relations: append([]string(nil), value.Relations...),
		Rewrite:   protoAuthorizationModelRewrite(value.Rewrite),
	}
}

func authorizationModelActionFromProto(value *proto.AuthorizationModelAction) *AuthorizationModelAction {
	if value == nil {
		return nil
	}
	return &AuthorizationModelAction{
		Name:      value.GetName(),
		Relations: append([]string(nil), value.GetRelations()...),
		Rewrite:   authorizationModelRewriteFromProto(value.GetRewrite()),
	}
}

func protoAuthorizationModelAllowedTarget(value *AuthorizationModelAllowedTarget) *proto.AuthorizationModelAllowedTarget {
	if value == nil {
		return nil
	}
	switch {
	case value.SubjectType != "":
		return &proto.AuthorizationModelAllowedTarget{
			Kind: &proto.AuthorizationModelAllowedTarget_SubjectType{SubjectType: value.SubjectType},
		}
	case value.ResourceType != "":
		return &proto.AuthorizationModelAllowedTarget{
			Kind: &proto.AuthorizationModelAllowedTarget_ResourceType{ResourceType: value.ResourceType},
		}
	case value.SubjectSet != nil:
		return &proto.AuthorizationModelAllowedTarget{
			Kind: &proto.AuthorizationModelAllowedTarget_SubjectSet{
				SubjectSet: &proto.AuthorizationModelSubjectSetTarget{
					ResourceType: value.SubjectSet.ResourceType,
					Relation:     value.SubjectSet.Relation,
				},
			},
		}
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
	case *proto.AuthorizationModelAllowedTarget_SubjectSet:
		if kind.SubjectSet == nil {
			return &AuthorizationModelAllowedTarget{}
		}
		return &AuthorizationModelAllowedTarget{
			SubjectSet: &AuthorizationModelSubjectSetTarget{
				ResourceType: kind.SubjectSet.GetResourceType(),
				Relation:     kind.SubjectSet.GetRelation(),
			},
		}
	default:
		return &AuthorizationModelAllowedTarget{}
	}
}

func protoAuthorizationModelRewrite(value *AuthorizationModelRewrite) *proto.AuthorizationModelRewrite {
	if value == nil {
		return nil
	}
	switch {
	case value.This != nil:
		return &proto.AuthorizationModelRewrite{
			Kind: &proto.AuthorizationModelRewrite_This{This: &proto.AuthorizationModelRewriteThis{}},
		}
	case value.ComputedUserset != nil:
		return &proto.AuthorizationModelRewrite{
			Kind: &proto.AuthorizationModelRewrite_ComputedUserset{
				ComputedUserset: &proto.AuthorizationModelComputedUserset{Relation: value.ComputedUserset.Relation},
			},
		}
	case value.TupleToUserset != nil:
		return &proto.AuthorizationModelRewrite{
			Kind: &proto.AuthorizationModelRewrite_TupleToUserset{
				TupleToUserset: &proto.AuthorizationModelTupleToUserset{
					TuplesetRelation: value.TupleToUserset.TuplesetRelation,
					ComputedRelation: value.TupleToUserset.ComputedRelation,
				},
			},
		}
	case value.Union != nil:
		children := make([]*proto.AuthorizationModelRewrite, 0, len(value.Union.Children))
		for _, child := range value.Union.Children {
			if out := protoAuthorizationModelRewrite(child); out != nil {
				children = append(children, out)
			}
		}
		return &proto.AuthorizationModelRewrite{
			Kind: &proto.AuthorizationModelRewrite_Union{
				Union: &proto.AuthorizationModelRewriteUnion{Children: children},
			},
		}
	default:
		return &proto.AuthorizationModelRewrite{}
	}
}

func authorizationModelRewriteFromProto(value *proto.AuthorizationModelRewrite) *AuthorizationModelRewrite {
	if value == nil {
		return nil
	}
	switch kind := value.GetKind().(type) {
	case *proto.AuthorizationModelRewrite_This:
		return &AuthorizationModelRewrite{This: &AuthorizationModelRewriteThis{}}
	case *proto.AuthorizationModelRewrite_ComputedUserset:
		return &AuthorizationModelRewrite{
			ComputedUserset: &AuthorizationModelComputedUserset{Relation: kind.ComputedUserset.GetRelation()},
		}
	case *proto.AuthorizationModelRewrite_TupleToUserset:
		return &AuthorizationModelRewrite{
			TupleToUserset: &AuthorizationModelTupleToUserset{
				TuplesetRelation: kind.TupleToUserset.GetTuplesetRelation(),
				ComputedRelation: kind.TupleToUserset.GetComputedRelation(),
			},
		}
	case *proto.AuthorizationModelRewrite_Union:
		var children []*AuthorizationModelRewrite
		if kind.Union != nil {
			children = make([]*AuthorizationModelRewrite, 0, len(kind.Union.GetChildren()))
			for _, child := range kind.Union.GetChildren() {
				children = append(children, authorizationModelRewriteFromProto(child))
			}
		}
		return &AuthorizationModelRewrite{Union: &AuthorizationModelRewriteUnion{Children: children}}
	default:
		return &AuthorizationModelRewrite{}
	}
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
	return &AuthorizationModelRef{
		Id:        value.GetId(),
		Version:   value.GetVersion(),
		CreatedAt: timeFromTimestamp(value.GetCreatedAt()),
	}
}

func protoGetActiveModelResponse(value *GetActiveModelResponse) *proto.GetActiveModelResponse {
	if value == nil {
		return nil
	}
	return &proto.GetActiveModelResponse{Model: protoAuthorizationModelRef(value.Model)}
}

func getActiveModelResponseFromProto(value *proto.GetActiveModelResponse) *GetActiveModelResponse {
	if value == nil {
		return nil
	}
	return &GetActiveModelResponse{Model: authorizationModelRefFromProto(value.GetModel())}
}

func protoListModelsRequest(value *ListModelsRequest) *proto.ListModelsRequest {
	if value == nil {
		return nil
	}
	return &proto.ListModelsRequest{
		PageSize:  value.PageSize,
		PageToken: value.PageToken,
	}
}

func listModelsRequestFromProto(value *proto.ListModelsRequest) *ListModelsRequest {
	if value == nil {
		return nil
	}
	return &ListModelsRequest{
		PageSize:  value.GetPageSize(),
		PageToken: value.GetPageToken(),
	}
}

func protoListModelsResponse(value *ListModelsResponse) *proto.ListModelsResponse {
	if value == nil {
		return nil
	}
	models := make([]*proto.AuthorizationModelRef, 0, len(value.Models))
	for _, model := range value.Models {
		if out := protoAuthorizationModelRef(model); out != nil {
			models = append(models, out)
		}
	}
	return &proto.ListModelsResponse{
		Models:        models,
		NextPageToken: value.NextPageToken,
	}
}

func listModelsResponseFromProto(value *proto.ListModelsResponse) *ListModelsResponse {
	if value == nil {
		return nil
	}
	models := make([]*AuthorizationModelRef, 0, len(value.GetModels()))
	for _, model := range value.GetModels() {
		models = append(models, authorizationModelRefFromProto(model))
	}
	return &ListModelsResponse{
		Models:        models,
		NextPageToken: value.GetNextPageToken(),
	}
}

func protoWriteModelRequest(value *WriteModelRequest) *proto.WriteModelRequest {
	if value == nil {
		return nil
	}
	return &proto.WriteModelRequest{Model: protoAuthorizationModel(value.Model)}
}

func writeModelRequestFromProto(value *proto.WriteModelRequest) *WriteModelRequest {
	if value == nil {
		return nil
	}
	return &WriteModelRequest{Model: authorizationModelFromProto(value.GetModel())}
}

func protoExpandRequest(value *ExpandRequest) (*proto.ExpandRequest, error) {
	if value == nil {
		return nil, nil
	}
	resource, err := protoAuthorizationResource(value.Resource)
	if err != nil {
		return nil, err
	}
	context, err := structFromAny(value.Context)
	if err != nil {
		return nil, fmt.Errorf("context: %w", err)
	}
	return &proto.ExpandRequest{
		Resource: resource,
		Relation: value.Relation,
		Context:  context,
		MaxDepth: value.MaxDepth,
		ModelId:  value.ModelId,
	}, nil
}

func expandRequestFromProto(value *proto.ExpandRequest) *ExpandRequest {
	if value == nil {
		return nil
	}
	return &ExpandRequest{
		Resource: authorizationResourceFromProto(value.GetResource()),
		Relation: value.GetRelation(),
		Context:  mapFromStruct(value.GetContext()),
		MaxDepth: value.GetMaxDepth(),
		ModelId:  value.GetModelId(),
	}
}

func protoExpandNode(value *ExpandNode) (*proto.ExpandNode, error) {
	if value == nil {
		return nil, nil
	}
	target, err := protoAuthorizationRelationshipTarget(value.Target)
	if err != nil {
		return nil, err
	}
	children := make([]*proto.ExpandNode, 0, len(value.Children))
	for i, child := range value.Children {
		out, err := protoExpandNode(child)
		if err != nil {
			return nil, fmt.Errorf("children[%d]: %w", i, err)
		}
		if out != nil {
			children = append(children, out)
		}
	}
	return &proto.ExpandNode{
		Target:   target,
		Relation: value.Relation,
		Children: children,
	}, nil
}

func expandNodeFromProto(value *proto.ExpandNode) *ExpandNode {
	if value == nil {
		return nil
	}
	children := make([]*ExpandNode, 0, len(value.GetChildren()))
	for _, child := range value.GetChildren() {
		children = append(children, expandNodeFromProto(child))
	}
	return &ExpandNode{
		Target:   authorizationRelationshipTargetFromProto(value.GetTarget()),
		Relation: value.GetRelation(),
		Children: children,
	}
}

func protoExpandResponse(value *ExpandResponse) (*proto.ExpandResponse, error) {
	if value == nil {
		return nil, nil
	}
	root, err := protoExpandNode(value.Root)
	if err != nil {
		return nil, err
	}
	return &proto.ExpandResponse{
		Root:            root,
		Truncated:       value.Truncated,
		CycleDetected:   value.CycleDetected,
		MaxDepthReached: value.MaxDepthReached,
		ModelId:         value.ModelId,
	}, nil
}

func expandResponseFromProto(value *proto.ExpandResponse) *ExpandResponse {
	if value == nil {
		return nil
	}
	return &ExpandResponse{
		Root:            expandNodeFromProto(value.GetRoot()),
		Truncated:       value.GetTruncated(),
		CycleDetected:   value.GetCycleDetected(),
		MaxDepthReached: value.GetMaxDepthReached(),
		ModelId:         value.GetModelId(),
	}
}
