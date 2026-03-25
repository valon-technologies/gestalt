package egress_test

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/internal/egress"
)

var errDenied = errors.New("denied by path prefix policy")

// pathPrefixPolicy is a PolicyEnforcer that allows requests matching any of the
// configured path prefixes and denies everything else.
type pathPrefixPolicy struct {
	allowed []string
}

func (p pathPrefixPolicy) Evaluate(_ context.Context, input egress.PolicyInput) error {
	for _, prefix := range p.allowed {
		if egress.MatchPathPrefix(prefix, input.Target.Path) {
			return nil
		}
	}
	return errDenied
}

func TestPathPrefixPolicyThroughResolver(t *testing.T) {
	t.Parallel()

	resolver := egress.Resolver{
		Subjects: egress.StaticSubjectResolver{
			Subject: egress.Subject{Kind: egress.SubjectSystem, ID: "test"},
		},
		Policy: pathPrefixPolicy{allowed: []string{"/repos/", "/api/v1"}},
	}

	cases := []struct {
		name    string
		path    string
		allowed bool
	}{
		{"trailing-slash prefix matches nested path", "/repos/my-org/my-repo", true},
		{"trailing-slash prefix matches exact base", "/repos", true},
		{"trailing-slash prefix matches with slash", "/repos/", true},
		{"no-slash prefix matches nested path", "/api/v1/users", true},
		{"no-slash prefix matches exact", "/api/v1", true},
		{"rejects partial segment match", "/repositories", false},
		{"rejects partial segment on no-slash prefix", "/api/v10", false},
		{"rejects disjoint path", "/admin/settings", false},
	}

	ctx := context.Background()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := resolver.Resolve(ctx, egress.ResolutionInput{
				Target: egress.Target{
					Method: "GET",
					Host:   "api.example.com",
					Path:   tc.path,
				},
			})
			if tc.allowed && err != nil {
				t.Fatalf("path %q should be allowed, got: %v", tc.path, err)
			}
			if !tc.allowed && !errors.Is(err, errDenied) {
				t.Fatalf("path %q should be denied, got: %v", tc.path, err)
			}
		})
	}
}

func TestEmptyPrefixPolicyDeniesAllPaths(t *testing.T) {
	t.Parallel()

	resolver := egress.Resolver{
		Subjects: egress.StaticSubjectResolver{
			Subject: egress.Subject{Kind: egress.SubjectSystem, ID: "test"},
		},
		Policy: pathPrefixPolicy{allowed: []string{""}},
	}

	paths := []string{"/", "/anything", "/repos/foo"}
	ctx := context.Background()
	for _, path := range paths {
		_, err := resolver.Resolve(ctx, egress.ResolutionInput{
			Target: egress.Target{Method: "GET", Host: "api.example.com", Path: path},
		})
		if !errors.Is(err, errDenied) {
			t.Errorf("empty prefix should deny %q, got: %v", path, err)
		}
	}
}

func TestRootPrefixPolicyAllowsAllPaths(t *testing.T) {
	t.Parallel()

	resolver := egress.Resolver{
		Subjects: egress.StaticSubjectResolver{
			Subject: egress.Subject{Kind: egress.SubjectSystem, ID: "test"},
		},
		Policy: pathPrefixPolicy{allowed: []string{"/"}},
	}

	paths := []string{"/", "/anything", "/deeply/nested/path", "/repos/foo"}
	ctx := context.Background()
	for _, path := range paths {
		_, err := resolver.Resolve(ctx, egress.ResolutionInput{
			Target: egress.Target{Method: "GET", Host: "api.example.com", Path: path},
		})
		if err != nil {
			t.Errorf("root prefix should allow %q, got: %v", path, err)
		}
	}
}
