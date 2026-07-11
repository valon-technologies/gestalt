package coretesting

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

// StubAuthorizationProvider is a no-op authorization provider for tests.
// When Allowed is nil, CheckAccess allows requests.
type StubAuthorizationProvider struct {
	Allowed *bool

	Called   bool
	Ctx      context.Context
	Request  *proto.CheckAccessRequest
	Requests []*proto.CheckAccessRequest

	CheckAccessFn func(context.Context, *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error)
}

// NewStubAuthorizationProvider returns an authorization stub that allows all requests.
func NewStubAuthorizationProvider() *StubAuthorizationProvider {
	return &StubAuthorizationProvider{}
}

func (p *StubAuthorizationProvider) CheckAccess(ctx context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	if p == nil {
		return &proto.CheckAccessResponse{}, nil
	}
	if p.CheckAccessFn != nil {
		return p.CheckAccessFn(ctx, req)
	}
	p.Called = true
	p.Ctx = ctx
	p.Request = req
	p.Requests = append(p.Requests, req)
	allowed := true
	if p.Allowed != nil {
		allowed = *p.Allowed
	}
	return &proto.CheckAccessResponse{Allowed: allowed}, nil
}

func (p *StubAuthorizationProvider) CheckAccessMany(ctx context.Context, req *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	resp := &proto.CheckAccessManyResponse{Decisions: make([]*proto.CheckAccessResponse, 0, len(req.GetRequests()))}
	for _, check := range req.GetRequests() {
		decision, err := p.CheckAccess(ctx, check)
		if err != nil {
			return nil, err
		}
		resp.Decisions = append(resp.Decisions, decision)
	}
	return resp, nil
}

func (p *StubAuthorizationProvider) ListRelationships(context.Context, *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	return &proto.ListRelationshipsResponse{}, nil
}

func (p *StubAuthorizationProvider) AddRelationship(context.Context, *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	return &proto.AddRelationshipResponse{}, nil
}

func (p *StubAuthorizationProvider) DeleteRelationship(context.Context, *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	return &proto.DeleteRelationshipResponse{}, nil
}

func (p *StubAuthorizationProvider) SetAuthorizationState(context.Context, *proto.SetAuthorizationStateRequest) (*proto.SetAuthorizationStateResponse, error) {
	return &proto.SetAuthorizationStateResponse{}, nil
}

func (p *StubAuthorizationProvider) GetActiveModelRef(context.Context) (*proto.GetActiveModelRefResponse, error) {
	return &proto.GetActiveModelRefResponse{}, nil
}

func (p *StubAuthorizationProvider) SetActiveModel(context.Context, *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error) {
	return &proto.SetActiveModelResponse{}, nil
}

func (p *StubAuthorizationProvider) ListActiveModelResourceTypes(context.Context, *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	return &proto.ListActiveModelResourceTypesResponse{}, nil
}

func (p *StubAuthorizationProvider) Ping(context.Context) error { return nil }

func (p *StubAuthorizationProvider) Close() error { return nil }

// AppRoutingStub returns a catalog-only app provider stub for remote routing tests.
func AppRoutingStub(name, operation string) *StubIntegration {
	return &StubIntegration{
		N:        name,
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Operations: []catalog.CatalogOperation{{ID: operation}},
		},
	}
}

// BoolPtr returns a pointer to the given bool value.
func BoolPtr(value bool) *bool {
	return &value
}
