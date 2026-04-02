package server

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

const pendingConnectionPath = "/api/v1/auth/pending-connection"

var pendingConnectionSelectionPage = template.Must(template.New("pending-connection-selection").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Select Connection</title>
  <style>
    :root {
      color-scheme: light dark;
      --bg: #f6f3ec;
      --surface: rgba(255, 255, 255, 0.92);
      --border: rgba(113, 89, 54, 0.18);
      --text: #241c13;
      --muted: #665748;
      --accent: #8f5d2c;
      --accent-strong: #73481f;
    }
    @media (prefers-color-scheme: dark) {
      :root {
        --bg: #17130e;
        --surface: rgba(31, 24, 18, 0.92);
        --border: rgba(214, 193, 166, 0.18);
        --text: #f4ede4;
        --muted: #c4b3a2;
        --accent: #d59a58;
        --accent-strong: #e8b16f;
      }
    }
    body {
      margin: 0;
      font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      background:
        radial-gradient(circle at top, rgba(143, 93, 44, 0.14), transparent 42%),
        linear-gradient(180deg, rgba(255,255,255,0.55), rgba(255,255,255,0)),
        var(--bg);
      color: var(--text);
      min-height: 100vh;
      display: grid;
      place-items: center;
      padding: 24px;
    }
    main {
      width: min(100%, 640px);
      background: var(--surface);
      border: 1px solid var(--border);
      border-radius: 20px;
      box-shadow: 0 24px 80px rgba(17, 12, 8, 0.14);
      padding: 28px;
      backdrop-filter: blur(18px);
    }
    h1 {
      margin: 0;
      font-size: 1.7rem;
      line-height: 1.2;
    }
    p {
      margin: 12px 0 0;
      color: var(--muted);
      line-height: 1.55;
    }
    ul {
      list-style: none;
      margin: 24px 0 0;
      padding: 0;
      display: grid;
      gap: 12px;
    }
    button {
      width: 100%;
      border: 1px solid var(--border);
      border-radius: 14px;
      background: rgba(255, 255, 255, 0.78);
      color: inherit;
      padding: 16px 18px;
      text-align: left;
      font: inherit;
      cursor: pointer;
      transition: border-color 120ms ease, transform 120ms ease, box-shadow 120ms ease;
    }
    button:hover {
      border-color: var(--accent);
      transform: translateY(-1px);
      box-shadow: 0 10px 24px rgba(41, 29, 18, 0.08);
    }
    strong {
      display: block;
      font-size: 1rem;
    }
    .subtle {
      display: block;
      margin-top: 4px;
      color: var(--muted);
      font-size: 0.92rem;
    }
    .footer {
      margin-top: 18px;
      font-size: 0.92rem;
    }
  </style>
</head>
<body>
  <main>
    <h1>Select a {{.Integration}} connection</h1>
    <p>Gestalt found more than one candidate. Choose the connection you want to save.</p>
    <ul>
      {{range .Candidates}}
      <li>
        <form method="post" action="{{$.Action}}">
          <input type="hidden" name="pending_token" value="{{$.PendingToken}}">
          <input type="hidden" name="candidate_id" value="{{.ID}}">
          <button type="submit">
            <strong>{{.Name}}</strong>
            <span class="subtle">{{.ID}}</span>
          </button>
        </form>
      </li>
      {{end}}
    </ul>
    <p class="footer">If you did not expect this screen, close this tab and restart the connect flow.</p>
  </main>
</body>
</html>
`))

var pendingConnectionSuccessPage = template.Must(template.New("pending-connection-success").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Connection Saved</title>
  <style>
    :root {
      color-scheme: light dark;
      --bg: #f4f7ef;
      --surface: rgba(255,255,255,0.94);
      --border: rgba(80, 120, 84, 0.2);
      --text: #1f2a21;
      --muted: #55655a;
      --accent: #2f7a4b;
    }
    @media (prefers-color-scheme: dark) {
      :root {
        --bg: #111611;
        --surface: rgba(24, 31, 24, 0.94);
        --border: rgba(128, 201, 149, 0.18);
        --text: #eff8ef;
        --muted: #adc4b0;
        --accent: #80c995;
      }
    }
    body {
      margin: 0;
      font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      background:
        radial-gradient(circle at top, rgba(47, 122, 75, 0.14), transparent 45%),
        var(--bg);
      color: var(--text);
      min-height: 100vh;
      display: grid;
      place-items: center;
      padding: 24px;
    }
    main {
      width: min(100%, 520px);
      background: var(--surface);
      border: 1px solid var(--border);
      border-radius: 20px;
      box-shadow: 0 24px 80px rgba(11, 18, 12, 0.14);
      padding: 28px;
      text-align: center;
    }
    h1 {
      margin: 0;
      font-size: 1.7rem;
    }
    p {
      margin: 12px 0 0;
      color: var(--muted);
      line-height: 1.55;
    }
    a {
      display: inline-block;
      margin-top: 18px;
      color: var(--accent);
      text-decoration: none;
      font-weight: 600;
    }
  </style>
</head>
<body>
  <main>
    <h1>{{.Integration}} connected</h1>
    <p>Your connection has been saved. You can close this tab now.</p>
    <a href="/integrations?connected={{.Integration}}">Open integrations</a>
  </main>
</body>
</html>
`))

type pendingConnectionSelectionView struct {
	Action       string
	Integration  string
	PendingToken string
	Candidates   []core.DiscoveryCandidate
}

func (s *Server) encodePendingConnectionToken(tm tokenMaterial, candidates []core.DiscoveryCandidate) (string, error) {
	if s.encryptor == nil {
		return "", fmt.Errorf("pending connection encryption is not configured")
	}
	tokenExpiresAt := int64(0)
	if tm.TokenExpiresAt != nil {
		tokenExpiresAt = tm.TokenExpiresAt.UTC().Unix()
	}
	return encodePendingConnectionState(s.encryptor, pendingConnectionState{
		UserID:         tm.UserID,
		Integration:    tm.Integration,
		Connection:     tm.Connection,
		Instance:       tm.Instance,
		AccessToken:    tm.AccessToken,
		RefreshToken:   tm.RefreshToken,
		TokenExpiresAt: tokenExpiresAt,
		MetadataJSON:   tm.MetadataJSON,
		Candidates:     candidates,
		ExpiresAt:      s.now().Add(pendingConnectionTTL).Unix(),
	})
}

func (s *Server) decodePendingConnectionToken(encoded string) (*pendingConnectionState, error) {
	if s.encryptor == nil {
		return nil, fmt.Errorf("pending connection encryption is not configured")
	}
	return decodePendingConnectionState(s.encryptor, encoded, s.now())
}

func pendingConnectionToTokenMaterial(state *pendingConnectionState) tokenMaterial {
	var tokenExpiresAt *time.Time
	if state.TokenExpiresAt > 0 {
		t := time.Unix(state.TokenExpiresAt, 0).UTC()
		tokenExpiresAt = &t
	}
	return tokenMaterial{
		UserID:         state.UserID,
		Integration:    state.Integration,
		Connection:     state.Connection,
		Instance:       state.Instance,
		AccessToken:    state.AccessToken,
		RefreshToken:   state.RefreshToken,
		TokenExpiresAt: tokenExpiresAt,
		MetadataJSON:   state.MetadataJSON,
	}
}

func findDiscoveryCandidate(candidates []core.DiscoveryCandidate, candidateID string) *core.DiscoveryCandidate {
	for i := range candidates {
		if candidates[i].ID == candidateID {
			return &candidates[i]
		}
	}
	return nil
}

func (s *Server) writePendingConnectionSelectionPage(w http.ResponseWriter, state *pendingConnectionState, pendingToken string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pendingConnectionSelectionPage.Execute(w, pendingConnectionSelectionView{
		Action:       pendingConnectionPath,
		Integration:  state.Integration,
		PendingToken: pendingToken,
		Candidates:   state.Candidates,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to render pending connection page")
	}
}

func (s *Server) writePendingConnectionSuccessPage(w http.ResponseWriter, integration string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pendingConnectionSuccessPage.Execute(w, struct{ Integration string }{Integration: integration}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to render success page")
	}
}

func (s *Server) resolvePendingConnectionUserID(r *http.Request) (string, bool, error) {
	if s.noAuth {
		return "", false, nil
	}

	var token string
	if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
		token = c.Value
	} else if header := r.Header.Get("Authorization"); header != "" {
		bearer := strings.TrimPrefix(header, core.BearerScheme)
		if bearer == header {
			return "", true, principal.ErrInvalidToken
		}
		token = bearer
	}
	if token == "" {
		return "", false, nil
	}

	p, err := s.resolver.ResolveToken(r.Context(), token)
	if err != nil {
		return "", true, err
	}
	if p.UserID != "" {
		return p.UserID, true, nil
	}
	if p.Identity == nil || p.Identity.Email == "" {
		return "", true, fmt.Errorf("authenticated principal missing email")
	}
	dbUser, err := s.datastore.FindOrCreateUser(r.Context(), p.Identity.Email)
	if err != nil {
		return "", true, err
	}
	if dbUser == nil || dbUser.ID == "" {
		return "", true, fmt.Errorf("authenticated principal missing user ID")
	}
	return dbUser.ID, true, nil
}

func (s *Server) authorizePendingConnection(w http.ResponseWriter, r *http.Request, state *pendingConnectionState) bool {
	userID, authenticated, err := s.resolvePendingConnectionUserID(r)
	if err != nil {
		if errors.Is(err, principal.ErrInvalidToken) {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return false
		}
		writeError(w, http.StatusInternalServerError, "failed to validate pending connection")
		return false
	}
	if authenticated && userID != state.UserID {
		writeError(w, http.StatusNotFound, "pending connection not found")
		return false
	}
	return true
}

func (s *Server) selectPendingConnection(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form body")
		return
	}
	pendingToken := r.Form.Get("pending_token")
	if pendingToken == "" {
		writeError(w, http.StatusBadRequest, "pending_token is required")
		return
	}
	candidateID := r.Form.Get("candidate_id")

	state, err := s.decodePendingConnectionToken(pendingToken)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errPendingConnectionExpired) {
			status = http.StatusGone
		}
		writeError(w, status, "invalid or expired pending connection")
		return
	}
	if !s.authorizePendingConnection(w, r, state) {
		return
	}
	if candidateID == "" {
		s.writePendingConnectionSelectionPage(w, state, pendingToken)
		return
	}

	selected := findDiscoveryCandidate(state.Candidates, candidateID)
	if selected == nil {
		writeError(w, http.StatusBadRequest, "candidate not found")
		return
	}
	if err := validateDiscoveryMetadata(selected.Metadata); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	merged, err := mergeMetadataJSON(state.MetadataJSON, selected.Metadata)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to merge metadata")
		return
	}

	tm := pendingConnectionToTokenMaterial(state)
	tm.MetadataJSON = merged
	if _, err := s.storeTokenFromMaterial(r.Context(), tm); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store connection")
		return
	}

	if _, err := r.Cookie(sessionCookieName); err == nil {
		http.Redirect(w, r, "/integrations?connected="+url.QueryEscape(state.Integration), http.StatusSeeOther)
		return
	}

	s.writePendingConnectionSuccessPage(w, state.Integration)
}
