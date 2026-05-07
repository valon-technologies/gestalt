package egressproxy

import (
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/services/egress"
)

func TestTokenManagerImpersonationRoundTrip(t *testing.T) {
	t.Parallel()

	mgr, err := NewTokenManager([]byte("test-secret-32-bytes-aaaaaaaaaaa"))
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}

	req := TokenRequest{
		PluginName:      "",
		AllowedHosts:    []string{"bigquery.googleapis.com", "api.linear.app"},
		DefaultAction:   egress.PolicyDeny,
		TTL:             1 * time.Hour,
		CallerSubjectID: "service_account:nicolebot",
		MayImpersonate:  true,
	}

	token, err := mgr.MintToken(req)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	got, err := mgr.ResolveToken(token)
	if err != nil {
		t.Fatalf("ResolveToken: %v", err)
	}
	if got.CallerSubjectID != "service_account:nicolebot" {
		t.Errorf("CallerSubjectID = %q, want %q", got.CallerSubjectID, "service_account:nicolebot")
	}
	if !got.MayImpersonate {
		t.Error("MayImpersonate = false, want true")
	}
	if len(got.AllowedHosts) != 2 || got.AllowedHosts[0] != "bigquery.googleapis.com" {
		t.Errorf("AllowedHosts = %v, want [bigquery.googleapis.com api.linear.app]", got.AllowedHosts)
	}
	if got.DefaultAction != egress.PolicyDeny {
		t.Errorf("DefaultAction = %q, want %q", got.DefaultAction, egress.PolicyDeny)
	}
}

func TestTokenManagerNonImpersonatingDefault(t *testing.T) {
	t.Parallel()

	mgr, err := NewTokenManager([]byte("test-secret-32-bytes-bbbbbbbbbbb"))
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}

	token, err := mgr.MintToken(TokenRequest{
		PluginName:   "bigquery",
		AllowedHosts: []string{"bigquery.googleapis.com"},
		TTL:          1 * time.Hour,
	})
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}

	got, err := mgr.ResolveToken(token)
	if err != nil {
		t.Fatalf("ResolveToken: %v", err)
	}
	if got.MayImpersonate {
		t.Error("MayImpersonate = true, want false (default)")
	}
	if got.CallerSubjectID != "" {
		t.Errorf("CallerSubjectID = %q, want empty string", got.CallerSubjectID)
	}
	if got.PluginName != "bigquery" {
		t.Errorf("PluginName = %q, want bigquery", got.PluginName)
	}
}

func TestTokenManagerSubjectFallback(t *testing.T) {
	t.Parallel()

	mgr, err := NewTokenManager([]byte("test-secret-32-bytes-ccccccccccc"))
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}

	// When PluginName is empty but CallerSubjectID is set, JWT subject claim
	// should fall back to CallerSubjectID rather than the generic "egress-proxy".
	token, err := mgr.MintToken(TokenRequest{
		CallerSubjectID: "service_account:nicolebot",
		MayImpersonate:  true,
		TTL:             1 * time.Hour,
	})
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	parsed, err := mgr.parseClaims(token)
	if err != nil {
		t.Fatalf("parseClaims: %v", err)
	}
	if parsed.Subject != "service_account:nicolebot" {
		t.Errorf("JWT subject = %q, want service_account:nicolebot", parsed.Subject)
	}
}

func TestTokenManagerInvalidToken(t *testing.T) {
	t.Parallel()

	mgr, err := NewTokenManager([]byte("test-secret-32-bytes-ddddddddddd"))
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}

	if _, err := mgr.ResolveToken(""); err == nil {
		t.Error("expected error for empty token")
	}
	if _, err := mgr.ResolveToken("not.a.jwt"); err == nil {
		t.Error("expected error for malformed token")
	}
}
