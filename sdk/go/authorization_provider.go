package gestalt

import (
	"context"
	"time"
)

type AuthorizationSubject struct {
	Type       string
	Id         string
	Properties map[string]any
}

type AuthorizationAction struct {
	Name       string
	Properties map[string]any
}

type AuthorizationResource struct {
	Type       string
	Id         string
	Properties map[string]any
}

type CheckAccessRequest struct {
	Subject  *AuthorizationSubject
	Action   *AuthorizationAction
	Resource *AuthorizationResource
}

type CheckAccessResponse struct {
	Allowed bool
	ModelId string
}

type CheckAccessManyRequest struct {
	Requests []*CheckAccessRequest
}

type CheckAccessManyResponse struct {
	Decisions []*CheckAccessResponse
}

type RelationshipTargetType int32

const (
	RelationshipTargetTypeUnspecified RelationshipTargetType = 0
	RelationshipTargetTypeSubject     RelationshipTargetType = 1
	RelationshipTargetTypeResource    RelationshipTargetType = 2
	RelationshipTargetTypeSubjectSet  RelationshipTargetType = 3
)

type SourceLayer int32

const (
	SourceLayerUnspecified  SourceLayer = 0
	SourceLayerStaticConfig SourceLayer = 1
	SourceLayerRuntime      SourceLayer = 2
)

type RelationshipFilter struct {
	Target           *RelationshipTarget
	Relation         string
	Resource         *AuthorizationResource
	TargetType       RelationshipTargetType
	TargetEntityType string
	ResourceType     string
	SourceLayer      SourceLayer
}

type ListRelationshipsRequest struct {
	Filter    *RelationshipFilter
	PageSize  int32
	PageToken string
}

type ListRelationshipsResponse struct {
	Relationships []*Relationship
	NextPageToken string
}

type AddRelationshipRequest struct {
	Relationship *Relationship
}

type AddRelationshipResponse struct {
	Relationship *Relationship
}

type DeleteRelationshipRequest struct {
	RelationshipTuple *RelationshipTuple
}

type DeleteRelationshipResponse struct{}

type SetAuthorizationStateRequest struct {
	Model         *AuthorizationModel
	Relationships []*Relationship
}

type SetAuthorizationStateResponse struct {
	ActiveModel *AuthorizationModelRef
}

type Relationship struct {
	Tuple       *RelationshipTuple
	Properties  map[string]any
	SourceLayer SourceLayer
}

type RelationshipTuple struct {
	Target   *RelationshipTarget
	Relation string
	Resource *AuthorizationResource
}

type RelationshipTarget struct {
	Subject    *AuthorizationSubject
	Resource   *AuthorizationResource
	SubjectSet *SubjectSet
}

type SubjectSet struct {
	Resource *AuthorizationResource
	Relation string
}

type AuthorizationModel struct {
	Id            string
	Version       string
	ResourceTypes []*AuthorizationModelResourceType
}

type DefaultAccessPolicy int32

const (
	DefaultAccessPolicyDeny  DefaultAccessPolicy = 0
	DefaultAccessPolicyAllow DefaultAccessPolicy = 1
)

type AuthorizationModelResourceType struct {
	Name                string
	Relations           []*ModelRelation
	Actions             []*ModelAction
	SourceLayer         SourceLayer
	DefaultAccessPolicy DefaultAccessPolicy
}

type ModelRelation struct {
	Name           string
	AllowedTargets []*ModelAllowedTarget
}

type ModelAction struct {
	Name      string
	Relations []string
}

type ModelAllowedTarget struct {
	SubjectType    string
	ResourceType   string
	SubjectSetType *SubjectSetType
}

type SubjectSetType struct {
	ResourceType string
	Relation     string
}

type AuthorizationModelRef struct {
	Id        string
	Version   string
	CreatedAt time.Time
}

type GetActiveModelRefResponse struct {
	Model *AuthorizationModelRef
}

type SetActiveModelRequest struct {
	Model *AuthorizationModel
}

type SetActiveModelResponse struct {
	Model *AuthorizationModelRef
}

type AuthorizationModelResourceTypeFilter struct {
	Name        string
	SourceLayer SourceLayer
}

type ListActiveModelResourceTypesRequest struct {
	Filter    *AuthorizationModelResourceTypeFilter
	PageSize  int32
	PageToken string
}

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
