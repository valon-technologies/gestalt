package authorization

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/protobuf/types/known/emptypb"
)

var _ core.AuthorizationProvider = (*Client)(nil)

type Client struct {
	grpc proto.AuthorizationClient
	opts Options
}

func (c *Client) CheckAccess(ctx context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	return c.grpc.CheckAccess(ctx, req)
}

func (c *Client) CheckAccessMany(ctx context.Context, req *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	return c.grpc.CheckAccessMany(ctx, req)
}

func (c *Client) ListRelationships(ctx context.Context, req *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	return c.grpc.ListRelationships(ctx, req)
}

func (c *Client) WriteRelationships(ctx context.Context, req *proto.WriteRelationshipsRequest) (*proto.WriteRelationshipsResponse, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	return c.grpc.WriteRelationships(ctx, req)
}

func (c *Client) AddRelationship(ctx context.Context, req *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	return c.grpc.AddRelationship(ctx, req)
}

func (c *Client) DeleteRelationship(ctx context.Context, req *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	return c.grpc.DeleteRelationship(ctx, req)
}

func (c *Client) SetAuthorizationState(ctx context.Context, req *proto.SetAuthorizationStateRequest) (*proto.SetAuthorizationStateResponse, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	return c.grpc.SetAuthorizationState(ctx, req)
}

func (c *Client) GetActiveModelRef(ctx context.Context) (*proto.GetActiveModelRefResponse, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	return c.grpc.GetActiveModelRef(ctx, &emptypb.Empty{})
}

func (c *Client) SetActiveModel(ctx context.Context, req *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	return c.grpc.SetActiveModel(ctx, req)
}

func (c *Client) ListActiveModelResourceTypes(ctx context.Context, req *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	return c.grpc.ListActiveModelResourceTypes(ctx, req)
}

func (c *Client) Ping(context.Context) error { return nil }

func (c *Client) Close() error { return nil }
