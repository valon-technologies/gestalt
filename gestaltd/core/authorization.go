package core

import (
	"context"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

type AuthorizationProvider interface {
	CheckAccess(ctx context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error)
	CheckAccessMany(ctx context.Context, req *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error)
	ListRelationships(ctx context.Context, req *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error)
	WriteRelationships(ctx context.Context, req *proto.WriteRelationshipsRequest) (*proto.WriteRelationshipsResponse, error)
	AddRelationship(ctx context.Context, req *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error)
	DeleteRelationship(ctx context.Context, req *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error)
	SetAuthorizationState(ctx context.Context, req *proto.SetAuthorizationStateRequest) (*proto.SetAuthorizationStateResponse, error)
	GetActiveModelRef(ctx context.Context) (*proto.GetActiveModelRefResponse, error)
	SetActiveModel(ctx context.Context, req *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error)
	ListActiveModelResourceTypes(ctx context.Context, req *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error)
	Ping(ctx context.Context) error
	Close() error
}
