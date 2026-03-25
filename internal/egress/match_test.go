package egress_test

import (
	"testing"

	"github.com/valon-technologies/gestalt/internal/egress"
)

func TestMatchPathPrefix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		prefix string
		path   string
		want   bool
	}{
		// Root prefix matches everything.
		{"/", "/", true},
		{"/", "/anything", true},
		{"/", "/a/b/c", true},

		// Trailing slash on root is equivalent.
		{"//", "/foo", true},

		// Exact segment match.
		{"/repos", "/repos", true},
		{"/repos/", "/repos", true},

		// Nested paths under the prefix.
		{"/repos", "/repos/foo", true},
		{"/repos/", "/repos/foo", true},
		{"/repos/", "/repos/foo/bar", true},

		// Trailing slash on path still matches.
		{"/repos", "/repos/", true},
		{"/repos/", "/repos/", true},

		// Multi-segment prefix.
		{"/api/v1", "/api/v1/users", true},
		{"/api/v1/", "/api/v1/users", true},
		{"/api/v1", "/api/v1", true},

		// Must not match partial segments.
		{"/repos", "/repositories", false},
		{"/api", "/apikeys", false},
		{"/api/v1", "/api/v10", false},

		// Disjoint paths.
		{"/foo", "/bar", false},
		{"/foo/bar", "/foo/baz", false},
	}

	for _, tc := range cases {
		got := egress.MatchPathPrefix(tc.prefix, tc.path)
		if got != tc.want {
			t.Errorf("MatchPathPrefix(%q, %q) = %v, want %v", tc.prefix, tc.path, got, tc.want)
		}
	}
}
