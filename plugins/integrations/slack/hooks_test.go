package slack

import (
	"testing"
)

func TestSlackOKCheck(t *testing.T) {
	t.Parallel()

	t.Run("ok true", func(t *testing.T) {
		t.Parallel()
		if err := slackOKCheck(200, []byte(`{"ok":true,"channels":[]}`)); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("ok false", func(t *testing.T) {
		t.Parallel()
		err := slackOKCheck(200, []byte(`{"ok":false,"error":"channel_not_found"}`))
		if err == nil {
			t.Fatal("expected error")
		}
		if got := err.Error(); got != "slack API error: channel_not_found" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("ok false no error field", func(t *testing.T) {
		t.Parallel()
		err := slackOKCheck(200, []byte(`{"ok":false}`))
		if err == nil {
			t.Fatal("expected error")
		}
		if got := err.Error(); got != "slack API error: unknown error" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("non-json 200 passthrough", func(t *testing.T) {
		t.Parallel()
		if err := slackOKCheck(200, []byte(`not json`)); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("non-json 500 falls back to status check", func(t *testing.T) {
		t.Parallel()
		err := slackOKCheck(500, []byte(`internal server error`))
		if err == nil {
			t.Fatal("expected error for non-JSON 500")
		}
	})
}
