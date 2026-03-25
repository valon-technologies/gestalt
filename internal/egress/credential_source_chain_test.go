package egress_test

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/valon-technologies/gestalt/internal/egress"
)

type recordingCredentialSource struct {
	name       string
	order      *[]string
	credential egress.CredentialMaterialization
	err        error
}

func (s recordingCredentialSource) ResolveCredential(_ context.Context, _ egress.Subject, _ egress.Target) (egress.CredentialMaterialization, error) {
	*s.order = append(*s.order, s.name)
	if s.err != nil {
		return egress.CredentialMaterialization{}, s.err
	}
	return s.credential, nil
}

func TestCredentialSourceChainUsesFirstNonEmptyCredential(t *testing.T) {
	t.Parallel()

	order := []string{}
	resolver := egress.Resolver{
		Subjects: egress.StaticSubjectResolver{
			Subject: egress.Subject{Kind: egress.SubjectUser, ID: "u-100"},
		},
		CredentialSources: egress.CredentialSourceChain{
			Sources: []egress.CredentialSource{
				recordingCredentialSource{name: "first-empty", order: &order},
				recordingCredentialSource{
					name:  "second-hit",
					order: &order,
					credential: egress.CredentialMaterialization{
						Authorization: "Bearer second-token",
					},
				},
				recordingCredentialSource{
					name:  "third-should-not-run",
					order: &order,
					credential: egress.CredentialMaterialization{
						Authorization: "Bearer third-token",
					},
				},
			},
		},
	}

	res, err := resolver.Resolve(context.Background(), egress.ResolutionInput{
		Target: egress.Target{Provider: "acme", Host: "api.acme.dev"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Credential.Authorization != "Bearer second-token" {
		t.Fatalf("expected second source credential, got %q", res.Credential.Authorization)
	}

	wantOrder := []string{"first-empty", "second-hit"}
	if !slices.Equal(order, wantOrder) {
		t.Fatalf("evaluation order = %v, want %v", order, wantOrder)
	}
}

func TestCredentialSourceChainStopsOnError(t *testing.T) {
	t.Parallel()

	order := []string{}
	resolver := egress.Resolver{
		Subjects: egress.StaticSubjectResolver{
			Subject: egress.Subject{Kind: egress.SubjectUser, ID: "u-100"},
		},
		CredentialSources: egress.CredentialSourceChain{
			Sources: []egress.CredentialSource{
				recordingCredentialSource{
					name:  "first-error",
					order: &order,
					err:   fmt.Errorf("backend unavailable"),
				},
				recordingCredentialSource{
					name:  "second-should-not-run",
					order: &order,
					credential: egress.CredentialMaterialization{
						Authorization: "Bearer never",
					},
				},
			},
		},
	}

	_, err := resolver.Resolve(context.Background(), egress.ResolutionInput{
		Target: egress.Target{Provider: "acme", Host: "api.acme.dev"},
	})
	if err == nil {
		t.Fatal("expected error from first source")
	}

	wantOrder := []string{"first-error"}
	if !slices.Equal(order, wantOrder) {
		t.Fatalf("evaluation order = %v, want %v", order, wantOrder)
	}
}

func TestCredentialSourceChainPreservesProviderBackedResolution(t *testing.T) {
	t.Parallel()

	resolver := egress.Resolver{
		Subjects: egress.StaticSubjectResolver{
			Subject: egress.Subject{Kind: egress.SubjectUser, ID: "u-100"},
		},
		CredentialSources: egress.CredentialSourceChain{
			Sources: []egress.CredentialSource{
				&egress.ProviderCredentialResolver{
					TokenResolver: &staticTokenResolver{token: "provider-token"},
					Materializer:  &staticMaterializer{style: egress.AuthStyleBearer},
					Grants: []egress.CredentialGrant{
						{MatchCriteria: egress.MatchCriteria{Provider: "acme", Host: "api.acme.dev"}},
					},
				},
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
	if res.Credential.Authorization != "Bearer provider-token" {
		t.Fatalf("expected provider-backed credential, got %q", res.Credential.Authorization)
	}
}
