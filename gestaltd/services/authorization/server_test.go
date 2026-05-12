package authorization

import (
	"context"
	"net"
	"reflect"
	"sync"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	gproto "google.golang.org/protobuf/proto"
)

func TestRemoteAuthorizationProviderOptionalCapabilitiesFollowMetadata(t *testing.T) {
	t.Parallel()

	base := newRemoteAuthorizationProvider(nil, nil, nil, "remote", nil)
	if _, ok := base.(core.AuthorizationProviderEffectiveSearch); ok {
		t.Fatal("remote provider without effective search capabilities unexpectedly implements effective search")
	}
	if _, ok := base.(core.AuthorizationProviderExpansion); ok {
		t.Fatal("remote provider without expansion capability unexpectedly implements expansion")
	}
	if got, want := authorizationHostCapabilities(base), []string{capabilitySearchSubjects}; !reflect.DeepEqual(got, want) {
		t.Fatalf("host capabilities without remote optional caps = %#v, want %#v", got, want)
	}

	effectiveOnly := newRemoteAuthorizationProvider(nil, nil, nil, "remote", []string{capabilityEffectiveSearchResources, capabilityEffectiveSearchSubjects})
	if _, ok := effectiveOnly.(core.AuthorizationProviderEffectiveSearch); !ok {
		t.Fatal("remote provider with effective search capabilities does not implement effective search")
	}
	if _, ok := effectiveOnly.(core.AuthorizationProviderExpansion); ok {
		t.Fatal("remote provider without expansion capability unexpectedly implements expansion")
	}

	allOptional := newRemoteAuthorizationProvider(nil, nil, nil, "remote", []string{
		capabilityEffectiveSearchResources,
		capabilityEffectiveSearchSubjects,
		capabilityExpand,
	})
	if _, ok := allOptional.(core.AuthorizationProviderEffectiveSearch); !ok {
		t.Fatal("remote provider with effective search capabilities does not implement effective search")
	}
	if _, ok := allOptional.(core.AuthorizationProviderExpansion); !ok {
		t.Fatal("remote provider with expansion capability does not implement expansion")
	}
}

func TestProviderServerPreservesOptionalStatusCodes(t *testing.T) {
	t.Parallel()

	provider := fakeEffectiveAuthorizationProvider{
		fakeAuthorizationProvider: fakeAuthorizationProvider{name: "fake"},
		err:                       status.Error(codes.Unimplemented, "optional unsupported"),
	}
	server := NewProviderServer(provider)

	_, err := server.EffectiveSearchResources(context.Background(), &proto.ResourceSearchRequest{})
	if got := status.Code(err); got != codes.Unimplemented {
		t.Fatalf("EffectiveSearchResources code = %v, want %v (err=%v)", got, codes.Unimplemented, err)
	}
	_, err = server.EffectiveSearchSubjects(context.Background(), &proto.EffectiveSubjectSearchRequest{})
	if got := status.Code(err); got != codes.Unimplemented {
		t.Fatalf("EffectiveSearchSubjects code = %v, want %v (err=%v)", got, codes.Unimplemented, err)
	}
	_, err = server.Expand(context.Background(), &proto.ExpandRequest{})
	if got := status.Code(err); got != codes.Unimplemented {
		t.Fatalf("Expand code = %v, want %v (err=%v)", got, codes.Unimplemented, err)
	}
}

func TestProviderServerWriteRelationshipsForwardsRequest(t *testing.T) {
	t.Parallel()

	provider := &recordingWriteAuthorizationProvider{
		fakeAuthorizationProvider: fakeAuthorizationProvider{name: "fake"},
	}
	client := newAuthorizationProviderTestClient(t, NewProviderServer(provider))
	req := &proto.WriteRelationshipsRequest{
		ModelId: "authz-model-1",
		Writes: []*proto.Relationship{{
			Subject: &proto.Subject{Type: "subject", Id: "user:shared"},
			Target: &proto.RelationshipTarget{
				Kind: &proto.RelationshipTarget_Subject{
					Subject: &proto.Subject{Type: "subject", Id: "user:shared"},
				},
			},
			Relation: "editor",
			Resource: &proto.Resource{Type: "agent_session", Id: "session-123"},
		}},
		Deletes: []*proto.RelationshipKey{{
			Target: &proto.RelationshipTarget{
				Kind: &proto.RelationshipTarget_SubjectSet{
					SubjectSet: &proto.SubjectSet{
						Resource: &proto.Resource{Type: "slack_channel", Id: "C123"},
						Relation: "member",
					},
				},
			},
			Relation: "viewer",
			Resource: &proto.Resource{Type: "agent_session", Id: "session-old"},
		}},
	}

	if _, err := client.WriteRelationships(context.Background(), req); err != nil {
		t.Fatalf("WriteRelationships: %v", err)
	}
	if got := provider.writeRequest(); !gproto.Equal(got, req) {
		t.Fatalf("forwarded request = %#v, want %#v", got, req)
	}
}

func TestProviderServerWriteRelationshipsPreservesStatusCodes(t *testing.T) {
	t.Parallel()

	provider := &recordingWriteAuthorizationProvider{
		fakeAuthorizationProvider: fakeAuthorizationProvider{name: "fake"},
		err:                       status.Error(codes.PermissionDenied, "write denied"),
	}
	client := newAuthorizationProviderTestClient(t, NewProviderServer(provider))

	_, err := client.WriteRelationships(context.Background(), &proto.WriteRelationshipsRequest{})
	if got := status.Code(err); got != codes.PermissionDenied {
		t.Fatalf("WriteRelationships code = %v, want %v (err=%v)", got, codes.PermissionDenied, err)
	}
}

func TestProviderServerWriteRelationshipsRejectsNilRequest(t *testing.T) {
	t.Parallel()

	server := NewProviderServer(fakeAuthorizationProvider{name: "fake"})
	_, err := server.WriteRelationships(context.Background(), nil)
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("WriteRelationships(nil) code = %v, want %v (err=%v)", got, codes.InvalidArgument, err)
	}
}

func newAuthorizationProviderTestClient(t *testing.T, server proto.AuthorizationProviderServer) proto.AuthorizationProviderClient {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	proto.RegisterAuthorizationProviderServer(srv, server)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return proto.NewAuthorizationProviderClient(conn)
}

type fakeAuthorizationProvider struct {
	name string
}

func (p fakeAuthorizationProvider) Name() string {
	return p.name
}

func (fakeAuthorizationProvider) Evaluate(context.Context, *core.AccessEvaluationRequest) (*core.AccessDecision, error) {
	return &core.AccessDecision{}, nil
}

func (fakeAuthorizationProvider) EvaluateMany(context.Context, *core.AccessEvaluationsRequest) (*core.AccessEvaluationsResponse, error) {
	return &core.AccessEvaluationsResponse{}, nil
}

func (fakeAuthorizationProvider) SearchResources(context.Context, *core.ResourceSearchRequest) (*core.ResourceSearchResponse, error) {
	return &core.ResourceSearchResponse{}, nil
}

func (fakeAuthorizationProvider) SearchSubjects(context.Context, *core.SubjectSearchRequest) (*core.SubjectSearchResponse, error) {
	return &core.SubjectSearchResponse{}, nil
}

func (fakeAuthorizationProvider) SearchActions(context.Context, *core.ActionSearchRequest) (*core.ActionSearchResponse, error) {
	return &core.ActionSearchResponse{}, nil
}

func (fakeAuthorizationProvider) GetMetadata(context.Context) (*core.AuthorizationMetadata, error) {
	return &core.AuthorizationMetadata{}, nil
}

func (fakeAuthorizationProvider) ReadRelationships(context.Context, *core.ReadRelationshipsRequest) (*core.ReadRelationshipsResponse, error) {
	return &core.ReadRelationshipsResponse{}, nil
}

func (fakeAuthorizationProvider) WriteRelationships(context.Context, *core.WriteRelationshipsRequest) error {
	return nil
}

func (fakeAuthorizationProvider) GetActiveModel(context.Context) (*core.GetActiveModelResponse, error) {
	return &core.GetActiveModelResponse{}, nil
}

func (fakeAuthorizationProvider) ListModels(context.Context, *core.ListModelsRequest) (*core.ListModelsResponse, error) {
	return &core.ListModelsResponse{}, nil
}

func (fakeAuthorizationProvider) WriteModel(context.Context, *core.WriteModelRequest) (*core.AuthorizationModelRef, error) {
	return &core.AuthorizationModelRef{}, nil
}

type fakeEffectiveAuthorizationProvider struct {
	fakeAuthorizationProvider
	err error
}

func (p fakeEffectiveAuthorizationProvider) EffectiveSearchResources(context.Context, *core.ResourceSearchRequest) (*core.ResourceSearchResponse, error) {
	return nil, p.err
}

func (p fakeEffectiveAuthorizationProvider) EffectiveSearchSubjects(context.Context, *core.EffectiveSubjectSearchRequest) (*core.EffectiveSubjectSearchResponse, error) {
	return nil, p.err
}

func (p fakeEffectiveAuthorizationProvider) Expand(context.Context, *core.ExpandRequest) (*core.ExpandResponse, error) {
	return nil, p.err
}

type recordingWriteAuthorizationProvider struct {
	fakeAuthorizationProvider

	mu  sync.Mutex
	req *proto.WriteRelationshipsRequest
	err error
}

func (p *recordingWriteAuthorizationProvider) WriteRelationships(_ context.Context, req *core.WriteRelationshipsRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if req != nil {
		p.req = gproto.Clone(req).(*proto.WriteRelationshipsRequest)
	} else {
		p.req = nil
	}
	return p.err
}

func (p *recordingWriteAuthorizationProvider) writeRequest() *proto.WriteRelationshipsRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.req == nil {
		return nil
	}
	return gproto.Clone(p.req).(*proto.WriteRelationshipsRequest)
}
