package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

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

	mounted := []MountedUI{
		{Name: "app:sample", Path: "/sample"},
		{Name: "app:nested", Path: "/nested"},
	}

	tests := []struct {
		name    string
		target  string
		referer string
		headers func(*http.Request)
		want    string
	}{
		{
			name:    "matches registered ui by longest prefix",
			target:  "http://valon.tools/api/v2/identity/userinfo",
			referer: "http://valon.tools/nested/page",
			headers: func(r *http.Request) {
				r.Header.Set("Sec-Fetch-Site", "same-origin")
			},
			want: "app:nested",
		},
		{
			name:    "unknown without referer",
			target:  "http://valon.tools/api/v2/identity/userinfo",
			referer: "",
			headers: func(r *http.Request) {
				r.Header.Set("Sec-Fetch-Site", "same-origin")
			},
			want: metricutil.ClientAppUnknown,
		},
		{
			name:    "unknown for cross origin referer",
			target:  "http://valon.tools/api/v2/identity/userinfo",
			referer: "http://example.com/sample/",
			headers: func(r *http.Request) {
				r.Header.Set("Sec-Fetch-Site", "cross-site")
			},
			want: metricutil.ClientAppUnknown,
		},
		{
			name:    "unknown for unmatched same origin referer",
			target:  "http://valon.tools/api/v2/identity/userinfo",
			referer: "http://valon.tools/other/",
			headers: func(r *http.Request) {
				r.Header.Set("Sec-Fetch-Site", "same-origin")
			},
			want: metricutil.ClientAppUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, tc.target, nil)
			req.Host = "valon.tools"
			if tc.referer != "" {
				req.Header.Set("Referer", tc.referer)
			}
			if tc.headers != nil {
				tc.headers(req)
			}
			if got := classifyClientAppFromReferrer(mounted, req); got != tc.want {
				t.Fatalf("classifyClientAppFromReferrer() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReferrerSameOrigin(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "http://valon.tools/api/v2/foo", nil)
	req.Host = "valon.tools"

	same, err := url.Parse("http://valon.tools/sample/")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !referrerSameOrigin(req, same) {
		t.Fatal("expected same-origin referer")
	}

	cross, err := url.Parse("https://valon.tools/sample/")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if referrerSameOrigin(req, cross) {
		t.Fatal("expected different scheme to be cross-origin")
	}
}
