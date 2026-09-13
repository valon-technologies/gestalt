package server

import (
	"net/http/httptest"
	"testing"
)

func TestAuthCallbackURLUsesOnlyConfiguredHTTPSOrigins(t *testing.T) {
	t.Setenv("GESTALTD_AUTH_CALLBACK_ORIGINS", "https://vt.valon.tools;https://valon.tools")
	s := &Server{publicBaseURL: "https://vt.valon.tools"}
	for _, tc := range []struct {
		host string
		want string
	}{
		{"vt.valon.tools", "https://vt.valon.tools/api/v1/auth/login/callback"},
		{"valon.tools", "https://valon.tools/api/v1/auth/login/callback"},
		{"VaLoN.ToOlS.:443", "https://valon.tools/api/v1/auth/login/callback"},
		{"valon.tools:8443", "https://vt.valon.tools/api/v1/auth/login/callback"},
		{"valon.tools.attacker.example", "https://vt.valon.tools/api/v1/auth/login/callback"},
		{"valon.tools@attacker.example", "https://vt.valon.tools/api/v1/auth/login/callback"},
		{"deploy.vt.valon.tools", "https://vt.valon.tools/api/v1/auth/login/callback"},
	} {
		t.Run(tc.host, func(t *testing.T) {
			r := httptest.NewRequest("GET", "https://vt.valon.tools/api/v1/auth/login", nil)
			r.Host = tc.host
			r.Header.Set("X-Forwarded-Host", "attacker.example")
			got, err := s.authCallbackURL(r)
			if err != nil || got != tc.want {
				t.Fatalf("callback = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestAuthCallbackURLInvalidOrAbsentOriginsKeepConfiguredBase(t *testing.T) {
	for _, origins := range []string{
		"",
		"http://valon.tools",
		"https://user@valon.tools",
		"https://valon.tools/other",
		"https://valon.tools?next=attacker",
		"https://valon.tools?",
		"https://valon.tools#fragment",
		"https://valon.tools:bad",
	} {
		t.Run(origins, func(t *testing.T) {
			t.Setenv("GESTALTD_AUTH_CALLBACK_ORIGINS", origins)
			s := &Server{publicBaseURL: "https://vt.valon.tools"}
			r := httptest.NewRequest("GET", "https://valon.tools/api/v1/auth/login", nil)
			got, err := s.authCallbackURL(r)
			if err != nil || got != "https://vt.valon.tools/api/v1/auth/login/callback" {
				t.Fatalf("callback = %q, %v; want configured base", got, err)
			}
		})
	}
}
