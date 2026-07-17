package authorization

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

var _ core.AuthorizationProvider = (*Client)(nil)

type Client struct {
	grpc      proto.AuthorizationClient
	opts      Options
	transport providergateway.Transport
}

func (c *Client) CheckAccess(ctx context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	var out proto.CheckAccessResponse
	if err := c.invoke(ctx, "CheckAccess", req, &out, func(ctx context.Context) (gproto.Message, error) {
		return c.grpc.CheckAccess(ctx, req)
	}); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CheckAccessMany(ctx context.Context, req *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	var out proto.CheckAccessManyResponse
	if err := c.invoke(ctx, "CheckAccessMany", req, &out, func(ctx context.Context) (gproto.Message, error) {
		return c.grpc.CheckAccessMany(ctx, req)
	}); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListRelationships(ctx context.Context, req *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	var out proto.ListRelationshipsResponse
	if err := c.invoke(ctx, "ListRelationships", req, &out, func(ctx context.Context) (gproto.Message, error) {
		return c.grpc.ListRelationships(ctx, req)
	}); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) AddRelationship(ctx context.Context, req *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	var out proto.AddRelationshipResponse
	if err := c.invoke(ctx, "AddRelationship", req, &out, func(ctx context.Context) (gproto.Message, error) {
		return c.grpc.AddRelationship(ctx, req)
	}); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteRelationship(ctx context.Context, req *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	var out proto.DeleteRelationshipResponse
	if err := c.invoke(ctx, "DeleteRelationship", req, &out, func(ctx context.Context) (gproto.Message, error) {
		return c.grpc.DeleteRelationship(ctx, req)
	}); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) SetAuthorizationState(ctx context.Context, req *proto.SetAuthorizationStateRequest) (*proto.SetAuthorizationStateResponse, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	var out proto.SetAuthorizationStateResponse
	if err := c.invoke(ctx, "SetAuthorizationState", req, &out, func(ctx context.Context) (gproto.Message, error) {
		return c.grpc.SetAuthorizationState(ctx, req)
	}); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetActiveModelRef(ctx context.Context) (*proto.GetActiveModelRefResponse, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	var out proto.GetActiveModelRefResponse
	if err := c.invoke(ctx, "GetActiveModelRef", &emptypb.Empty{}, &out, func(ctx context.Context) (gproto.Message, error) {
		return c.grpc.GetActiveModelRef(ctx, &emptypb.Empty{})
	}); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) SetActiveModel(ctx context.Context, req *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	var out proto.SetActiveModelResponse
	if err := c.invoke(ctx, "SetActiveModel", req, &out, func(ctx context.Context) (gproto.Message, error) {
		return c.grpc.SetActiveModel(ctx, req)
	}); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListActiveModelResourceTypes(ctx context.Context, req *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	var out proto.ListActiveModelResourceTypesResponse
	if err := c.invoke(ctx, "ListActiveModelResourceTypes", req, &out, func(ctx context.Context) (gproto.Message, error) {
		return c.grpc.ListActiveModelResourceTypes(ctx, req)
	}); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Ping(context.Context) error { return nil }

func (c *Client) Close() error { return nil }

func (c *Client) invoke(ctx context.Context, operation string, in gproto.Message, out gproto.Message, call func(context.Context) (gproto.Message, error)) error {
	if c == nil {
		return fmt.Errorf("authorization client is nil")
	}
	if c.transport == nil {
		return fmt.Errorf("provider gateway is required")
	}
	payload, err := gproto.Marshal(in)
	if err != nil {
		return fmt.Errorf("provider gateway: encode %s request: %w", operation, err)
	}
	resp, err := c.transport.Invoke(ctx, providergateway.ProviderGatewayRequest{
		ProviderID:     c.opts.ProviderID,
		ProviderKind:   providergateway.ProviderKindAuthorization,
		ServiceName:    proto.Authorization_ServiceDesc.ServiceName,
		Operation:      operation,
		RequestContext: providergateway.RequestContextFromContext(ctx),
		Payload:        payload,
	}, func(ctx context.Context, _ providergateway.ProviderGatewayRequest) (providergateway.ProviderGatewayResponse, error) {
		msg, err := call(ctx)
		if err != nil {
			return providergateway.ProviderGatewayResponse{}, err
		}
		payload, err := gproto.Marshal(msg)
		if err != nil {
			return providergateway.ProviderGatewayResponse{}, fmt.Errorf("provider gateway: encode %s response: %w", operation, err)
		}
		return providergateway.ProviderGatewayResponse{Payload: payload}, nil
	})
	if err != nil {
		return err
	}
	if len(resp.Payload) == 0 {
		return nil
	}
	if err := gproto.Unmarshal(resp.Payload, out); err != nil {
		return fmt.Errorf("provider gateway: decode %s response: %w", operation, err)
	}
	return nil
}
