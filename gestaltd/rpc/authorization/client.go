package authorization

import (
	"context"
	"fmt"

	sdkauthorization "github.com/valon-technologies/gestalt/sdk/go/authorization"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/protobuf/types/known/emptypb"
)

var _ sdkauthorization.Authorization = (*rpcClient)(nil)

type rpcClient struct {
	grpc proto.AuthorizationProviderClient
	opts Options
}

func (c *rpcClient) Close() error { return nil }

func (c *rpcClient) CheckAccess(ctx context.Context, req *CheckAccessRequest) (*CheckAccessResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	pbReq, err := protoCheckAccessRequest(req)
	if err != nil {
		return nil, err
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.CheckAccess(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return checkAccessResponseFromProto(resp), nil
}

func (c *rpcClient) CheckAccessMany(ctx context.Context, req *CheckAccessManyRequest) (*CheckAccessManyResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	pbReq, err := protoCheckAccessManyRequest(req)
	if err != nil {
		return nil, err
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.CheckAccessMany(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return checkAccessManyResponseFromProto(resp), nil
}

func (c *rpcClient) ListRelationships(ctx context.Context, req *ListRelationshipsRequest) (*ListRelationshipsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	pbReq, err := protoListRelationshipsRequest(req)
	if err != nil {
		return nil, err
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.ListRelationships(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return listRelationshipsResponseFromProto(resp), nil
}

func (c *rpcClient) AddRelationship(ctx context.Context, req *AddRelationshipRequest) (*AddRelationshipResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	pbReq, err := protoAddRelationshipRequest(req)
	if err != nil {
		return nil, err
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.AddRelationship(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return addRelationshipResponseFromProto(resp), nil
}

func (c *rpcClient) DeleteRelationship(ctx context.Context, req *DeleteRelationshipRequest) (*DeleteRelationshipResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	pbReq, err := protoDeleteRelationshipRequest(req)
	if err != nil {
		return nil, err
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	if _, err := c.grpc.DeleteRelationship(ctx, pbReq); err != nil {
		return nil, err
	}
	return &DeleteRelationshipResponse{}, nil
}

func (c *rpcClient) SetRelationships(ctx context.Context, req *SetRelationshipsRequest) (*SetRelationshipsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	pbReq, err := protoSetRelationshipsRequest(req)
	if err != nil {
		return nil, err
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.SetRelationships(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return setRelationshipsResponseFromProto(resp), nil
}

func (c *rpcClient) GetActiveModelRef(ctx context.Context) (*GetActiveModelRefResponse, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.GetActiveModelRef(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	return getActiveModelRefResponseFromProto(resp), nil
}

func (c *rpcClient) SetActiveModel(ctx context.Context, req *SetActiveModelRequest) (*SetActiveModelResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.SetActiveModel(ctx, protoSetActiveModelRequest(req))
	if err != nil {
		return nil, err
	}
	return setActiveModelResponseFromProto(resp), nil
}

func (c *rpcClient) ListActiveModelResourceTypes(ctx context.Context, req *ListActiveModelResourceTypesRequest) (*ListActiveModelResourceTypesResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.ListActiveModelResourceTypes(ctx, protoListActiveModelResourceTypesRequest(req))
	if err != nil {
		return nil, err
	}
	return listActiveModelResourceTypesResponseFromProto(resp), nil
}
