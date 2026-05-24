package authorization

import (
	"context"
	"fmt"

	sdkauthorization "github.com/valon-technologies/gestalt/sdk/go/authorization"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/protobuf/types/known/emptypb"
)

var (
	_ sdkauthorization.Client                = (*rpcClient)(nil)
	_ sdkauthorization.EffectiveSearchClient = (*rpcClient)(nil)
	_ sdkauthorization.ExpansionClient       = (*rpcClient)(nil)
)

type rpcClient struct {
	grpc proto.AuthorizationProviderClient
	opts Options
}

// Close is a no-op because this client uses shared transport.
func (c *rpcClient) Close() error { return nil }

func (c *rpcClient) Evaluate(ctx context.Context, req *AccessEvaluationRequest) (*AccessDecision, error) {
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	pbReq, err := protoAccessEvaluationRequest(req)
	if err != nil {
		return nil, err
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.Evaluate(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return accessDecisionFromProto(resp), nil
}

func (c *rpcClient) EvaluateMany(ctx context.Context, req *AccessEvaluationsRequest) (*AccessEvaluationsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	pbReq, err := protoAccessEvaluationsRequest(req)
	if err != nil {
		return nil, err
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.EvaluateMany(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return accessEvaluationsResponseFromProto(resp), nil
}

func (c *rpcClient) SearchResources(ctx context.Context, req *ResourceSearchRequest) (*ResourceSearchResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	pbReq, err := protoResourceSearchRequest(req)
	if err != nil {
		return nil, err
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.SearchResources(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return resourceSearchResponseFromProto(resp), nil
}

func (c *rpcClient) SearchSubjects(ctx context.Context, req *SubjectSearchRequest) (*SubjectSearchResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	pbReq, err := protoSubjectSearchRequest(req)
	if err != nil {
		return nil, err
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.SearchSubjects(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return subjectSearchResponseFromProto(resp), nil
}

func (c *rpcClient) EffectiveSearchResources(ctx context.Context, req *ResourceSearchRequest) (*ResourceSearchResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	pbReq, err := protoResourceSearchRequest(req)
	if err != nil {
		return nil, err
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.EffectiveSearchResources(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return resourceSearchResponseFromProto(resp), nil
}

func (c *rpcClient) EffectiveSearchSubjects(ctx context.Context, req *EffectiveSubjectSearchRequest) (*EffectiveSubjectSearchResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	pbReq, err := protoEffectiveSubjectSearchRequest(req)
	if err != nil {
		return nil, err
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.EffectiveSearchSubjects(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return effectiveSubjectSearchResponseFromProto(resp), nil
}

func (c *rpcClient) SearchActions(ctx context.Context, req *ActionSearchRequest) (*ActionSearchResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	pbReq, err := protoActionSearchRequest(req)
	if err != nil {
		return nil, err
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.SearchActions(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return actionSearchResponseFromProto(resp), nil
}

func (c *rpcClient) Expand(ctx context.Context, req *ExpandRequest) (*ExpandResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	pbReq, err := protoExpandRequest(req)
	if err != nil {
		return nil, err
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.Expand(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return expandResponseFromProto(resp), nil
}

func (c *rpcClient) ReadRelationships(ctx context.Context, req *ReadRelationshipsRequest) (*ReadRelationshipsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	pbReq, err := protoReadRelationshipsRequest(req)
	if err != nil {
		return nil, err
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.ReadRelationships(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return readRelationshipsResponseFromProto(resp), nil
}

func (c *rpcClient) WriteRelationships(ctx context.Context, req *WriteRelationshipsRequest) error {
	if req == nil {
		return fmt.Errorf("authorization: request is required")
	}
	pbReq, err := protoWriteRelationshipsRequest(req)
	if err != nil {
		return err
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	_, err = c.grpc.WriteRelationships(ctx, pbReq)
	return err
}

func (c *rpcClient) GetMetadata(ctx context.Context) (*AuthorizationMetadata, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.GetMetadata(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	return authorizationMetadataFromProto(resp), nil
}

func (c *rpcClient) GetActiveModel(ctx context.Context) (*GetActiveModelResponse, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.GetActiveModel(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	return getActiveModelResponseFromProto(resp), nil
}

func (c *rpcClient) ListModels(ctx context.Context, req *ListModelsRequest) (*ListModelsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.ListModels(ctx, protoListModelsRequest(req))
	if err != nil {
		return nil, err
	}
	return listModelsResponseFromProto(resp), nil
}

func (c *rpcClient) WriteModel(ctx context.Context, req *WriteModelRequest) (*AuthorizationModelRef, error) {
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.WriteModel(ctx, protoWriteModelRequest(req))
	if err != nil {
		return nil, err
	}
	return authorizationModelRefFromProto(resp), nil
}
