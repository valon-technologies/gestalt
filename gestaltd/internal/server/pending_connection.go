package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/authorization"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

const pendingConnectionPath = "/api/v1/auth/pending-connection"
const pendingConnectionCookieName = "pending_connection_state"

var pendingConnectionSelectionPage = template.Must(template.New("pending-connection-selection").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}}</title>
  <style>
    body {
      margin: 0;
      font: 16px/1.5 ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      background: #f7f4ee;
      color: #221c15;
      min-height: 100vh;
      display: grid;
      place-items: center;
      padding: 20px;
    }
    main {
      width: min(100%, 640px);
      background: #fff;
      border: 1px solid #ddd4c6;
      border-radius: 16px;
      padding: 24px;
      box-shadow: 0 16px 40px rgba(34, 28, 21, 0.08);
    }
    h1 {
      margin: 0;
      font-size: 1.5rem;
      line-height: 1.25;
    }
    p, .footer {
      margin: 12px 0 0;
      color: #5f5448;
    }
    ul {
      list-style: none;
      margin: 20px 0 0;
      padding: 0;
      display: grid;
      gap: 10px;
    }
    button {
      width: 100%;
      border: 1px solid #ddd4c6;
      border-radius: 12px;
      background: #fff;
      color: inherit;
      padding: 14px 16px;
      text-align: left;
      font: inherit;
      cursor: pointer;
      transition: border-color 120ms ease, box-shadow 120ms ease;
    }
    button:hover {
      border-color: #9a6a37;
      box-shadow: 0 10px 24px rgba(41, 29, 18, 0.08);
    }
    strong, a {
      color: #7b5228;
    }
    .subtle {
      display: block;
      margin-top: 4px;
      color: #5f5448;
      font-size: 0.92rem;
    }
  </style>
</head>
<body>
  <main>
    <h1>{{.Title}}</h1>
    <p>{{.Message}}</p>
    {{if .Candidates}}
    <ul>
      {{range $i, $candidate := .Candidates}}
      <li>
        <form method="post" action="{{$.Action}}">
          <input type="hidden" name="pending_token" value="{{$.PendingToken}}">
          <input type="hidden" name="candidate_index" value="{{$i}}">
          <button type="submit">
            <strong>{{$candidate.Name}}</strong>
            <span class="subtle">{{$candidate.ID}}</span>
          </button>
        </form>
      </li>
      {{end}}
    </ul>
    {{end}}
    {{if .LinkURL}}
    <p><a href="{{.LinkURL}}">{{.LinkLabel}}</a></p>
    {{end}}
    {{if .Footer}}
    <p class="footer">{{.Footer}}</p>
    {{end}}
  </main>
</body>
</html>
`))

type pendingConnectionPageView struct {
	Title        string
	Message      string
	Action       string
	PendingToken string
	Candidates   []core.DiscoveryCandidate
	LinkURL      string
	LinkLabel    string
	Footer       string
}

func writePendingConnectionPage(w http.ResponseWriter, status int, view pendingConnectionPageView, renderErr string) {
	var buf bytes.Buffer
	if err := pendingConnectionSelectionPage.Execute(&buf, view); err != nil {
		writeError(w, http.StatusInternalServerError, renderErr)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func (s *Server) encodePendingConnectionToken(tm credentialMaterial, candidates []core.DiscoveryCandidate) (string, error) {
	if s.encryptor == nil {
		return "", fmt.Errorf("pending connection encryption is not configured")
	}
	return encodePendingConnectionState(s.encryptor, pendingConnectionState{
		Credential: tm,
		BindingKey: uuid.NewString(),
		Candidates: candidates,
		ExpiresAt:  s.now().Add(pendingConnectionTTL).Unix(),
	})
}

func (s *Server) decodePendingConnectionToken(encoded string) (*pendingConnectionState, error) {
	if s.encryptor == nil {
		return nil, fmt.Errorf("pending connection encryption is not configured")
	}
	return decodePendingConnectionState(s.encryptor, encoded, s.now())
}

func findDiscoveryCandidate(candidates []core.DiscoveryCandidate, candidateID string) *core.DiscoveryCandidate {
	for i := range candidates {
		if candidates[i].ID == candidateID {
			return &candidates[i]
		}
	}
	return nil
}

func findDiscoveryCandidateByIndex(candidates []core.DiscoveryCandidate, rawIndex string) (*core.DiscoveryCandidate, error) {
	index, err := strconv.Atoi(rawIndex)
	if err != nil {
		return nil, fmt.Errorf("invalid candidate index")
	}
	if index < 0 || index >= len(candidates) {
		return nil, fmt.Errorf("candidate not found")
	}
	return &candidates[index], nil
}

func (s *Server) setPendingConnectionCookie(w http.ResponseWriter, state *pendingConnectionState) {
	if s.encryptor == nil {
		return
	}
	encoded, err := encodePendingConnectionBindingState(s.encryptor, pendingConnectionBindingState{
		BindingKey: state.BindingKey,
		ExpiresAt:  state.ExpiresAt,
	})
	if err != nil {
		return
	}
	maxAge := int(time.Until(time.Unix(state.ExpiresAt, 0)).Seconds())
	if maxAge < 0 {
		maxAge = 0
	}
	http.SetCookie(w, &http.Cookie{
		Name:     pendingConnectionCookieName,
		Value:    encoded,
		Path:     pendingConnectionPath,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearPendingConnectionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     pendingConnectionCookieName,
		Value:    "",
		Path:     pendingConnectionPath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) writePendingConnectionSelectionPage(w http.ResponseWriter, state *pendingConnectionState, pendingToken string) {
	s.setPendingConnectionCookie(w, state)
	writePendingConnectionPage(w, http.StatusOK, pendingConnectionPageView{
		Title:        "Select a " + state.Credential.Integration + " connection",
		Message:      "Gestalt found more than one candidate. Choose the connection you want to save.",
		Action:       pendingConnectionPath,
		PendingToken: pendingToken,
		Candidates:   state.Candidates,
		Footer:       "If you did not expect this screen, close this tab and restart the connect flow.",
	}, "failed to render pending connection page")
}

func (s *Server) writePendingConnectionSuccessPage(w http.ResponseWriter, integration string) {
	s.clearPendingConnectionCookie(w)
	linkURL := "/integrations"
	if connectedURL, err := setURLQueryParam(linkURL, "connected", integration); err == nil {
		linkURL = connectedURL
	}
	writePendingConnectionPage(w, http.StatusOK, pendingConnectionPageView{
		Title:     integration + " connected",
		Message:   "Your connection has been saved. You can close this tab now.",
		LinkURL:   linkURL,
		LinkLabel: "Open integrations",
	}, "failed to render success page")
}

func (s *Server) resolvePendingConnectionPrincipal(r *http.Request) (*principal.Principal, bool, error) {
	if s.noAuth {
		return nil, false, nil
	}
	p, err := s.resolveRequestPrincipalWithUserID(r)
	if err != nil {
		if errors.Is(err, errInvalidAuthorizationHeader) {
			return nil, true, principal.ErrInvalidToken
		}
		return nil, true, err
	}
	if p == nil {
		return nil, false, nil
	}
	if principal.IsNonUserPrincipal(p) {
		return nil, true, errUserRequired
	}
	if strings.TrimSpace(p.SubjectID) == "" && strings.TrimSpace(p.UserID) == "" {
		return nil, true, fmt.Errorf("authenticated principal missing subject ID")
	}
	return principal.Canonicalized(p), true, nil
}

func (s *Server) authorizePendingConnectionByCookie(r *http.Request, state *pendingConnectionState) error {
	if s.noAuth {
		return nil
	}
	if s.encryptor == nil {
		return fmt.Errorf("pending connection encryption is not configured")
	}
	cookie, err := r.Cookie(pendingConnectionCookieName)
	if err != nil {
		return fmt.Errorf("missing pending connection cookie")
	}
	binding, err := decodePendingConnectionBindingState(s.encryptor, cookie.Value, s.now())
	if err != nil {
		return err
	}
	if binding.BindingKey != state.BindingKey {
		return fmt.Errorf("pending connection cookie does not match")
	}
	return nil
}

func (s *Server) authorizePendingConnection(w http.ResponseWriter, r *http.Request, state *pendingConnectionState) (*principal.Principal, bool) {
	p, authenticated, err := s.resolvePendingConnectionPrincipal(r)
	if err != nil {
		if errors.Is(err, errUserRequired) {
			writeError(w, http.StatusForbidden, errUserRequired.Error())
			return nil, false
		}
		if errors.Is(err, principal.ErrInvalidToken) {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return nil, false
		}
		writeError(w, http.StatusInternalServerError, "failed to validate pending connection")
		return nil, false
	}
	if authenticated {
		subjectID := strings.TrimSpace(p.SubjectID)
		if subjectID == "" && strings.TrimSpace(p.UserID) != "" {
			subjectID = principal.UserSubjectID(p.UserID)
		}
		if subjectID != state.Credential.SubjectID {
			if !s.authorizeManagedSubjectPendingConnection(r.Context(), p, state.Credential.SubjectID) {
				writeError(w, http.StatusNotFound, "pending connection not found")
				return nil, false
			}
		}
		return p, true
	}
	if !authenticated {
		if _, err := canonicalServiceAccountSubjectID(state.Credential.SubjectID); err == nil {
			writeError(w, http.StatusUnauthorized, "pending connection authorization required")
			return nil, false
		}
		if err := s.authorizePendingConnectionByCookie(r, state); err != nil {
			writeError(w, http.StatusUnauthorized, "pending connection authorization required")
			return nil, false
		}
	}
	return nil, true
}

func (s *Server) authorizeManagedSubjectPendingConnection(ctx context.Context, p *principal.Principal, subjectID string) bool {
	if _, err := canonicalServiceAccountSubjectID(subjectID); err != nil {
		return false
	}
	if !managedSubjectCallerIsUnscoped(p) {
		return false
	}
	if s.managedSubjects == nil || s.authorizationProvider == nil || s.authorizer == nil {
		return false
	}
	if _, err := s.managedSubjects.GetManagedSubject(ctx, subjectID); err != nil {
		return false
	}
	allowed, err := s.managedSubjectActionAllowed(principal.WithPrincipal(ctx, p), subjectID, authorization.ProviderManagedSubjectActionConnect)
	return err == nil && allowed
}

func (s *Server) selectPendingConnection(w http.ResponseWriter, r *http.Request) {
	auditAllowed := false
	auditErr := errors.New("pending connection selection failed")
	auditSubjectID := ""
	auditAuthSource := ""
	auditTarget := auditTarget{Kind: auditTargetKindConnection}
	providerName := ""
	defer func() {
		if auditSubjectID != "" {
			s.auditHTTPEventWithSubjectIDAndTarget(r.Context(), auditSubjectID, auditAuthSource, providerName, "connection.pending.select", auditAllowed, auditErr, auditTarget)
			return
		}
		s.auditHTTPEventWithTarget(r.Context(), PrincipalFromContext(r.Context()), providerName, "connection.pending.select", auditAllowed, auditErr, auditTarget)
	}()

	if err := r.ParseForm(); err != nil {
		auditErr = errors.New("invalid form body")
		writeError(w, http.StatusBadRequest, "invalid form body")
		return
	}
	pendingToken := r.Form.Get("pending_token")
	if pendingToken == "" {
		auditErr = errors.New("pending_token is required")
		writeError(w, http.StatusBadRequest, "pending_token is required")
		return
	}
	candidateIndex := r.Form.Get("candidate_index")
	candidateID := r.Form.Get("candidate_id")

	state, err := s.decodePendingConnectionToken(pendingToken)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errPendingConnectionExpired) {
			status = http.StatusGone
			s.clearPendingConnectionCookie(w)
		}
		auditErr = errors.New("invalid or expired pending connection")
		writeError(w, status, "invalid or expired pending connection")
		return
	}
	providerName = state.Credential.Integration
	auditTarget = connectionAuditTarget(state.Credential.Integration, state.Credential.Connection, state.Credential.Instance)
	completionPrincipal, ok := s.authorizePendingConnection(w, r, state)
	if !ok {
		auditErr = errors.New("pending connection authorization required")
		return
	}
	auditSubjectID = state.Credential.SubjectID
	auditAuthSource = state.Credential.AuthSource
	if candidateIndex == "" && candidateID == "" {
		auditAllowed = true
		auditErr = nil
		s.writePendingConnectionSelectionPage(w, state, pendingToken)
		return
	}

	var selected *core.DiscoveryCandidate
	if candidateIndex != "" {
		selected, err = findDiscoveryCandidateByIndex(state.Candidates, candidateIndex)
		if err != nil {
			auditErr = errors.New(err.Error())
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	} else {
		selected = findDiscoveryCandidate(state.Candidates, candidateID)
		if selected == nil {
			auditErr = errors.New("candidate not found")
			writeError(w, http.StatusBadRequest, "candidate not found")
			return
		}
	}
	if selected == nil {
		auditErr = errors.New("candidate not found")
		writeError(w, http.StatusBadRequest, "candidate not found")
		return
	}
	if err := validateDiscoveryMetadata(selected.Metadata); err != nil {
		auditErr = errors.New(err.Error())
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	merged, err := mergeMetadataJSON(state.Credential.MetadataJSON, selected.Metadata)
	if err != nil {
		auditErr = errors.New("failed to merge metadata")
		writeError(w, http.StatusInternalServerError, "failed to merge metadata")
		return
	}

	tm := state.Credential
	tm.MetadataJSON = merged
	prov, ok := s.getProvider(w, state.Credential.Integration)
	if !ok {
		auditErr = errors.New("integration not found")
		return
	}
	if _, err := s.completeConnection(credentialMaterialContext(r.Context(), completionPrincipal, tm), prov, tm); err != nil {
		auditErr = errors.New("failed to store connection")
		writeError(w, http.StatusInternalServerError, "failed to store connection")
		return
	}

	if _, err := r.Cookie(sessionCookieName); err == nil {
		s.clearPendingConnectionCookie(w)
		connectedURL := "/integrations"
		if nextURL, err := setURLQueryParam(connectedURL, "connected", state.Credential.Integration); err == nil {
			connectedURL = nextURL
		}
		auditAllowed = true
		auditErr = nil
		http.Redirect(w, r, connectedURL, http.StatusSeeOther)
		return
	}

	auditAllowed = true
	auditErr = nil
	s.writePendingConnectionSuccessPage(w, state.Credential.Integration)
}
