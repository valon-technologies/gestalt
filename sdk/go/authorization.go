package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
)

// AuthorizationMetadata describes the host authorization provider.
type AuthorizationMetadata = proto.AuthorizationMetadata

// AuthorizationSubject is a generated subject descriptor.
type AuthorizationSubject = proto.Subject

// AuthorizationResource is a generated resource descriptor.
type AuthorizationResource = proto.Resource

// AuthorizationAction is a generated action descriptor.
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
type Relationship = proto.Relationship

// RelationshipKey identifies one authorization relationship tuple.
type RelationshipKey = proto.RelationshipKey

// ReadRelationshipsRequest selects authorization relationships to read.
type ReadRelationshipsRequest = proto.ReadRelationshipsRequest

// ReadRelationshipsResponse contains authorization relationships.
type ReadRelationshipsResponse = proto.ReadRelationshipsResponse

// WriteRelationshipsRequest mutates authorization relationships.
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

// AuthorizationProvider serves authorization APIs to the host.
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
