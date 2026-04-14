package server

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"github.com/valon-technologies/gestalt/server/internal/oauth"
	"github.com/valon-technologies/gestalt/server/internal/paraminterp"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

type startOAuthRequest struct {
	Integration      string            `json:"integration"`
	Connection       string            `json:"connection"`
	Instance         string            `json:"instance"`
	Scopes           []string          `json:"scopes"`
	ConnectionParams map[string]string `json:"connectionParams"`
}

func (s *Server) startIntegrationOAuth(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	auditAllowed := false
	auditErr := errors.New("oauth start failed")
	auditTarget := auditTarget{Kind: auditTargetKindConnection}
	providerName := ""
	metricProviderName := metricutil.UnknownAttrValue
	connectionMode := metricutil.UnknownAttrValue
	defer func() {
		metricutil.RecordConnectionAuthMetrics(r.Context(), startedAt, metricProviderName, "oauth", "start", connectionMode, auditErr != nil)
		s.auditHTTPEventWithTarget(r.Context(), PrincipalFromContext(r.Context()), providerName, "connection.oauth.start", auditAllowed, auditErr, auditTarget)
	}()
	if p := PrincipalFromContext(r.Context()); p != nil && p.Kind == principal.KindWorkload {
		auditErr = errWorkloadForbidden
		writeError(w, http.StatusForbidden, "workload callers are not allowed on this route")
		return
	}

	var req startOAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auditErr = errors.New("invalid JSON body")
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	providerName = req.Integration

	prov, ok := s.getProvider(w, req.Integration)
	if !ok {
		auditErr = errors.New("integration not found")
		return
	}
	metricProviderName = req.Integration
	connectionMode = metricutil.NormalizeConnectionMode(prov.ConnectionMode())

	connection, ok := s.resolveRequestedConnection(w, req.Integration, req.Connection)
	if !ok {
		auditErr = errors.New("invalid connection")
		return
	}

	handler, ok := s.requireOAuthHandler(w, req.Integration, connection)
	if !ok {
		auditErr = errors.New("oauth is not configured")
		return
	}

	if s.stateCodec == nil {
		auditErr = errors.New("oauth state encryption is not configured")
		writeError(w, http.StatusInternalServerError, "oauth state encryption is not configured")
		return
	}

	dbUserID, err := s.resolveUserID(w, r)
	if err != nil {
		auditErr = err
		return
	}

	instance, ok := resolveRequestedInstance(w, req.Instance)
	if !ok {
		auditErr = errors.New("invalid instance")
		return
	}
	auditTarget = connectionAuditTarget(req.Integration, connection, instance)

	connParams, ok := resolveConnectionParams(w, prov, req.ConnectionParams)
	if !ok {
		auditErr = errors.New("invalid connection parameters")
		return
	}

	var (
		authURL  string
		verifier string
	)

	if len(connParams) > 0 {
		rawAuthURL := handler.AuthorizationBaseURL()
		resolvedAuthURL := paraminterp.Interpolate(rawAuthURL, connParams)
		if resolvedAuthURL != rawAuthURL {
			authURL, verifier = handler.StartOAuthWithOverride(resolvedAuthURL, "_", req.Scopes)
		}
	}
	if authURL == "" {
		authURL, verifier = handler.StartOAuth("_", req.Scopes)
	}

	authSource := ""
	if p := PrincipalFromContext(r.Context()); p != nil {
		authSource = p.AuthSource()
	}
	state, err := s.stateCodec.Encode(integrationOAuthState{
		UserID:           dbUserID,
		AuthSource:       authSource,
		Integration:      req.Integration,
		Connection:       connection,
		Instance:         instance,
		Verifier:         verifier,
		ConnectionParams: connParams,
		ExpiresAt:        s.now().Add(integrationOAuthStateTTL).Unix(),
	})
	if err != nil {
		auditErr = errors.New("failed to encode oauth state")
		writeError(w, http.StatusInternalServerError, "failed to encode oauth state")
		return
	}

	authURL, err = setURLQueryParam(authURL, "state", state)
	if err != nil {
		auditErr = errors.New("failed to prepare oauth URL")
		writeError(w, http.StatusInternalServerError, "failed to prepare oauth URL")
		return
	}

	auditAllowed = true
	auditErr = nil
	writeJSON(w, http.StatusOK, map[string]string{"url": authURL, "state": state})
}

func (s *Server) integrationOAuthCallback(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	auditAllowed := false
	auditErr := errors.New("oauth callback failed")
	auditUserID := ""
	auditTarget := auditTarget{Kind: auditTargetKindConnection}
	stateAuthSource := ""
	providerName := ""
	connectionMode := metricutil.UnknownAttrValue
	defer func() {
		metricutil.RecordConnectionAuthMetrics(r.Context(), startedAt, providerName, "oauth", "complete", connectionMode, auditErr != nil)
		if auditUserID != "" {
			s.auditHTTPEventWithUserIDAndTarget(r.Context(), auditUserID, stateAuthSource, providerName, "connection.oauth.complete", auditAllowed, auditErr, auditTarget)
			return
		}
		s.auditHTTPEventWithTarget(r.Context(), nil, providerName, "connection.oauth.complete", auditAllowed, auditErr, auditTarget)
	}()

	writeCallbackError := func(status int, apiMessage, title, pageMessage string) {
		if strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html") {
			writePendingConnectionPage(w, status, pendingConnectionPageView{
				Title:     title,
				Message:   pageMessage,
				LinkURL:   "/integrations",
				LinkLabel: "Open integrations",
			}, "failed to render oauth error page")
			return
		}
		writeError(w, status, apiMessage)
	}

	code := r.URL.Query().Get("code")
	encodedState := r.URL.Query().Get("state")
	if code == "" || encodedState == "" {
		auditErr = errors.New("missing code or state parameter")
		writeCallbackError(
			http.StatusBadRequest,
			"missing code or state parameter",
			"Connection failed",
			"The OAuth provider did not return the required callback parameters. Start the connection again from Integrations.",
		)
		return
	}

	if s.stateCodec == nil {
		auditErr = errors.New("oauth state encryption is not configured")
		writeCallbackError(
			http.StatusInternalServerError,
			"oauth state encryption is not configured",
			"Connection failed",
			"Gestalt could not validate this connection attempt. Contact your administrator and try again.",
		)
		return
	}

	state, err := s.stateCodec.Decode(encodedState, s.now())
	if err != nil {
		auditErr = errors.New("invalid or expired oauth state")
		writeCallbackError(
			http.StatusBadRequest,
			"invalid or expired oauth state",
			"Connection expired",
			"This connection attempt is no longer valid. Start a new connection from Integrations.",
		)
		return
	}
	providerName = state.Integration
	auditUserID = state.UserID
	stateAuthSource = state.AuthSource
	auditTarget = connectionAuditTarget(state.Integration, state.Connection, state.Instance)
	handler, ok := s.requireOAuthHandler(w, providerName, state.Connection)
	if !ok {
		auditErr = errors.New("oauth is not configured")
		return
	}

	prov, _ := s.providers.Get(providerName)
	if prov != nil {
		connectionMode = metricutil.NormalizeConnectionMode(prov.ConnectionMode())
	}

	var exchangeOpts []oauth.ExchangeOption
	connParams := state.ConnectionParams
	if len(connParams) > 0 {
		rawURL := handler.TokenURL()
		resolved := paraminterp.Interpolate(rawURL, connParams)
		if resolved != rawURL {
			exchangeOpts = append(exchangeOpts, oauth.WithTokenURL(resolved))
		}
	}

	var tokenResp *core.TokenResponse
	tokenResp, err = handler.ExchangeCodeWithVerifier(r.Context(), code, state.Verifier, exchangeOpts...)
	if err != nil {
		auditErr = errors.New("token exchange failed")
		slog.ErrorContext(r.Context(), "token exchange failed", "provider", providerName, "error", err)
		writeCallbackError(
			http.StatusBadGateway,
			"token exchange failed",
			providerName+" connection failed",
			"The OAuth provider did not complete the connection. Start the connection again from Integrations.",
		)
		return
	}

	metadata, metaErr := buildConnectionMetadata(prov, connParams, tokenResp)
	if metaErr != nil {
		auditErr = errors.New("failed to extract connection metadata from token response")
		slog.ErrorContext(r.Context(), "connection metadata extraction failed", "provider", providerName, "error", metaErr)
		writeCallbackError(
			http.StatusBadGateway,
			"failed to extract connection metadata from token response",
			providerName+" connection failed",
			"Gestalt could not finish saving this connection. Start the connection again from Integrations.",
		)
		return
	}

	callbackInstance := state.Instance
	if callbackInstance == "" {
		callbackInstance = defaultTokenInstance
	}

	var tokenExpiresAt *time.Time
	if tokenResp.ExpiresIn > 0 {
		t := s.now().UTC().Truncate(time.Second).Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
		tokenExpiresAt = &t
	}

	tm := tokenMaterial{
		UserID:         state.UserID,
		AuthSource:     state.AuthSource,
		Integration:    providerName,
		Connection:     state.Connection,
		Instance:       callbackInstance,
		AccessToken:    tokenResp.AccessToken,
		RefreshToken:   tokenResp.RefreshToken,
		TokenExpiresAt: tokenExpiresAt,
		MetadataJSON:   metadata,
	}

	result, err := s.runPostConnect(r.Context(), prov, tm)
	if err != nil {
		auditErr = errors.New("connection setup failed")
		slog.ErrorContext(r.Context(), "post_connect failed", "provider", providerName, "error", err)
		writeCallbackError(
			http.StatusBadGateway,
			"connection setup failed",
			providerName+" connection failed",
			"Gestalt could not finish saving this connection. Start the connection again from Integrations.",
		)
		return
	}

	if result.Status == "selection_required" {
		state, err := s.decodePendingConnectionToken(result.PendingToken)
		if err != nil {
			auditErr = errors.New("failed to prepare pending connection")
			writeError(w, http.StatusInternalServerError, "failed to prepare pending connection")
			return
		}
		auditAllowed = true
		auditErr = nil
		s.writePendingConnectionSelectionPage(w, state, result.PendingToken)
		return
	}
	auditAllowed = true
	auditErr = nil
	http.Redirect(w, r, "/integrations?connected="+url.QueryEscape(providerName), http.StatusSeeOther)
}
