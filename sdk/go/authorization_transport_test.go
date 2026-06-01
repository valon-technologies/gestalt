package gestalt_test

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

type authorizationTransportHarness struct {
	proto.UnimplementedAuthorizationProviderServer

	mu                    sync.Mutex
	checks                []*proto.CheckAccessRequest
	checkMany             []*proto.CheckAccessManyRequest
	lists                 []*proto.ListRelationshipsRequest
	adds                  []*proto.AddRelationshipRequest
	deletes               []*proto.DeleteRelationshipRequest
	sets                  []*proto.SetRelationshipsRequest
	setModels             []*proto.SetActiveModelRequest
	listResourceTypes     []*proto.ListActiveModelResourceTypesRequest
	getActiveModelRefHits int
	tokens                []string
}

func (h *authorizationTransportHarness) recordToken(ctx context.Context) {
	md, _ := metadata.FromIncomingContext(ctx)
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
}

func (h *authorizationTransportHarness) CheckAccess(ctx context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recordToken(ctx)
	h.checks = append(h.checks, gproto.Clone(req).(*proto.CheckAccessRequest))
	return &proto.CheckAccessResponse{Allowed: true, ModelId: "authz-model-1"}, nil
}

func (h *authorizationTransportHarness) CheckAccessMany(ctx context.Context, req *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recordToken(ctx)
	h.checkMany = append(h.checkMany, gproto.Clone(req).(*proto.CheckAccessManyRequest))
	return &proto.CheckAccessManyResponse{Decisions: []*proto.CheckAccessResponse{{Allowed: true, ModelId: "authz-model-1"}}}, nil
}

func (h *authorizationTransportHarness) ListRelationships(ctx context.Context, req *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recordToken(ctx)
	h.lists = append(h.lists, gproto.Clone(req).(*proto.ListRelationshipsRequest))
	return &proto.ListRelationshipsResponse{
		Relationships: []*proto.Relationship{relationshipProto("user", "user-123", "editor", "agent_session", "session-123")},
		NextPageToken: "next-page",
	}, nil
}

func (h *authorizationTransportHarness) AddRelationship(ctx context.Context, req *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recordToken(ctx)
	h.adds = append(h.adds, gproto.Clone(req).(*proto.AddRelationshipRequest))
	return &proto.AddRelationshipResponse{Relationship: req.GetRelationship()}, nil
}

func (h *authorizationTransportHarness) DeleteRelationship(ctx context.Context, req *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recordToken(ctx)
	h.deletes = append(h.deletes, gproto.Clone(req).(*proto.DeleteRelationshipRequest))
	return &proto.DeleteRelationshipResponse{}, nil
}

func (h *authorizationTransportHarness) SetRelationships(ctx context.Context, req *proto.SetRelationshipsRequest) (*proto.SetRelationshipsResponse, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recordToken(ctx)
	h.sets = append(h.sets, gproto.Clone(req).(*proto.SetRelationshipsRequest))
	return &proto.SetRelationshipsResponse{Relationships: req.GetRelationships()}, nil
}

func (h *authorizationTransportHarness) GetActiveModelRef(ctx context.Context, _ *emptypb.Empty) (*proto.GetActiveModelRefResponse, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recordToken(ctx)
	h.getActiveModelRefHits++
	return &proto.GetActiveModelRefResponse{Model: &proto.AuthorizationModelRef{Id: "authz-model-1", Version: "v1"}}, nil
}

func (h *authorizationTransportHarness) SetActiveModel(ctx context.Context, req *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recordToken(ctx)
	h.setModels = append(h.setModels, gproto.Clone(req).(*proto.SetActiveModelRequest))
	return &proto.SetActiveModelResponse{Model: &proto.AuthorizationModelRef{Id: req.GetModel().GetId(), Version: req.GetModel().GetVersion()}}, nil
}

func (h *authorizationTransportHarness) ListActiveModelResourceTypes(ctx context.Context, req *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recordToken(ctx)
	h.listResourceTypes = append(h.listResourceTypes, gproto.Clone(req).(*proto.ListActiveModelResourceTypesRequest))
	return &proto.ListActiveModelResourceTypesResponse{ResourceTypes: []*proto.AuthorizationModelResourceType{{Name: "agent_session"}}}, nil
}

func TestTransport_AuthorizationTCPTargetTokenEnv(t *testing.T) {
	address := reserveTCPAddress()
	lis, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	harness := &authorizationTransportHarness{}
	srv := grpc.NewServer()
	proto.RegisterAuthorizationProviderServer(srv, harness)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	t.Setenv(gestalt.EnvHostServiceSocket, "tcp://"+address)
	t.Setenv(gestalt.EnvHostServiceToken, "relay-token-go")

	client, err := gestalt.Authorization()
	if err != nil {
		t.Fatalf("Authorization: %v", err)
	}

	subject := gestalt.NewAuthorizationSubject("user", "user-123")
	action := gestalt.NewAuthorizationAction("edit")
	resource := gestalt.NewAuthorizationResource("agent_session", "session-123")
	relationship := gestalt.NewRelationship(subject, "editor", resource)

	if resp, err := client.CheckAccess(context.Background(), gestalt.NewCheckAccessRequest(subject, action, resource)); err != nil {
		t.Fatalf("CheckAccess: %v", err)
	} else if !resp.GetAllowed() || resp.GetModelId() != "authz-model-1" {
		t.Fatalf("CheckAccess response = %#v, want allowed authz-model-1", resp)
	}
	if resp, err := client.CheckAccessMany(context.Background(), &gestalt.CheckAccessManyRequest{Requests: []*gestalt.CheckAccessRequest{gestalt.NewCheckAccessRequest(subject, action, resource)}}); err != nil {
		t.Fatalf("CheckAccessMany: %v", err)
	} else if len(resp.GetDecisions()) != 1 || !resp.GetDecisions()[0].GetAllowed() {
		t.Fatalf("CheckAccessMany response = %#v, want one allowed decision", resp)
	}
	if resp, err := client.ListRelationships(context.Background(), &gestalt.ListRelationshipsRequest{Filter: &gestalt.RelationshipFilter{Resource: resource}, PageSize: 10}); err != nil {
		t.Fatalf("ListRelationships: %v", err)
	} else if len(resp.GetRelationships()) != 1 || resp.GetNextPageToken() != "next-page" {
		t.Fatalf("ListRelationships response = %#v, want one relationship and next page", resp)
	}
	if resp, err := client.AddRelationship(context.Background(), &gestalt.AddRelationshipRequest{Relationship: relationship}); err != nil {
		t.Fatalf("AddRelationship: %v", err)
	} else if resp.GetRelationship().GetTuple().GetRelation() != "editor" {
		t.Fatalf("AddRelationship response = %#v, want editor", resp)
	}
	if _, err := client.DeleteRelationship(context.Background(), &gestalt.DeleteRelationshipRequest{RelationshipTuple: relationship.GetTuple()}); err != nil {
		t.Fatalf("DeleteRelationship: %v", err)
	}
	if resp, err := client.SetRelationships(context.Background(), &gestalt.SetRelationshipsRequest{Relationships: []*gestalt.Relationship{relationship}}); err != nil {
		t.Fatalf("SetRelationships: %v", err)
	} else if len(resp.GetRelationships()) != 1 {
		t.Fatalf("SetRelationships response = %#v, want one relationship", resp)
	}
	if resp, err := client.GetActiveModelRef(context.Background()); err != nil {
		t.Fatalf("GetActiveModelRef: %v", err)
	} else if resp.GetModel().GetId() != "authz-model-1" {
		t.Fatalf("GetActiveModelRef response = %#v, want authz-model-1", resp)
	}
	if resp, err := client.SetActiveModel(context.Background(), &gestalt.SetActiveModelRequest{Model: &gestalt.AuthorizationModel{Id: "authz-model-2", Version: "v2"}}); err != nil {
		t.Fatalf("SetActiveModel: %v", err)
	} else if resp.GetModel().GetId() != "authz-model-2" {
		t.Fatalf("SetActiveModel response = %#v, want authz-model-2", resp)
	}
	if resp, err := client.ListActiveModelResourceTypes(context.Background(), &gestalt.ListActiveModelResourceTypesRequest{ModelId: "authz-model-1"}); err != nil {
		t.Fatalf("ListActiveModelResourceTypes: %v", err)
	} else if len(resp.GetResourceTypes()) != 1 || resp.GetResourceTypes()[0].GetName() != "agent_session" {
		t.Fatalf("ListActiveModelResourceTypes response = %#v, want agent_session", resp)
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.tokens) != 9 {
		t.Fatalf("relay tokens = %#v, want nine relay-token-go entries", harness.tokens)
	}
	for i, token := range harness.tokens {
		if token != "relay-token-go" {
			t.Fatalf("relay token %d = %q, want relay-token-go", i, token)
		}
	}
	if harness.checks[0].GetSubject().GetId() != "user-123" {
		t.Fatalf("check subject = %q, want user-123", harness.checks[0].GetSubject().GetId())
	}
	if harness.lists[0].GetFilter().GetResource().GetId() != "session-123" {
		t.Fatalf("list resource = %q, want session-123", harness.lists[0].GetFilter().GetResource().GetId())
	}
	if harness.adds[0].GetRelationship().GetTuple().GetRelation() != "editor" {
		t.Fatalf("add relationship = %#v, want editor", harness.adds[0].GetRelationship())
	}
	if harness.deletes[0].GetRelationshipTuple().GetResource().GetId() != "session-123" {
		t.Fatalf("delete tuple = %#v, want session resource", harness.deletes[0].GetRelationshipTuple())
	}
	if harness.setModels[0].GetModel().GetId() != "authz-model-2" {
		t.Fatalf("set model = %#v, want authz-model-2", harness.setModels[0].GetModel())
	}
}

type fullAuthorizationProvider struct {
	closeTracker
}

func (*fullAuthorizationProvider) Configure(context.Context, string, map[string]any) error {
	return nil
}

func (*fullAuthorizationProvider) Metadata() gestalt.ProviderMetadata {
	return gestalt.ProviderMetadata{
		Kind:        gestalt.ProviderKindAuthorization,
		Name:        "stub-authorization",
		DisplayName: "Stub Authorization",
		Version:     "1.0",
	}
}

func (*fullAuthorizationProvider) CheckAccess(context.Context, *gestalt.CheckAccessRequest) (*gestalt.CheckAccessResponse, error) {
	return &gestalt.CheckAccessResponse{Allowed: true, ModelId: "authz-model-1"}, nil
}

func (*fullAuthorizationProvider) CheckAccessMany(_ context.Context, req *gestalt.CheckAccessManyRequest) (*gestalt.CheckAccessManyResponse, error) {
	resp := &gestalt.CheckAccessManyResponse{}
	for range req.GetRequests() {
		resp.Decisions = append(resp.Decisions, &gestalt.CheckAccessResponse{Allowed: true, ModelId: "authz-model-1"})
	}
	return resp, nil
}

func (*fullAuthorizationProvider) ListRelationships(context.Context, *gestalt.ListRelationshipsRequest) (*gestalt.ListRelationshipsResponse, error) {
	return &gestalt.ListRelationshipsResponse{
		Relationships: []*gestalt.Relationship{gestalt.NewRelationship(gestalt.NewAuthorizationSubject("user", "user-123"), "editor", gestalt.NewAuthorizationResource("agent_session", "session-123"))},
		NextPageToken: "next-page",
	}, nil
}

func (*fullAuthorizationProvider) AddRelationship(_ context.Context, req *gestalt.AddRelationshipRequest) (*gestalt.AddRelationshipResponse, error) {
	return &gestalt.AddRelationshipResponse{Relationship: req.GetRelationship()}, nil
}

func (*fullAuthorizationProvider) DeleteRelationship(context.Context, *gestalt.DeleteRelationshipRequest) (*gestalt.DeleteRelationshipResponse, error) {
	return &gestalt.DeleteRelationshipResponse{}, nil
}

func (*fullAuthorizationProvider) SetRelationships(_ context.Context, req *gestalt.SetRelationshipsRequest) (*gestalt.SetRelationshipsResponse, error) {
	return &gestalt.SetRelationshipsResponse{Relationships: req.GetRelationships()}, nil
}

func (*fullAuthorizationProvider) GetActiveModelRef(context.Context) (*gestalt.GetActiveModelRefResponse, error) {
	return &gestalt.GetActiveModelRefResponse{Model: gestalt.NewAuthorizationModelRef("authz-model-1", "v1", time.Unix(1, 0))}, nil
}

func (*fullAuthorizationProvider) SetActiveModel(_ context.Context, req *gestalt.SetActiveModelRequest) (*gestalt.SetActiveModelResponse, error) {
	return &gestalt.SetActiveModelResponse{Model: gestalt.NewAuthorizationModelRef(req.GetModel().GetId(), req.GetModel().GetVersion(), time.Unix(1, 0))}, nil
}

func (*fullAuthorizationProvider) ListActiveModelResourceTypes(context.Context, *gestalt.ListActiveModelResourceTypesRequest) (*gestalt.ListActiveModelResourceTypesResponse, error) {
	return &gestalt.ListActiveModelResourceTypesResponse{ResourceTypes: []*gestalt.AuthorizationModelResourceType{{Name: "agent_session"}}}, nil
}

func TestServeAuthorizationProviderTransport(t *testing.T) {
	socket := newSocketPath(t, "authorization.sock")
	t.Setenv(proto.EnvProviderSocket, socket)

	ctx, cancel := context.WithCancel(context.Background())
	provider := &fullAuthorizationProvider{}
	errCh := make(chan error, 1)
	go func() {
		errCh <- gestalt.ServeAuthorizationProvider(ctx, provider)
	}()
	t.Cleanup(func() {
		cancel()
		waitServeResult(t, errCh)
		if !provider.closed.Load() {
			t.Fatal("authorization provider Close was not called")
		}
	})

	conn := newUnixConn(t, socket)
	client := proto.NewAuthorizationProviderClient(conn)

	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer rpcCancel()

	checkResp, err := client.CheckAccess(rpcCtx, &proto.CheckAccessRequest{
		Subject:  &proto.Subject{Type: "user", Id: "user-123"},
		Action:   &proto.Action{Name: "edit"},
		Resource: &proto.Resource{Type: "agent_session", Id: "session-123"},
	}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("CheckAccess: %v", err)
	}
	if !checkResp.GetAllowed() || checkResp.GetModelId() != "authz-model-1" {
		t.Fatalf("CheckAccess response = %#v, want allowed authz-model-1", checkResp)
	}

	listResp, err := client.ListRelationships(rpcCtx, &proto.ListRelationshipsRequest{}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("ListRelationships: %v", err)
	}
	if len(listResp.GetRelationships()) != 1 || listResp.GetRelationships()[0].GetTuple().GetRelation() != "editor" {
		t.Fatalf("ListRelationships response = %#v, want editor relationship", listResp)
	}

	modelResp, err := client.GetActiveModelRef(rpcCtx, &emptypb.Empty{}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("GetActiveModelRef: %v", err)
	}
	if modelResp.GetModel().GetId() != "authz-model-1" {
		t.Fatalf("GetActiveModelRef response = %#v, want authz-model-1", modelResp)
	}
}

func relationshipProto(subjectType, subjectID, relation, resourceType, resourceID string) *proto.Relationship {
	return &proto.Relationship{
		Tuple: &proto.RelationshipTuple{
			Target:   &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{Type: subjectType, Id: subjectID}}},
			Relation: relation,
			Resource: &proto.Resource{Type: resourceType, Id: resourceID},
		},
	}
}
