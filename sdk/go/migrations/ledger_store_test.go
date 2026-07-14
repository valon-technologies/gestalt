package migrations_test

import (
	"testing"

	"github.com/valon-technologies/gestalt/sdk/go/migrations"
)

func TestProviderLedgerStore(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		want     string
	}{
		{name: "camelCase provider", provider: "gIssues", want: "g_issues_migrations"},
		{name: "multi-word camelCase", provider: "dealHub", want: "deal_hub_migrations"},
		{name: "hyphenated slug", provider: "deal-hub", want: "deal_hub_migrations"},
		{name: "scoped package name", provider: "@scope/gIssues", want: "g_issues_migrations"},
		{name: "path-like provider name", provider: "github.com/foo/myApp", want: "my_app_migrations"},
		{name: "spaces become separate dashes", provider: "my  app", want: "my__app_migrations"},
		{name: "empty falls back", provider: "   ", want: "_gestalt_migrations"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := migrations.ProviderLedgerStore(tt.provider); got != tt.want {
				t.Fatalf("ProviderLedgerStore(%q) = %q, want %q", tt.provider, got, tt.want)
			}
		})
	}
}
