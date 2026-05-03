package externalcredentials

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"google.golang.org/grpc"
)

type wrappedNotFoundExternalCredentialProvider struct{}

func (*wrappedNotFoundExternalCredentialProvider) PutCredential(context.Context, *core.ExternalCredential) error {
	return nil
}

func (*wrappedNotFoundExternalCredentialProvider) RestoreCredential(context.Context, *core.ExternalCredential) error {
	return nil
}

func (*wrappedNotFoundExternalCredentialProvider) GetCredential(context.Context, string, string, string) (*core.ExternalCredential, error) {
	return nil, fmt.Errorf("lookup failed: %w", core.ErrNotFound)
}

func (*wrappedNotFoundExternalCredentialProvider) ListCredentials(context.Context, string) ([]*core.ExternalCredential, error) {
	return nil, nil
}

func (*wrappedNotFoundExternalCredentialProvider) ListCredentialsForConnection(context.Context, string, string) ([]*core.ExternalCredential, error) {
	return nil, nil
}

func (*wrappedNotFoundExternalCredentialProvider) DeleteCredential(context.Context, string) error {
	return fmt.Errorf("delete failed: %w", core.ErrNotFound)
}

func (*wrappedNotFoundExternalCredentialProvider) ValidateCredentialConfig(context.Context, *core.ValidateExternalCredentialConfigRequest) error {
	return nil
}

func (*wrappedNotFoundExternalCredentialProvider) ResolveCredential(context.Context, *core.ResolveExternalCredentialRequest) (*core.ResolveExternalCredentialResponse, error) {
	return nil, fmt.Errorf("lookup failed: %w", core.ErrNotFound)
}

func (*wrappedNotFoundExternalCredentialProvider) ExchangeCredential(context.Context, *core.ExchangeExternalCredentialRequest) (*core.ExchangeExternalCredentialResponse, error) {
	return nil, fmt.Errorf("lookup failed: %w", core.ErrNotFound)
}

func TestExternalCredentialProviderTransportHandlesWrappedNotFound(t *testing.T) {
	t.Parallel()

	conn := newBufconnConn(t, func(server *grpc.Server) {
		proto.RegisterExternalCredentialProviderServer(server, NewProviderServer(&wrappedNotFoundExternalCredentialProvider{}))
	})
	remote := &remoteExternalCredentialProvider{client: proto.NewExternalCredentialProviderClient(conn)}

	_, err := remote.GetCredential(context.Background(), "user:test", "github:default", "")
	if !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("GetCredential error = %v, want core.ErrNotFound", err)
	}
	if err := remote.DeleteCredential(context.Background(), "missing"); err != nil {
		t.Fatalf("DeleteCredential error = %v, want nil", err)
	}
}

type restoreTrackingExternalCredentialProvider struct {
	putCalls     int
	restoreCalls int
	stored       *core.ExternalCredential
}

func (p *restoreTrackingExternalCredentialProvider) PutCredential(_ context.Context, credential *core.ExternalCredential) error {
	p.putCalls++
	copy := *credential
	now := time.Unix(1_700_000_100, 0).UTC()
	copy.CreatedAt = now
	copy.UpdatedAt = now
	p.stored = &copy
	return nil
}

func (p *restoreTrackingExternalCredentialProvider) RestoreCredential(_ context.Context, credential *core.ExternalCredential) error {
	p.restoreCalls++
	copy := *credential
	p.stored = &copy
	return nil
}

func (p *restoreTrackingExternalCredentialProvider) GetCredential(context.Context, string, string, string) (*core.ExternalCredential, error) {
	if p.stored == nil {
		return nil, core.ErrNotFound
	}
	copy := *p.stored
	return &copy, nil
}

func (*restoreTrackingExternalCredentialProvider) ListCredentials(context.Context, string) ([]*core.ExternalCredential, error) {
	return nil, nil
}

func (*restoreTrackingExternalCredentialProvider) ListCredentialsForConnection(context.Context, string, string) ([]*core.ExternalCredential, error) {
	return nil, nil
}

func (*restoreTrackingExternalCredentialProvider) DeleteCredential(context.Context, string) error {
	return nil
}

func (*restoreTrackingExternalCredentialProvider) ValidateCredentialConfig(context.Context, *core.ValidateExternalCredentialConfigRequest) error {
	return nil
}

func (p *restoreTrackingExternalCredentialProvider) ResolveCredential(context.Context, *core.ResolveExternalCredentialRequest) (*core.ResolveExternalCredentialResponse, error) {
	if p.stored == nil {
		return nil, core.ErrNotFound
	}
	copy := *p.stored
	return &core.ResolveExternalCredentialResponse{Token: copy.AccessToken, Credential: &copy}, nil
}

func (*restoreTrackingExternalCredentialProvider) ExchangeCredential(context.Context, *core.ExchangeExternalCredentialRequest) (*core.ExchangeExternalCredentialResponse, error) {
	return &core.ExchangeExternalCredentialResponse{}, nil
}

func TestExternalCredentialProviderRestorePreservesTimestampsOverTransport(t *testing.T) {
	t.Parallel()

	provider := &restoreTrackingExternalCredentialProvider{}
	conn := newBufconnConn(t, func(server *grpc.Server) {
		proto.RegisterExternalCredentialProviderServer(server, NewProviderServer(provider))
	})
	remote := &remoteExternalCredentialProvider{client: proto.NewExternalCredentialProviderClient(conn)}

	createdAt := time.Unix(1_700_000_000, 0).UTC()
	updatedAt := time.Unix(1_700_000_001, 0).UTC()
	credential := &core.ExternalCredential{
		ID:           "cred-1",
		SubjectID:    "user:test",
		ConnectionID: "github:default",
		Integration:  "github",
		Connection:   "default",
		Instance:     "org",
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
	}

	if err := remote.RestoreCredential(context.Background(), credential); err != nil {
		t.Fatalf("RestoreCredential error = %v", err)
	}
	if provider.putCalls != 0 {
		t.Fatalf("PutCredential calls = %d, want 0", provider.putCalls)
	}
	if provider.restoreCalls != 1 {
		t.Fatalf("RestoreCredential calls = %d, want 1", provider.restoreCalls)
	}
	if !credential.CreatedAt.Equal(createdAt) {
		t.Fatalf("created_at = %v, want %v", credential.CreatedAt, createdAt)
	}
	if !credential.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("updated_at = %v, want %v", credential.UpdatedAt, updatedAt)
	}
}
