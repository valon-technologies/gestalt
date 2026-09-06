package gcsauth

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/oauth2"
)

const ReadOnlyScope = "https://www.googleapis.com/auth/devstorage.read_only"

// NewHTTPClient returns a copy of base that authenticates matching requests.
func NewHTTPClient(base *http.Client, tokenSource oauth2.TokenSource, authenticate func(*url.URL) bool) *http.Client {
	if base == nil {
		base = http.DefaultClient
	}
	client := *base
	client.Transport = &roundTripper{
		base:         base.Transport,
		tokenSource:  oauth2.ReuseTokenSource(nil, tokenSource),
		authenticate: authenticate,
	}
	return &client
}

type roundTripper struct {
	base         http.RoundTripper
	tokenSource  oauth2.TokenSource
	authenticate func(*url.URL) bool
}

func (t *roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	if t.authenticate == nil || !t.authenticate(req.URL) {
		return base.RoundTrip(req)
	}
	token, err := t.tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("get GCS ADC token: %w", err)
	}
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	token.SetAuthHeader(clone)
	return base.RoundTrip(clone)
}

func IsStorageURL(u *url.URL) bool {
	if u == nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "storage.googleapis.com") {
		return false
	}
	return u.Port() == "" || u.Port() == "443"
}
