package gestalt

import (
	"context"
	"fmt"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/protobuf/types/known/emptypb"
)

// AuthorizationClient calls the host authorization provider.
//
// The client accepts SDK authorization request types from this package and
// hides the generated protobuf transport used on the wire.
type AuthorizationClient struct {
	client proto.AuthorizationProviderClient
}

var sharedAuthorizationTransport sharedManagerTransport[proto.AuthorizationProviderClient]

// Authorization returns a shared authorization client.
func Authorization() (*AuthorizationClient, error) {
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
	return &AuthorizationClient{client: client}, nil
}

// Close is a no-op because this client uses shared transport.
func (c *AuthorizationClient) Close() error { return nil }

// Evaluate evaluates one authorization request.
func (c *AuthorizationClient) Evaluate(ctx context.Context, req *AccessEvaluationRequest) (*AccessDecision, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("authorization: client is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	pbReq, err := protoAccessEvaluationRequest(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Evaluate(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return accessDecisionFromProto(resp), nil
}

// EvaluateMany evaluates multiple authorization requests in one RPC.
func (c *AuthorizationClient) EvaluateMany(ctx context.Context, req *AccessEvaluationsRequest) (*AccessEvaluationsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("authorization: client is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	pbReq, err := protoAccessEvaluationsRequest(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.EvaluateMany(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return accessEvaluationsResponseFromProto(resp), nil
}

// SearchResources searches resources visible to a subject for an action.
func (c *AuthorizationClient) SearchResources(ctx context.Context, req *ResourceSearchRequest) (*ResourceSearchResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("authorization: client is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	pbReq, err := protoResourceSearchRequest(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.SearchResources(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return resourceSearchResponseFromProto(resp), nil
}

// SearchSubjects searches subjects related to a resource and action.
func (c *AuthorizationClient) SearchSubjects(ctx context.Context, req *SubjectSearchRequest) (*SubjectSearchResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("authorization: client is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	pbReq, err := protoSubjectSearchRequest(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.SearchSubjects(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return subjectSearchResponseFromProto(resp), nil
}

// EffectiveSearchResources searches resources visible to a subject through
// computed usersets and inherited relationships.
func (c *AuthorizationClient) EffectiveSearchResources(ctx context.Context, req *ResourceSearchRequest) (*ResourceSearchResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("authorization: client is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	pbReq, err := protoResourceSearchRequest(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.EffectiveSearchResources(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return resourceSearchResponseFromProto(resp), nil
}

// EffectiveSearchSubjects searches effective subjects or subject sets related
// to a resource and action.
func (c *AuthorizationClient) EffectiveSearchSubjects(ctx context.Context, req *EffectiveSubjectSearchRequest) (*EffectiveSubjectSearchResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("authorization: client is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	pbReq, err := protoEffectiveSubjectSearchRequest(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.EffectiveSearchSubjects(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return effectiveSubjectSearchResponseFromProto(resp), nil
}

// SearchActions searches actions available between a subject and resource.
func (c *AuthorizationClient) SearchActions(ctx context.Context, req *ActionSearchRequest) (*ActionSearchResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("authorization: client is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	pbReq, err := protoActionSearchRequest(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.SearchActions(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return actionSearchResponseFromProto(resp), nil
}

// Expand explains the relationship targets contributing to one resource relation.
func (c *AuthorizationClient) Expand(ctx context.Context, req *ExpandRequest) (*ExpandResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("authorization: client is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	pbReq, err := protoExpandRequest(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Expand(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return expandResponseFromProto(resp), nil
}

// ReadRelationships reads authorization relationships matching a request.
func (c *AuthorizationClient) ReadRelationships(ctx context.Context, req *ReadRelationshipsRequest) (*ReadRelationshipsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("authorization: client is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	pbReq, err := protoReadRelationshipsRequest(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.ReadRelationships(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return readRelationshipsResponseFromProto(resp), nil
}

// WriteRelationships writes and deletes authorization relationships.
func (c *AuthorizationClient) WriteRelationships(ctx context.Context, req *WriteRelationshipsRequest) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("authorization: client is not initialized")
	}
	if req == nil {
		return fmt.Errorf("authorization: request is required")
	}
	pbReq, err := protoWriteRelationshipsRequest(req)
	if err != nil {
		return err
	}
	_, err = c.client.WriteRelationships(ctx, pbReq)
	return err
}

// GetMetadata returns host authorization provider metadata.
func (c *AuthorizationClient) GetMetadata(ctx context.Context) (*AuthorizationMetadata, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("authorization: client is not initialized")
	}
	resp, err := c.client.GetMetadata(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	return authorizationMetadataFromProto(resp), nil
}
