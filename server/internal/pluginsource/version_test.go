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
		{name: "valid", version: "1.2.3"},
		{name: "valid with prerelease", version: "1.0.0-alpha.1"},

		{name: "reject leading v", version: "v1.2.3", wantErr: true},
		{name: "reject empty", version: "", wantErr: true},
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
