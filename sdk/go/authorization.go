package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
)

// AuthorizationMetadata describes the host authorization provider.
type AuthorizationMetadata = proto.AuthorizationMetadata

// AuthorizationSubject identifies a subject in the authorization graph.
//
// SDK callers should construct values with [NewAuthorizationSubject] instead of
// importing generated protobuf packages directly.
type AuthorizationSubject = proto.Subject

// AuthorizationResource identifies a protected resource in the authorization graph.
//
// SDK callers should construct values with [NewAuthorizationResource] instead of
// importing generated protobuf packages directly.
type AuthorizationResource = proto.Resource

// AuthorizationAction identifies an action being evaluated against a resource.
//
// SDK callers should construct values with [NewAuthorizationAction] instead of
// importing generated protobuf packages directly.
type AuthorizationAction = proto.Action

// AccessEvaluationRequest asks whether one subject can perform one action.
type AccessEvaluationRequest = proto.AccessEvaluationRequest

// AccessDecision is the result of evaluating one access request.
type AccessDecision = proto.AccessDecision

// AccessEvaluationsRequest batches access evaluation requests.
type AccessEvaluationsRequest = proto.AccessEvaluationsRequest

// AccessEvaluationsResponse batches access evaluation results.
type AccessEvaluationsResponse = proto.AccessEvaluationsResponse

// ResourceSearchRequest searches resources visible to a subject.
type ResourceSearchRequest = proto.ResourceSearchRequest

// ResourceSearchResponse contains resources visible to a subject.
type ResourceSearchResponse = proto.ResourceSearchResponse

// SubjectSearchRequest searches subjects related to a resource and action.
type SubjectSearchRequest = proto.SubjectSearchRequest

// SubjectSearchResponse contains subjects related to a resource and action.
type SubjectSearchResponse = proto.SubjectSearchResponse

// ActionSearchRequest searches actions available between a subject and resource.
type ActionSearchRequest = proto.ActionSearchRequest

// ActionSearchResponse contains actions available between a subject and resource.
type ActionSearchResponse = proto.ActionSearchResponse

// Relationship describes one authorization relationship tuple.
//
// A relationship grants a subject a relation on a resource, for example
// subject "user:123" has relation "editor" on resource "agent_session:sess_123".
type Relationship = proto.Relationship

// RelationshipKey identifies one authorization relationship tuple.
type RelationshipKey = proto.RelationshipKey

// ReadRelationshipsRequest selects authorization relationships to read.
type ReadRelationshipsRequest = proto.ReadRelationshipsRequest

// ReadRelationshipsResponse contains authorization relationships.
type ReadRelationshipsResponse = proto.ReadRelationshipsResponse

// WriteRelationshipsRequest mutates authorization relationships.
//
// Writes are upserts and deletes remove exact [RelationshipKey] tuples.
type WriteRelationshipsRequest = proto.WriteRelationshipsRequest

// AuthorizationModel describes an authorization model.
type AuthorizationModel = proto.AuthorizationModel

// AuthorizationModelResourceType describes one resource type in a model.
type AuthorizationModelResourceType = proto.AuthorizationModelResourceType

// AuthorizationModelRelation describes one relation in a model.
type AuthorizationModelRelation = proto.AuthorizationModelRelation

// AuthorizationModelAction describes one action in a model.
type AuthorizationModelAction = proto.AuthorizationModelAction

// AuthorizationModelRef identifies a stored authorization model.
type AuthorizationModelRef = proto.AuthorizationModelRef

// GetActiveModelResponse returns the active authorization model.
type GetActiveModelResponse = proto.GetActiveModelResponse

// ListModelsRequest selects authorization models to list.
type ListModelsRequest = proto.ListModelsRequest

// ListModelsResponse contains authorization model refs.
type ListModelsResponse = proto.ListModelsResponse

// WriteModelRequest stores an authorization model.
type WriteModelRequest = proto.WriteModelRequest

// NewAuthorizationSubject creates a subject reference for authorization requests.
func NewAuthorizationSubject(subjectType, id string) *AuthorizationSubject {
	return &AuthorizationSubject{Type: subjectType, Id: id}
}

// NewAuthorizationResource creates a resource reference for authorization requests.
func NewAuthorizationResource(resourceType, id string) *AuthorizationResource {
	return &AuthorizationResource{Type: resourceType, Id: id}
}

// NewAuthorizationAction creates an action reference for authorization requests.
func NewAuthorizationAction(name string) *AuthorizationAction {
	return &AuthorizationAction{Name: name}
}

// NewAccessEvaluationRequest creates an access-evaluation request.
func NewAccessEvaluationRequest(subject *AuthorizationSubject, action *AuthorizationAction, resource *AuthorizationResource) *AccessEvaluationRequest {
	return &AccessEvaluationRequest{
		Subject:  subject,
		Action:   action,
		Resource: resource,
	}
}

// NewRelationship creates a relationship tuple for authorization writes.
func NewRelationship(subject *AuthorizationSubject, relation string, resource *AuthorizationResource) *Relationship {
	return &Relationship{
		Subject:  subject,
		Relation: relation,
		Resource: resource,
	}
}

// NewRelationshipKey creates a relationship key for authorization deletes.
func NewRelationshipKey(subject *AuthorizationSubject, relation string, resource *AuthorizationResource) *RelationshipKey {
	return &RelationshipKey{
		Subject:  subject,
		Relation: relation,
		Resource: resource,
	}
}

// NewWriteRelationshipsRequest creates a relationship mutation request.
func NewWriteRelationshipsRequest(writes []*Relationship, deletes []*RelationshipKey) *WriteRelationshipsRequest {
	return &WriteRelationshipsRequest{
		Writes:  writes,
		Deletes: deletes,
	}
}

// AuthorizationProvider serves authorization APIs to the host.
//
// Provider implementations can use the SDK authorization aliases and
// constructors in this package; they do not need to import generated protobuf
// packages directly.
type AuthorizationProvider interface {
	Provider
	Evaluate(ctx context.Context, req *AccessEvaluationRequest) (*AccessDecision, error)
	EvaluateMany(ctx context.Context, req *AccessEvaluationsRequest) (*AccessEvaluationsResponse, error)
	SearchResources(ctx context.Context, req *ResourceSearchRequest) (*ResourceSearchResponse, error)
	SearchSubjects(ctx context.Context, req *SubjectSearchRequest) (*SubjectSearchResponse, error)
	SearchActions(ctx context.Context, req *ActionSearchRequest) (*ActionSearchResponse, error)
	GetMetadata(ctx context.Context) (*AuthorizationMetadata, error)
	ReadRelationships(ctx context.Context, req *ReadRelationshipsRequest) (*ReadRelationshipsResponse, error)
	WriteRelationships(ctx context.Context, req *WriteRelationshipsRequest) error
	GetActiveModel(ctx context.Context) (*GetActiveModelResponse, error)
	ListModels(ctx context.Context, req *ListModelsRequest) (*ListModelsResponse, error)
	WriteModel(ctx context.Context, req *WriteModelRequest) (*AuthorizationModelRef, error)
}
