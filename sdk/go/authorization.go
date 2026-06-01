package gestalt

import (
	"context"
	"time"

	sdkauthorization "github.com/valon-technologies/gestalt/sdk/go/authorization"
	rpcauthorization "github.com/valon-technologies/gestalt/server/rpc/authorization"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

type (
	SourceLayer                          = sdkauthorization.SourceLayer
	RelationshipTargetType               = sdkauthorization.RelationshipTargetType
	AuthorizationSourceLayer             = sdkauthorization.SourceLayer
	AuthorizationRelationshipTargetType  = sdkauthorization.RelationshipTargetType
	AuthorizationSubject                 = sdkauthorization.AuthorizationSubject
	AuthorizationResource                = sdkauthorization.AuthorizationResource
	AuthorizationSubjectSet              = sdkauthorization.AuthorizationSubjectSet
	AuthorizationRelationshipTarget      = sdkauthorization.AuthorizationRelationshipTarget
	AuthorizationAction                  = sdkauthorization.AuthorizationAction
	CheckAccessRequest                   = sdkauthorization.CheckAccessRequest
	CheckAccessResponse                  = sdkauthorization.CheckAccessResponse
	CheckAccessManyRequest               = sdkauthorization.CheckAccessManyRequest
	CheckAccessManyResponse              = sdkauthorization.CheckAccessManyResponse
	Relationship                         = sdkauthorization.Relationship
	RelationshipTuple                    = sdkauthorization.RelationshipTuple
	RelationshipFilter                   = sdkauthorization.RelationshipFilter
	ListRelationshipsRequest             = sdkauthorization.ListRelationshipsRequest
	ListRelationshipsResponse            = sdkauthorization.ListRelationshipsResponse
	AddRelationshipRequest               = sdkauthorization.AddRelationshipRequest
	AddRelationshipResponse              = sdkauthorization.AddRelationshipResponse
	DeleteRelationshipRequest            = sdkauthorization.DeleteRelationshipRequest
	DeleteRelationshipResponse           = sdkauthorization.DeleteRelationshipResponse
	SetRelationshipsRequest              = sdkauthorization.SetRelationshipsRequest
	SetRelationshipsResponse             = sdkauthorization.SetRelationshipsResponse
	AuthorizationModel                   = sdkauthorization.AuthorizationModel
	AuthorizationModelRef                = sdkauthorization.AuthorizationModelRef
	AuthorizationModelResourceType       = sdkauthorization.AuthorizationModelResourceType
	AuthorizationModelRelation           = sdkauthorization.AuthorizationModelRelation
	AuthorizationModelAction             = sdkauthorization.AuthorizationModelAction
	AuthorizationModelAllowedTarget      = sdkauthorization.AuthorizationModelAllowedTarget
	SubjectSetType                       = sdkauthorization.SubjectSetType
	AuthorizationSubjectSetType          = sdkauthorization.SubjectSetType
	GetActiveModelRefResponse            = sdkauthorization.GetActiveModelRefResponse
	SetActiveModelRequest                = sdkauthorization.SetActiveModelRequest
	SetActiveModelResponse               = sdkauthorization.SetActiveModelResponse
	AuthorizationModelResourceTypeFilter = sdkauthorization.AuthorizationModelResourceTypeFilter
	ListActiveModelResourceTypesRequest  = sdkauthorization.ListActiveModelResourceTypesRequest
	ListActiveModelResourceTypesResponse = sdkauthorization.ListActiveModelResourceTypesResponse
)

const (
	// AuthorizationSubjectTypeSubject identifies canonical Gestalt subjects in
	// managed authorization relationships.
	AuthorizationSubjectTypeSubject      = sdkauthorization.AuthorizationSubjectTypeSubject
	AuthorizationSourceLayerUnspecified  = sdkauthorization.SourceLayerUnspecified
	AuthorizationSourceLayerStaticConfig = sdkauthorization.SourceLayerStaticConfig
	AuthorizationSourceLayerRuntime      = sdkauthorization.SourceLayerRuntime
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

// NewCheckAccessRequest creates an access-check request.
func NewCheckAccessRequest(subject *AuthorizationSubject, action *AuthorizationAction, resource *AuthorizationResource) *CheckAccessRequest {
	return sdkauthorization.NewCheckAccessRequest(subject, action, resource)
}

// NewRelationship creates a relationship tuple for authorization writes.
func NewRelationship(subject *AuthorizationSubject, relation string, resource *AuthorizationResource) *Relationship {
	return sdkauthorization.NewRelationship(subject, relation, resource)
}

// NewRelationshipWithTarget creates a generalized authorization tuple.
func NewRelationshipWithTarget(target *AuthorizationRelationshipTarget, relation string, resource *AuthorizationResource) *Relationship {
	return sdkauthorization.NewRelationshipWithTarget(target, relation, resource)
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

var sharedAuthorizationTransport sharedManagerTransport[proto.AuthorizationProviderClient]

// Authorization returns a shared authorization capability.
func Authorization() (sdkauthorization.Authorization, error) {
	target, token, err := hostServiceTarget("authorization")
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := managerTransportClient(ctx, "authorization", target, token, &sharedAuthorizationTransport, proto.NewAuthorizationProviderClient)
	if err != nil {
		return nil, err
	}
	return rpcauthorization.NewClient(client, rpcauthorization.Options{}), nil
}

// AuthorizationProvider serves authorization APIs to the host.
type AuthorizationProvider interface {
	Provider
	sdkauthorization.Provider
}
