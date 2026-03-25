package egress_test

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/internal/egress"
)

func TestStaticPolicyNoRulesAllowsByDefault(t *testing.T) {
	t.Parallel()

	resolver := egress.Resolver{
		Subjects: egress.StaticSubjectResolver{
			Subject: egress.Subject{Kind: egress.SubjectAgent, ID: "bot-1"},
		},
	}

	ctx := context.Background()
	_, err := resolver.Resolve(ctx, egress.ResolutionInput{
		Target: egress.Target{
			Provider:  "acme-api",
			Operation: "list_items",
			Method:    "GET",
			Host:      "api.acme.test",
			Path:      "/v1/items",
		},
	})
	if err != nil {
		t.Fatalf("no policy should allow all requests, got: %v", err)
	}
}

func TestStaticPolicyDenyRuleBlocksResolution(t *testing.T) {
	t.Parallel()

	resolver := egress.Resolver{
		Subjects: egress.StaticSubjectResolver{
			Subject: egress.Subject{Kind: egress.SubjectAgent, ID: "bot-1"},
		},
		Policy: egress.StaticPolicyEnforcer{
			DefaultAction: egress.PolicyAllow,
			Rules: []egress.StaticPolicyRule{
				{
					Action:        egress.PolicyDeny,
					MatchCriteria: egress.MatchCriteria{Provider: "secret-api"},
				},
			},
		},
	}

	ctx := context.Background()

	_, err := resolver.Resolve(ctx, egress.ResolutionInput{
		Target: egress.Target{
			Provider:  "secret-api",
			Operation: "read_secrets",
			Method:    "GET",
			Host:      "secrets.internal.test",
			Path:      "/v1/secrets",
		},
	})
	if !errors.Is(err, egress.ErrEgressDenied) {
		t.Fatalf("deny rule should block resolution, got: %v", err)
	}

	_, err = resolver.Resolve(ctx, egress.ResolutionInput{
		Target: egress.Target{
			Provider:  "public-api",
			Operation: "list_items",
			Method:    "GET",
			Host:      "api.public.test",
			Path:      "/v1/items",
		},
	})
	if err != nil {
		t.Fatalf("non-matching request should be allowed, got: %v", err)
	}
}

func TestStaticPolicyDefaultDenyBlocksUnmatched(t *testing.T) {
	t.Parallel()

	resolver := egress.Resolver{
		Subjects: egress.StaticSubjectResolver{
			Subject: egress.Subject{Kind: egress.SubjectUser, ID: "user-42"},
		},
		Policy: egress.StaticPolicyEnforcer{
			DefaultAction: egress.PolicyDeny,
			Rules: []egress.StaticPolicyRule{
				{
					Action:        egress.PolicyAllow,
					MatchCriteria: egress.MatchCriteria{Provider: "trusted-api"},
				},
			},
		},
	}

	ctx := context.Background()

	_, err := resolver.Resolve(ctx, egress.ResolutionInput{
		Target: egress.Target{
			Provider: "trusted-api",
			Method:   "POST",
			Host:     "api.trusted.test",
			Path:     "/v1/actions",
		},
	})
	if err != nil {
		t.Fatalf("allow rule should permit resolution, got: %v", err)
	}

	_, err = resolver.Resolve(ctx, egress.ResolutionInput{
		Target: egress.Target{
			Provider: "unknown-api",
			Method:   "GET",
			Host:     "api.unknown.test",
			Path:     "/v1/data",
		},
	})
	if !errors.Is(err, egress.ErrEgressDenied) {
		t.Fatalf("default-deny should block unmatched requests, got: %v", err)
	}
}

func TestStaticPolicyFirstMatchWins(t *testing.T) {
	t.Parallel()

	resolver := egress.Resolver{
		Subjects: egress.StaticSubjectResolver{
			Subject: egress.Subject{Kind: egress.SubjectAgent, ID: "bot-1"},
		},
		Policy: egress.StaticPolicyEnforcer{
			DefaultAction: egress.PolicyAllow,
			Rules: []egress.StaticPolicyRule{
				{
					Action:        egress.PolicyDeny,
					MatchCriteria: egress.MatchCriteria{Provider: "multi-api", PathPrefix: "/v1/admin"},
				},
				{
					Action:        egress.PolicyAllow,
					MatchCriteria: egress.MatchCriteria{Provider: "multi-api"},
				},
			},
		},
	}

	ctx := context.Background()

	_, err := resolver.Resolve(ctx, egress.ResolutionInput{
		Target: egress.Target{
			Provider: "multi-api",
			Method:   "GET",
			Host:     "api.multi.test",
			Path:     "/v1/admin/users",
		},
	})
	if !errors.Is(err, egress.ErrEgressDenied) {
		t.Fatalf("first matching rule (deny) should win over later allow, got: %v", err)
	}

	_, err = resolver.Resolve(ctx, egress.ResolutionInput{
		Target: egress.Target{
			Provider: "multi-api",
			Method:   "GET",
			Host:     "api.multi.test",
			Path:     "/v1/public/items",
		},
	})
	if err != nil {
		t.Fatalf("second rule (allow) should match non-admin paths, got: %v", err)
	}
}

func TestStaticPolicyMultiFieldMatch(t *testing.T) {
	t.Parallel()

	resolver := egress.Resolver{
		Subjects: egress.StaticSubjectResolver{
			Subject: egress.Subject{Kind: egress.SubjectAgent, ID: "bot-1"},
		},
		Policy: egress.StaticPolicyEnforcer{
			DefaultAction: egress.PolicyAllow,
			Rules: []egress.StaticPolicyRule{
				{
					Action: egress.PolicyDeny,
					MatchCriteria: egress.MatchCriteria{
						SubjectKind: egress.SubjectAgent,
						Provider:    "restricted-api",
						Method:      "DELETE",
					},
				},
			},
		},
	}

	ctx := context.Background()

	_, err := resolver.Resolve(ctx, egress.ResolutionInput{
		Target: egress.Target{
			Provider: "restricted-api",
			Method:   "DELETE",
			Host:     "api.restricted.test",
			Path:     "/v1/resources/123",
		},
	})
	if !errors.Is(err, egress.ErrEgressDenied) {
		t.Fatalf("all-fields-match rule should deny, got: %v", err)
	}

	_, err = resolver.Resolve(ctx, egress.ResolutionInput{
		Target: egress.Target{
			Provider: "restricted-api",
			Method:   "GET",
			Host:     "api.restricted.test",
			Path:     "/v1/resources/123",
		},
	})
	if err != nil {
		t.Fatalf("partial match (wrong method) should not trigger deny, got: %v", err)
	}
}
