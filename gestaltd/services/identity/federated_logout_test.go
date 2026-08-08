package identity

import "testing"

func TestBuildOIDCFederatedLogoutURL(t *testing.T) {
	t.Parallel()

	got, err := BuildOIDCFederatedLogoutURL(
		"https://tenant.us.auth0.com/",
		"client-id",
		"https://valon.tools/apps",
	)
	if err != nil {
		t.Fatalf("BuildOIDCFederatedLogoutURL() error = %v", err)
	}
	want := "https://tenant.us.auth0.com/v2/logout?client_id=client-id&returnTo=https%3A%2F%2Fvalon.tools%2Fapps"
	if got != want {
		t.Fatalf("BuildOIDCFederatedLogoutURL() = %q, want %q", got, want)
	}
}

func TestBuildOIDCFederatedLogoutURLRejectsNonAuth0Issuer(t *testing.T) {
	t.Parallel()

	for _, issuer := range []string{
		"https://login.example.com/",
		"https://tenant.auth0.com.example.com/",
		"not-a-url",
	} {
		if _, err := BuildOIDCFederatedLogoutURL(issuer, "client-id", "https://valon.tools/"); err == nil {
			t.Errorf("BuildOIDCFederatedLogoutURL(%q) error = nil, want unsupported issuer error", issuer)
		}
	}
}

func TestRemoteIdentityProviderFederatedLogoutURL(t *testing.T) {
	t.Parallel()

	provider := &remoteIdentityProvider{
		oidcIssuerURL: "https://tenant.us.auth0.com/",
		oidcClientID:  "client-id",
	}
	got, err := provider.FederatedLogoutURL("https://valon.tools/")
	if err != nil {
		t.Fatalf("FederatedLogoutURL() error = %v", err)
	}
	want := "https://tenant.us.auth0.com/v2/logout?client_id=client-id&returnTo=https%3A%2F%2Fvalon.tools%2F"
	if got != want {
		t.Fatalf("FederatedLogoutURL() = %q, want %q", got, want)
	}
}
