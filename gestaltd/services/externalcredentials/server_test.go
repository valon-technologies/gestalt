package externalcredentials

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
)

func newRemoteProvider(t *testing.T, provider core.ExternalCredentialProvider) *remoteExternalCredentialProvider {
	t.Helper()

	conn := newBufconnConn(t, func(server *grpc.Server) {
		proto.RegisterExternalCredentialsServer(server, NewProviderServer(provider))
	})
	return &remoteExternalCredentialProvider{client: proto.NewExternalCredentialsClient(conn)}
}

type wrappedNotFoundExternalCredentialProvider struct{}

func (*wrappedNotFoundExternalCredentialProvider) CreateCredential(context.Context, *core.ExternalCredential) error {
	return nil
}

func (*wrappedNotFoundExternalCredentialProvider) UpsertCredential(context.Context, *core.ExternalCredential) error {
	return nil
}

func (*wrappedNotFoundExternalCredentialProvider) GetCredential(context.Context, string, string, string) (*core.ExternalCredential, error) {
	return nil, fmt.Errorf("lookup failed: %w", core.ErrNotFound)
}

func (*wrappedNotFoundExternalCredentialProvider) ListCredentials(context.Context, string, string) ([]*core.ExternalCredential, error) {
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

	remote := newRemoteProvider(t, &wrappedNotFoundExternalCredentialProvider{})

	_, err := remote.GetCredential(context.Background(), "user:test", "github:default", "")
	if !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("GetCredential error = %v, want core.ErrNotFound", err)
	}
	if err := remote.DeleteCredential(context.Background(), "missing"); err != nil {
		t.Fatalf("DeleteCredential error = %v, want nil", err)
	}
}

type authRoundTripExternalCredentialProvider struct {
	validateAuth core.ExternalCredentialAuthConfig
	resolveAuth  core.ExternalCredentialAuthConfig
	exchangeAuth core.ExternalCredentialAuthConfig
}

func (*authRoundTripExternalCredentialProvider) CreateCredential(context.Context, *core.ExternalCredential) error {
	return nil
}

func (*authRoundTripExternalCredentialProvider) UpsertCredential(context.Context, *core.ExternalCredential) error {
	return nil
}

func (*authRoundTripExternalCredentialProvider) GetCredential(context.Context, string, string, string) (*core.ExternalCredential, error) {
	return nil, core.ErrNotFound
}

func (*authRoundTripExternalCredentialProvider) ListCredentials(context.Context, string, string) ([]*core.ExternalCredential, error) {
	return nil, nil
}

func (*authRoundTripExternalCredentialProvider) DeleteCredential(context.Context, string) error {
	return nil
}

func (p *authRoundTripExternalCredentialProvider) ValidateCredentialConfig(_ context.Context, req *core.ValidateExternalCredentialConfigRequest) error {
	p.validateAuth = req.Auth
	return nil
}

func (p *authRoundTripExternalCredentialProvider) ResolveCredential(_ context.Context, req *core.ResolveExternalCredentialRequest) (*core.ResolveExternalCredentialResponse, error) {
	p.resolveAuth = req.Auth
	return &core.ResolveExternalCredentialResponse{Token: "resolved-token"}, nil
}

func (p *authRoundTripExternalCredentialProvider) ExchangeCredential(_ context.Context, req *core.ExchangeExternalCredentialRequest) (*core.ExchangeExternalCredentialResponse, error) {
	p.exchangeAuth = req.Auth
	return &core.ExchangeExternalCredentialResponse{}, nil
}

func TestExternalCredentialAuthConfigRefreshTokenRoundTripsOverTransport(t *testing.T) {
	t.Parallel()

	provider := &authRoundTripExternalCredentialProvider{}
	remote := newRemoteProvider(t, provider)
	auth := core.ExternalCredentialAuthConfig{
		Type:         "oauth2",
		GrantType:    "refresh_token",
		TokenURL:     "https://oauth2.example.test/token",
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RefreshToken: "refresh-token",
		RefreshParams: map[string]string{
			"audience": "gmail-platform",
		},
	}

	if err := remote.ValidateCredentialConfig(context.Background(), &core.ValidateExternalCredentialConfigRequest{Mode: core.ConnectionModeSubject, Auth: auth}); err != nil {
		t.Fatalf("ValidateCredentialConfig: %v", err)
	}
	if _, err := remote.ResolveCredential(context.Background(), &core.ResolveExternalCredentialRequest{Mode: core.ConnectionModeSubject, Auth: auth}); err != nil {
		t.Fatalf("ResolveCredential: %v", err)
	}
	if _, err := remote.ExchangeCredential(context.Background(), &core.ExchangeExternalCredentialRequest{Auth: auth}); err != nil {
		t.Fatalf("ExchangeCredential: %v", err)
	}
	for label, got := range map[string]core.ExternalCredentialAuthConfig{
		"validate": provider.validateAuth,
		"resolve":  provider.resolveAuth,
		"exchange": provider.exchangeAuth,
	} {
		if got.RefreshToken != "refresh-token" {
			t.Fatalf("%s refreshToken = %q, want refresh-token", label, got.RefreshToken)
		}
		if got.RefreshParams["audience"] != "gmail-platform" {
			t.Fatalf("%s refreshParams = %#v, want audience", label, got.RefreshParams)
		}
	}
}

func TestExternalCredentialRoundTripsOverTransport(t *testing.T) {
	t.Parallel()

	expiresAt := time.Unix(1_700_000_000, 0).UTC()
	lastRefreshedAt := time.Unix(1_700_000_100, 0).UTC()
	clientSecretExpiresAt := time.Unix(1_700_000_200, 0).UTC()

	cases := []struct {
		name       string
		credential core.ExternalCredential
	}{
		{
			name: "grant",
			credential: core.ExternalCredential{
				Subject:   "user:test",
				Audience:  "github:default",
				Qualifier: "org",
				Grant: &core.ExternalCredentialGrant{
					AccessToken:       "access-token",
					RefreshToken:      "refresh-token",
					Scope:             "repo",
					ExpiresAt:         &expiresAt,
					LastRefreshedAt:   &lastRefreshedAt,
					RefreshErrorCount: 2,
				},
				MetadataJSON: `{"workspace":"acme"}`,
			},
		},
		{
			name: "client",
			credential: core.ExternalCredential{
				Subject:  "user:test",
				Audience: "mcp:default",
				Client: &core.ExternalCredentialClientInfo{
					ClientID:              "client-id",
					ClientSecret:          "client-secret",
					ClientSecretExpiresAt: &clientSecretExpiresAt,
				},
			},
		},
		{
			name: "opaque",
			credential: core.ExternalCredential{
				Subject:  "user:test",
				Audience: "basic:default",
				Opaque: &core.ExternalCredentialOpaque{
					Fields: map[string]string{"username": "alice", "password": "secret"},
				},
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			remote := newRemoteProvider(t, coretesting.NewStubExternalCredentialProvider())
			want := tc.credential

			stored := tc.credential
			if err := remote.UpsertCredential(context.Background(), &stored); err != nil {
				t.Fatalf("UpsertCredential: %v", err)
			}
			if stored.ID == "" {
				t.Fatal("UpsertCredential left credential ID empty")
			}

			got, err := remote.GetCredential(context.Background(), want.Subject, want.Audience, want.Qualifier)
			if err != nil {
				t.Fatalf("GetCredential: %v", err)
			}
			if got.Subject != want.Subject || got.Audience != want.Audience || got.Qualifier != want.Qualifier {
				t.Fatalf("identity = (%q,%q,%q), want (%q,%q,%q)",
					got.Subject, got.Audience, got.Qualifier, want.Subject, want.Audience, want.Qualifier)
			}
			if got.MetadataJSON != want.MetadataJSON {
				t.Fatalf("metadataJSON = %q, want %q", got.MetadataJSON, want.MetadataJSON)
			}
			if !reflect.DeepEqual(got.Grant, want.Grant) {
				t.Fatalf("grant = %+v, want %+v", got.Grant, want.Grant)
			}
			if !reflect.DeepEqual(got.Client, want.Client) {
				t.Fatalf("client = %+v, want %+v", got.Client, want.Client)
			}
			if !reflect.DeepEqual(got.Opaque, want.Opaque) {
				t.Fatalf("opaque = %+v, want %+v", got.Opaque, want.Opaque)
			}
		})
	}
}

func TestExternalCredentialCreateConflictSurfacesAlreadyExistsOverTransport(t *testing.T) {
	t.Parallel()

	remote := newRemoteProvider(t, coretesting.NewStubExternalCredentialProvider())
	newCredential := func() *core.ExternalCredential {
		return &core.ExternalCredential{
			Subject:  "user:test",
			Audience: "github:default",
			Grant:    &core.ExternalCredentialGrant{AccessToken: "access-token"},
		}
	}

	if err := remote.CreateCredential(context.Background(), newCredential()); err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	if err := remote.CreateCredential(context.Background(), newCredential()); !errors.Is(err, core.ErrAlreadyExists) {
		t.Fatalf("CreateCredential conflict error = %v, want core.ErrAlreadyExists", err)
	}
}
