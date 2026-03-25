package egress_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/valon-technologies/gestalt/internal/egress"
)

type staticTokenResolver struct {
	token string
	err   error
}

func (r *staticTokenResolver) ResolveProviderToken(_ context.Context, _ egress.Subject, _, _ string) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	return r.token, nil
}

type staticMaterializer struct {
	style egress.AuthStyle
}

func (m *staticMaterializer) MaterializeProviderCredential(_ string, token string) (egress.CredentialMaterialization, error) {
	return egress.MaterializeCredential(token, m.style, nil)
}

func TestResolverUsesCredentialResolverForMatchingGrant(t *testing.T) {
	t.Parallel()

	resolver := egress.Resolver{
		Subjects: egress.StaticSubjectResolver{
			Subject: egress.Subject{Kind: egress.SubjectUser, ID: "u-100"},
		},
		Credentials: &egress.ProviderCredentialResolver{
			TokenResolver: &staticTokenResolver{token: "resolved-tok"},
			Materializer:  &staticMaterializer{style: egress.AuthStyleBearer},
			Grants: []egress.CredentialGrant{
				{MatchCriteria: egress.MatchCriteria{Provider: "acme", Host: "api.acme.dev"}},
			},
		},
	}

	res, err := resolver.Resolve(context.Background(), egress.ResolutionInput{
		Target: egress.Target{
			Provider: "acme",
			Method:   "GET",
			Host:     "api.acme.dev",
			Path:     "/v1/items",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Credential.Authorization != "Bearer resolved-tok" {
		t.Fatalf("expected resolved credential, got %q", res.Credential.Authorization)
	}
}

func TestResolverPreservesExplicitCredential(t *testing.T) {
	t.Parallel()

	resolver := egress.Resolver{
		Subjects: egress.StaticSubjectResolver{
			Subject: egress.Subject{Kind: egress.SubjectUser, ID: "u-100"},
		},
		Credentials: &egress.ProviderCredentialResolver{
			TokenResolver: &staticTokenResolver{token: "should-not-be-used"},
			Materializer:  &staticMaterializer{style: egress.AuthStyleBearer},
			Grants: []egress.CredentialGrant{
				{MatchCriteria: egress.MatchCriteria{Provider: "acme"}},
			},
		},
	}

	res, err := resolver.Resolve(context.Background(), egress.ResolutionInput{
		Target: egress.Target{
			Provider: "acme",
			Method:   "POST",
			Host:     "api.acme.dev",
			Path:     "/v1/items",
		},
		Credential: egress.CredentialMaterialization{
			Authorization: "Bearer explicit-tok",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Credential.Authorization != "Bearer explicit-tok" {
		t.Fatalf("expected explicit credential preserved, got %q", res.Credential.Authorization)
	}
}

func TestResolverReturnsEmptyWhenNoGrantMatches(t *testing.T) {
	t.Parallel()

	resolver := egress.Resolver{
		Subjects: egress.StaticSubjectResolver{
			Subject: egress.Subject{Kind: egress.SubjectUser, ID: "u-100"},
		},
		Credentials: &egress.ProviderCredentialResolver{
			TokenResolver: &staticTokenResolver{token: "should-not-be-used"},
			Materializer:  &staticMaterializer{style: egress.AuthStyleBearer},
			Grants: []egress.CredentialGrant{
				{MatchCriteria: egress.MatchCriteria{Provider: "other-provider"}},
			},
		},
	}

	res, err := resolver.Resolve(context.Background(), egress.ResolutionInput{
		Target: egress.Target{
			Provider: "acme",
			Method:   "GET",
			Host:     "api.acme.dev",
			Path:     "/v1/items",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Credential.Authorization != "" {
		t.Fatalf("expected no credential, got %q", res.Credential.Authorization)
	}
}

func TestResolverPropagatesCredentialResolutionError(t *testing.T) {
	t.Parallel()

	resolver := egress.Resolver{
		Subjects: egress.StaticSubjectResolver{
			Subject: egress.Subject{Kind: egress.SubjectUser, ID: "u-100"},
		},
		Credentials: &egress.ProviderCredentialResolver{
			TokenResolver: &staticTokenResolver{err: fmt.Errorf("token store unavailable")},
			Materializer:  &staticMaterializer{style: egress.AuthStyleBearer},
			Grants: []egress.CredentialGrant{
				{MatchCriteria: egress.MatchCriteria{Provider: "acme"}},
			},
		},
	}

	_, err := resolver.Resolve(context.Background(), egress.ResolutionInput{
		Target: egress.Target{
			Provider: "acme",
			Method:   "GET",
			Host:     "api.acme.dev",
			Path:     "/v1/items",
		},
	})
	if err == nil {
		t.Fatal("expected error when token resolution fails")
	}
}

func TestCredentialGrantMatchesPathPrefix(t *testing.T) {
	t.Parallel()

	resolver := egress.Resolver{
		Subjects: egress.StaticSubjectResolver{
			Subject: egress.Subject{Kind: egress.SubjectUser, ID: "u-100"},
		},
		Credentials: &egress.ProviderCredentialResolver{
			TokenResolver: &staticTokenResolver{token: "scoped-tok"},
			Materializer:  &staticMaterializer{style: egress.AuthStyleBearer},
			Grants: []egress.CredentialGrant{
				{MatchCriteria: egress.MatchCriteria{Provider: "acme", PathPrefix: "/v1/items"}},
			},
		},
	}

	ctx := context.Background()

	res, err := resolver.Resolve(ctx, egress.ResolutionInput{
		Target: egress.Target{
			Provider: "acme",
			Method:   "GET",
			Host:     "api.acme.dev",
			Path:     "/v1/items/123",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Credential.Authorization != "Bearer scoped-tok" {
		t.Fatalf("expected credential for matching path, got %q", res.Credential.Authorization)
	}

	res, err = resolver.Resolve(ctx, egress.ResolutionInput{
		Target: egress.Target{
			Provider: "acme",
			Method:   "GET",
			Host:     "api.acme.dev",
			Path:     "/v2/other",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Credential.Authorization != "" {
		t.Fatalf("expected no credential for non-matching path, got %q", res.Credential.Authorization)
	}
}

func TestCredentialGrantMatchesSubjectKind(t *testing.T) {
	t.Parallel()

	resolver := egress.Resolver{
		Subjects: egress.StaticSubjectResolver{
			Subject: egress.Subject{Kind: egress.SubjectSystem, ID: "daemon"},
		},
		Credentials: &egress.ProviderCredentialResolver{
			TokenResolver: &staticTokenResolver{token: "user-tok"},
			Materializer:  &staticMaterializer{style: egress.AuthStyleBearer},
			Grants: []egress.CredentialGrant{
				{MatchCriteria: egress.MatchCriteria{Provider: "acme", SubjectKind: egress.SubjectUser}},
			},
		},
	}

	res, err := resolver.Resolve(context.Background(), egress.ResolutionInput{
		Target: egress.Target{
			Provider: "acme",
			Method:   "GET",
			Host:     "api.acme.dev",
			Path:     "/v1/items",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Credential.Authorization != "" {
		t.Fatalf("expected no credential for non-matching subject kind, got %q", res.Credential.Authorization)
	}
}

func TestCredentialFallsBackToBearerWithoutMaterializer(t *testing.T) {
	t.Parallel()

	resolver := egress.Resolver{
		Subjects: egress.StaticSubjectResolver{
			Subject: egress.Subject{Kind: egress.SubjectUser, ID: "u-100"},
		},
		Credentials: &egress.ProviderCredentialResolver{
			TokenResolver: &staticTokenResolver{token: "fallback-tok"},
			Grants: []egress.CredentialGrant{
				{MatchCriteria: egress.MatchCriteria{Provider: "acme"}},
			},
		},
	}

	res, err := resolver.Resolve(context.Background(), egress.ResolutionInput{
		Target: egress.Target{
			Provider: "acme",
			Method:   "GET",
			Host:     "api.acme.dev",
			Path:     "/v1/items",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Credential.Authorization != "Bearer fallback-tok" {
		t.Fatalf("expected bearer fallback, got %q", res.Credential.Authorization)
	}
}
