package pluginpkg

import (
	"strings"
	"testing"
)

func TestValidateSpecURLField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		wantErr string
	}{
		{name: "empty passes", value: "", wantErr: ""},
		{name: "http URL passes", value: "https://api.example.com/openapi.yaml", wantErr: ""},
		{name: "file scheme passes through", value: "file://openapi.yaml", wantErr: ""},
		{name: "valid relative passes", value: "openapi.yaml", wantErr: ""},
		{name: "valid nested relative passes", value: "specs/openapi.yaml", wantErr: ""},
		{name: "traversal blocked", value: "../../../etc/passwd", wantErr: "must stay within the package"},
		{name: "parent traversal blocked", value: "../openapi.yaml", wantErr: "must stay within the package"},
		{name: "absolute path blocked", value: "/etc/passwd", wantErr: "must be relative"},
		{name: "backslash blocked", value: "specs\\openapi.yaml", wantErr: "must use forward slashes"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateSpecURLField(tc.value, "test.field")
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}
