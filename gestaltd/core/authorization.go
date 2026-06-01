package core

import (
	"context"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

type AuthorizationSourceLayer = proto.SourceLayer
type AuthorizationRelationshipTargetType = proto.RelationshipTargetType
type SubjectRef = proto.Subject
type ResourceRef = proto.Resource
type SubjectSetRef = proto.SubjectSet
type RelationshipTargetRef = proto.RelationshipTarget
type ActionRef = proto.Action

type CheckAccessRequest = proto.CheckAccessRequest
type CheckAccessResponse = proto.CheckAccessResponse
type CheckAccessManyRequest = proto.CheckAccessManyRequest
type CheckAccessManyResponse = proto.CheckAccessManyResponse

type Relationship = proto.Relationship
type RelationshipTuple = proto.RelationshipTuple
type RelationshipFilter = proto.RelationshipFilter
type ListRelationshipsRequest = proto.ListRelationshipsRequest
type ListRelationshipsResponse = proto.ListRelationshipsResponse
type AddRelationshipRequest = proto.AddRelationshipRequest
type AddRelationshipResponse = proto.AddRelationshipResponse
type DeleteRelationshipRequest = proto.DeleteRelationshipRequest
type DeleteRelationshipResponse = proto.DeleteRelationshipResponse
type SetRelationshipsRequest = proto.SetRelationshipsRequest
type SetRelationshipsResponse = proto.SetRelationshipsResponse

type AuthorizationModel = proto.AuthorizationModel
type AuthorizationModelRef = proto.AuthorizationModelRef
type AuthorizationModelResourceType = proto.AuthorizationModelResourceType
type AuthorizationModelRelation = proto.AuthorizationModelRelation
type AuthorizationModelAction = proto.AuthorizationModelAction
type AuthorizationModelAllowedTarget = proto.AuthorizationModelAllowedTarget
type AuthorizationSubjectSetType = proto.SubjectSetType
type GetActiveModelRefResponse = proto.GetActiveModelRefResponse
type SetActiveModelRequest = proto.SetActiveModelRequest
type SetActiveModelResponse = proto.SetActiveModelResponse
type AuthorizationModelResourceTypeFilter = proto.AuthorizationModelResourceTypeFilter
type ListActiveModelResourceTypesRequest = proto.ListActiveModelResourceTypesRequest
type ListActiveModelResourceTypesResponse = proto.ListActiveModelResourceTypesResponse

const (
	AuthorizationSourceLayerUnspecified  = proto.SourceLayer_SOURCE_LAYER_UNSPECIFIED
	AuthorizationSourceLayerStaticConfig = proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG
	AuthorizationSourceLayerRuntime      = proto.SourceLayer_SOURCE_LAYER_RUNTIME
)

type AuthorizationProvider interface {
	Name() string

	CheckAccess(ctx context.Context, req *CheckAccessRequest) (*CheckAccessResponse, error)
	CheckAccessMany(ctx context.Context, req *CheckAccessManyRequest) (*CheckAccessManyResponse, error)

	ListRelationships(ctx context.Context, req *ListRelationshipsRequest) (*ListRelationshipsResponse, error)
	AddRelationship(ctx context.Context, req *AddRelationshipRequest) (*AddRelationshipResponse, error)
	DeleteRelationship(ctx context.Context, req *DeleteRelationshipRequest) (*DeleteRelationshipResponse, error)
	SetRelationships(ctx context.Context, req *SetRelationshipsRequest) (*SetRelationshipsResponse, error)

	GetActiveModelRef(ctx context.Context) (*GetActiveModelRefResponse, error)
	SetActiveModel(ctx context.Context, req *SetActiveModelRequest) (*SetActiveModelResponse, error)
	ListActiveModelResourceTypes(ctx context.Context, req *ListActiveModelResourceTypesRequest) (*ListActiveModelResourceTypesResponse, error)
}
