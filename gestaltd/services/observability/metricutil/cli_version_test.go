package metricutil

import (
	"strings"
	"testing"
)

func TestNormalizeCLIVersionAcceptsDeclaredSemver(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw  string
		want string
	}{
		{raw: "0.0.2-alpha.17", want: "0.0.2-alpha.17"},
		{raw: " 0.0.2-alpha.17 ", want: "0.0.2-alpha.17"},
		{raw: "0.0.2-alpha.16", want: "0.0.2-alpha.16"},
		{raw: "1.2.3", want: "1.2.3"},
		{raw: "v1.2.3", want: "v1.2.3"},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeCLIVersion(tc.raw); got != tc.want {
				t.Fatalf("NormalizeCLIVersion(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestNormalizeCLIVersionRejectsInvalidHeaders(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"",
		"   ",
		"not-a-version",
		"01.02.03",
		strings.Repeat("0", maxCLIVersionHeaderLen+1),
	} {
		if got := NormalizeCLIVersion(raw); got != ClientVersionUnknown {
			t.Fatalf("NormalizeCLIVersion(%q) = %q, want %q", raw, got, ClientVersionUnknown)
		}
	}
}
