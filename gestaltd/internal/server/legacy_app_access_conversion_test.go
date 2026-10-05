package server

import "testing"

// The conversion turns on operations an app made default-on after a user's
// allow-list was saved, which is what overloaded an upstream provider on
// valon.tools. It stays off unless an operator asks for it.
func TestLegacyAppAccessConversionIsOffUnlessRequested(t *testing.T) {
	for name, tc := range map[string]struct {
		value string
		want  bool
	}{
		"unset":   {value: "", want: false},
		"enabled": {value: "1", want: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(legacyAppAccessConversionEnv, tc.value)
			if got := legacyAppAccessConversionEnabled(); got != tc.want {
				t.Fatalf("legacyAppAccessConversionEnabled() = %v, want %v", got, tc.want)
			}
		})
	}
}
