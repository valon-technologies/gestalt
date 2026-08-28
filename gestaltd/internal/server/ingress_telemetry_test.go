package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
)

func TestClassifyClientKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		header func(*http.Request)
		want   string
	}{
		{
			name: "cli header",
			header: func(r *http.Request) {
				r.Header.Set(metricutil.HeaderGestaltClient, metricutil.ClientKindCLI)
			},
			want: metricutil.ClientKindCLI,
		},
		{
			name: "sec fetch site",
			header: func(r *http.Request) {
				r.Header.Set("Sec-Fetch-Site", "same-origin")
			},
			want: metricutil.ClientKindWeb,
		},
		{
			name: "any sec fetch header",
			header: func(r *http.Request) {
				r.Header.Set("Sec-Fetch-Dest", "document")
			},
			want: metricutil.ClientKindWeb,
		},
		{
			name:   "unknown",
			header: func(*http.Request) {},
			want:   metricutil.ClientKindUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			tc.header(req)
			if got := classifyClientKind(req); got != tc.want {
				t.Fatalf("classifyClientKind() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClassifyClientAppFromReferrer(t *testing.T) {
	t.Parallel()

	s := &Server{
		publicBaseURL: "https://valon.tools",
		mountedUIs: []MountedUI{
			{Name: "app:nested", Path: "/nested"},
			{Name: "app:deeply-nested", Path: "/nested/admin"},
		},
	}

	tests := []struct {
		name    string
		referer string
		want    string
	}{
		{
			name:    "matches registered ui by longest prefix",
			referer: "https://valon.tools/nested/admin/page",
			want:    "app:deeply-nested",
		},
		{
			name:    "unknown without referer",
			referer: "",
			want:    metricutil.ClientAppUnknown,
		},
		{
			name:    "unknown for cross origin referer",
			referer: "https://example.com/nested/",
			want:    metricutil.ClientAppUnknown,
		},
		{
			name:    "unknown for unmatched same origin referer",
			referer: "https://valon.tools/other/",
			want:    metricutil.ClientAppUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "http://internal/api/v2/identity/userinfo", nil)
			if tc.referer != "" {
				req.Header.Set("Referer", tc.referer)
			}
			if got := s.classifyClientAppFromReferrer(req); got != tc.want {
				t.Fatalf("classifyClientAppFromReferrer() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSubjectLabelFromPrincipal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		p    *principal.Principal
		want string
	}{
		{
			name: "normalized user email",
			p: &principal.Principal{
				Identity: &core.UserIdentity{Email: " USER@Example.com "},
				Kind:     principal.KindUser,
			},
			want: "user@example.com",
		},
		{
			name: "service account name",
			p: &principal.Principal{
				SubjectID: "service_account:oauth-bot",
				Kind:      "service_account",
				Identity:  &core.UserIdentity{Email: "wrong@example.com"},
			},
			want: "oauth-bot",
		},
		{
			name: "unsupported principal with identity",
			p: &principal.Principal{
				SubjectID: "agent:researcher",
				Kind:      "agent",
				Identity:  &core.UserIdentity{Email: "wrong@example.com"},
			},
			want: metricutil.SubjectLabelUnknown,
		},
		{
			name: "missing identity",
			p:    &principal.Principal{SubjectID: "user:9f2c1c2e-1a2b-4c3d-8e4f-5a6b7c8d9e01", Kind: principal.KindUser},
			want: metricutil.SubjectLabelUnknown,
		},
		{
			name: "nil principal",
			p:    nil,
			want: metricutil.SubjectLabelUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := subjectLabelFromPrincipal(tc.p); got != tc.want {
				t.Fatalf("subjectLabelFromPrincipal() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReferrerSameOrigin(t *testing.T) {
	t.Parallel()

	s := &Server{publicBaseURL: "https://valon.tools"}
	req := httptest.NewRequest(http.MethodGet, "http://internal/api/v2/foo", nil)
	req.Header.Set("X-Forwarded-Proto", "http")

	tests := []struct {
		name    string
		referer string
		want    bool
	}{
		{name: "same origin", referer: "https://valon.tools/sample/", want: true},
		{name: "canonical default port", referer: "https://valon.tools:443/sample/", want: true},
		{name: "different scheme", referer: "http://valon.tools/sample/", want: false},
		{name: "non-default port", referer: "https://valon.tools:8443/sample/", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			referer, err := url.Parse(tc.referer)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got := s.referrerSameOrigin(req, referer); got != tc.want {
				t.Fatalf("referrerSameOrigin() = %t, want %t", got, tc.want)
			}
		})
	}
}
