package core

import (
	"context"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
)

type AuthorizationMetadata = proto.AuthorizationMetadata
type SubjectRef = proto.Subject
type ResourceRef = proto.Resource
type ActionRef = proto.Action

type AccessEvaluationRequest = proto.AccessEvaluationRequest
type AccessDecision = proto.AccessDecision
type AccessEvaluationsRequest = proto.AccessEvaluationsRequest
type AccessEvaluationsResponse = proto.AccessEvaluationsResponse

type ResourceSearchRequest = proto.ResourceSearchRequest
type ResourceSearchResponse = proto.ResourceSearchResponse
type SubjectSearchRequest = proto.SubjectSearchRequest
type SubjectSearchResponse = proto.SubjectSearchResponse
type ActionSearchRequest = proto.ActionSearchRequest
type ActionSearchResponse = proto.ActionSearchResponse

type Relationship = proto.Relationship
type RelationshipKey = proto.RelationshipKey
type ReadRelationshipsRequest = proto.ReadRelationshipsRequest
type ReadRelationshipsResponse = proto.ReadRelationshipsResponse
type WriteRelationshipsRequest = proto.WriteRelationshipsRequest

type AuthorizationModel = proto.AuthorizationModel
type AuthorizationModelResourceType = proto.AuthorizationModelResourceType
type AuthorizationModelRelation = proto.AuthorizationModelRelation
type AuthorizationModelAction = proto.AuthorizationModelAction
type AuthorizationModelRef = proto.AuthorizationModelRef
type GetActiveModelResponse = proto.GetActiveModelResponse
type ListModelsRequest = proto.ListModelsRequest
type ListModelsResponse = proto.ListModelsResponse
type WriteModelRequest = proto.WriteModelRequest

type AuthorizationProvider interface {
	Name() string

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
