package server

import (
	"net/http"
	"net/http/httptest"
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
