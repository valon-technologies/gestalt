package identity_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/hostserviceingress"
	"github.com/valon-technologies/gestalt/server/services/identity"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type recordingIdentityProvider struct {
	mu              sync.Mutex
	callerSubjectID string
}

func (p *recordingIdentityProvider) Configure(context.Context, string, map[string]any) error {
	return nil
}

func (p *recordingIdentityProvider) Metadata() gestalt.ProviderMetadata {
	return gestalt.ProviderMetadata{
		Kind:        gestalt.ProviderKindIdentity,
		Name:        "hop-auth",
		DisplayName: "Hop Auth",
		Version:     "1.0",
	}
}

func (p *recordingIdentityProvider) Warnings() []string { return nil }

func (p *recordingIdentityProvider) HealthCheck(context.Context) error { return nil }

func (p *recordingIdentityProvider) Start(context.Context) error { return nil }

func (p *recordingIdentityProvider) Close() error { return nil }

func (p *recordingIdentityProvider) Authorize(context.Context, *gestalt.AuthorizeRequest) (*gestalt.AuthorizeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

func (p *recordingIdentityProvider) Token(ctx context.Context, _ *gestalt.TokenRequest) (*gestalt.TokenResponse, error) {
	call := gestalt.IdentityCallContextFromContext(gestalt.AuthCallContextFromIncoming(ctx))
	p.mu.Lock()
	p.callerSubjectID = call.CallerSubjectID
	p.mu.Unlock()
	return &gestalt.TokenResponse{AccessToken: "hop-token", TokenType: "Bearer", GrantID: "grant-hop"}, nil
}

func (p *recordingIdentityProvider) Introspect(context.Context, *gestalt.IntrospectRequest) (*gestalt.IntrospectResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

func (p *recordingIdentityProvider) UserInfo(context.Context, *gestalt.UserInfoRequest) (*gestalt.UserInfoResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

func (p *recordingIdentityProvider) ListGrants(ctx context.Context, _ *gestalt.ListGrantsRequest) (*gestalt.ListGrantsResponse, error) {
	call := gestalt.IdentityCallContextFromContext(gestalt.AuthCallContextFromIncoming(ctx))
	p.mu.Lock()
	p.callerSubjectID = call.CallerSubjectID
	p.mu.Unlock()
	return &gestalt.ListGrantsResponse{}, nil
}

func (p *recordingIdentityProvider) GetGrant(context.Context, *gestalt.GetGrantRequest) (*gestalt.GetGrantResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

func (p *recordingIdentityProvider) RevokeGrant(context.Context, *gestalt.RevokeGrantRequest) (*gestalt.RevokeGrantResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

func (p *recordingIdentityProvider) callerSubject() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.callerSubjectID
}

func TestHostServiceIdentityHopPropagatesCallerSubject(t *testing.T) {
	socketDir, err := os.MkdirTemp("", "gsh-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	providerSocket := filepath.Join(socketDir, "provider.sock")
	t.Setenv(proto.EnvProviderSocket, providerSocket)

	recording := &recordingIdentityProvider{}
	providerCtx, providerCancel := context.WithCancel(context.Background())
	defer providerCancel()
	providerErrCh := make(chan error, 1)
	go func() {
		providerErrCh <- gestalt.ServeIdentityProvider(providerCtx, recording)
	}()
	t.Cleanup(func() {
		providerCancel()
		if err := <-providerErrCh; err != nil && err != context.Canceled {
			t.Fatalf("ServeIdentityProvider: %v", err)
		}
	})

	providerConn := dialIdentityTestUnix(t, providerSocket)
	defer func() { _ = providerConn.Close() }()

	readyCtx, readyCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer readyCancel()
	if _, err := proto.NewProviderLifecycleClient(providerConn).GetProviderIdentity(
		readyCtx,
		&emptypb.Empty{},
		grpc.WaitForReady(true),
	); err != nil {
		t.Fatalf("GetProviderIdentity: %v", err)
	}

	remote, err := identity.NewRemote(proto.NewIdentityClient(providerConn), "hop-auth")
	if err != nil {
		t.Fatalf("NewRemote: %v", err)
	}

	secret := []byte("identity-hop-test-secret-0123456")
	tokenManager, err := runtimehost.NewHostServiceRelayTokenManager(secret)
	if err != nil {
		t.Fatalf("NewHostServiceRelayTokenManager: %v", err)
	}
	tokenManager.SetCapabilityIngressDecorator(hostserviceingress.ApplyCapability)

	started, err := runtimehost.StartHostServices([]runtimehost.HostService{{
		Name:           "identity",
		MethodPrefixes: []string{"/" + proto.Identity_ServiceDesc.ServiceName + "/"},
		Register: func(srv *grpc.Server) {
			proto.RegisterIdentityServer(srv, identity.NewProviderServer(remote))
		},
	}}, runtimehost.WithHostServicesGRPCServerOptions(tokenManager.HostServiceGRPCServerOptions()...))
	if err != nil {
		t.Fatalf("StartHostServices: %v", err)
	}
	t.Cleanup(func() {
		if err := started.Close(); err != nil {
			t.Fatalf("Close host services: %v", err)
		}
	})

	hostConn := dialIdentityTestUnix(t, started.SocketBinding().SocketPath)
	defer func() { _ = hostConn.Close() }()

	capability, err := tokenManager.MintToken(runtimehost.HostServiceRelayTokenRequest{
		Service:      "identity",
		MethodPrefix: "/" + proto.Identity_ServiceDesc.ServiceName + "/",
		Caller:       &runtimehost.PrincipalClaims{SubjectID: "user:hop-caller"},
	})
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer rpcCancel()
	rpcCtx = metadata.AppendToOutgoingContext(
		rpcCtx,
		runtimehost.HostServiceRelayTokenHeader,
		capability,
	)

	_, err = proto.NewIdentityClient(hostConn).ListGrants(rpcCtx, &proto.ListGrantsRequest{})
	if err != nil {
		t.Fatalf("ListGrants via host service: %v", err)
	}
	if got := recording.callerSubject(); got != "user:hop-caller" {
		t.Fatalf("executable provider CallerSubjectID = %q, want user:hop-caller", got)
	}

	_, err = proto.NewIdentityClient(hostConn).Token(rpcCtx, &proto.TokenRequest{
		GrantType: "urn:ietf:params:oauth:grant-type:token-exchange",
	})
	if err != nil {
		t.Fatalf("Token via host service: %v", err)
	}
	if got := recording.callerSubject(); got != "user:hop-caller" {
		t.Fatalf("Token() executable provider CallerSubjectID = %q, want user:hop-caller", got)
	}
}

func dialIdentityTestUnix(t *testing.T, socketPath string) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(
		"unix://"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		}),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient(%q): %v", socketPath, err)
	}
	return conn
}
