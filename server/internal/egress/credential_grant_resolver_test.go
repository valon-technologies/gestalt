package egress

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

type stubSecretResolver struct {
	secrets map[string]string
	lookups []string
}

func (s *stubSecretResolver) GetSecret(_ context.Context, name string) (string, error) {
	s.lookups = append(s.lookups, name)
	return s.secrets[name], nil
}

func TestCredentialGrantResolver_ConfigGrant(t *testing.T) {
	t.Parallel()

	secrets := &stubSecretResolver{
		secrets: map[string]string{"vendor-api-key": "sk-test-secret-abc123"},
	}
	resolver := &CredentialGrantResolver{
		Loaders: []CredentialGrantLoader{
			&StaticCredentialGrantLoader{
				Grants: []CredentialGrant{
					{
						SecretRef: "vendor-api-key",
						AuthStyle: AuthStyleBearer,
						MatchCriteria: MatchCriteria{
							Host: "api.vendor.test",
						},
					},
				},
			},
		},
		SecretResolver: secrets,
	}

	materialized, err := resolver.ResolveCredential(context.Background(),
		Subject{Kind: SubjectUser, ID: "user-1"},
		Target{Method: http.MethodGet, Host: "api.vendor.test", Path: "/v1/data"},
	)
	if err != nil {
		t.Fatalf("ResolveCredential: %v", err)
	}

	const wantAuth = "Bearer sk-test-secret-abc123"
	if materialized.Authorization != wantAuth {
		t.Fatalf("Authorization = %q, want %q", materialized.Authorization, wantAuth)
	}
	if len(secrets.lookups) != 1 || secrets.lookups[0] != "vendor-api-key" {
		t.Fatalf("secret lookups = %v, want [vendor-api-key]", secrets.lookups)
	}
}

func TestCredentialGrantResolver_MultiTenantHostMatching(t *testing.T) {
	t.Parallel()

	secrets := &stubSecretResolver{
		secrets: map[string]string{
			"shop-1-key": "sk-shop-one",
			"shop-2-key": "sk-shop-two",
		},
	}
	resolver := &CredentialGrantResolver{
		Loaders: []CredentialGrantLoader{
			&StaticCredentialGrantLoader{
				Grants: []CredentialGrant{
					{
						SecretRef: "shop-1-key",
						AuthStyle: AuthStyleRaw,
						MatchCriteria: MatchCriteria{
							Host: "shop-1.example.com",
						},
					},
					{
						SecretRef: "shop-2-key",
						AuthStyle: AuthStyleRaw,
						MatchCriteria: MatchCriteria{
							Host: "shop-2.example.com",
						},
					},
				},
			},
		},
		SecretResolver: secrets,
	}

	resolve := func(host string) string {
		t.Helper()
		materialized, err := resolver.ResolveCredential(context.Background(),
			Subject{Kind: SubjectUser, ID: "user-1"},
			Target{Method: http.MethodGet, Host: host, Path: "/api/orders"},
		)
		if err != nil {
			t.Fatalf("ResolveCredential(%s): %v", host, err)
		}
		return materialized.Authorization
	}

	if auth := resolve("shop-1.example.com"); auth != "sk-shop-one" {
		t.Fatalf("shop-1 auth = %q, want %q", auth, "sk-shop-one")
	}
	if auth := resolve("shop-2.example.com"); auth != "sk-shop-two" {
		t.Fatalf("shop-2 auth = %q, want %q", auth, "sk-shop-two")
	}
}

func TestCredentialGrantResolver_RejectsSecretURIPrefix(t *testing.T) {
	t.Parallel()

	secrets := &stubSecretResolver{
		secrets: map[string]string{"prefixed-key": "sk-with-prefix"},
	}
	resolver := &CredentialGrantResolver{
		Loaders: []CredentialGrantLoader{
			&StaticCredentialGrantLoader{
				Grants: []CredentialGrant{
					{
						SecretRef: "secret://prefixed-key",
						AuthStyle: AuthStyleBearer,
						MatchCriteria: MatchCriteria{
							Host: "api.prefix.test",
						},
					},
				},
			},
		},
		SecretResolver: secrets,
	}

	materialized, err := resolver.ResolveCredential(context.Background(),
		Subject{Kind: SubjectUser, ID: "user-1"},
		Target{Method: http.MethodGet, Host: "api.prefix.test", Path: "/v1"},
	)
	if err == nil {
		t.Fatal("ResolveCredential: expected error, got nil")
	}
	if materialized.Authorization != "" || len(materialized.Headers) != 0 {
		t.Fatalf("materialized = %+v, want empty authorization and headers", materialized)
	}
	if len(secrets.lookups) != 0 {
		t.Fatalf("secret lookups = %v, want []", secrets.lookups)
	}
	if !strings.Contains(err.Error(), "bare secret name") {
		t.Fatalf("error = %q, want bare secret name guidance", err)
	}
}
