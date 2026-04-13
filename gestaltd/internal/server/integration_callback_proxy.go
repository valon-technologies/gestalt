package server

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

type IntegrationOAuthCallbackProxyConfig struct {
	StateSecret []byte
	Readiness   ReadinessChecker
	Now         func() time.Time
}

func NewIntegrationOAuthCallbackProxy(cfg IntegrationOAuthCallbackProxyConfig) (http.Handler, error) {
	if len(cfg.StateSecret) == 0 {
		return nil, fmt.Errorf("state secret is required")
	}
	stateCodec, err := newIntegrationOAuthStateCodec(cfg.StateSecret)
	if err != nil {
		return nil, fmt.Errorf("init oauth state codec: %w", err)
	}

	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) {
		if cfg.Readiness != nil {
			if reason := cfg.Readiness(); reason != "" {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": reason})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc(config.IntegrationCallbackPath, func(w http.ResponseWriter, r *http.Request) {
		encodedState := r.URL.Query().Get("state")
		if encodedState == "" {
			writeError(w, http.StatusBadRequest, "missing state parameter")
			return
		}

		state, err := stateCodec.Decode(encodedState, now())
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid or expired oauth state")
			return
		}

		returnBaseURL, err := normalizeIntegrationReturnBaseURL(state.ReturnBaseURL)
		if err != nil {
			writeError(w, http.StatusBadRequest, "oauth state missing valid return base URL")
			return
		}

		target := returnBaseURL + config.IntegrationCallbackPath
		if rawQuery := r.URL.RawQuery; rawQuery != "" {
			target += "?" + rawQuery
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
	})

	return requestMetaMiddleware(mux), nil
}

func normalizeIntegrationReturnBaseURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("return base URL is required")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse return base URL: %w", err)
	}
	if !parsed.IsAbs() || parsed.Host == "" {
		return "", fmt.Errorf("return base URL must be absolute")
	}
	switch parsed.Scheme {
	case "https", "http":
	default:
		return "", fmt.Errorf("unsupported return base URL scheme %q", parsed.Scheme)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("return base URL must not include query or fragment")
	}
	return strings.TrimRight(parsed.Scheme+"://"+parsed.Host+parsed.EscapedPath(), "/"), nil
}
