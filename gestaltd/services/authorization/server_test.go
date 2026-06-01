package authorization

import (
	"context"
	"net"
	"sync"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	gproto "google.golang.org/protobuf/proto"
)

func TestProviderServerSetRelationshipsForwardsRequest(t *testing.T) {
	t.Parallel()

	provider := &recordingSetRelationshipsAuthorizationProvider{
		fakeAuthorizationProvider: fakeAuthorizationProvider{name: "fake"},
	}
	client := newAuthorizationProviderTestClient(t, NewProviderServer(provider))
	req := &proto.SetRelationshipsRequest{
		Relationships: []*proto.Relationship{{
			Tuple: &proto.RelationshipTuple{
				Target: &proto.RelationshipTarget{
					Kind: &proto.RelationshipTarget_Subject{
						Subject: &proto.Subject{Type: "subject", Id: "user:shared"},
					},
				},
				Relation: "member",
				Resource: &proto.Resource{Type: "team", Id: "team-123"},
			},
		}},
	}

	if _, err := client.SetRelationships(context.Background(), req); err != nil {
		t.Fatalf("SetRelationships: %v", err)
	}
	if got := provider.setRequest(); !gproto.Equal(got, req) {
		t.Fatalf("forwarded request = %#v, want %#v", got, req)
	}
}

func TestProviderServerSetRelationshipsPreservesStatusCodes(t *testing.T) {
	t.Parallel()

	provider := &recordingSetRelationshipsAuthorizationProvider{
		fakeAuthorizationProvider: fakeAuthorizationProvider{name: "fake"},
		err:                       status.Error(codes.PermissionDenied, "set denied"),
	}
	client := newAuthorizationProviderTestClient(t, NewProviderServer(provider))

	_, err := client.SetRelationships(context.Background(), &proto.SetRelationshipsRequest{})
	if got := status.Code(err); got != codes.PermissionDenied {
		t.Fatalf("SetRelationships code = %v, want %v (err=%v)", got, codes.PermissionDenied, err)
	}
}

func TestProviderServerSetRelationshipsRejectsNilRequest(t *testing.T) {
	t.Parallel()

	server := NewProviderServer(fakeAuthorizationProvider{name: "fake"})
	_, err := server.SetRelationships(context.Background(), nil)
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("SetRelationships(nil) code = %v, want %v (err=%v)", got, codes.InvalidArgument, err)
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

func (fakeAuthorizationProvider) CheckAccess(context.Context, *core.CheckAccessRequest) (*core.CheckAccessResponse, error) {
	return &core.CheckAccessResponse{}, nil
}

func (fakeAuthorizationProvider) CheckAccessMany(context.Context, *core.CheckAccessManyRequest) (*core.CheckAccessManyResponse, error) {
	return &core.CheckAccessManyResponse{}, nil
}

func (fakeAuthorizationProvider) ListRelationships(context.Context, *core.ListRelationshipsRequest) (*core.ListRelationshipsResponse, error) {
	return &core.ListRelationshipsResponse{}, nil
}

func (fakeAuthorizationProvider) AddRelationship(_ context.Context, req *core.AddRelationshipRequest) (*core.AddRelationshipResponse, error) {
	return &core.AddRelationshipResponse{Relationship: req.GetRelationship()}, nil
}

func (fakeAuthorizationProvider) DeleteRelationship(context.Context, *core.DeleteRelationshipRequest) (*core.DeleteRelationshipResponse, error) {
	return &core.DeleteRelationshipResponse{}, nil
}

func (fakeAuthorizationProvider) SetRelationships(_ context.Context, req *core.SetRelationshipsRequest) (*core.SetRelationshipsResponse, error) {
	return &core.SetRelationshipsResponse{Relationships: req.GetRelationships()}, nil
}

func (fakeAuthorizationProvider) GetActiveModelRef(context.Context) (*core.GetActiveModelRefResponse, error) {
	return &core.GetActiveModelRefResponse{}, nil
}

func (fakeAuthorizationProvider) SetActiveModel(context.Context, *core.SetActiveModelRequest) (*core.SetActiveModelResponse, error) {
	return &core.SetActiveModelResponse{Model: &core.AuthorizationModelRef{}}, nil
}

func (fakeAuthorizationProvider) ListActiveModelResourceTypes(context.Context, *core.ListActiveModelResourceTypesRequest) (*core.ListActiveModelResourceTypesResponse, error) {
	return &core.ListActiveModelResourceTypesResponse{}, nil
}

type recordingSetRelationshipsAuthorizationProvider struct {
	fakeAuthorizationProvider

	mu  sync.Mutex
	req *proto.SetRelationshipsRequest
	err error
}

func (p *recordingSetRelationshipsAuthorizationProvider) SetRelationships(_ context.Context, req *core.SetRelationshipsRequest) (*core.SetRelationshipsResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if req != nil {
		p.req = gproto.Clone(req).(*proto.SetRelationshipsRequest)
	} else {
		p.req = nil
	}
	if p.err != nil {
		return nil, p.err
	}
	return &core.SetRelationshipsResponse{Relationships: req.GetRelationships()}, nil
}

func (p *recordingSetRelationshipsAuthorizationProvider) setRequest() *proto.SetRelationshipsRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.req == nil {
		return nil
	}
	return gproto.Clone(p.req).(*proto.SetRelationshipsRequest)
}
