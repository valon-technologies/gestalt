package gestalt_test

import (
	"context"
	"net"
	"sync"
	"testing"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

type authorizationTransportHarness struct {
	proto.UnimplementedAuthorizationProviderServer

	mu       sync.Mutex
	requests []*proto.SubjectSearchRequest
	writes   []*proto.WriteRelationshipsRequest
	tokens   []string
}

func (h *authorizationTransportHarness) SearchSubjects(ctx context.Context, req *proto.SubjectSearchRequest) (*proto.SubjectSearchResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.requests = append(h.requests, gproto.Clone(req).(*proto.SubjectSearchRequest))
	h.mu.Unlock()

	return &proto.SubjectSearchResponse{
		Subjects: []*proto.Subject{{
			Type: "user",
			Id:   "user:user-123",
		}},
		ModelId: "authz-model-1",
	}, nil
}

func (h *authorizationTransportHarness) WriteRelationships(ctx context.Context, req *proto.WriteRelationshipsRequest) (*emptypb.Empty, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.writes = append(h.writes, gproto.Clone(req).(*proto.WriteRelationshipsRequest))
	h.mu.Unlock()

	return &emptypb.Empty{}, nil
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

	t.Setenv(gestalt.EnvAuthorizationSocket, "tcp://"+address)
	t.Setenv(gestalt.EnvAuthorizationSocketToken, "relay-token-go")

	client, err := gestalt.Authorization()
	if err != nil {
		t.Fatalf("Authorization: %v", err)
	}
	defer func() { _ = client.Close() }()

	resp, err := client.SearchSubjects(context.Background(), &gestalt.SubjectSearchRequest{
		SubjectType: "user",
		Resource:    gestalt.NewAuthorizationResource("slack_identity", "team:T123:user:U456"),
		Action:      gestalt.NewAuthorizationAction("assume"),
		PageSize:    1,
	})
	if err != nil {
		t.Fatalf("SearchSubjects: %v", err)
	}
	if resp.GetModelId() != "authz-model-1" {
		t.Fatalf("model id = %q, want %q", resp.GetModelId(), "authz-model-1")
	}
	if len(resp.GetSubjects()) != 1 || resp.GetSubjects()[0].GetId() != "user:user-123" {
		t.Fatalf("subjects = %#v, want [user:user-123]", resp.GetSubjects())
	}
	if err := client.WriteRelationships(context.Background(), gestalt.NewWriteRelationshipsRequest(
		[]*gestalt.Relationship{
			gestalt.NewRelationship(
				gestalt.NewAuthorizationSubject("subject", "user:user-123"),
				"editor",
				gestalt.NewAuthorizationResource("agent_session", "session-123"),
			),
		},
		nil,
	)); err != nil {
		t.Fatalf("WriteRelationships: %v", err)
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.tokens) != 2 || harness.tokens[0] != "relay-token-go" || harness.tokens[1] != "relay-token-go" {
		t.Fatalf("relay tokens = %#v, want two relay-token-go entries", harness.tokens)
	}
	if len(harness.requests) != 1 {
		t.Fatalf("search subject requests len = %d, want 1", len(harness.requests))
	}
	if harness.requests[0].GetSubjectType() != "user" {
		t.Fatalf("subject type = %q, want %q", harness.requests[0].GetSubjectType(), "user")
	}
	if harness.requests[0].GetResource().GetId() != "team:T123:user:U456" {
		t.Fatalf("resource id = %q, want %q", harness.requests[0].GetResource().GetId(), "team:T123:user:U456")
	}
	if harness.requests[0].GetAction().GetName() != "assume" {
		t.Fatalf("action name = %q, want %q", harness.requests[0].GetAction().GetName(), "assume")
	}
	if len(harness.writes) != 1 {
		t.Fatalf("write relationship requests len = %d, want 1", len(harness.writes))
	}
	write := harness.writes[0].GetWrites()[0]
	if write.GetSubject().GetType() != "subject" || write.GetSubject().GetId() != "user:user-123" {
		t.Fatalf("write subject = %#v, want subject/user:user-123", write.GetSubject())
	}
	if write.GetRelation() != "editor" {
		t.Fatalf("write relation = %q, want editor", write.GetRelation())
	}
	if write.GetResource().GetType() != "agent_session" || write.GetResource().GetId() != "session-123" {
		t.Fatalf("write resource = %#v, want agent_session/session-123", write.GetResource())
	}
}
