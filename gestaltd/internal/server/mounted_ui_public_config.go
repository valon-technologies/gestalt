package server

import (
	"encoding/json"
	"net/http"
	"strings"
)

// The app owns this payload's schema. It inherits the static mount's access
// policy and never includes provider config or resolved provider secrets.
func mountedUIPublicConfigHandler(mounted MountedUI, next http.Handler) http.Handler {
	if mounted.AppName == "" {
		return next
	}
	config := mounted.PublicConfig
	if config == nil {
		config = map[string]any{}
	}
	body, err := json.Marshal(config)
	configPath := strings.TrimRight(mounted.Path, "/") + "/public-config.json"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != configPath {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "invalid public UI config")
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method == http.MethodGet {
			_, _ = w.Write(body)
		}
	})
}
