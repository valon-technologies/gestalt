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

func (p *stubExternalCredentialProvider) UpsertCredential(_ context.Context, req *gestalt.UpsertExternalCredentialRequest) (*gestalt.ExternalCredential, error) {
	if req == nil || req.GetCredential() == nil {
		return nil, fmt.Errorf("credential is required")
	}
	value := cloneExternalCredential(req.GetCredential())

	p.mu.Lock()
	defer p.mu.Unlock()

	key := externalCredentialLookupKey(&gestalt.ExternalCredentialLookup{
		SubjectID:    value.GetSubjectId(),
		ConnectionID: value.GetConnectionId(),
		Instance:     value.GetInstance(),
	})
	existing := p.credentials[key]
	now := time.Now().UTC()
	if existing != nil {
		value.ID = existing.GetId()
		value.CreatedAt = existing.GetCreatedAt()
	} else {
		if value.GetId() == "" {
			value.ID = "cred-" + value.GetConnectionId() + "-" + value.GetInstance()
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
	if req == nil || req.GetLookup() == nil {
		return nil, fmt.Errorf("lookup is required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	credential, ok := p.credentials[externalCredentialLookupKey(req.GetLookup())]
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
		if req.GetSubjectId() != "" && credential.GetSubjectId() != req.GetSubjectId() {
			continue
		}
		if req.GetConnectionId() != "" && credential.GetConnectionId() != req.GetConnectionId() {
			continue
		}
		if req.GetInstance() != "" && credential.GetInstance() != req.GetInstance() {
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
	credential, err := p.GetCredential(ctx, &gestalt.GetExternalCredentialRequest{Lookup: &gestalt.ExternalCredentialLookup{
		SubjectID:    req.GetCredentialSubjectId(),
		ConnectionID: req.GetConnectionId(),
		Instance:     req.GetInstance(),
	}})
	if err != nil {
		return nil, err
	}
	return &gestalt.ResolveExternalCredentialResponse{
		Token:      credential.GetAccessToken(),
		ExpiresAt:  credential.GetExpiresAt(),
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

	lookup := &proto.ExternalCredentialLookup{
		SubjectId:    "user:user-123",
		ConnectionId: "slack:default",
		Instance:     "workspace-1",
	}
	upserted, err := client.UpsertCredential(rpcCtx, &proto.UpsertExternalCredentialRequest{
		Credential: &proto.ExternalCredential{
			SubjectId:    lookup.GetSubjectId(),
			ConnectionId: lookup.GetConnectionId(),
			Instance:     lookup.GetInstance(),
			AccessToken:  "xoxb-123",
			Scopes:       "channels:read chat:write",
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

	got, err := client.GetCredential(rpcCtx, &proto.GetExternalCredentialRequest{Lookup: lookup}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("GetCredential: %v", err)
	}
	if got.GetAccessToken() != "xoxb-123" {
		t.Fatalf("access token = %q, want %q", got.GetAccessToken(), "xoxb-123")
	}

	listed, err := client.ListCredentials(rpcCtx, &proto.ListExternalCredentialsRequest{
		SubjectId:    lookup.GetSubjectId(),
		ConnectionId: lookup.GetConnectionId(),
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

	_, err = client.GetCredential(rpcCtx, &proto.GetExternalCredentialRequest{Lookup: lookup}, grpc.WaitForReady(true))
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
			SubjectId:    "user:user-123",
			ConnectionId: "slack:default",
			Instance:     "workspace-1",
			AccessToken:  "xoxb-123",
		},
	})
	if err != nil {
		t.Fatalf("UpsertCredential: %v", err)
	}
	if credential.Id != "cred-transport-1" {
		t.Fatalf("credential id = %q, want %q", credential.Id, "cred-transport-1")
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
	if got.GetSubjectId() != "user:user-123" {
		t.Fatalf("subject id = %q, want %q", got.GetSubjectId(), "user:user-123")
	}
	if got.GetConnectionId() != "slack:default" || got.GetInstance() != "workspace-1" {
		t.Fatalf("credential = %+v, want connection_id=slack:default instance=workspace-1", got)
	}
}

func externalCredentialLookupKey(lookup interface {
	GetSubjectId() string
	GetConnectionId() string
	GetInstance() string
}) string {
	if lookup == nil {
		return ""
	}
	return lookup.GetSubjectId() + "\x00" + lookup.GetConnectionId() + "\x00" + lookup.GetInstance()
}

func cloneExternalCredential(value *gestalt.ExternalCredential) *gestalt.ExternalCredential {
	if value == nil {
		return nil
	}
	clone := *value
	clone.ExpiresAt = cloneTime(value.ExpiresAt)
	clone.LastRefreshedAt = cloneTime(value.LastRefreshedAt)
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
