package providermanifestv1

import "testing"

func TestNormalizeKindMapsLegacyPluginToApp(t *testing.T) {
	t.Parallel()

	if got := NormalizeKind("plugin"); got != KindApp {
		t.Fatalf("NormalizeKind(\"plugin\") = %q, want %q", got, KindApp)
	}
}
