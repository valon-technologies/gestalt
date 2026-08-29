package semvervalidate

import "testing"

func TestValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		version string
		want    bool
	}{
		{version: "1.2.3", want: true},
		{version: "1.0.0-alpha.1", want: true},
		{version: "0.0.2-alpha.16", want: true},
		{version: "v1.2.3", want: false},
		{version: "", want: false},
		{version: "not-a-version", want: false},
		{version: "01.02.03", want: false},
	}
	for _, tc := range tests {
		if got := Valid(tc.version); got != tc.want {
			t.Fatalf("Valid(%q) = %v, want %v", tc.version, got, tc.want)
		}
	}
}
