package providerdev

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
)

func newReverseProxy(upstream *url.URL) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(upstream)
			r.Out.Host = r.In.Host
		},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "dev server unavailable", http.StatusBadGateway)
		},
	}
}

func serveNotReady(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "1")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = fmt.Fprint(w, `<!DOCTYPE html><html><head><meta http-equiv="refresh" content="1"></head><body><p>dev server starting…</p></body></html>`)
}
