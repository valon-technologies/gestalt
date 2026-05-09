package gestalt

import (
	"context"
	"time"

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

// AuthorizationSubjectSet identifies a Zanzibar-style userset target.
type AuthorizationSubjectSet = proto.SubjectSet

// AuthorizationRelationshipTarget identifies the left side of an authorization
// relationship. It can be a subject, resource, or subject set.
type AuthorizationRelationshipTarget = proto.RelationshipTarget

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

// EffectiveSubjectSearchRequest searches effective targets related to a
// resource and action through computed usersets and inherited relationships.
type EffectiveSubjectSearchRequest = proto.EffectiveSubjectSearchRequest

// EffectiveSubjectSearchResponse contains effective subjects or subject sets
// related to a resource and action.
type EffectiveSubjectSearchResponse = proto.EffectiveSubjectSearchResponse

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

// AuthorizationModelAllowedTarget describes one valid target kind for a relation.
type AuthorizationModelAllowedTarget = proto.AuthorizationModelAllowedTarget

// AuthorizationModelSubjectSetTarget describes a valid userset target.
type AuthorizationModelSubjectSetTarget = proto.AuthorizationModelSubjectSetTarget

// AuthorizationModelRewrite describes how to compute one relation or action.
type AuthorizationModelRewrite = proto.AuthorizationModelRewrite

// AuthorizationModelRewriteThis includes directly related targets in a rewrite.
type AuthorizationModelRewriteThis = proto.AuthorizationModelRewriteThis

// AuthorizationModelComputedUserset references another relation on the same resource.
type AuthorizationModelComputedUserset = proto.AuthorizationModelComputedUserset

// AuthorizationModelTupleToUserset follows one relation and computes another
// relation on each related resource.
type AuthorizationModelTupleToUserset = proto.AuthorizationModelTupleToUserset

// AuthorizationModelRewriteUnion unions multiple rewrite branches.
type AuthorizationModelRewriteUnion = proto.AuthorizationModelRewriteUnion

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

// ExpandRequest asks a provider to explain one resource relation.
type ExpandRequest = proto.ExpandRequest

// ExpandNode describes one node in an expanded authorization graph.
type ExpandNode = proto.ExpandNode

// ExpandResponse contains an expanded authorization graph.
type ExpandResponse = proto.ExpandResponse

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
	return &AuthorizationRelationshipTarget{
		Kind: &proto.RelationshipTarget_Subject{Subject: subject},
	}
}

// NewAuthorizationResourceTarget creates a relationship target from a resource.
func NewAuthorizationResourceTarget(resource *AuthorizationResource) *AuthorizationRelationshipTarget {
	return &AuthorizationRelationshipTarget{
		Kind: &proto.RelationshipTarget_Resource{Resource: resource},
	}
}

// NewAuthorizationSubjectSetTarget creates a relationship target from a subject set.
func NewAuthorizationSubjectSetTarget(resource *AuthorizationResource, relation string) *AuthorizationRelationshipTarget {
	return &AuthorizationRelationshipTarget{
		Kind: &proto.RelationshipTarget_SubjectSet{
			SubjectSet: NewAuthorizationSubjectSet(resource, relation),
		},
	}
}

// NewAuthorizationAction creates an action reference for authorization requests.
func NewAuthorizationAction(name string) *AuthorizationAction {
	return &AuthorizationAction{Name: name}
}

// NewAuthorizationModelRef creates an authorization model reference from native
// Go values.
func NewAuthorizationModelRef(id, version string, createdAt time.Time) *AuthorizationModelRef {
	return &AuthorizationModelRef{
		Id:        id,
		Version:   version,
		CreatedAt: timestampFromNonZeroTime(createdAt),
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
	return &AuthorizationModelAllowedTarget{
		Kind: &proto.AuthorizationModelAllowedTarget_SubjectType{SubjectType: subjectType},
	}
}

// NewAuthorizationModelResourceTypeTarget allows a relation target resource type.
func NewAuthorizationModelResourceTypeTarget(resourceType string) *AuthorizationModelAllowedTarget {
	return &AuthorizationModelAllowedTarget{
		Kind: &proto.AuthorizationModelAllowedTarget_ResourceType{ResourceType: resourceType},
	}
}

// NewAuthorizationModelSubjectSetAllowedTarget allows a relation target subject set.
func NewAuthorizationModelSubjectSetAllowedTarget(resourceType, relation string) *AuthorizationModelAllowedTarget {
	return &AuthorizationModelAllowedTarget{
		Kind: &proto.AuthorizationModelAllowedTarget_SubjectSet{
			SubjectSet: &AuthorizationModelSubjectSetTarget{
				ResourceType: resourceType,
				Relation:     relation,
			},
		},
	}
}

// NewAuthorizationModelThisRewrite includes directly related targets.
func NewAuthorizationModelThisRewrite() *AuthorizationModelRewrite {
	return &AuthorizationModelRewrite{
		Kind: &proto.AuthorizationModelRewrite_This{This: &AuthorizationModelRewriteThis{}},
	}
}

// NewAuthorizationModelComputedUsersetRewrite computes another relation on the same resource.
func NewAuthorizationModelComputedUsersetRewrite(relation string) *AuthorizationModelRewrite {
	return &AuthorizationModelRewrite{
		Kind: &proto.AuthorizationModelRewrite_ComputedUserset{
			ComputedUserset: &AuthorizationModelComputedUserset{Relation: relation},
		},
	}
}

// NewAuthorizationModelTupleToUsersetRewrite follows tuplesetRelation and then
// computes computedRelation on each related resource.
func NewAuthorizationModelTupleToUsersetRewrite(tuplesetRelation, computedRelation string) *AuthorizationModelRewrite {
	return &AuthorizationModelRewrite{
		Kind: &proto.AuthorizationModelRewrite_TupleToUserset{
			TupleToUserset: &AuthorizationModelTupleToUserset{
				TuplesetRelation: tuplesetRelation,
				ComputedRelation: computedRelation,
			},
		},
	}
}

// NewAuthorizationModelUnionRewrite unions multiple rewrite branches.
func NewAuthorizationModelUnionRewrite(children ...*AuthorizationModelRewrite) *AuthorizationModelRewrite {
	return &AuthorizationModelRewrite{
		Kind: &proto.AuthorizationModelRewrite_Union{
			Union: &AuthorizationModelRewriteUnion{Children: children},
		},
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

// AuthorizationProviderEffectiveSearch is implemented by providers that can
// search through computed usersets and inherited relationships.
type AuthorizationProviderEffectiveSearch interface {
	EffectiveSearchResources(ctx context.Context, req *ResourceSearchRequest) (*ResourceSearchResponse, error)
	EffectiveSearchSubjects(ctx context.Context, req *EffectiveSubjectSearchRequest) (*EffectiveSubjectSearchResponse, error)
}

// AuthorizationProviderExpansion is implemented by providers that can explain
// the relationship targets contributing to one resource relation.
type AuthorizationProviderExpansion interface {
	Expand(ctx context.Context, req *ExpandRequest) (*ExpandResponse, error)
}
