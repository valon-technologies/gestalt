package daemon

import (
	"strings"
	"testing"
)

func TestMaybeRunServeProviderLocalRejectsRemoteWithRemotePreview(t *testing.T) {
	t.Parallel()

	_, err := maybeRunServeProviderLocal(serveProviderLocalOptions{
		Paths:         []string{"./app"},
		Remote:        "https://remote.test",
		RemotePreview: "https://preview.test",
	})
	if err == nil || !strings.Contains(err.Error(), "cannot be used together") {
		t.Fatalf("error = %v, want mutual exclusion", err)
	}
}
