package gestalt_test

import (
	"context"
	"fmt"
	"net"
	"sort"
	"sync"
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	sdkclient "github.com/valon-technologies/gestalt/sdk/go/client"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

type stubExternalCredentialProvider struct {
	closeTracker

	mu          sync.Mutex
	credentials map[string]*gestalt.ExternalCredential
	lookupByID  map[string]string
}

func newStubExternalCredentialProvider() *stubExternalCredentialProvider {
	return &stubExternalCredentialProvider{
		credentials: make(map[string]*gestalt.ExternalCredential),
		lookupByID:  make(map[string]string),
	}
}

func (p *stubExternalCredentialProvider) Configure(context.Context, string, map[string]any) error {
	return nil
}

func (p *stubExternalCredentialProvider) CreateCredential(_ context.Context, req *gestalt.CreateExternalCredentialRequest) (*gestalt.ExternalCredential, error) {
	if req == nil || req.GetCredential() == nil {
		return nil, fmt.Errorf("credential is required")
	}
	value := cloneExternalCredential(req.GetCredential())

	p.mu.Lock()
	defer p.mu.Unlock()

	key := externalCredentialLookupKey(value.GetSubject(), value.GetAudience(), value.GetQualifier())
	if _, exists := p.credentials[key]; exists {
		return nil, gestalt.ErrAlreadyExists
	}
	now := time.Now().UTC()
	if value.GetId() == "" {
		value.ID = "cred-" + value.GetAudience() + "-" + value.GetQualifier()
	}
	value.CreatedAt = &now
	value.UpdatedAt = &now
	p.credentials[key] = cloneExternalCredential(value)
	p.lookupByID[value.GetId()] = key
	return value, nil
}

func (p *stubExternalCredentialProvider) UpsertCredential(_ context.Context, req *gestalt.UpsertExternalCredentialRequest) (*gestalt.ExternalCredential, error) {
	if req == nil || req.GetCredential() == nil {
		return nil, fmt.Errorf("credential is required")
	}
	value := cloneExternalCredential(req.GetCredential())

	p.mu.Lock()
	defer p.mu.Unlock()

	key := externalCredentialLookupKey(value.GetSubject(), value.GetAudience(), value.GetQualifier())
	existing := p.credentials[key]
	now := time.Now().UTC()
	if existing != nil {
		value.ID = existing.GetId()
		value.CreatedAt = existing.GetCreatedAt()
	} else {
		if value.GetId() == "" {
			value.ID = "cred-" + value.GetAudience() + "-" + value.GetQualifier()
		}
		if value.GetCreatedAt() == nil {
			value.CreatedAt = &now
		}
	}
	value.UpdatedAt = &now
	p.credentials[key] = cloneExternalCredential(value)
	p.lookupByID[value.GetId()] = key
	return value, nil
}

func (p *stubExternalCredentialProvider) GetCredential(_ context.Context, req *gestalt.GetExternalCredentialRequest) (*gestalt.ExternalCredential, error) {
	if req == nil || req.GetSubject() == "" {
		return nil, fmt.Errorf("subject is required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	credential, ok := p.credentials[externalCredentialLookupKey(req.GetSubject(), req.GetAudience(), req.GetQualifier())]
	if !ok {
		return nil, gestalt.ErrExternalCredentialNotFound
	}
	return cloneExternalCredential(credential), nil
}

func (p *stubExternalCredentialProvider) ListCredentials(_ context.Context, req *gestalt.ListExternalCredentialsRequest) (*gestalt.ListExternalCredentialsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("request is required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	credentials := make([]*gestalt.ExternalCredential, 0, len(p.credentials))
	for _, credential := range p.credentials {
		if req.GetSubject() != "" && credential.GetSubject() != req.GetSubject() {
			continue
		}
		if req.GetAudience() != "" && credential.GetAudience() != req.GetAudience() {
			continue
		}
		credentials = append(credentials, cloneExternalCredential(credential))
	}
	sort.Slice(credentials, func(i, j int) bool {
		return credentials[i].GetId() < credentials[j].GetId()
	})
	return &gestalt.ListExternalCredentialsResponse{Credentials: credentials}, nil
}

func (p *stubExternalCredentialProvider) DeleteCredential(_ context.Context, req *gestalt.DeleteExternalCredentialRequest) error {
	if req == nil || req.GetId() == "" {
		return fmt.Errorf("credential id is required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	key, ok := p.lookupByID[req.GetId()]
	if !ok {
		return gestalt.ErrExternalCredentialNotFound
	}
	delete(p.lookupByID, req.GetId())
	delete(p.credentials, key)
	return nil
}

func (p *stubExternalCredentialProvider) ValidateCredentialConfig(context.Context, *gestalt.ValidateExternalCredentialConfigRequest) error {
	return nil
}

func (p *stubExternalCredentialProvider) ResolveCredential(ctx context.Context, req *gestalt.ResolveExternalCredentialRequest) (*gestalt.ResolveExternalCredentialResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("request is required")
	}
	credential, err := p.GetCredential(ctx, &gestalt.GetExternalCredentialRequest{
		Subject:   req.GetCredentialSubjectId(),
		Audience:  req.GetConnectionId(),
		Qualifier: req.GetInstance(),
	})
	if err != nil {
		return nil, err
	}
	return &gestalt.ResolveExternalCredentialResponse{
		Token:      credential.GetGrant().GetAccessToken(),
		ExpiresAt:  credential.GetGrant().GetExpiresAt(),
		Credential: credential,
	}, nil
}

func (*stubExternalCredentialProvider) ExchangeCredential(context.Context, *gestalt.ExchangeExternalCredentialRequest) (*gestalt.ExchangeExternalCredentialResponse, error) {
	return &gestalt.ExchangeExternalCredentialResponse{}, nil
}

type externalCredentialTransportHarness struct {
	proto.UnimplementedExternalCredentialsServer

	mu       sync.Mutex
	requests []*proto.UpsertExternalCredentialRequest
	tokens   []string
}

func (h *externalCredentialTransportHarness) UpsertCredential(ctx context.Context, req *proto.UpsertExternalCredentialRequest) (*proto.ExternalCredential, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.requests = append(h.requests, gproto.Clone(req).(*proto.UpsertExternalCredentialRequest))
	h.mu.Unlock()

	value := &proto.ExternalCredential{}
	if req.GetCredential() != nil {
		value = gproto.Clone(req.GetCredential()).(*proto.ExternalCredential)
	}
	if value.GetId() == "" {
		value.Id = "cred-transport-1"
	}
	return value, nil
}

func TestExternalCredentialProviderRoundTrip(t *testing.T) {
	socket := newSocketPath(t, "external-credential.sock")
	t.Setenv(proto.EnvProviderSocket, socket)

	ctx, cancel := context.WithCancel(context.Background())
	provider := newStubExternalCredentialProvider()
	errCh := make(chan error, 1)
	go func() {
		errCh <- gestalt.ServeExternalCredentialProvider(ctx, provider)
	}()
	t.Cleanup(func() {
		cancel()
		waitServeResult(t, errCh)
		if !provider.closed.Load() {
			t.Fatal("provider Close was not called")
		}
	})

	conn := newUnixConn(t, socket)
	lifecycle := proto.NewProviderLifecycleClient(conn)
	client := proto.NewExternalCredentialsClient(conn)

	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer rpcCancel()

	meta, err := lifecycle.GetProviderIdentity(rpcCtx, &emptypb.Empty{}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("GetProviderIdentity: %v", err)
	}
	if meta.GetKind() != proto.ProviderKind_PROVIDER_KIND_EXTERNAL_CREDENTIAL {
		t.Fatalf("provider kind = %v, want %v", meta.GetKind(), proto.ProviderKind_PROVIDER_KIND_EXTERNAL_CREDENTIAL)
	}

	upserted, err := client.UpsertCredential(rpcCtx, &proto.UpsertExternalCredentialRequest{
		Credential: &proto.ExternalCredential{
			Subject:   "user:user-123",
			Audience:  "slack:default",
			Qualifier: "workspace-1",
			Credential: &proto.ExternalCredential_Grant{Grant: &proto.ExternalCredentialGrant{
				AccessToken: "xoxb-123",
				Scope:       "channels:read chat:write",
			}},
		},
	}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("UpsertCredential: %v", err)
	}
	if upserted.GetId() == "" {
		t.Fatal("UpsertCredential returned empty id")
	}
	if upserted.GetUpdatedAt() == nil {
		t.Fatal("UpsertCredential returned nil updated_at")
	}

	created, err := client.CreateCredential(rpcCtx, &proto.CreateExternalCredentialRequest{
		Credential: &proto.ExternalCredential{
			Subject:   "system:gestaltd",
			Audience:  "https://auth.example.com",
			Qualifier: "https://gestalt.example.com/callback",
			Credential: &proto.ExternalCredential_Client{Client: &proto.ExternalCredentialClientInfo{
				ClientId:     "client-001",
				ClientSecret: "secret-001",
			}},
		},
	}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	if created.GetClient().GetClientId() != "client-001" {
		t.Fatalf("created client_id = %q, want client-001", created.GetClient().GetClientId())
	}

	_, err = client.CreateCredential(rpcCtx, &proto.CreateExternalCredentialRequest{
		Credential: &proto.ExternalCredential{
			Subject:   "system:gestaltd",
			Audience:  "https://auth.example.com",
			Qualifier: "https://gestalt.example.com/callback",
			Credential: &proto.ExternalCredential_Client{Client: &proto.ExternalCredentialClientInfo{
				ClientId: "client-002",
			}},
		},
	}, grpc.WaitForReady(true))
	if s, ok := status.FromError(err); !ok || s.Code() != codes.AlreadyExists {
		t.Fatalf("CreateCredential conflict code = %v, want ALREADY_EXISTS", err)
	}

	got, err := client.GetCredential(rpcCtx, &proto.GetExternalCredentialRequest{
		Subject:   "user:user-123",
		Audience:  "slack:default",
		Qualifier: "workspace-1",
	}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("GetCredential: %v", err)
	}
	if got.GetGrant().GetAccessToken() != "xoxb-123" {
		t.Fatalf("access token = %q, want %q", got.GetGrant().GetAccessToken(), "xoxb-123")
	}

	listed, err := client.ListCredentials(rpcCtx, &proto.ListExternalCredentialsRequest{
		Subject:  "user:user-123",
		Audience: "slack:default",
	}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("ListCredentials: %v", err)
	}
	if len(listed.GetCredentials()) != 1 {
		t.Fatalf("credentials len = %d, want 1", len(listed.GetCredentials()))
	}

	if _, err := client.DeleteCredential(rpcCtx, &proto.DeleteExternalCredentialRequest{Id: upserted.GetId()}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("DeleteCredential: %v", err)
	}

	_, err = client.GetCredential(rpcCtx, &proto.GetExternalCredentialRequest{
		Subject:   "user:user-123",
		Audience:  "slack:default",
		Qualifier: "workspace-1",
	}, grpc.WaitForReady(true))
	if err == nil {
		t.Fatal("GetCredential after delete should return error")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.NotFound {
		t.Fatalf("GetCredential after delete code = %v, want NOT_FOUND", err)
	}
}

func TestTransport_ExternalCredentialTCPTargetTokenEnv(t *testing.T) {
	address := reserveTCPAddress()
	lis, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	harness := &externalCredentialTransportHarness{}
	srv := grpc.NewServer()
	proto.RegisterExternalCredentialsServer(srv, harness)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	t.Setenv(gestalt.EnvHostServiceSocket, "tcp://"+address)
	t.Setenv(gestalt.EnvHostServiceToken, "relay-token-go")

	externalCredentials, err := sdkclient.ConnectExternalCredentials(context.Background(), "")
	if err != nil {
		t.Fatalf("ExternalCredentials: %v", err)
	}

	credential, err := externalCredentials.UpsertCredentialRaw(context.Background(), &sdkclient.UpsertExternalCredentialRequest{
		Credential: &sdkclient.ExternalCredential{
			Subject:   "user:user-123",
			Audience:  "slack:default",
			Qualifier: "workspace-1",
			Credential: &sdkclient.ExternalCredentialCredentialGrant{Value: &sdkclient.ExternalCredentialGrant{
				AccessToken: "xoxb-123",
			}},
		},
	})
	if err != nil {
		t.Fatalf("UpsertCredential: %v", err)
	}
	if credential.ID != "cred-transport-1" {
		t.Fatalf("credential id = %q, want %q", credential.ID, "cred-transport-1")
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.tokens) != 1 || harness.tokens[0] != "relay-token-go" {
		t.Fatalf("relay tokens = %#v, want [relay-token-go]", harness.tokens)
	}
	if len(harness.requests) != 1 {
		t.Fatalf("upsert requests len = %d, want 1", len(harness.requests))
	}
	got := harness.requests[0].GetCredential()
	if got.GetSubject() != "user:user-123" {
		t.Fatalf("subject = %q, want %q", got.GetSubject(), "user:user-123")
	}
	if got.GetAudience() != "slack:default" || got.GetQualifier() != "workspace-1" {
		t.Fatalf("credential = %+v, want audience=slack:default qualifier=workspace-1", got)
	}
}

func externalCredentialLookupKey(subject, audience, qualifier string) string {
	return subject + "\x00" + audience + "\x00" + qualifier
}

func cloneExternalCredential(value *gestalt.ExternalCredential) *gestalt.ExternalCredential {
	if value == nil {
		return nil
	}
	clone := *value
	if value.Grant != nil {
		grant := *value.Grant
		grant.ExpiresAt = cloneTime(value.Grant.ExpiresAt)
		grant.LastRefreshedAt = cloneTime(value.Grant.LastRefreshedAt)
		clone.Grant = &grant
	}
	if value.Client != nil {
		client := *value.Client
		client.ClientSecretExpiresAt = cloneTime(value.Client.ClientSecretExpiresAt)
		clone.Client = &client
	}
	if value.Opaque != nil {
		fields := make(map[string]string, len(value.Opaque.Fields))
		for k, v := range value.Opaque.Fields {
			fields[k] = v
		}
		clone.Opaque = &gestalt.ExternalCredentialOpaque{Fields: fields}
	}
	clone.CreatedAt = cloneTime(value.CreatedAt)
	clone.UpdatedAt = cloneTime(value.UpdatedAt)
	return &clone
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}
