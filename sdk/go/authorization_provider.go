package gestalt

import (
	"context"

	"github.com/valon-technologies/gestalt/sdk/go/authorization"
)

// AuthorizationProvider is implemented by providers that serve the generic
// authorization model, relationship, and access-check surface over gRPC.
type AuthorizationProvider interface {
	Provider
	CheckAccess(ctx context.Context, req *authorization.CheckAccessRequest) (*authorization.CheckAccessResponse, error)
	CheckAccessMany(ctx context.Context, req *authorization.CheckAccessManyRequest) (*authorization.CheckAccessManyResponse, error)
	ListRelationships(ctx context.Context, req *authorization.ListRelationshipsRequest) (*authorization.ListRelationshipsResponse, error)
	AddRelationship(ctx context.Context, req *authorization.AddRelationshipRequest) (*authorization.AddRelationshipResponse, error)
	DeleteRelationship(ctx context.Context, req *authorization.DeleteRelationshipRequest) (*authorization.DeleteRelationshipResponse, error)
	SetAuthorizationState(ctx context.Context, req *authorization.SetAuthorizationStateRequest) (*authorization.SetAuthorizationStateResponse, error)
	GetActiveModelRef(ctx context.Context) (*authorization.GetActiveModelRefResponse, error)
	SetActiveModel(ctx context.Context, req *authorization.SetActiveModelRequest) (*authorization.SetActiveModelResponse, error)
	ListActiveModelResourceTypes(ctx context.Context, req *authorization.ListActiveModelResourceTypesRequest) (*authorization.ListActiveModelResourceTypesResponse, error)
}
