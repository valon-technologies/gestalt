package gestalt

import (
	"context"
	"fmt"
	"time"

	sdkauthorization "github.com/valon-technologies/gestalt/sdk/go/authorization"
	rpcauthorization "github.com/valon-technologies/gestalt/server/rpc/authorization"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

// AuthorizationClient calls the host authorization provider.
//
// The client accepts SDK authorization request types from this package and
// hides the generated protobuf transport used on the wire.
type AuthorizationClient struct {
	client sdkauthorization.Client
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
	return &AuthorizationClient{client: rpcauthorization.NewClient(client, rpcauthorization.Options{})}, nil
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
	return c.client.Evaluate(ctx, req)
}

// EvaluateMany evaluates multiple authorization requests in one RPC.
func (c *AuthorizationClient) EvaluateMany(ctx context.Context, req *AccessEvaluationsRequest) (*AccessEvaluationsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("authorization: client is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	return c.client.EvaluateMany(ctx, req)
}

// SearchResources searches resources visible to a subject for an action.
func (c *AuthorizationClient) SearchResources(ctx context.Context, req *ResourceSearchRequest) (*ResourceSearchResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("authorization: client is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	return c.client.SearchResources(ctx, req)
}

// SearchSubjects searches subjects related to a resource and action.
func (c *AuthorizationClient) SearchSubjects(ctx context.Context, req *SubjectSearchRequest) (*SubjectSearchResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("authorization: client is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	return c.client.SearchSubjects(ctx, req)
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
	client, ok := c.client.(sdkauthorization.EffectiveSearchClient)
	if !ok {
		return nil, fmt.Errorf("authorization: client does not implement effective search")
	}
	return client.EffectiveSearchResources(ctx, req)
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
	client, ok := c.client.(sdkauthorization.EffectiveSearchClient)
	if !ok {
		return nil, fmt.Errorf("authorization: client does not implement effective search")
	}
	return client.EffectiveSearchSubjects(ctx, req)
}

// SearchActions searches actions available between a subject and resource.
func (c *AuthorizationClient) SearchActions(ctx context.Context, req *ActionSearchRequest) (*ActionSearchResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("authorization: client is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	return c.client.SearchActions(ctx, req)
}

// Expand explains the relationship targets contributing to one resource relation.
func (c *AuthorizationClient) Expand(ctx context.Context, req *ExpandRequest) (*ExpandResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("authorization: client is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	client, ok := c.client.(sdkauthorization.ExpansionClient)
	if !ok {
		return nil, fmt.Errorf("authorization: client does not implement expansion")
	}
	return client.Expand(ctx, req)
}

// ReadRelationships reads authorization relationships matching a request.
func (c *AuthorizationClient) ReadRelationships(ctx context.Context, req *ReadRelationshipsRequest) (*ReadRelationshipsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("authorization: client is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	return c.client.ReadRelationships(ctx, req)
}

// WriteRelationships writes and deletes authorization relationships.
func (c *AuthorizationClient) WriteRelationships(ctx context.Context, req *WriteRelationshipsRequest) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("authorization: client is not initialized")
	}
	if req == nil {
		return fmt.Errorf("authorization: request is required")
	}
	return c.client.WriteRelationships(ctx, req)
}

// GetMetadata returns host authorization provider metadata.
func (c *AuthorizationClient) GetMetadata(ctx context.Context) (*AuthorizationMetadata, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("authorization: client is not initialized")
	}
	return c.client.GetMetadata(ctx)
}

// GetActiveModel returns the active authorization model.
func (c *AuthorizationClient) GetActiveModel(ctx context.Context) (*GetActiveModelResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("authorization: client is not initialized")
	}
	return c.client.GetActiveModel(ctx)
}

// ListModels lists stored authorization model refs.
func (c *AuthorizationClient) ListModels(ctx context.Context, req *ListModelsRequest) (*ListModelsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("authorization: client is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	return c.client.ListModels(ctx, req)
}

// WriteModel stores an authorization model and returns its ref.
func (c *AuthorizationClient) WriteModel(ctx context.Context, req *WriteModelRequest) (*AuthorizationModelRef, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("authorization: client is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("authorization: request is required")
	}
	return c.client.WriteModel(ctx, req)
}
