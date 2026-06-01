package authorization

import (
	"context"
	"time"
)

// AuthorizationSubjectTypeSubject identifies canonical Gestalt subjects in
// managed authorization relationships.
const AuthorizationSubjectTypeSubject = "subject"

type SourceLayer int32

const (
	SourceLayerUnspecified  SourceLayer = 0
	SourceLayerStaticConfig SourceLayer = 1
	SourceLayerRuntime      SourceLayer = 2
)

type RelationshipTargetType int32

const (
	RelationshipTargetTypeUnspecified RelationshipTargetType = 0
	RelationshipTargetTypeSubject     RelationshipTargetType = 1
	RelationshipTargetTypeResource    RelationshipTargetType = 2
	RelationshipTargetTypeSubjectSet  RelationshipTargetType = 3
)

// AuthorizationSubject identifies a subject in the authorization graph.
type AuthorizationSubject struct {
	Type       string
	Id         string
	Properties map[string]any
}

func (s *AuthorizationSubject) GetType() string {
	if s == nil {
		return ""
	}
	return s.Type
}

func (s *AuthorizationSubject) GetId() string {
	if s == nil {
		return ""
	}
	return s.Id
}

func (s *AuthorizationSubject) GetProperties() map[string]any {
	if s == nil {
		return nil
	}
	return s.Properties
}

// AuthorizationResource identifies a protected resource in the authorization graph.
type AuthorizationResource struct {
	Type       string
	Id         string
	Properties map[string]any
}

func (r *AuthorizationResource) GetType() string {
	if r == nil {
		return ""
	}
	return r.Type
}

func (r *AuthorizationResource) GetId() string {
	if r == nil {
		return ""
	}
	return r.Id
}

func (r *AuthorizationResource) GetProperties() map[string]any {
	if r == nil {
		return nil
	}
	return r.Properties
}

// AuthorizationSubjectSet identifies a Zanzibar-style userset target.
type AuthorizationSubjectSet struct {
	Resource *AuthorizationResource
	Relation string
}

func (s *AuthorizationSubjectSet) GetResource() *AuthorizationResource {
	if s == nil {
		return nil
	}
	return s.Resource
}

func (s *AuthorizationSubjectSet) GetRelation() string {
	if s == nil {
		return ""
	}
	return s.Relation
}

// AuthorizationRelationshipTarget identifies the left side of an authorization
// relationship. Exactly one of Subject, Resource, or SubjectSet should be set.
type AuthorizationRelationshipTarget struct {
	Subject    *AuthorizationSubject
	Resource   *AuthorizationResource
	SubjectSet *AuthorizationSubjectSet
}

func (t *AuthorizationRelationshipTarget) GetSubject() *AuthorizationSubject {
	if t == nil {
		return nil
	}
	return t.Subject
}

func (t *AuthorizationRelationshipTarget) GetResource() *AuthorizationResource {
	if t == nil {
		return nil
	}
	return t.Resource
}

func (t *AuthorizationRelationshipTarget) GetSubjectSet() *AuthorizationSubjectSet {
	if t == nil {
		return nil
	}
	return t.SubjectSet
}

// AuthorizationAction identifies an action being evaluated against a resource.
type AuthorizationAction struct {
	Name       string
	Properties map[string]any
}

func (a *AuthorizationAction) GetName() string {
	if a == nil {
		return ""
	}
	return a.Name
}

func (a *AuthorizationAction) GetProperties() map[string]any {
	if a == nil {
		return nil
	}
	return a.Properties
}

type CheckAccessRequest struct {
	Subject  *AuthorizationSubject
	Action   *AuthorizationAction
	Resource *AuthorizationResource
}

func (r *CheckAccessRequest) GetSubject() *AuthorizationSubject {
	if r == nil {
		return nil
	}
	return r.Subject
}

func (r *CheckAccessRequest) GetAction() *AuthorizationAction {
	if r == nil {
		return nil
	}
	return r.Action
}

func (r *CheckAccessRequest) GetResource() *AuthorizationResource {
	if r == nil {
		return nil
	}
	return r.Resource
}

type CheckAccessResponse struct {
	Allowed bool
	ModelId string
}

func (r *CheckAccessResponse) GetAllowed() bool {
	if r == nil {
		return false
	}
	return r.Allowed
}

func (r *CheckAccessResponse) GetModelId() string {
	if r == nil {
		return ""
	}
	return r.ModelId
}

type CheckAccessManyRequest struct {
	Requests []*CheckAccessRequest
}

func (r *CheckAccessManyRequest) GetRequests() []*CheckAccessRequest {
	if r == nil {
		return nil
	}
	return r.Requests
}

type CheckAccessManyResponse struct {
	Decisions []*CheckAccessResponse
}

func (r *CheckAccessManyResponse) GetDecisions() []*CheckAccessResponse {
	if r == nil {
		return nil
	}
	return r.Decisions
}

type Relationship struct {
	Tuple       *RelationshipTuple
	Properties  map[string]any
	SourceLayer SourceLayer
}

func (r *Relationship) GetTuple() *RelationshipTuple {
	if r == nil {
		return nil
	}
	return r.Tuple
}

func (r *Relationship) GetProperties() map[string]any {
	if r == nil {
		return nil
	}
	return r.Properties
}

func (r *Relationship) GetSourceLayer() SourceLayer {
	if r == nil {
		return SourceLayerUnspecified
	}
	return r.SourceLayer
}

type RelationshipTuple struct {
	Target   *AuthorizationRelationshipTarget
	Relation string
	Resource *AuthorizationResource
}

func (t *RelationshipTuple) GetTarget() *AuthorizationRelationshipTarget {
	if t == nil {
		return nil
	}
	return t.Target
}

func (t *RelationshipTuple) GetRelation() string {
	if t == nil {
		return ""
	}
	return t.Relation
}

func (t *RelationshipTuple) GetResource() *AuthorizationResource {
	if t == nil {
		return nil
	}
	return t.Resource
}

type RelationshipFilter struct {
	Target           *AuthorizationRelationshipTarget
	Relation         string
	Resource         *AuthorizationResource
	TargetType       RelationshipTargetType
	TargetEntityType string
	ResourceType     string
	SourceLayer      SourceLayer
}

func (f *RelationshipFilter) GetTarget() *AuthorizationRelationshipTarget {
	if f == nil {
		return nil
	}
	return f.Target
}

func (f *RelationshipFilter) GetRelation() string {
	if f == nil {
		return ""
	}
	return f.Relation
}

func (f *RelationshipFilter) GetResource() *AuthorizationResource {
	if f == nil {
		return nil
	}
	return f.Resource
}

func (f *RelationshipFilter) GetTargetType() RelationshipTargetType {
	if f == nil {
		return RelationshipTargetTypeUnspecified
	}
	return f.TargetType
}

func (f *RelationshipFilter) GetTargetEntityType() string {
	if f == nil {
		return ""
	}
	return f.TargetEntityType
}

func (f *RelationshipFilter) GetResourceType() string {
	if f == nil {
		return ""
	}
	return f.ResourceType
}

func (f *RelationshipFilter) GetSourceLayer() SourceLayer {
	if f == nil {
		return SourceLayerUnspecified
	}
	return f.SourceLayer
}

type ListRelationshipsRequest struct {
	Filter    *RelationshipFilter
	PageSize  int32
	PageToken string
}

func (r *ListRelationshipsRequest) GetFilter() *RelationshipFilter {
	if r == nil {
		return nil
	}
	return r.Filter
}

func (r *ListRelationshipsRequest) GetPageSize() int32 {
	if r == nil {
		return 0
	}
	return r.PageSize
}

func (r *ListRelationshipsRequest) GetPageToken() string {
	if r == nil {
		return ""
	}
	return r.PageToken
}

type ListRelationshipsResponse struct {
	Relationships []*Relationship
	NextPageToken string
}

func (r *ListRelationshipsResponse) GetRelationships() []*Relationship {
	if r == nil {
		return nil
	}
	return r.Relationships
}

func (r *ListRelationshipsResponse) GetNextPageToken() string {
	if r == nil {
		return ""
	}
	return r.NextPageToken
}

type AddRelationshipRequest struct {
	Relationship *Relationship
}

func (r *AddRelationshipRequest) GetRelationship() *Relationship {
	if r == nil {
		return nil
	}
	return r.Relationship
}

type AddRelationshipResponse struct {
	Relationship *Relationship
}

func (r *AddRelationshipResponse) GetRelationship() *Relationship {
	if r == nil {
		return nil
	}
	return r.Relationship
}

type DeleteRelationshipRequest struct {
	RelationshipTuple *RelationshipTuple
}

func (r *DeleteRelationshipRequest) GetRelationshipTuple() *RelationshipTuple {
	if r == nil {
		return nil
	}
	return r.RelationshipTuple
}

type DeleteRelationshipResponse struct{}

type SetRelationshipsRequest struct {
	Relationships []*Relationship
}

func (r *SetRelationshipsRequest) GetRelationships() []*Relationship {
	if r == nil {
		return nil
	}
	return r.Relationships
}

type SetRelationshipsResponse struct {
	Relationships []*Relationship
}

func (r *SetRelationshipsResponse) GetRelationships() []*Relationship {
	if r == nil {
		return nil
	}
	return r.Relationships
}

type AuthorizationModel struct {
	Id            string
	Version       string
	ResourceTypes []*AuthorizationModelResourceType
}

func (m *AuthorizationModel) GetId() string {
	if m == nil {
		return ""
	}
	return m.Id
}

func (m *AuthorizationModel) GetVersion() string {
	if m == nil {
		return ""
	}
	return m.Version
}

func (m *AuthorizationModel) GetResourceTypes() []*AuthorizationModelResourceType {
	if m == nil {
		return nil
	}
	return m.ResourceTypes
}

type AuthorizationModelRef struct {
	Id        string
	Version   string
	CreatedAt time.Time
}

func (m *AuthorizationModelRef) GetId() string {
	if m == nil {
		return ""
	}
	return m.Id
}

func (m *AuthorizationModelRef) GetVersion() string {
	if m == nil {
		return ""
	}
	return m.Version
}

func (m *AuthorizationModelRef) GetCreatedAt() time.Time {
	if m == nil {
		return time.Time{}
	}
	return m.CreatedAt
}

type AuthorizationModelResourceType struct {
	Name        string
	Relations   []*AuthorizationModelRelation
	Actions     []*AuthorizationModelAction
	SourceLayer SourceLayer
}

func (r *AuthorizationModelResourceType) GetName() string {
	if r == nil {
		return ""
	}
	return r.Name
}

func (r *AuthorizationModelResourceType) GetRelations() []*AuthorizationModelRelation {
	if r == nil {
		return nil
	}
	return r.Relations
}

func (r *AuthorizationModelResourceType) GetActions() []*AuthorizationModelAction {
	if r == nil {
		return nil
	}
	return r.Actions
}

func (r *AuthorizationModelResourceType) GetSourceLayer() SourceLayer {
	if r == nil {
		return SourceLayerUnspecified
	}
	return r.SourceLayer
}

type AuthorizationModelRelation struct {
	Name           string
	AllowedTargets []*AuthorizationModelAllowedTarget
}

func (r *AuthorizationModelRelation) GetName() string {
	if r == nil {
		return ""
	}
	return r.Name
}

func (r *AuthorizationModelRelation) GetAllowedTargets() []*AuthorizationModelAllowedTarget {
	if r == nil {
		return nil
	}
	return r.AllowedTargets
}

type AuthorizationModelAction struct {
	Name      string
	Relations []string
}

func (a *AuthorizationModelAction) GetName() string {
	if a == nil {
		return ""
	}
	return a.Name
}

func (a *AuthorizationModelAction) GetRelations() []string {
	if a == nil {
		return nil
	}
	return a.Relations
}

type AuthorizationModelAllowedTarget struct {
	SubjectType    string
	ResourceType   string
	SubjectSetType *SubjectSetType
}

func (t *AuthorizationModelAllowedTarget) GetSubjectType() string {
	if t == nil {
		return ""
	}
	return t.SubjectType
}

func (t *AuthorizationModelAllowedTarget) GetResourceType() string {
	if t == nil {
		return ""
	}
	return t.ResourceType
}

func (t *AuthorizationModelAllowedTarget) GetSubjectSetType() *SubjectSetType {
	if t == nil {
		return nil
	}
	return t.SubjectSetType
}

type SubjectSetType struct {
	ResourceType string
	Relation     string
}

func (t *SubjectSetType) GetResourceType() string {
	if t == nil {
		return ""
	}
	return t.ResourceType
}

func (t *SubjectSetType) GetRelation() string {
	if t == nil {
		return ""
	}
	return t.Relation
}

type GetActiveModelRefResponse struct {
	Model *AuthorizationModelRef
}

func (r *GetActiveModelRefResponse) GetModel() *AuthorizationModelRef {
	if r == nil {
		return nil
	}
	return r.Model
}

type SetActiveModelRequest struct {
	Model *AuthorizationModel
}

func (r *SetActiveModelRequest) GetModel() *AuthorizationModel {
	if r == nil {
		return nil
	}
	return r.Model
}

type SetActiveModelResponse struct {
	Model *AuthorizationModelRef
}

func (r *SetActiveModelResponse) GetModel() *AuthorizationModelRef {
	if r == nil {
		return nil
	}
	return r.Model
}

type AuthorizationModelResourceTypeFilter struct {
	Name        string
	SourceLayer SourceLayer
}

func (f *AuthorizationModelResourceTypeFilter) GetName() string {
	if f == nil {
		return ""
	}
	return f.Name
}

func (f *AuthorizationModelResourceTypeFilter) GetSourceLayer() SourceLayer {
	if f == nil {
		return SourceLayerUnspecified
	}
	return f.SourceLayer
}

type ListActiveModelResourceTypesRequest struct {
	ModelId string
	Filter  *AuthorizationModelResourceTypeFilter
}

func (r *ListActiveModelResourceTypesRequest) GetModelId() string {
	if r == nil {
		return ""
	}
	return r.ModelId
}

func (r *ListActiveModelResourceTypesRequest) GetFilter() *AuthorizationModelResourceTypeFilter {
	if r == nil {
		return nil
	}
	return r.Filter
}

type ListActiveModelResourceTypesResponse struct {
	ResourceTypes []*AuthorizationModelResourceType
}

func (r *ListActiveModelResourceTypesResponse) GetResourceTypes() []*AuthorizationModelResourceType {
	if r == nil {
		return nil
	}
	return r.ResourceTypes
}

// Authorization is the client contract for the host-configured authorization provider.
type Authorization interface {
	CheckAccess(ctx context.Context, req *CheckAccessRequest) (*CheckAccessResponse, error)
	CheckAccessMany(ctx context.Context, req *CheckAccessManyRequest) (*CheckAccessManyResponse, error)
	ListRelationships(ctx context.Context, req *ListRelationshipsRequest) (*ListRelationshipsResponse, error)
	AddRelationship(ctx context.Context, req *AddRelationshipRequest) (*AddRelationshipResponse, error)
	DeleteRelationship(ctx context.Context, req *DeleteRelationshipRequest) (*DeleteRelationshipResponse, error)
	SetRelationships(ctx context.Context, req *SetRelationshipsRequest) (*SetRelationshipsResponse, error)
	GetActiveModelRef(ctx context.Context) (*GetActiveModelRefResponse, error)
	SetActiveModel(ctx context.Context, req *SetActiveModelRequest) (*SetActiveModelResponse, error)
	ListActiveModelResourceTypes(ctx context.Context, req *ListActiveModelResourceTypesRequest) (*ListActiveModelResourceTypesResponse, error)
	Close() error
}

// Provider is the base authorization contract implemented by authorization providers.
type Provider interface {
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

func NewAuthorizationSubject(subjectType, id string) *AuthorizationSubject {
	return &AuthorizationSubject{Type: subjectType, Id: id}
}

func NewAuthorizationResource(resourceType, id string) *AuthorizationResource {
	return &AuthorizationResource{Type: resourceType, Id: id}
}

func NewAuthorizationSubjectSet(resource *AuthorizationResource, relation string) *AuthorizationSubjectSet {
	return &AuthorizationSubjectSet{Resource: resource, Relation: relation}
}

func NewAuthorizationSubjectTarget(subject *AuthorizationSubject) *AuthorizationRelationshipTarget {
	return &AuthorizationRelationshipTarget{Subject: subject}
}

func NewAuthorizationResourceTarget(resource *AuthorizationResource) *AuthorizationRelationshipTarget {
	return &AuthorizationRelationshipTarget{Resource: resource}
}

func NewAuthorizationSubjectSetTarget(resource *AuthorizationResource, relation string) *AuthorizationRelationshipTarget {
	return &AuthorizationRelationshipTarget{SubjectSet: NewAuthorizationSubjectSet(resource, relation)}
}

func NewAuthorizationAction(name string) *AuthorizationAction {
	return &AuthorizationAction{Name: name}
}

func NewAuthorizationModelRef(id, version string, createdAt time.Time) *AuthorizationModelRef {
	return &AuthorizationModelRef{Id: id, Version: version, CreatedAt: createdAt}
}

func NewCheckAccessRequest(subject *AuthorizationSubject, action *AuthorizationAction, resource *AuthorizationResource) *CheckAccessRequest {
	return &CheckAccessRequest{Subject: subject, Action: action, Resource: resource}
}

func NewRelationship(subject *AuthorizationSubject, relation string, resource *AuthorizationResource) *Relationship {
	return NewRelationshipWithTarget(NewAuthorizationSubjectTarget(subject), relation, resource)
}

func NewRelationshipWithTarget(target *AuthorizationRelationshipTarget, relation string, resource *AuthorizationResource) *Relationship {
	return &Relationship{Tuple: &RelationshipTuple{Target: target, Relation: relation, Resource: resource}}
}

func NewAuthorizationModelSubjectTypeTarget(subjectType string) *AuthorizationModelAllowedTarget {
	return &AuthorizationModelAllowedTarget{SubjectType: subjectType}
}

func NewAuthorizationModelResourceTypeTarget(resourceType string) *AuthorizationModelAllowedTarget {
	return &AuthorizationModelAllowedTarget{ResourceType: resourceType}
}

func NewAuthorizationModelSubjectSetAllowedTarget(resourceType, relation string) *AuthorizationModelAllowedTarget {
	return &AuthorizationModelAllowedTarget{SubjectSetType: &SubjectSetType{ResourceType: resourceType, Relation: relation}}
}
