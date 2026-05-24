package authorization

import (
	"context"
	"time"
)

// AuthorizationMetadata describes the host authorization provider.
type AuthorizationMetadata struct {
	Capabilities  []string
	ActiveModelId string
}

func (m *AuthorizationMetadata) GetCapabilities() []string {
	if m == nil {
		return nil
	}
	return m.Capabilities
}

func (m *AuthorizationMetadata) GetActiveModelId() string {
	if m == nil {
		return ""
	}
	return m.ActiveModelId
}

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

// AccessEvaluationRequest asks whether one subject can perform one action.
type AccessEvaluationRequest struct {
	Subject  *AuthorizationSubject
	Action   *AuthorizationAction
	Resource *AuthorizationResource
	Context  map[string]any
}

func (r *AccessEvaluationRequest) GetSubject() *AuthorizationSubject {
	if r == nil {
		return nil
	}
	return r.Subject
}

func (r *AccessEvaluationRequest) GetAction() *AuthorizationAction {
	if r == nil {
		return nil
	}
	return r.Action
}

func (r *AccessEvaluationRequest) GetResource() *AuthorizationResource {
	if r == nil {
		return nil
	}
	return r.Resource
}

func (r *AccessEvaluationRequest) GetContext() map[string]any {
	if r == nil {
		return nil
	}
	return r.Context
}

// AccessDecision is the result of evaluating one access request.
type AccessDecision struct {
	Allowed bool
	Context map[string]any
	ModelId string
}

func (d *AccessDecision) GetAllowed() bool {
	if d == nil {
		return false
	}
	return d.Allowed
}

func (d *AccessDecision) GetContext() map[string]any {
	if d == nil {
		return nil
	}
	return d.Context
}

func (d *AccessDecision) GetModelId() string {
	if d == nil {
		return ""
	}
	return d.ModelId
}

// AccessEvaluationsRequest batches access evaluation requests.
type AccessEvaluationsRequest struct {
	Requests []*AccessEvaluationRequest
}

func (r *AccessEvaluationsRequest) GetRequests() []*AccessEvaluationRequest {
	if r == nil {
		return nil
	}
	return r.Requests
}

// AccessEvaluationsResponse batches access evaluation results.
type AccessEvaluationsResponse struct {
	Decisions []*AccessDecision
}

func (r *AccessEvaluationsResponse) GetDecisions() []*AccessDecision {
	if r == nil {
		return nil
	}
	return r.Decisions
}

// ResourceSearchRequest searches resources visible to a subject.
type ResourceSearchRequest struct {
	Subject      *AuthorizationSubject
	Action       *AuthorizationAction
	ResourceType string
	Context      map[string]any
	PageSize     int32
	PageToken    string
}

func (r *ResourceSearchRequest) GetSubject() *AuthorizationSubject {
	if r == nil {
		return nil
	}
	return r.Subject
}

func (r *ResourceSearchRequest) GetAction() *AuthorizationAction {
	if r == nil {
		return nil
	}
	return r.Action
}

func (r *ResourceSearchRequest) GetResourceType() string {
	if r == nil {
		return ""
	}
	return r.ResourceType
}

func (r *ResourceSearchRequest) GetContext() map[string]any {
	if r == nil {
		return nil
	}
	return r.Context
}

func (r *ResourceSearchRequest) GetPageSize() int32 {
	if r == nil {
		return 0
	}
	return r.PageSize
}

func (r *ResourceSearchRequest) GetPageToken() string {
	if r == nil {
		return ""
	}
	return r.PageToken
}

// ResourceSearchResponse contains resources visible to a subject.
type ResourceSearchResponse struct {
	Resources     []*AuthorizationResource
	NextPageToken string
	ModelId       string
}

func (r *ResourceSearchResponse) GetResources() []*AuthorizationResource {
	if r == nil {
		return nil
	}
	return r.Resources
}

func (r *ResourceSearchResponse) GetNextPageToken() string {
	if r == nil {
		return ""
	}
	return r.NextPageToken
}

func (r *ResourceSearchResponse) GetModelId() string {
	if r == nil {
		return ""
	}
	return r.ModelId
}

// SubjectSearchRequest searches subjects related to a resource and action.
type SubjectSearchRequest struct {
	Resource    *AuthorizationResource
	Action      *AuthorizationAction
	SubjectType string
	Context     map[string]any
	PageSize    int32
	PageToken   string
}

func (r *SubjectSearchRequest) GetResource() *AuthorizationResource {
	if r == nil {
		return nil
	}
	return r.Resource
}

func (r *SubjectSearchRequest) GetAction() *AuthorizationAction {
	if r == nil {
		return nil
	}
	return r.Action
}

func (r *SubjectSearchRequest) GetSubjectType() string {
	if r == nil {
		return ""
	}
	return r.SubjectType
}

func (r *SubjectSearchRequest) GetContext() map[string]any {
	if r == nil {
		return nil
	}
	return r.Context
}

func (r *SubjectSearchRequest) GetPageSize() int32 {
	if r == nil {
		return 0
	}
	return r.PageSize
}

func (r *SubjectSearchRequest) GetPageToken() string {
	if r == nil {
		return ""
	}
	return r.PageToken
}

// SubjectSearchResponse contains subjects related to a resource and action.
type SubjectSearchResponse struct {
	Subjects      []*AuthorizationSubject
	NextPageToken string
	ModelId       string
}

func (r *SubjectSearchResponse) GetSubjects() []*AuthorizationSubject {
	if r == nil {
		return nil
	}
	return r.Subjects
}

func (r *SubjectSearchResponse) GetNextPageToken() string {
	if r == nil {
		return ""
	}
	return r.NextPageToken
}

func (r *SubjectSearchResponse) GetModelId() string {
	if r == nil {
		return ""
	}
	return r.ModelId
}

// EffectiveSubjectSearchRequest searches effective targets related to a
// resource and action through computed usersets and inherited relationships.
type EffectiveSubjectSearchRequest struct {
	Resource  *AuthorizationResource
	Action    *AuthorizationAction
	Context   map[string]any
	PageSize  int32
	PageToken string
}

func (r *EffectiveSubjectSearchRequest) GetResource() *AuthorizationResource {
	if r == nil {
		return nil
	}
	return r.Resource
}

func (r *EffectiveSubjectSearchRequest) GetAction() *AuthorizationAction {
	if r == nil {
		return nil
	}
	return r.Action
}

func (r *EffectiveSubjectSearchRequest) GetContext() map[string]any {
	if r == nil {
		return nil
	}
	return r.Context
}

func (r *EffectiveSubjectSearchRequest) GetPageSize() int32 {
	if r == nil {
		return 0
	}
	return r.PageSize
}

func (r *EffectiveSubjectSearchRequest) GetPageToken() string {
	if r == nil {
		return ""
	}
	return r.PageToken
}

// EffectiveSubjectSearchResponse contains effective subjects or subject sets
// related to a resource and action.
type EffectiveSubjectSearchResponse struct {
	Targets       []*AuthorizationRelationshipTarget
	NextPageToken string
	ModelId       string
	Truncated     bool
}

func (r *EffectiveSubjectSearchResponse) GetTargets() []*AuthorizationRelationshipTarget {
	if r == nil {
		return nil
	}
	return r.Targets
}

func (r *EffectiveSubjectSearchResponse) GetNextPageToken() string {
	if r == nil {
		return ""
	}
	return r.NextPageToken
}

func (r *EffectiveSubjectSearchResponse) GetModelId() string {
	if r == nil {
		return ""
	}
	return r.ModelId
}

func (r *EffectiveSubjectSearchResponse) GetTruncated() bool {
	if r == nil {
		return false
	}
	return r.Truncated
}

// ActionSearchRequest searches actions available between a subject and resource.
type ActionSearchRequest struct {
	Subject   *AuthorizationSubject
	Resource  *AuthorizationResource
	Context   map[string]any
	PageSize  int32
	PageToken string
}

func (r *ActionSearchRequest) GetSubject() *AuthorizationSubject {
	if r == nil {
		return nil
	}
	return r.Subject
}

func (r *ActionSearchRequest) GetResource() *AuthorizationResource {
	if r == nil {
		return nil
	}
	return r.Resource
}

func (r *ActionSearchRequest) GetContext() map[string]any {
	if r == nil {
		return nil
	}
	return r.Context
}

func (r *ActionSearchRequest) GetPageSize() int32 {
	if r == nil {
		return 0
	}
	return r.PageSize
}

func (r *ActionSearchRequest) GetPageToken() string {
	if r == nil {
		return ""
	}
	return r.PageToken
}

// ActionSearchResponse contains actions available between a subject and resource.
type ActionSearchResponse struct {
	Actions       []*AuthorizationAction
	NextPageToken string
	ModelId       string
}

func (r *ActionSearchResponse) GetActions() []*AuthorizationAction {
	if r == nil {
		return nil
	}
	return r.Actions
}

func (r *ActionSearchResponse) GetNextPageToken() string {
	if r == nil {
		return ""
	}
	return r.NextPageToken
}

func (r *ActionSearchResponse) GetModelId() string {
	if r == nil {
		return ""
	}
	return r.ModelId
}

// Relationship describes one authorization relationship tuple.
//
// A relationship grants a subject a relation on a resource, for example
// subject "user:123" has relation "member" on resource "team:servicing".
type Relationship struct {
	Subject    *AuthorizationSubject
	Relation   string
	Resource   *AuthorizationResource
	Properties map[string]any
	Target     *AuthorizationRelationshipTarget
}

func (r *Relationship) GetSubject() *AuthorizationSubject {
	if r == nil {
		return nil
	}
	return r.Subject
}

func (r *Relationship) GetRelation() string {
	if r == nil {
		return ""
	}
	return r.Relation
}

func (r *Relationship) GetResource() *AuthorizationResource {
	if r == nil {
		return nil
	}
	return r.Resource
}

func (r *Relationship) GetProperties() map[string]any {
	if r == nil {
		return nil
	}
	return r.Properties
}

func (r *Relationship) GetTarget() *AuthorizationRelationshipTarget {
	if r == nil {
		return nil
	}
	return r.Target
}

// RelationshipKey identifies one authorization relationship tuple.
type RelationshipKey struct {
	Subject  *AuthorizationSubject
	Relation string
	Resource *AuthorizationResource
	Target   *AuthorizationRelationshipTarget
}

func (r *RelationshipKey) GetSubject() *AuthorizationSubject {
	if r == nil {
		return nil
	}
	return r.Subject
}

func (r *RelationshipKey) GetRelation() string {
	if r == nil {
		return ""
	}
	return r.Relation
}

func (r *RelationshipKey) GetResource() *AuthorizationResource {
	if r == nil {
		return nil
	}
	return r.Resource
}

func (r *RelationshipKey) GetTarget() *AuthorizationRelationshipTarget {
	if r == nil {
		return nil
	}
	return r.Target
}

// ReadRelationshipsRequest selects authorization relationships to read.
type ReadRelationshipsRequest struct {
	Subject   *AuthorizationSubject
	Relation  string
	Resource  *AuthorizationResource
	PageSize  int32
	PageToken string
	ModelId   string
	Target    *AuthorizationRelationshipTarget
}

func (r *ReadRelationshipsRequest) GetSubject() *AuthorizationSubject {
	if r == nil {
		return nil
	}
	return r.Subject
}

func (r *ReadRelationshipsRequest) GetRelation() string {
	if r == nil {
		return ""
	}
	return r.Relation
}

func (r *ReadRelationshipsRequest) GetResource() *AuthorizationResource {
	if r == nil {
		return nil
	}
	return r.Resource
}

func (r *ReadRelationshipsRequest) GetPageSize() int32 {
	if r == nil {
		return 0
	}
	return r.PageSize
}

func (r *ReadRelationshipsRequest) GetPageToken() string {
	if r == nil {
		return ""
	}
	return r.PageToken
}

func (r *ReadRelationshipsRequest) GetModelId() string {
	if r == nil {
		return ""
	}
	return r.ModelId
}

func (r *ReadRelationshipsRequest) GetTarget() *AuthorizationRelationshipTarget {
	if r == nil {
		return nil
	}
	return r.Target
}

// ReadRelationshipsResponse contains authorization relationships.
type ReadRelationshipsResponse struct {
	Relationships []*Relationship
	NextPageToken string
	ModelId       string
}

func (r *ReadRelationshipsResponse) GetRelationships() []*Relationship {
	if r == nil {
		return nil
	}
	return r.Relationships
}

func (r *ReadRelationshipsResponse) GetNextPageToken() string {
	if r == nil {
		return ""
	}
	return r.NextPageToken
}

func (r *ReadRelationshipsResponse) GetModelId() string {
	if r == nil {
		return ""
	}
	return r.ModelId
}

// WriteRelationshipsRequest mutates authorization relationships.
//
// Writes are upserts and deletes remove exact RelationshipKey tuples.
type WriteRelationshipsRequest struct {
	Writes  []*Relationship
	Deletes []*RelationshipKey
	ModelId string
}

func (r *WriteRelationshipsRequest) GetWrites() []*Relationship {
	if r == nil {
		return nil
	}
	return r.Writes
}

func (r *WriteRelationshipsRequest) GetDeletes() []*RelationshipKey {
	if r == nil {
		return nil
	}
	return r.Deletes
}

func (r *WriteRelationshipsRequest) GetModelId() string {
	if r == nil {
		return ""
	}
	return r.ModelId
}

// AuthorizationModel describes an authorization model.
type AuthorizationModel struct {
	Version       int32
	ResourceTypes []*AuthorizationModelResourceType
}

func (m *AuthorizationModel) GetVersion() int32 {
	if m == nil {
		return 0
	}
	return m.Version
}

func (m *AuthorizationModel) GetResourceTypes() []*AuthorizationModelResourceType {
	if m == nil {
		return nil
	}
	return m.ResourceTypes
}

// AuthorizationModelResourceType describes one resource type in a model.
type AuthorizationModelResourceType struct {
	Name      string
	Relations []*AuthorizationModelRelation
	Actions   []*AuthorizationModelAction
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

// AuthorizationModelRelation describes one relation in a model.
type AuthorizationModelRelation struct {
	Name           string
	SubjectTypes   []string
	AllowedTargets []*AuthorizationModelAllowedTarget
	Rewrite        *AuthorizationModelRewrite
}

func (r *AuthorizationModelRelation) GetName() string {
	if r == nil {
		return ""
	}
	return r.Name
}

func (r *AuthorizationModelRelation) GetSubjectTypes() []string {
	if r == nil {
		return nil
	}
	return r.SubjectTypes
}

func (r *AuthorizationModelRelation) GetAllowedTargets() []*AuthorizationModelAllowedTarget {
	if r == nil {
		return nil
	}
	return r.AllowedTargets
}

func (r *AuthorizationModelRelation) GetRewrite() *AuthorizationModelRewrite {
	if r == nil {
		return nil
	}
	return r.Rewrite
}

// AuthorizationModelAction describes one action in a model.
type AuthorizationModelAction struct {
	Name      string
	Relations []string
	Rewrite   *AuthorizationModelRewrite
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

func (a *AuthorizationModelAction) GetRewrite() *AuthorizationModelRewrite {
	if a == nil {
		return nil
	}
	return a.Rewrite
}

// AuthorizationModelAllowedTarget describes one valid target kind for a relation.
type AuthorizationModelAllowedTarget struct {
	SubjectType  string
	ResourceType string
	SubjectSet   *AuthorizationModelSubjectSetTarget
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

func (t *AuthorizationModelAllowedTarget) GetSubjectSet() *AuthorizationModelSubjectSetTarget {
	if t == nil {
		return nil
	}
	return t.SubjectSet
}

// AuthorizationModelSubjectSetTarget describes a valid userset target.
type AuthorizationModelSubjectSetTarget struct {
	ResourceType string
	Relation     string
}

func (t *AuthorizationModelSubjectSetTarget) GetResourceType() string {
	if t == nil {
		return ""
	}
	return t.ResourceType
}

func (t *AuthorizationModelSubjectSetTarget) GetRelation() string {
	if t == nil {
		return ""
	}
	return t.Relation
}

// AuthorizationModelRewrite describes how to compute one relation or action.
type AuthorizationModelRewrite struct {
	This            *AuthorizationModelRewriteThis
	ComputedUserset *AuthorizationModelComputedUserset
	TupleToUserset  *AuthorizationModelTupleToUserset
	Union           *AuthorizationModelRewriteUnion
}

func (r *AuthorizationModelRewrite) GetThis() *AuthorizationModelRewriteThis {
	if r == nil {
		return nil
	}
	return r.This
}

func (r *AuthorizationModelRewrite) GetComputedUserset() *AuthorizationModelComputedUserset {
	if r == nil {
		return nil
	}
	return r.ComputedUserset
}

func (r *AuthorizationModelRewrite) GetTupleToUserset() *AuthorizationModelTupleToUserset {
	if r == nil {
		return nil
	}
	return r.TupleToUserset
}

func (r *AuthorizationModelRewrite) GetUnion() *AuthorizationModelRewriteUnion {
	if r == nil {
		return nil
	}
	return r.Union
}

// AuthorizationModelRewriteThis includes directly related targets in a rewrite.
type AuthorizationModelRewriteThis struct{}

// AuthorizationModelComputedUserset references another relation on the same resource.
type AuthorizationModelComputedUserset struct {
	Relation string
}

func (r *AuthorizationModelComputedUserset) GetRelation() string {
	if r == nil {
		return ""
	}
	return r.Relation
}

// AuthorizationModelTupleToUserset follows one relation and computes another
// relation on each related resource.
type AuthorizationModelTupleToUserset struct {
	TuplesetRelation string
	ComputedRelation string
}

func (r *AuthorizationModelTupleToUserset) GetTuplesetRelation() string {
	if r == nil {
		return ""
	}
	return r.TuplesetRelation
}

func (r *AuthorizationModelTupleToUserset) GetComputedRelation() string {
	if r == nil {
		return ""
	}
	return r.ComputedRelation
}

// AuthorizationModelRewriteUnion unions multiple rewrite branches.
type AuthorizationModelRewriteUnion struct {
	Children []*AuthorizationModelRewrite
}

func (r *AuthorizationModelRewriteUnion) GetChildren() []*AuthorizationModelRewrite {
	if r == nil {
		return nil
	}
	return r.Children
}

// AuthorizationModelRef identifies a stored authorization model.
type AuthorizationModelRef struct {
	Id        string
	Version   string
	CreatedAt time.Time
}

func (r *AuthorizationModelRef) GetId() string {
	if r == nil {
		return ""
	}
	return r.Id
}

func (r *AuthorizationModelRef) GetVersion() string {
	if r == nil {
		return ""
	}
	return r.Version
}

func (r *AuthorizationModelRef) GetCreatedAt() time.Time {
	if r == nil {
		return time.Time{}
	}
	return r.CreatedAt
}

// GetActiveModelResponse returns the active authorization model.
type GetActiveModelResponse struct {
	Model *AuthorizationModelRef
}

func (r *GetActiveModelResponse) GetModel() *AuthorizationModelRef {
	if r == nil {
		return nil
	}
	return r.Model
}

// ListModelsRequest selects authorization models to list.
type ListModelsRequest struct {
	PageSize  int32
	PageToken string
}

func (r *ListModelsRequest) GetPageSize() int32 {
	if r == nil {
		return 0
	}
	return r.PageSize
}

func (r *ListModelsRequest) GetPageToken() string {
	if r == nil {
		return ""
	}
	return r.PageToken
}

// ListModelsResponse contains authorization model refs.
type ListModelsResponse struct {
	Models        []*AuthorizationModelRef
	NextPageToken string
}

func (r *ListModelsResponse) GetModels() []*AuthorizationModelRef {
	if r == nil {
		return nil
	}
	return r.Models
}

func (r *ListModelsResponse) GetNextPageToken() string {
	if r == nil {
		return ""
	}
	return r.NextPageToken
}

// WriteModelRequest stores an authorization model.
type WriteModelRequest struct {
	Model *AuthorizationModel
}

func (r *WriteModelRequest) GetModel() *AuthorizationModel {
	if r == nil {
		return nil
	}
	return r.Model
}

// ExpandRequest asks a provider to explain one resource relation.
type ExpandRequest struct {
	Resource *AuthorizationResource
	Relation string
	Context  map[string]any
	MaxDepth int32
	ModelId  string
}

func (r *ExpandRequest) GetResource() *AuthorizationResource {
	if r == nil {
		return nil
	}
	return r.Resource
}

func (r *ExpandRequest) GetRelation() string {
	if r == nil {
		return ""
	}
	return r.Relation
}

func (r *ExpandRequest) GetContext() map[string]any {
	if r == nil {
		return nil
	}
	return r.Context
}

func (r *ExpandRequest) GetMaxDepth() int32 {
	if r == nil {
		return 0
	}
	return r.MaxDepth
}

func (r *ExpandRequest) GetModelId() string {
	if r == nil {
		return ""
	}
	return r.ModelId
}

// ExpandNode describes one node in an expanded authorization graph.
type ExpandNode struct {
	Target   *AuthorizationRelationshipTarget
	Relation string
	Children []*ExpandNode
}

func (n *ExpandNode) GetTarget() *AuthorizationRelationshipTarget {
	if n == nil {
		return nil
	}
	return n.Target
}

func (n *ExpandNode) GetRelation() string {
	if n == nil {
		return ""
	}
	return n.Relation
}

func (n *ExpandNode) GetChildren() []*ExpandNode {
	if n == nil {
		return nil
	}
	return n.Children
}

// ExpandResponse contains an expanded authorization graph.
type ExpandResponse struct {
	Root            *ExpandNode
	Truncated       bool
	CycleDetected   bool
	MaxDepthReached bool
	ModelId         string
}

func (r *ExpandResponse) GetRoot() *ExpandNode {
	if r == nil {
		return nil
	}
	return r.Root
}

func (r *ExpandResponse) GetTruncated() bool {
	if r == nil {
		return false
	}
	return r.Truncated
}

func (r *ExpandResponse) GetCycleDetected() bool {
	if r == nil {
		return false
	}
	return r.CycleDetected
}

func (r *ExpandResponse) GetMaxDepthReached() bool {
	if r == nil {
		return false
	}
	return r.MaxDepthReached
}

func (r *ExpandResponse) GetModelId() string {
	if r == nil {
		return ""
	}
	return r.ModelId
}

const (
	// AuthorizationSubjectTypeSubject identifies canonical Gestalt subjects in
	// managed authorization relationships.
	AuthorizationSubjectTypeSubject = "subject"
)

// NewAuthorizationSubject creates a subject reference for authorization requests.
func NewAuthorizationSubject(subjectType, id string) *AuthorizationSubject {
	return &AuthorizationSubject{Type: subjectType, Id: id}
}

// NewAuthorizationResource creates a resource reference for authorization requests.
func NewAuthorizationResource(resourceType, id string) *AuthorizationResource {
	return &AuthorizationResource{Type: resourceType, Id: id}
}

// NewAuthorizationSubjectSet creates a subject-set reference.
func NewAuthorizationSubjectSet(resource *AuthorizationResource, relation string) *AuthorizationSubjectSet {
	return &AuthorizationSubjectSet{Resource: resource, Relation: relation}
}

// NewAuthorizationSubjectTarget creates a relationship target from a subject.
func NewAuthorizationSubjectTarget(subject *AuthorizationSubject) *AuthorizationRelationshipTarget {
	return &AuthorizationRelationshipTarget{Subject: subject}
}

// NewAuthorizationResourceTarget creates a relationship target from a resource.
func NewAuthorizationResourceTarget(resource *AuthorizationResource) *AuthorizationRelationshipTarget {
	return &AuthorizationRelationshipTarget{Resource: resource}
}

// NewAuthorizationSubjectSetTarget creates a relationship target from a subject set.
func NewAuthorizationSubjectSetTarget(resource *AuthorizationResource, relation string) *AuthorizationRelationshipTarget {
	return &AuthorizationRelationshipTarget{
		SubjectSet: NewAuthorizationSubjectSet(resource, relation),
	}
}

// NewAuthorizationAction creates an action reference for authorization requests.
func NewAuthorizationAction(name string) *AuthorizationAction {
	return &AuthorizationAction{Name: name}
}

// NewAuthorizationModelRef creates an authorization model reference.
func NewAuthorizationModelRef(id, version string, createdAt time.Time) *AuthorizationModelRef {
	return &AuthorizationModelRef{
		Id:        id,
		Version:   version,
		CreatedAt: createdAt,
	}
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

// NewRelationshipWithTarget creates a generalized authorization tuple.
func NewRelationshipWithTarget(target *AuthorizationRelationshipTarget, relation string, resource *AuthorizationResource) *Relationship {
	return &Relationship{
		Target:   target,
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

// NewRelationshipKeyWithTarget creates a generalized authorization tuple key.
func NewRelationshipKeyWithTarget(target *AuthorizationRelationshipTarget, relation string, resource *AuthorizationResource) *RelationshipKey {
	return &RelationshipKey{
		Target:   target,
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

// NewAuthorizationModelSubjectTypeTarget allows a relation target subject type.
func NewAuthorizationModelSubjectTypeTarget(subjectType string) *AuthorizationModelAllowedTarget {
	return &AuthorizationModelAllowedTarget{SubjectType: subjectType}
}

// NewAuthorizationModelResourceTypeTarget allows a relation target resource type.
func NewAuthorizationModelResourceTypeTarget(resourceType string) *AuthorizationModelAllowedTarget {
	return &AuthorizationModelAllowedTarget{ResourceType: resourceType}
}

// NewAuthorizationModelSubjectSetAllowedTarget allows a relation target subject set.
func NewAuthorizationModelSubjectSetAllowedTarget(resourceType, relation string) *AuthorizationModelAllowedTarget {
	return &AuthorizationModelAllowedTarget{
		SubjectSet: &AuthorizationModelSubjectSetTarget{
			ResourceType: resourceType,
			Relation:     relation,
		},
	}
}

// NewAuthorizationModelThisRewrite includes directly related targets.
func NewAuthorizationModelThisRewrite() *AuthorizationModelRewrite {
	return &AuthorizationModelRewrite{This: &AuthorizationModelRewriteThis{}}
}

// NewAuthorizationModelComputedUsersetRewrite computes another relation on the same resource.
func NewAuthorizationModelComputedUsersetRewrite(relation string) *AuthorizationModelRewrite {
	return &AuthorizationModelRewrite{
		ComputedUserset: &AuthorizationModelComputedUserset{Relation: relation},
	}
}

// NewAuthorizationModelTupleToUsersetRewrite follows tuplesetRelation and then
// computes computedRelation on each related resource.
func NewAuthorizationModelTupleToUsersetRewrite(tuplesetRelation, computedRelation string) *AuthorizationModelRewrite {
	return &AuthorizationModelRewrite{
		TupleToUserset: &AuthorizationModelTupleToUserset{
			TuplesetRelation: tuplesetRelation,
			ComputedRelation: computedRelation,
		},
	}
}

// NewAuthorizationModelUnionRewrite unions multiple rewrite branches.
func NewAuthorizationModelUnionRewrite(children ...*AuthorizationModelRewrite) *AuthorizationModelRewrite {
	return &AuthorizationModelRewrite{
		Union: &AuthorizationModelRewriteUnion{Children: children},
	}
}

// Client is the app-facing authorization capability exposed by gestaltd and implemented by authorization providers.
type Client interface {
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

// EffectiveSearchClient is implemented by clients that can search through computed usersets and inherited relationships.
type EffectiveSearchClient interface {
	EffectiveSearchResources(ctx context.Context, req *ResourceSearchRequest) (*ResourceSearchResponse, error)
	EffectiveSearchSubjects(ctx context.Context, req *EffectiveSubjectSearchRequest) (*EffectiveSubjectSearchResponse, error)
}

// ExpansionClient is implemented by clients that can explain the relationship targets contributing to one resource relation.
type ExpansionClient interface {
	Expand(ctx context.Context, req *ExpandRequest) (*ExpandResponse, error)
}
