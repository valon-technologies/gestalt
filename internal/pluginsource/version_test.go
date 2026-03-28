package pluginsource

import (
	"testing"
)

func TestValidateVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		wantErr bool
	}{
		{name: "basic", version: "1.2.3"},
		{name: "zeros", version: "0.0.1"},
		{name: "one zero zero", version: "1.0.0"},
		{name: "prerelease", version: "1.0.0-alpha.1"},
		{name: "build metadata", version: "1.0.0+build.123"},
		{name: "prerelease and build", version: "1.0.0-beta.1+build.456"},

		{name: "reject leading v", version: "v1.2.3", wantErr: true},
		{name: "reject two segments", version: "1.2", wantErr: true},
		{name: "reject one segment", version: "1", wantErr: true},
		{name: "reject leading zero major", version: "01.2.3", wantErr: true},
		{name: "reject leading zero minor", version: "1.02.3", wantErr: true},
		{name: "reject empty", version: "", wantErr: true},
		{name: "reject leading whitespace", version: " 1.2.3", wantErr: true},
		{name: "reject trailing whitespace", version: "1.2.3 ", wantErr: true},
		{name: "reject leading zero in numeric prerelease", version: "1.0.0-01", wantErr: true},
		{name: "accept zero prerelease", version: "1.0.0-0"},
		{name: "accept alphanumeric prerelease with leading zero", version: "1.0.0-0abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateVersion(tt.version)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ValidateVersion(%q) succeeded, want error", tt.version)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateVersion(%q) error: %v", tt.version, err)
			}
		})
	}
}
