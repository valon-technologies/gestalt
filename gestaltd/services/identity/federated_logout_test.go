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
