package gestalt

import (
	"context"
	"time"
)

// AuthorizationSubject identifies the subject of a relationship or check.
type AuthorizationSubject struct {
	Type       string
	Id         string
	Properties map[string]any
}

// AuthorizationAction names one action of a resource type.
type AuthorizationAction struct {
	Name       string
	Properties map[string]any
}

// AuthorizationResource identifies the resource of a relationship or check.
type AuthorizationResource struct {
	Type       string
	Id         string
	Properties map[string]any
}

// CheckAccessRequest is the native message type for gestalt.provider.v1.CheckAccessRequest.
type CheckAccessRequest struct {
	Subject  *AuthorizationSubject
	Action   *AuthorizationAction
	Resource *AuthorizationResource
}

// CheckAccessResponse is the native message type for gestalt.provider.v1.CheckAccessResponse.
type CheckAccessResponse struct {
	Allowed bool
	ModelId string
}

// CheckAccessManyRequest is the native message type for gestalt.provider.v1.CheckAccessManyRequest.
type CheckAccessManyRequest struct {
	Requests []*CheckAccessRequest
}

// CheckAccessManyResponse is the native message type for gestalt.provider.v1.CheckAccessManyResponse.
type CheckAccessManyResponse struct {
	Decisions []*CheckAccessResponse
}

// RelationshipTargetType is the native message type for gestalt.provider.v1.RelationshipTargetType.
type RelationshipTargetType int32

// The relationship target types.
const (
	RelationshipTargetTypeUnspecified RelationshipTargetType = 0
	RelationshipTargetTypeSubject     RelationshipTargetType = 1
	RelationshipTargetTypeResource    RelationshipTargetType = 2
	RelationshipTargetTypeSubjectSet  RelationshipTargetType = 3
)

// SourceLayer is the native message type for gestalt.provider.v1.SourceLayer.
type SourceLayer int32

// The relationship source layers.
const (
	SourceLayerUnspecified  SourceLayer = 0
	SourceLayerStaticConfig SourceLayer = 1
	SourceLayerRuntime      SourceLayer = 2
)

// RelationshipFilter is the native message type for gestalt.provider.v1.RelationshipFilter.
type RelationshipFilter struct {
	Target           *RelationshipTarget
	Relation         string
	Resource         *AuthorizationResource
	TargetType       RelationshipTargetType
	TargetEntityType string
	ResourceType     string
	SourceLayer      SourceLayer
}

// ListRelationshipsRequest is the native message type for gestalt.provider.v1.ListRelationshipsRequest.
type ListRelationshipsRequest struct {
	Filter    *RelationshipFilter
	PageSize  int32
	PageToken string
}

// ListRelationshipsResponse is the native message type for gestalt.provider.v1.ListRelationshipsResponse.
type ListRelationshipsResponse struct {
	Relationships []*Relationship
	NextPageToken string
}

// AddRelationshipRequest is the native message type for gestalt.provider.v1.AddRelationshipRequest.
type AddRelationshipRequest struct {
	Relationship *Relationship
}

// AddRelationshipResponse is the native message type for gestalt.provider.v1.AddRelationshipResponse.
type AddRelationshipResponse struct {
	Relationship *Relationship
}

// DeleteRelationshipRequest is the native message type for gestalt.provider.v1.DeleteRelationshipRequest.
type DeleteRelationshipRequest struct {
	RelationshipTuple *RelationshipTuple
}

// DeleteRelationshipResponse is the native message type for gestalt.provider.v1.DeleteRelationshipResponse.
type DeleteRelationshipResponse struct{}

// SetAuthorizationStateRequest is the native message type for gestalt.provider.v1.SetAuthorizationStateRequest.
type SetAuthorizationStateRequest struct {
	Model         *AuthorizationModel
	Relationships []*Relationship
}

// SetAuthorizationStateResponse is the native message type for gestalt.provider.v1.SetAuthorizationStateResponse.
type SetAuthorizationStateResponse struct {
	ActiveModel *AuthorizationModelRef
}

// Relationship is the native message type for gestalt.provider.v1.Relationship.
type Relationship struct {
	Tuple       *RelationshipTuple
	Properties  map[string]any
	SourceLayer SourceLayer
}

// RelationshipTuple is the native message type for gestalt.provider.v1.RelationshipTuple.
type RelationshipTuple struct {
	Target   *RelationshipTarget
	Relation string
	Resource *AuthorizationResource
}

// RelationshipTarget is the native message type for gestalt.provider.v1.RelationshipTarget.
type RelationshipTarget struct {
	Subject    *AuthorizationSubject
	Resource   *AuthorizationResource
	SubjectSet *SubjectSet
}

// SubjectSet is the native message type for gestalt.provider.v1.SubjectSet.
type SubjectSet struct {
	Resource *AuthorizationResource
	Relation string
}

// AuthorizationModel is the native message type for gestalt.provider.v1.AuthorizationModel.
type AuthorizationModel struct {
	Id            string
	Version       string
	ResourceTypes []*AuthorizationModelResourceType
}

// AuthorizationModelResourceType is the native message type for gestalt.provider.v1.AuthorizationModelResourceType.
type AuthorizationModelResourceType struct {
	Name        string
	Relations   []*ModelRelation
	Actions     []*ModelAction
	SourceLayer SourceLayer
	DefaultRole string
}

// ModelRelation is the native message type for gestalt.provider.v1.ModelRelation.
type ModelRelation struct {
	Name           string
	AllowedTargets []*ModelAllowedTarget
}

// ModelAction is the native message type for gestalt.provider.v1.ModelAction.
type ModelAction struct {
	Name      string
	Relations []string
}

// ModelAllowedTarget is the native message type for gestalt.provider.v1.ModelAllowedTarget.
type ModelAllowedTarget struct {
	SubjectType    string
	ResourceType   string
	SubjectSetType *SubjectSetType
}

// SubjectSetType is the native message type for gestalt.provider.v1.SubjectSetType.
type SubjectSetType struct {
	ResourceType string
	Relation     string
}

// AuthorizationModelRef is the native message type for gestalt.provider.v1.AuthorizationModelRef.
type AuthorizationModelRef struct {
	Id        string
	Version   string
	CreatedAt time.Time
}

// GetActiveModelRefResponse is the native message type for gestalt.provider.v1.GetActiveModelRefResponse.
type GetActiveModelRefResponse struct {
	Model *AuthorizationModelRef
}

// SetActiveModelRequest is the native message type for gestalt.provider.v1.SetActiveModelRequest.
type SetActiveModelRequest struct {
	Model *AuthorizationModel
}

// SetActiveModelResponse is the native message type for gestalt.provider.v1.SetActiveModelResponse.
type SetActiveModelResponse struct {
	Model *AuthorizationModelRef
}

// AuthorizationModelResourceTypeFilter is the native message type for gestalt.provider.v1.AuthorizationModelResourceTypeFilter.
type AuthorizationModelResourceTypeFilter struct {
	Name        string
	SourceLayer SourceLayer
}

// ListActiveModelResourceTypesRequest is the native message type for gestalt.provider.v1.ListActiveModelResourceTypesRequest.
type ListActiveModelResourceTypesRequest struct {
	Filter    *AuthorizationModelResourceTypeFilter
	PageSize  int32
	PageToken string
}

// ListActiveModelResourceTypesResponse is the native message type for gestalt.provider.v1.ListActiveModelResourceTypesResponse.
type ListActiveModelResourceTypesResponse struct {
	ResourceTypes []*AuthorizationModelResourceType
	NextPageToken string
	ModelId       string
}

// AuthorizationProvider is implemented by providers that serve the generic
// authorization model, relationship, and access-check surface over gRPC.
type AuthorizationProvider interface {
	Provider
	CheckAccess(ctx context.Context, req *CheckAccessRequest) (*CheckAccessResponse, error)
	CheckAccessMany(ctx context.Context, req *CheckAccessManyRequest) (*CheckAccessManyResponse, error)
	ListRelationships(ctx context.Context, req *ListRelationshipsRequest) (*ListRelationshipsResponse, error)
	AddRelationship(ctx context.Context, req *AddRelationshipRequest) (*AddRelationshipResponse, error)
	DeleteRelationship(ctx context.Context, req *DeleteRelationshipRequest) (*DeleteRelationshipResponse, error)
	SetAuthorizationState(ctx context.Context, req *SetAuthorizationStateRequest) (*SetAuthorizationStateResponse, error)
	GetActiveModelRef(ctx context.Context) (*GetActiveModelRefResponse, error)
	SetActiveModel(ctx context.Context, req *SetActiveModelRequest) (*SetActiveModelResponse, error)
	ListActiveModelResourceTypes(ctx context.Context, req *ListActiveModelResourceTypesRequest) (*ListActiveModelResourceTypesResponse, error)
}
