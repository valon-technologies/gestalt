package server

import "net/http"

func lazyDevHandler(resolve func(string) http.Handler, name string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var handler http.Handler
		if resolve != nil {
			handler = resolve(name)
		}
		if handler == nil {
			serveDevNotReady(w)
			return
		}
		handler.ServeHTTP(w, r)
	})
}

func serveDevNotReady(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "1")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(`<!DOCTYPE html><html><head><meta http-equiv="refresh" content="1"></head><body><p>dev server starting…</p></body></html>`))
}
