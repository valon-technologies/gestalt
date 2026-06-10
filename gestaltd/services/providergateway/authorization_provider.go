package providergateway

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

var _ core.AuthorizationProvider = (*AuthorizationProvider)(nil)

type AuthorizationProvider struct {
	providerID string
	gateway    ProviderGateway
	downstream core.AuthorizationProvider
}

func NewAuthorizationProvider(providerID string, gateway ProviderGateway, downstream core.AuthorizationProvider) *AuthorizationProvider {
	return &AuthorizationProvider{
		providerID: providerID,
		gateway:    gateway,
		downstream: downstream,
	}
}

func WrapAuthorizationProviders(providers map[string]core.AuthorizationProvider) map[string]core.AuthorizationProvider {
	if len(providers) == 0 {
		return providers
	}
	wrapped := make(map[string]core.AuthorizationProvider, len(providers))
	for providerID, provider := range providers {
		if provider == nil {
			continue
		}
		gateway := New(WithAuthorizationProvider(providerID, provider))
		wrapped[providerID] = NewAuthorizationProvider(providerID, gateway, provider)
	}
	return wrapped
}

func (p *AuthorizationProvider) CheckAccess(ctx context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	var out proto.CheckAccessResponse
	if err := p.invoke(ctx, "CheckAccess", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (p *AuthorizationProvider) CheckAccessMany(ctx context.Context, req *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	var out proto.CheckAccessManyResponse
	if err := p.invoke(ctx, "CheckAccessMany", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (p *AuthorizationProvider) ListRelationships(ctx context.Context, req *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	var out proto.ListRelationshipsResponse
	if err := p.invoke(ctx, "ListRelationships", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (p *AuthorizationProvider) AddRelationship(ctx context.Context, req *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	var out proto.AddRelationshipResponse
	if err := p.invoke(ctx, "AddRelationship", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (p *AuthorizationProvider) DeleteRelationship(ctx context.Context, req *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	var out proto.DeleteRelationshipResponse
	if err := p.invoke(ctx, "DeleteRelationship", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (p *AuthorizationProvider) SetAuthorizationState(ctx context.Context, req *proto.SetAuthorizationStateRequest) (*proto.SetAuthorizationStateResponse, error) {
	var out proto.SetAuthorizationStateResponse
	if err := p.invoke(ctx, "SetAuthorizationState", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (p *AuthorizationProvider) GetActiveModelRef(ctx context.Context) (*proto.GetActiveModelRefResponse, error) {
	var out proto.GetActiveModelRefResponse
	if err := p.invoke(ctx, "GetActiveModelRef", &emptypb.Empty{}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (p *AuthorizationProvider) SetActiveModel(ctx context.Context, req *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error) {
	var out proto.SetActiveModelResponse
	if err := p.invoke(ctx, "SetActiveModel", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (p *AuthorizationProvider) ListActiveModelResourceTypes(ctx context.Context, req *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	var out proto.ListActiveModelResourceTypesResponse
	if err := p.invoke(ctx, "ListActiveModelResourceTypes", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (p *AuthorizationProvider) Ping(ctx context.Context) error {
	if p == nil || p.downstream == nil {
		return nil
	}
	return p.downstream.Ping(ctx)
}

func (p *AuthorizationProvider) Close() error {
	if p == nil || p.downstream == nil {
		return nil
	}
	return p.downstream.Close()
}

func (p *AuthorizationProvider) invoke(ctx context.Context, operation string, in gproto.Message, out gproto.Message) error {
	if p == nil || p.gateway == nil {
		return fmt.Errorf("provider gateway: authorization provider %q is not configured", p.providerID)
	}
	payload, err := gproto.Marshal(in)
	if err != nil {
		return fmt.Errorf("provider gateway: encode %s request: %w", operation, err)
	}
	resp, err := p.gateway.Invoke(ctx, ProviderGatewayRequest{
		ProviderID:        p.providerID,
		ProviderKind:      ProviderKindAuthorization,
		FullMethod:        "/" + proto.Authorization_ServiceDesc.ServiceName + "/" + operation,
		InvokingSubjectID: invokingSubjectID(ctx, in),
		RequestContext:    RequestContextFromContext(ctx),
		Source:            SourceFromContext(ctx),
		Payload:           payload,
	})
	if err != nil {
		return err
	}
	if out == nil || len(resp.Payload) == 0 {
		return nil
	}
	if err := gproto.Unmarshal(resp.Payload, out); err != nil {
		return fmt.Errorf("provider gateway: decode %s response: %w", operation, err)
	}
	return nil
}

func invokingSubjectID(ctx context.Context, msg gproto.Message) string {
	if subjectID := InvokingSubjectIDFromContext(ctx); subjectID != "" {
		return subjectID
	}
	switch req := msg.(type) {
	case *proto.CheckAccessRequest:
		return req.GetSubject().GetId()
	case *proto.CheckAccessManyRequest:
		if len(req.GetRequests()) > 0 {
			return req.GetRequests()[0].GetSubject().GetId()
		}
	}
	return ""
}
