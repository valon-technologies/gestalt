package hex

import (
	"testing"
)

func TestHexResponseCheck(t *testing.T) {
	t.Parallel()

	t.Run("success passthrough", func(t *testing.T) {
		t.Parallel()
		if err := hexResponseCheck(200, []byte(`{"data":"ok"}`)); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("error with message", func(t *testing.T) {
		t.Parallel()
		err := hexResponseCheck(404, []byte(`{"message":"Project not found"}`))
		if err == nil {
			t.Fatal("expected error")
		}
		if got := err.Error(); got != "hex API error (HTTP 404): Project not found" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("error non-json", func(t *testing.T) {
		t.Parallel()
		err := hexResponseCheck(500, []byte(`internal server error`))
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
