package ui

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// DevProxyHandler returns an http.Handler that reverse-proxies every request to
// target, a local framework dev server (e.g. `next dev`). It streams responses
// immediately (FlushInterval = -1) so server-sent hot-reload events and the HMR
// WebSocket upgrade are forwarded without buffering. The incoming request path
// is forwarded unchanged, so the dev server must serve the bundle under the
// same base path as the mount.
func DevProxyHandler(target string) (http.Handler, error) {
	u, err := url.Parse(strings.TrimSpace(target))
	if err != nil || !u.IsAbs() || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("dev proxy target must be an absolute http(s) URL, got %q", target)
	}
	return &httputil.ReverseProxy{
		FlushInterval: -1,
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(u)
			r.Out.Host = u.Host
		},
	}, nil
}
