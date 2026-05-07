package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/egressproxy"
)

// mountAdminEgressProxyRoutes is mounted under /admin/api/v1/ behind the
// admin auth middleware. It exposes operations for issuing long-lived egress
// proxy tokens to in-house service callers (e.g. nicolebot) that need to
// impersonate users on outbound HTTP calls.
func (s *Server) mountAdminEgressProxyRoutes(r chi.Router) {
	r.Post("/egress-proxy/tokens", s.postAdminEgressProxyToken)
}

type adminMintEgressProxyTokenRequest struct {
	CallerSubjectID string   `json:"caller_subject_id"`
	PluginName      string   `json:"plugin_name,omitempty"`
	SessionID       string   `json:"session_id,omitempty"`
	AllowedHosts    []string `json:"allowed_hosts"`
	DefaultAction   string   `json:"default_action,omitempty"`
	MayImpersonate  bool     `json:"may_impersonate,omitempty"`
	TTLSeconds      int64    `json:"ttl_seconds,omitempty"`
}

type adminMintEgressProxyTokenResponse struct {
	Token        string `json:"token"`
	ExpiresAtSec int64  `json:"expires_at_sec,omitempty"`
}

func (s *Server) postAdminEgressProxyToken(w http.ResponseWriter, r *http.Request) {
	if s.egressProxyTokens == nil {
		writeError(w, http.StatusServiceUnavailable, "egress proxy tokens are not configured")
		return
	}

	var req adminMintEgressProxyTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	caller := strings.TrimSpace(req.CallerSubjectID)
	if caller == "" {
		writeError(w, http.StatusBadRequest, "caller_subject_id is required")
		return
	}
	if req.MayImpersonate {
		if _, _, ok := core.ParseSubjectID(caller); !ok {
			writeError(w, http.StatusBadRequest, "caller_subject_id must be in the form <kind>:<id>")
			return
		}
	}
	allowedHosts := normalizeRequestedHosts(req.AllowedHosts)
	if len(allowedHosts) == 0 {
		writeError(w, http.StatusBadRequest, "allowed_hosts must contain at least one entry")
		return
	}

	defaultAction, err := parseRequestedDefaultAction(req.DefaultAction)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ttl := time.Duration(req.TTLSeconds) * time.Second
	token, err := s.egressProxyTokens.MintToken(egressproxy.TokenRequest{
		CallerSubjectID: caller,
		PluginName:      strings.TrimSpace(req.PluginName),
		SessionID:       strings.TrimSpace(req.SessionID),
		AllowedHosts:    allowedHosts,
		DefaultAction:   defaultAction,
		MayImpersonate:  req.MayImpersonate,
		TTL:             ttl,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, adminMintEgressProxyTokenResponse{Token: token})
}

func normalizeRequestedHosts(hosts []string) []string {
	out := make([]string, 0, len(hosts))
	seen := make(map[string]struct{}, len(hosts))
	for _, h := range hosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "" {
			continue
		}
		if _, dup := seen[h]; dup {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseRequestedDefaultAction(action string) (egress.PolicyAction, error) {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "", string(egress.PolicyAllow):
		return egress.PolicyAllow, nil
	case string(egress.PolicyDeny):
		return egress.PolicyDeny, nil
	default:
		return "", &policyActionError{action: action}
	}
}

type policyActionError struct{ action string }

func (e *policyActionError) Error() string {
	return "default_action must be \"allow\" or \"deny\" (got " + e.action + ")"
}
