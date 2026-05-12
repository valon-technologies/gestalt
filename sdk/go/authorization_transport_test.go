package gestalt_test

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

type authorizationTransportHarness struct {
	proto.UnimplementedAuthorizationProviderServer

	mu                        sync.Mutex
	requests                  []*proto.SubjectSearchRequest
	effectiveResourceRequests []*proto.ResourceSearchRequest
	effectiveSubjectRequests  []*proto.EffectiveSubjectSearchRequest
	expands                   []*proto.ExpandRequest
	writes                    []*proto.WriteRelationshipsRequest
	tokens                    []string
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

func (h *authorizationTransportHarness) EffectiveSearchResources(ctx context.Context, req *proto.ResourceSearchRequest) (*proto.ResourceSearchResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.effectiveResourceRequests = append(h.effectiveResourceRequests, gproto.Clone(req).(*proto.ResourceSearchRequest))
	h.mu.Unlock()

	return &proto.ResourceSearchResponse{
		Resources: []*proto.Resource{{
			Type: "agent_session",
			Id:   "session-123",
		}},
		ModelId: "authz-model-1",
	}, nil
}

func (h *authorizationTransportHarness) EffectiveSearchSubjects(ctx context.Context, req *proto.EffectiveSubjectSearchRequest) (*proto.EffectiveSubjectSearchResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.effectiveSubjectRequests = append(h.effectiveSubjectRequests, gproto.Clone(req).(*proto.EffectiveSubjectSearchRequest))
	h.mu.Unlock()

	return &proto.EffectiveSubjectSearchResponse{
		Targets: []*proto.RelationshipTarget{
			gestalt.NewAuthorizationSubjectTarget(gestalt.NewAuthorizationSubject("subject", "user:user-123")),
			gestalt.NewAuthorizationSubjectSetTarget(gestalt.NewAuthorizationResource("slack_channel", "C123"), "member"),
		},
		ModelId:   "authz-model-1",
		Truncated: true,
	}, nil
}

func (h *authorizationTransportHarness) Expand(ctx context.Context, req *proto.ExpandRequest) (*proto.ExpandResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.expands = append(h.expands, gproto.Clone(req).(*proto.ExpandRequest))
	h.mu.Unlock()

	return &proto.ExpandResponse{
		Root: &proto.ExpandNode{
			Target:   gestalt.NewAuthorizationResourceTarget(req.GetResource()),
			Relation: req.GetRelation(),
			Children: []*proto.ExpandNode{{
				Target:   gestalt.NewAuthorizationSubjectTarget(gestalt.NewAuthorizationSubject("subject", "user:user-123")),
				Relation: "member",
			}},
		},
		ModelId:         "authz-model-1",
		MaxDepthReached: true,
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
	resourceResp, err := client.EffectiveSearchResources(context.Background(), &gestalt.ResourceSearchRequest{
		Subject:      gestalt.NewAuthorizationSubject("subject", "user:user-123"),
		Action:       gestalt.NewAuthorizationAction("edit"),
		ResourceType: "agent_session",
	})
	if err != nil {
		t.Fatalf("EffectiveSearchResources: %v", err)
	}
	if len(resourceResp.GetResources()) != 1 || resourceResp.GetResources()[0].GetId() != "session-123" {
		t.Fatalf("effective resources = %#v, want [session-123]", resourceResp.GetResources())
	}
	targetResp, err := client.EffectiveSearchSubjects(context.Background(), &gestalt.EffectiveSubjectSearchRequest{
		Resource: gestalt.NewAuthorizationResource("agent_session", "session-123"),
		Action:   gestalt.NewAuthorizationAction("edit"),
	})
	if err != nil {
		t.Fatalf("EffectiveSearchSubjects: %v", err)
	}
	if len(targetResp.GetTargets()) != 2 || targetResp.GetTargets()[1].GetSubjectSet().GetRelation() != "member" {
		t.Fatalf("effective targets = %#v, want subject and slack_channel#member", targetResp.GetTargets())
	}
	if !targetResp.GetTruncated() {
		t.Fatal("effective subject response truncated = false, want true")
	}
	expandResp, err := client.Expand(context.Background(), &gestalt.ExpandRequest{
		Resource: gestalt.NewAuthorizationResource("agent_session", "session-123"),
		Relation: "editor",
		MaxDepth: 1,
	})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if expandResp.GetRoot().GetTarget().GetResource().GetType() != "agent_session" {
		t.Fatalf("expand root = %#v, want agent_session resource target", expandResp.GetRoot().GetTarget())
	}
	if !expandResp.GetMaxDepthReached() {
		t.Fatal("expand max_depth_reached = false, want true")
	}
	if err := client.WriteRelationships(context.Background(), gestalt.NewWriteRelationshipsRequest(
		[]*gestalt.Relationship{
			gestalt.NewRelationshipWithTarget(
				gestalt.NewAuthorizationSubjectSetTarget(gestalt.NewAuthorizationResource("slack_channel", "C123"), "member"),
				"editor",
				gestalt.NewAuthorizationResource("agent_session", "session-123"),
			),
		},
		nil,
	)); err != nil {
		t.Fatalf("WriteRelationships: %v", err)
	}
	if err := client.GrantAgentSessionEditor(context.Background(), "user:user-123", "session-123"); err != nil {
		t.Fatalf("GrantAgentSessionEditor: %v", err)
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.tokens) != 6 {
		t.Fatalf("relay tokens = %#v, want six relay-token-go entries", harness.tokens)
	}
	for i, token := range harness.tokens {
		if token != "relay-token-go" {
			t.Fatalf("relay token %d = %q, want relay-token-go", i, token)
		}
	}
	if len(harness.requests) != 1 {
		t.Fatalf("search subject requests len = %d, want 1", len(harness.requests))
	}
	if len(harness.effectiveResourceRequests) != 1 {
		t.Fatalf("effective resource requests len = %d, want 1", len(harness.effectiveResourceRequests))
	}
	if harness.effectiveResourceRequests[0].GetResourceType() != "agent_session" {
		t.Fatalf("effective resource type = %q, want agent_session", harness.effectiveResourceRequests[0].GetResourceType())
	}
	if len(harness.effectiveSubjectRequests) != 1 {
		t.Fatalf("effective subject requests len = %d, want 1", len(harness.effectiveSubjectRequests))
	}
	if harness.effectiveSubjectRequests[0].GetResource().GetId() != "session-123" {
		t.Fatalf("effective subject resource = %q, want session-123", harness.effectiveSubjectRequests[0].GetResource().GetId())
	}
	if len(harness.expands) != 1 {
		t.Fatalf("expand requests len = %d, want 1", len(harness.expands))
	}
	if harness.expands[0].GetRelation() != "editor" || harness.expands[0].GetMaxDepth() != 1 {
		t.Fatalf("expand request = %#v, want editor max_depth 1", harness.expands[0])
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
	if len(harness.writes) != 2 {
		t.Fatalf("write relationship requests len = %d, want 2", len(harness.writes))
	}
	write := harness.writes[0].GetWrites()[0]
	if write.GetTarget().GetSubjectSet().GetResource().GetType() != "slack_channel" {
		t.Fatalf("write target = %#v, want slack_channel subject set", write.GetTarget())
	}
	if write.GetTarget().GetSubjectSet().GetRelation() != "member" {
		t.Fatalf("write subject set relation = %q, want member", write.GetTarget().GetSubjectSet().GetRelation())
	}
	if write.GetRelation() != "editor" {
		t.Fatalf("write relation = %q, want editor", write.GetRelation())
	}
	if write.GetResource().GetType() != "agent_session" || write.GetResource().GetId() != "session-123" {
		t.Fatalf("write resource = %#v, want agent_session/session-123", write.GetResource())
	}
	helperWrite := harness.writes[1].GetWrites()[0]
	expectedHelperWrite := gestalt.NewAgentSessionEditorWriteRequest("user:user-123", "session-123").GetWrites()[0]
	if !gproto.Equal(helperWrite, expectedHelperWrite) {
		t.Fatalf("helper write = %#v, want %#v", helperWrite, expectedHelperWrite)
	}
	if helperWrite.GetSubject().GetType() != "subject" || helperWrite.GetSubject().GetId() != "user:user-123" {
		t.Fatalf("helper write subject = %#v, want subject/user:user-123", helperWrite.GetSubject())
	}
	if helperWrite.GetTarget().GetSubject().GetType() != "subject" || helperWrite.GetTarget().GetSubject().GetId() != "user:user-123" {
		t.Fatalf("helper write target subject = %#v, want subject/user:user-123", helperWrite.GetTarget().GetSubject())
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

func (*fullAuthorizationProvider) Evaluate(context.Context, *gestalt.AccessEvaluationRequest) (*gestalt.AccessDecision, error) {
	return &gestalt.AccessDecision{Allowed: true, ModelId: "authz-model-1"}, nil
}

func (*fullAuthorizationProvider) EvaluateMany(_ context.Context, req *gestalt.AccessEvaluationsRequest) (*gestalt.AccessEvaluationsResponse, error) {
	resp := &gestalt.AccessEvaluationsResponse{}
	for range req.GetRequests() {
		resp.Decisions = append(resp.Decisions, &gestalt.AccessDecision{Allowed: true, ModelId: "authz-model-1"})
	}
	return resp, nil
}

func (*fullAuthorizationProvider) SearchResources(context.Context, *gestalt.ResourceSearchRequest) (*gestalt.ResourceSearchResponse, error) {
	return &gestalt.ResourceSearchResponse{ModelId: "authz-model-1"}, nil
}

func (*fullAuthorizationProvider) SearchSubjects(context.Context, *gestalt.SubjectSearchRequest) (*gestalt.SubjectSearchResponse, error) {
	return &gestalt.SubjectSearchResponse{ModelId: "authz-model-1"}, nil
}

func (*fullAuthorizationProvider) SearchActions(context.Context, *gestalt.ActionSearchRequest) (*gestalt.ActionSearchResponse, error) {
	return &gestalt.ActionSearchResponse{ModelId: "authz-model-1"}, nil
}

func (*fullAuthorizationProvider) GetMetadata(context.Context) (*gestalt.AuthorizationMetadata, error) {
	return &gestalt.AuthorizationMetadata{ActiveModelId: "authz-model-1"}, nil
}

func (*fullAuthorizationProvider) ReadRelationships(context.Context, *gestalt.ReadRelationshipsRequest) (*gestalt.ReadRelationshipsResponse, error) {
	return &gestalt.ReadRelationshipsResponse{ModelId: "authz-model-1"}, nil
}

func (*fullAuthorizationProvider) WriteRelationships(context.Context, *gestalt.WriteRelationshipsRequest) error {
	return nil
}

func (*fullAuthorizationProvider) GetActiveModel(context.Context) (*gestalt.GetActiveModelResponse, error) {
	return &gestalt.GetActiveModelResponse{Model: gestalt.NewAuthorizationModelRef("authz-model-1", "v1", time.Unix(1, 0))}, nil
}

func (*fullAuthorizationProvider) ListModels(context.Context, *gestalt.ListModelsRequest) (*gestalt.ListModelsResponse, error) {
	return &gestalt.ListModelsResponse{Models: []*gestalt.AuthorizationModelRef{
		gestalt.NewAuthorizationModelRef("authz-model-1", "v1", time.Unix(1, 0)),
	}}, nil
}

func (*fullAuthorizationProvider) WriteModel(context.Context, *gestalt.WriteModelRequest) (*gestalt.AuthorizationModelRef, error) {
	return gestalt.NewAuthorizationModelRef("authz-model-1", "v1", time.Unix(1, 0)), nil
}

type zanzibarAuthorizationProvider struct {
	fullAuthorizationProvider
}

func (*zanzibarAuthorizationProvider) EffectiveSearchResources(context.Context, *gestalt.ResourceSearchRequest) (*gestalt.ResourceSearchResponse, error) {
	return &gestalt.ResourceSearchResponse{
		Resources: []*gestalt.AuthorizationResource{
			gestalt.NewAuthorizationResource("agent_session", "session-123"),
		},
		ModelId: "authz-model-1",
	}, nil
}

func (*zanzibarAuthorizationProvider) EffectiveSearchSubjects(context.Context, *gestalt.EffectiveSubjectSearchRequest) (*gestalt.EffectiveSubjectSearchResponse, error) {
	return &gestalt.EffectiveSubjectSearchResponse{
		Targets: []*gestalt.AuthorizationRelationshipTarget{
			gestalt.NewAuthorizationSubjectTarget(gestalt.NewAuthorizationSubject("subject", "user:user-123")),
			gestalt.NewAuthorizationSubjectSetTarget(gestalt.NewAuthorizationResource("slack_channel", "C123"), "member"),
		},
		ModelId: "authz-model-1",
	}, nil
}

func (*zanzibarAuthorizationProvider) Expand(_ context.Context, req *gestalt.ExpandRequest) (*gestalt.ExpandResponse, error) {
	return &gestalt.ExpandResponse{
		Root: &gestalt.ExpandNode{
			Target:   gestalt.NewAuthorizationResourceTarget(req.GetResource()),
			Relation: req.GetRelation(),
			Children: []*gestalt.ExpandNode{{
				Target:   gestalt.NewAuthorizationSubjectTarget(gestalt.NewAuthorizationSubject("subject", "user:user-123")),
				Relation: "member",
			}},
		},
		ModelId: "authz-model-1",
	}, nil
}

func TestAuthorizationProviderOptionalZanzibarTransport(t *testing.T) {
	socket := newSocketPath(t, "authorization-zanzibar.sock")
	t.Setenv(proto.EnvProviderSocket, socket)

	ctx, cancel := context.WithCancel(context.Background())
	provider := &zanzibarAuthorizationProvider{}
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

	meta, err := client.GetMetadata(rpcCtx, &emptypb.Empty{}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if !hasCapabilities(meta.GetCapabilities(), "effective_search_resources", "effective_search_subjects", "expand") {
		t.Fatalf("GetMetadata capabilities = %#v, want optional Zanzibar capabilities", meta.GetCapabilities())
	}

	resourceResp, err := client.EffectiveSearchResources(rpcCtx, &proto.ResourceSearchRequest{
		Subject:      gestalt.NewAuthorizationSubject("subject", "user:user-123"),
		Action:       gestalt.NewAuthorizationAction("edit"),
		ResourceType: "agent_session",
	}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("EffectiveSearchResources: %v", err)
	}
	if len(resourceResp.GetResources()) != 1 || resourceResp.GetResources()[0].GetId() != "session-123" {
		t.Fatalf("effective resources = %#v, want session-123", resourceResp.GetResources())
	}

	subjectResp, err := client.EffectiveSearchSubjects(rpcCtx, &proto.EffectiveSubjectSearchRequest{
		Resource: gestalt.NewAuthorizationResource("agent_session", "session-123"),
		Action:   gestalt.NewAuthorizationAction("edit"),
	})
	if err != nil {
		t.Fatalf("EffectiveSearchSubjects: %v", err)
	}
	if subjectResp.GetTargets()[1].GetSubjectSet().GetResource().GetId() != "C123" {
		t.Fatalf("effective targets = %#v, want slack channel subject set", subjectResp.GetTargets())
	}

	expandResp, err := client.Expand(rpcCtx, &proto.ExpandRequest{
		Resource: gestalt.NewAuthorizationResource("agent_session", "session-123"),
		Relation: "editor",
	})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if expandResp.GetRoot().GetTarget().GetResource().GetId() != "session-123" {
		t.Fatalf("expand root = %#v, want session resource target", expandResp.GetRoot().GetTarget())
	}
}

func TestAuthorizationProviderOptionalZanzibarTransportUnimplemented(t *testing.T) {
	socket := newSocketPath(t, "authorization-unimplemented.sock")
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

	_, err := client.EffectiveSearchSubjects(rpcCtx, &proto.EffectiveSubjectSearchRequest{
		Resource: gestalt.NewAuthorizationResource("agent_session", "session-123"),
		Action:   gestalt.NewAuthorizationAction("edit"),
	}, grpc.WaitForReady(true))
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("EffectiveSearchSubjects error = %v, want Unimplemented", err)
	}
}

func hasCapabilities(actual []string, required ...string) bool {
	seen := make(map[string]struct{}, len(actual))
	for _, capability := range actual {
		seen[capability] = struct{}{}
	}
	for _, capability := range required {
		if _, ok := seen[capability]; !ok {
			return false
		}
	}
	return true
}
