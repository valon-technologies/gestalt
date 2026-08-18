package core

import (
	"testing"
	"time"
)

func TestMarkGrantReconnectRequired(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 18, 17, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)

	t.Run("unexpired grant is force-expired", func(t *testing.T) {
		grant := &ExternalCredentialGrant{
			AccessToken: "tok",
			ExpiresAt:   &future,
		}
		if !MarkGrantReconnectRequired(grant, now) {
			t.Fatal("expected grant to change")
		}
		if grant.RefreshErrorCount != 1 {
			t.Fatalf("RefreshErrorCount = %d, want 1", grant.RefreshErrorCount)
		}
		if grant.ExpiresAt == nil || grant.ExpiresAt.After(now) {
			t.Fatalf("ExpiresAt = %v, want not after now", grant.ExpiresAt)
		}
	})

	t.Run("already reconnect-required is a no-op", func(t *testing.T) {
		past := now.Add(-time.Minute)
		grant := &ExternalCredentialGrant{
			AccessToken:       "tok",
			ExpiresAt:         &past,
			RefreshErrorCount: 2,
		}
		if MarkGrantReconnectRequired(grant, now) {
			t.Fatal("already-invalid grant should not change")
		}
		if grant.RefreshErrorCount != 2 {
			t.Fatalf("RefreshErrorCount = %d, want 2", grant.RefreshErrorCount)
		}
	})

	t.Run("nil grant is a no-op", func(t *testing.T) {
		if MarkGrantReconnectRequired(nil, now) {
			t.Fatal("nil grant should not change")
		}
	})
}
