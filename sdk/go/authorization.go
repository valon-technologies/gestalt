package gestalt

import (
	"time"

	sdkauthorization "github.com/valon-technologies/gestalt/sdk/go/authorization"
)

type (
	AuthorizationMetadata              = sdkauthorization.AuthorizationMetadata
	AuthorizationSubject               = sdkauthorization.AuthorizationSubject
	AuthorizationResource              = sdkauthorization.AuthorizationResource
	AuthorizationSubjectSet            = sdkauthorization.AuthorizationSubjectSet
	AuthorizationRelationshipTarget    = sdkauthorization.AuthorizationRelationshipTarget
	AuthorizationAction                = sdkauthorization.AuthorizationAction
	AccessEvaluationRequest            = sdkauthorization.AccessEvaluationRequest
	AccessDecision                     = sdkauthorization.AccessDecision
	AccessEvaluationsRequest           = sdkauthorization.AccessEvaluationsRequest
	AccessEvaluationsResponse          = sdkauthorization.AccessEvaluationsResponse
	ResourceSearchRequest              = sdkauthorization.ResourceSearchRequest
	ResourceSearchResponse             = sdkauthorization.ResourceSearchResponse
	SubjectSearchRequest               = sdkauthorization.SubjectSearchRequest
	SubjectSearchResponse              = sdkauthorization.SubjectSearchResponse
	EffectiveSubjectSearchRequest      = sdkauthorization.EffectiveSubjectSearchRequest
	EffectiveSubjectSearchResponse     = sdkauthorization.EffectiveSubjectSearchResponse
	ActionSearchRequest                = sdkauthorization.ActionSearchRequest
	ActionSearchResponse               = sdkauthorization.ActionSearchResponse
	Relationship                       = sdkauthorization.Relationship
	RelationshipKey                    = sdkauthorization.RelationshipKey
	ReadRelationshipsRequest           = sdkauthorization.ReadRelationshipsRequest
	ReadRelationshipsResponse          = sdkauthorization.ReadRelationshipsResponse
	WriteRelationshipsRequest          = sdkauthorization.WriteRelationshipsRequest
	AuthorizationModel                 = sdkauthorization.AuthorizationModel
	AuthorizationModelResourceType     = sdkauthorization.AuthorizationModelResourceType
	AuthorizationModelRelation         = sdkauthorization.AuthorizationModelRelation
	AuthorizationModelAction           = sdkauthorization.AuthorizationModelAction
	AuthorizationModelAllowedTarget    = sdkauthorization.AuthorizationModelAllowedTarget
	AuthorizationModelSubjectSetTarget = sdkauthorization.AuthorizationModelSubjectSetTarget
	AuthorizationModelRewrite          = sdkauthorization.AuthorizationModelRewrite
	AuthorizationModelRewriteThis      = sdkauthorization.AuthorizationModelRewriteThis
	AuthorizationModelComputedUserset  = sdkauthorization.AuthorizationModelComputedUserset
	AuthorizationModelTupleToUserset   = sdkauthorization.AuthorizationModelTupleToUserset
	AuthorizationModelRewriteUnion     = sdkauthorization.AuthorizationModelRewriteUnion
	AuthorizationModelRef              = sdkauthorization.AuthorizationModelRef
	GetActiveModelResponse             = sdkauthorization.GetActiveModelResponse
	ListModelsRequest                  = sdkauthorization.ListModelsRequest
	ListModelsResponse                 = sdkauthorization.ListModelsResponse
	WriteModelRequest                  = sdkauthorization.WriteModelRequest
	ExpandRequest                      = sdkauthorization.ExpandRequest
	ExpandNode                         = sdkauthorization.ExpandNode
	ExpandResponse                     = sdkauthorization.ExpandResponse
)

const (
	// AuthorizationSubjectTypeSubject identifies canonical Gestalt subjects in
	// managed authorization relationships.
	AuthorizationSubjectTypeSubject = sdkauthorization.AuthorizationSubjectTypeSubject
)

// NewAuthorizationSubject creates a subject reference for authorization requests.
func NewAuthorizationSubject(subjectType, id string) *AuthorizationSubject {
	return sdkauthorization.NewAuthorizationSubject(subjectType, id)
}

// NewAuthorizationResource creates a resource reference for authorization requests.
func NewAuthorizationResource(resourceType, id string) *AuthorizationResource {
	return sdkauthorization.NewAuthorizationResource(resourceType, id)
}

// NewAuthorizationSubjectSet creates a subject-set reference.
func NewAuthorizationSubjectSet(resource *AuthorizationResource, relation string) *AuthorizationSubjectSet {
	return sdkauthorization.NewAuthorizationSubjectSet(resource, relation)
}

// NewAuthorizationSubjectTarget creates a relationship target from a subject.
func NewAuthorizationSubjectTarget(subject *AuthorizationSubject) *AuthorizationRelationshipTarget {
	return sdkauthorization.NewAuthorizationSubjectTarget(subject)
}

// NewAuthorizationResourceTarget creates a relationship target from a resource.
func NewAuthorizationResourceTarget(resource *AuthorizationResource) *AuthorizationRelationshipTarget {
	return sdkauthorization.NewAuthorizationResourceTarget(resource)
}

// NewAuthorizationSubjectSetTarget creates a relationship target from a subject set.
func NewAuthorizationSubjectSetTarget(resource *AuthorizationResource, relation string) *AuthorizationRelationshipTarget {
	return sdkauthorization.NewAuthorizationSubjectSetTarget(resource, relation)
}

// NewAuthorizationAction creates an action reference for authorization requests.
func NewAuthorizationAction(name string) *AuthorizationAction {
	return sdkauthorization.NewAuthorizationAction(name)
}

// NewAuthorizationModelRef creates an authorization model reference.
func NewAuthorizationModelRef(id, version string, createdAt time.Time) *AuthorizationModelRef {
	return sdkauthorization.NewAuthorizationModelRef(id, version, createdAt)
}

// NewAccessEvaluationRequest creates an access-evaluation request.
func NewAccessEvaluationRequest(subject *AuthorizationSubject, action *AuthorizationAction, resource *AuthorizationResource) *AccessEvaluationRequest {
	return sdkauthorization.NewAccessEvaluationRequest(subject, action, resource)
}

// NewRelationship creates a relationship tuple for authorization writes.
func NewRelationship(subject *AuthorizationSubject, relation string, resource *AuthorizationResource) *Relationship {
	return sdkauthorization.NewRelationship(subject, relation, resource)
}

// NewRelationshipWithTarget creates a generalized authorization tuple.
func NewRelationshipWithTarget(target *AuthorizationRelationshipTarget, relation string, resource *AuthorizationResource) *Relationship {
	return sdkauthorization.NewRelationshipWithTarget(target, relation, resource)
}

// NewRelationshipKey creates a relationship key for authorization deletes.
func NewRelationshipKey(subject *AuthorizationSubject, relation string, resource *AuthorizationResource) *RelationshipKey {
	return sdkauthorization.NewRelationshipKey(subject, relation, resource)
}

// NewRelationshipKeyWithTarget creates a generalized authorization tuple key.
func NewRelationshipKeyWithTarget(target *AuthorizationRelationshipTarget, relation string, resource *AuthorizationResource) *RelationshipKey {
	return sdkauthorization.NewRelationshipKeyWithTarget(target, relation, resource)
}

// NewWriteRelationshipsRequest creates a relationship mutation request.
func NewWriteRelationshipsRequest(writes []*Relationship, deletes []*RelationshipKey) *WriteRelationshipsRequest {
	return sdkauthorization.NewWriteRelationshipsRequest(writes, deletes)
}

// NewAuthorizationModelSubjectTypeTarget allows a relation target subject type.
func NewAuthorizationModelSubjectTypeTarget(subjectType string) *AuthorizationModelAllowedTarget {
	return sdkauthorization.NewAuthorizationModelSubjectTypeTarget(subjectType)
}

// NewAuthorizationModelResourceTypeTarget allows a relation target resource type.
func NewAuthorizationModelResourceTypeTarget(resourceType string) *AuthorizationModelAllowedTarget {
	return sdkauthorization.NewAuthorizationModelResourceTypeTarget(resourceType)
}

// NewAuthorizationModelSubjectSetAllowedTarget allows a relation target subject set.
func NewAuthorizationModelSubjectSetAllowedTarget(resourceType, relation string) *AuthorizationModelAllowedTarget {
	return sdkauthorization.NewAuthorizationModelSubjectSetAllowedTarget(resourceType, relation)
}

// NewAuthorizationModelThisRewrite includes directly related targets.
func NewAuthorizationModelThisRewrite() *AuthorizationModelRewrite {
	return sdkauthorization.NewAuthorizationModelThisRewrite()
}

// NewAuthorizationModelComputedUsersetRewrite computes another relation on the same resource.
func NewAuthorizationModelComputedUsersetRewrite(relation string) *AuthorizationModelRewrite {
	return sdkauthorization.NewAuthorizationModelComputedUsersetRewrite(relation)
}

// NewAuthorizationModelTupleToUsersetRewrite follows tuplesetRelation and then
// computes computedRelation on each related resource.
func NewAuthorizationModelTupleToUsersetRewrite(tuplesetRelation, computedRelation string) *AuthorizationModelRewrite {
	return sdkauthorization.NewAuthorizationModelTupleToUsersetRewrite(tuplesetRelation, computedRelation)
}

// NewAuthorizationModelUnionRewrite unions multiple rewrite branches.
func NewAuthorizationModelUnionRewrite(children ...*AuthorizationModelRewrite) *AuthorizationModelRewrite {
	return sdkauthorization.NewAuthorizationModelUnionRewrite(children...)
}

// AuthorizationProvider serves authorization APIs to the host.
type AuthorizationProvider interface {
	Provider
	sdkauthorization.Client
}

// AuthorizationProviderEffectiveSearch is implemented by providers that can
// search through computed usersets and inherited relationships.
type AuthorizationProviderEffectiveSearch = sdkauthorization.EffectiveSearchClient

// AuthorizationProviderExpansion is implemented by providers that can explain
// the relationship targets contributing to one resource relation.
type AuthorizationProviderExpansion = sdkauthorization.ExpansionClient
