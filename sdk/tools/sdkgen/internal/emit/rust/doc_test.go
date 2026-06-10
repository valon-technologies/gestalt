package rust

import "testing"

// TestSanitizeDocLine covers the rustdoc-warning escapes no current proto
// comment exercises: square brackets must not parse as intra-doc links and
// bare URLs must satisfy rustdoc::bare_urls.
func TestSanitizeDocLine(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"plain prose stays untouched", "plain prose stays untouched"},
		{"backticks `keep` their meaning", "backticks `keep` their meaning"},
		{"values in [0, 10) are accepted", `values in \[0, 10) are accepted`},
		{"see [RFC 3339] for details", `see \[RFC 3339\] for details`},
		{"see https://example.com/spec for details", "see <https://example.com/spec> for details"},
		{"docs at https://example.com.", "docs at <https://example.com>."},
		{"already wrapped <https://example.com> stays", "already wrapped <https://example.com> stays"},
		{"http://a.test and [b] and https://c.test/d?x=1", `<http://a.test> and \[b\] and <https://c.test/d?x=1>`},
	}
	for _, tc := range cases {
		if got := sanitizeDocLine(tc.in); got != tc.want {
			t.Errorf("sanitizeDocLine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
