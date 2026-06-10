package providergateway

import (
	"context"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

type Gateway struct {
	authorization map[string]core.AuthorizationProvider
}

type Option func(*Gateway)

func WithAuthorizationProvider(providerID string, provider core.AuthorizationProvider) Option {
	return func(g *Gateway) {
		if provider == nil {
			return
		}
		if g.authorization == nil {
			g.authorization = map[string]core.AuthorizationProvider{}
		}
		g.authorization[strings.TrimSpace(providerID)] = provider
	}
}

func New(opts ...Option) *Gateway {
	g := &Gateway{}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

func (g *Gateway) Invoke(ctx context.Context, req ProviderGatewayRequest) (ProviderGatewayResponse, error) {
	switch req.ProviderKind {
	case ProviderKindAuthorization:
		return g.invokeAuthorization(ctx, req)
	default:
		return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: unsupported provider kind %q", req.ProviderKind)
	}
}

func (g *Gateway) invokeAuthorization(ctx context.Context, req ProviderGatewayRequest) (ProviderGatewayResponse, error) {
	provider := g.authorization[strings.TrimSpace(req.ProviderID)]
	if provider == nil {
		return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: authorization provider %q is not configured", req.ProviderID)
	}
	method := strings.TrimPrefix(strings.TrimSpace(req.FullMethod), authorizationMethodPrefix())
	if method == req.FullMethod || method == "" {
		return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: unsupported authorization method %q", req.FullMethod)
	}

	switch method {
	case "CheckAccess":
		var in proto.CheckAccessRequest
		if err := gproto.Unmarshal(req.Payload, &in); err != nil {
			return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: decode CheckAccess request: %w", err)
		}
		out, err := provider.CheckAccess(ctx, &in)
		return marshalGatewayResponse(out, err)
	case "CheckAccessMany":
		var in proto.CheckAccessManyRequest
		if err := gproto.Unmarshal(req.Payload, &in); err != nil {
			return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: decode CheckAccessMany request: %w", err)
		}
		out, err := provider.CheckAccessMany(ctx, &in)
		return marshalGatewayResponse(out, err)
	case "ListRelationships":
		var in proto.ListRelationshipsRequest
		if err := gproto.Unmarshal(req.Payload, &in); err != nil {
			return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: decode ListRelationships request: %w", err)
		}
		out, err := provider.ListRelationships(ctx, &in)
		return marshalGatewayResponse(out, err)
	case "AddRelationship":
		var in proto.AddRelationshipRequest
		if err := gproto.Unmarshal(req.Payload, &in); err != nil {
			return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: decode AddRelationship request: %w", err)
		}
		out, err := provider.AddRelationship(ctx, &in)
		return marshalGatewayResponse(out, err)
	case "DeleteRelationship":
		var in proto.DeleteRelationshipRequest
		if err := gproto.Unmarshal(req.Payload, &in); err != nil {
			return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: decode DeleteRelationship request: %w", err)
		}
		out, err := provider.DeleteRelationship(ctx, &in)
		return marshalGatewayResponse(out, err)
	case "SetAuthorizationState":
		var in proto.SetAuthorizationStateRequest
		if err := gproto.Unmarshal(req.Payload, &in); err != nil {
			return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: decode SetAuthorizationState request: %w", err)
		}
		out, err := provider.SetAuthorizationState(ctx, &in)
		return marshalGatewayResponse(out, err)
	case "GetActiveModelRef":
		if len(req.Payload) > 0 {
			var in emptypb.Empty
			if err := gproto.Unmarshal(req.Payload, &in); err != nil {
				return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: decode GetActiveModelRef request: %w", err)
			}
		}
		out, err := provider.GetActiveModelRef(ctx)
		return marshalGatewayResponse(out, err)
	case "SetActiveModel":
		var in proto.SetActiveModelRequest
		if err := gproto.Unmarshal(req.Payload, &in); err != nil {
			return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: decode SetActiveModel request: %w", err)
		}
		out, err := provider.SetActiveModel(ctx, &in)
		return marshalGatewayResponse(out, err)
	case "ListActiveModelResourceTypes":
		var in proto.ListActiveModelResourceTypesRequest
		if err := gproto.Unmarshal(req.Payload, &in); err != nil {
			return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: decode ListActiveModelResourceTypes request: %w", err)
		}
		out, err := provider.ListActiveModelResourceTypes(ctx, &in)
		return marshalGatewayResponse(out, err)
	default:
		return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: unsupported authorization method %q", method)
	}
}

func authorizationMethodPrefix() string {
	return "/" + proto.Authorization_ServiceDesc.ServiceName + "/"
}

func marshalGatewayResponse(msg gproto.Message, err error) (ProviderGatewayResponse, error) {
	if err != nil {
		return ProviderGatewayResponse{}, err
	}
	if msg == nil {
		return ProviderGatewayResponse{}, nil
	}
	payload, err := gproto.Marshal(msg)
	if err != nil {
		return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: encode response: %w", err)
	}
	return ProviderGatewayResponse{Payload: payload}, nil
}
