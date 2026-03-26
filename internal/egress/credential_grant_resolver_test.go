package egress

import (
	"context"
	"fmt"
	"testing"
)

type stubSecretResolver struct {
	secrets map[string]string
}

func (r *stubSecretResolver) GetSecret(_ context.Context, name string) (string, error) {
	v, ok := r.secrets[name]
	if !ok {
		return "", fmt.Errorf("secret %q not found", name)
	}
	return v, nil
}

type stubTokenResolver struct {
	token string
	err   error
}

func (r *stubTokenResolver) ResolveProviderToken(_ context.Context, _ Subject, _, _ string) (string, error) {
	return r.token, r.err
}

func TestCredentialGrantResolver_SecretBackedGrant(t *testing.T) {
	t.Parallel()

	sr := &stubSecretResolver{secrets: map[string]string{"api-key": "sk-secret"}}
	r := &CredentialGrantResolver{
		Loaders: []CredentialGrantLoader{
			&StaticCredentialGrantLoader{Grants: []CredentialGrant{
				{
					SecretRef:     "api-key",
					AuthStyle:     AuthStyleRaw,
					MatchCriteria: MatchCriteria{Host: "api.vendor.test"},
					Source:        &SecretCredentialSource{Resolver: sr, SecretRef: "api-key", AuthStyle: AuthStyleRaw},
				},
			}},
		},
	}

	mat, err := r.ResolveCredential(context.Background(), Subject{Kind: SubjectAgent, ID: "a-1"}, Target{Host: "api.vendor.test", Method: "GET"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mat.Authorization != "sk-secret" {
		t.Fatalf("expected raw secret, got %q", mat.Authorization)
	}
}

func TestCredentialGrantResolver_ProviderTokenGrant(t *testing.T) {
	t.Parallel()

	tr := &stubTokenResolver{token: "tok-abc"}
	r := &CredentialGrantResolver{
		Loaders: []CredentialGrantLoader{
			&StaticCredentialGrantLoader{Grants: []CredentialGrant{
				{
					MatchCriteria: MatchCriteria{Provider: "vendorx"},
					Source:        &ProviderTokenCredentialSource{TokenResolver: tr},
				},
			}},
		},
	}

	mat, err := r.ResolveCredential(context.Background(), Subject{Kind: SubjectUser, ID: "u-1"}, Target{Provider: "vendorx", Host: "api.test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mat.Authorization != "Bearer tok-abc" {
		t.Fatalf("expected Bearer tok-abc, got %q", mat.Authorization)
	}
}

func TestCredentialGrantResolver_NonPrincipalSubjectReturnsEmpty(t *testing.T) {
	t.Parallel()

	tr := &stubTokenResolver{token: "tok-abc"}
	r := &CredentialGrantResolver{
		Loaders: []CredentialGrantLoader{
			&StaticCredentialGrantLoader{Grants: []CredentialGrant{
				{
					MatchCriteria: MatchCriteria{Provider: "vendorx"},
					Source:        &ProviderTokenCredentialSource{TokenResolver: tr},
				},
			}},
		},
	}

	mat, err := r.ResolveCredential(context.Background(), Subject{Kind: SubjectSystem, ID: "sys"}, Target{Provider: "vendorx", Host: "api.test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mat.Authorization != "" || len(mat.Headers) > 0 {
		t.Fatalf("expected empty materialization for non-principal subject, got %+v", mat)
	}
}

func TestCredentialGrantResolver_SecretGrantWorksForNonPrincipal(t *testing.T) {
	t.Parallel()

	sr := &stubSecretResolver{secrets: map[string]string{"key": "secret-val"}}
	r := &CredentialGrantResolver{
		Loaders: []CredentialGrantLoader{
			&StaticCredentialGrantLoader{Grants: []CredentialGrant{
				{
					SecretRef:     "key",
					AuthStyle:     AuthStyleRaw,
					MatchCriteria: MatchCriteria{Host: "api.test"},
					Source:        &SecretCredentialSource{Resolver: sr, SecretRef: "key", AuthStyle: AuthStyleRaw},
				},
			}},
		},
	}

	mat, err := r.ResolveCredential(context.Background(), Subject{Kind: SubjectSystem, ID: "sys"}, Target{Host: "api.test", Method: "GET"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mat.Authorization != "secret-val" {
		t.Fatalf("expected secret-val, got %q", mat.Authorization)
	}
}

func TestCredentialGrantResolver_LoaderErrorFailsClosed(t *testing.T) {
	t.Parallel()

	loaderErr := fmt.Errorf("store unavailable")
	sr := &stubSecretResolver{secrets: map[string]string{"key": "val"}}
	r := &CredentialGrantResolver{
		Loaders: []CredentialGrantLoader{
			CredentialGrantLoaderFunc(func(_ context.Context) ([]CredentialGrant, error) {
				return nil, loaderErr
			}),
			&StaticCredentialGrantLoader{Grants: []CredentialGrant{
				{
					SecretRef:     "key",
					AuthStyle:     AuthStyleRaw,
					MatchCriteria: MatchCriteria{Host: "api.test"},
					Source:        &SecretCredentialSource{Resolver: sr, SecretRef: "key", AuthStyle: AuthStyleRaw},
				},
			}},
		},
	}

	_, err := r.ResolveCredential(context.Background(), Subject{Kind: SubjectAgent, ID: "a-1"}, Target{Host: "api.test", Method: "GET"})
	if err == nil {
		t.Fatal("expected loader error to propagate")
	}
}

func TestCredentialGrantResolver_FirstLoaderWins(t *testing.T) {
	t.Parallel()

	sr := &stubSecretResolver{secrets: map[string]string{
		"first-key":  "first-val",
		"second-key": "second-val",
	}}
	r := &CredentialGrantResolver{
		Loaders: []CredentialGrantLoader{
			&StaticCredentialGrantLoader{Grants: []CredentialGrant{
				{
					SecretRef:     "first-key",
					AuthStyle:     AuthStyleRaw,
					MatchCriteria: MatchCriteria{Host: "api.test"},
					Source:        &SecretCredentialSource{Resolver: sr, SecretRef: "first-key", AuthStyle: AuthStyleRaw},
				},
			}},
			&StaticCredentialGrantLoader{Grants: []CredentialGrant{
				{
					SecretRef:     "second-key",
					AuthStyle:     AuthStyleRaw,
					MatchCriteria: MatchCriteria{Host: "api.test"},
					Source:        &SecretCredentialSource{Resolver: sr, SecretRef: "second-key", AuthStyle: AuthStyleRaw},
				},
			}},
		},
	}

	mat, err := r.ResolveCredential(context.Background(), Subject{Kind: SubjectAgent, ID: "a-1"}, Target{Host: "api.test", Method: "GET"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mat.Authorization != "first-val" {
		t.Fatalf("expected first loader to win, got %q", mat.Authorization)
	}
}

func TestCredentialGrantResolver_FallsToSecondLoader(t *testing.T) {
	t.Parallel()

	sr := &stubSecretResolver{secrets: map[string]string{
		"first-key":  "first-val",
		"second-key": "second-val",
	}}
	r := &CredentialGrantResolver{
		Loaders: []CredentialGrantLoader{
			&StaticCredentialGrantLoader{Grants: []CredentialGrant{
				{
					SecretRef:     "first-key",
					AuthStyle:     AuthStyleRaw,
					MatchCriteria: MatchCriteria{Host: "other.test"},
					Source:        &SecretCredentialSource{Resolver: sr, SecretRef: "first-key", AuthStyle: AuthStyleRaw},
				},
			}},
			&StaticCredentialGrantLoader{Grants: []CredentialGrant{
				{
					SecretRef:     "second-key",
					AuthStyle:     AuthStyleRaw,
					MatchCriteria: MatchCriteria{Host: "api.test"},
					Source:        &SecretCredentialSource{Resolver: sr, SecretRef: "second-key", AuthStyle: AuthStyleRaw},
				},
			}},
		},
	}

	mat, err := r.ResolveCredential(context.Background(), Subject{Kind: SubjectAgent, ID: "a-1"}, Target{Host: "api.test", Method: "GET"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mat.Authorization != "second-val" {
		t.Fatalf("expected second loader fallback, got %q", mat.Authorization)
	}
}

func TestCredentialGrantResolver_EmptyResultFallsToNextLoader(t *testing.T) {
	t.Parallel()

	tr := &stubTokenResolver{token: "tok-abc"}
	sr := &stubSecretResolver{secrets: map[string]string{"fallback-key": "secret-val"}}
	r := &CredentialGrantResolver{
		Loaders: []CredentialGrantLoader{
			&StaticCredentialGrantLoader{Grants: []CredentialGrant{
				{
					MatchCriteria: MatchCriteria{Provider: "vendorx"},
					Source:        &ProviderTokenCredentialSource{TokenResolver: tr},
				},
			}},
			&StaticCredentialGrantLoader{Grants: []CredentialGrant{
				{
					SecretRef:     "fallback-key",
					AuthStyle:     AuthStyleRaw,
					MatchCriteria: MatchCriteria{Provider: "vendorx"},
					Source:        &SecretCredentialSource{Resolver: sr, SecretRef: "fallback-key", AuthStyle: AuthStyleRaw},
				},
			}},
		},
	}

	// Non-principal subject matches the provider-based grant in the first loader
	// but can't resolve tokens. The resolver should fall through to the second
	// loader's secret-based grant instead of returning empty.
	mat, err := r.ResolveCredential(context.Background(), Subject{Kind: SubjectSystem, ID: "sys"}, Target{Provider: "vendorx", Host: "api.test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mat.Authorization != "secret-val" {
		t.Fatalf("expected fallback to secret grant in second loader, got %q", mat.Authorization)
	}
}

// CredentialGrantLoaderFunc is a function adapter for CredentialGrantLoader.
type CredentialGrantLoaderFunc func(ctx context.Context) ([]CredentialGrant, error)

func (f CredentialGrantLoaderFunc) LoadCredentialGrants(ctx context.Context) ([]CredentialGrant, error) {
	return f(ctx)
}
