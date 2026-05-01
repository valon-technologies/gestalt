package egress

import "net/http"

// CloneDefaultTransport returns an isolated transport initialized from the
// process default transport when possible.
func CloneDefaultTransport() *http.Transport {
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		return transport.Clone()
	}
	return &http.Transport{}
}
