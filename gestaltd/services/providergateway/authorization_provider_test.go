package providergateway

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	gproto "google.golang.org/protobuf/proto"
)

func TestAuthorizationProviderWrapsCheckAccessInGatewayRequest(t *testing.T) {
	gateway := &recordingGateway{
		response: mustMarshal(t, &proto.CheckAccessResponse{Allowed: true, ModelId: "model-1"}),
	}
	provider := NewAuthorizationProvider("authz", gateway, nil)
	ctx := WithSource(context.Background(), GatewaySourceHTTP)
	ctx = WithInvokingSubjectID(ctx, "user:caller")

	resp, err := provider.CheckAccess(ctx, &proto.CheckAccessRequest{
		Subject:  &proto.Subject{Type: "subject", Id: "user:target"},
		Action:   &proto.Action{Name: "view"},
		Resource: &proto.Resource{Type: "team", Id: "servicing"},
	})
	if err != nil {
		t.Fatalf("CheckAccess: %v", err)
	}
	if !resp.GetAllowed() || resp.GetModelId() != "model-1" {
		t.Fatalf("response = %+v, want allowed model-1", resp)
	}

	got := gateway.request
	if got.ProviderID != "authz" {
		t.Fatalf("ProviderID = %q, want authz", got.ProviderID)
	}
	if got.ProviderKind != ProviderKindAuthorization {
		t.Fatalf("ProviderKind = %q, want %q", got.ProviderKind, ProviderKindAuthorization)
	}
	if got.FullMethod != "/gestalt.provider.v1.Authorization/CheckAccess" {
		t.Fatalf("FullMethod = %q", got.FullMethod)
	}
	if got.Source != GatewaySourceHTTP {
		t.Fatalf("Source = %q, want %q", got.Source, GatewaySourceHTTP)
	}
	if got.InvokingSubjectID != "user:caller" {
		t.Fatalf("InvokingSubjectID = %q, want user:caller", got.InvokingSubjectID)
	}
	var payload proto.CheckAccessRequest
	if err := gproto.Unmarshal(got.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.GetSubject().GetId() != "user:target" {
		t.Fatalf("payload subject = %q, want user:target", payload.GetSubject().GetId())
	}
}

func TestGatewayInvokesAuthorizationProvider(t *testing.T) {
	authz := &stubAuthorizationProvider{
		checkAccess: &proto.CheckAccessResponse{Allowed: true},
	}
	gateway := New(WithAuthorizationProvider("authz", authz))
	payload := mustMarshal(t, &proto.CheckAccessRequest{
		Subject:  &proto.Subject{Type: "subject", Id: "user:alice"},
		Action:   &proto.Action{Name: "manage"},
		Resource: &proto.Resource{Type: "team", Id: "servicing"},
	})

	resp, err := gateway.Invoke(context.Background(), ProviderGatewayRequest{
		ProviderID:   "authz",
		ProviderKind: ProviderKindAuthorization,
		FullMethod:   "/gestalt.provider.v1.Authorization/CheckAccess",
		Payload:      payload,
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	var out proto.CheckAccessResponse
	if err := gproto.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !out.GetAllowed() {
		t.Fatal("allowed = false, want true")
	}
	if authz.lastCheckAccess.GetSubject().GetId() != "user:alice" {
		t.Fatalf("provider subject = %q, want user:alice", authz.lastCheckAccess.GetSubject().GetId())
	}
}

func TestAuthorizationProviderDoesNotInferInvokingSubjectFromPayload(t *testing.T) {
	gateway := &recordingGateway{
		response: mustMarshal(t, &proto.CheckAccessResponse{Allowed: true}),
	}
	provider := NewAuthorizationProvider("authz", gateway, nil)

	_, err := provider.CheckAccess(context.Background(), &proto.CheckAccessRequest{
		Subject:  &proto.Subject{Type: "subject", Id: "user:checked"},
		Action:   &proto.Action{Name: "view"},
		Resource: &proto.Resource{Type: "team", Id: "servicing"},
	})
	if err != nil {
		t.Fatalf("CheckAccess: %v", err)
	}
	if gateway.request.InvokingSubjectID != "" {
		t.Fatalf("InvokingSubjectID = %q, want empty", gateway.request.InvokingSubjectID)
	}
}

type recordingGateway struct {
	request  ProviderGatewayRequest
	response []byte
}

func (g *recordingGateway) Invoke(_ context.Context, req ProviderGatewayRequest) (ProviderGatewayResponse, error) {
	g.request = req
	return ProviderGatewayResponse{Payload: g.response}, nil
}

type stubAuthorizationProvider struct {
	core.AuthorizationProvider
	checkAccess     *proto.CheckAccessResponse
	lastCheckAccess *proto.CheckAccessRequest
}

func (p *stubAuthorizationProvider) CheckAccess(_ context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	p.lastCheckAccess = req
	return p.checkAccess, nil
}

func mustMarshal(t *testing.T, msg gproto.Message) []byte {
	t.Helper()
	payload, err := gproto.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return payload
}
