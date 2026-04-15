package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
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

type oauthStartTarget struct {
	OwnerKind       string
	OwnerID         string
	InitiatorUserID string
	AuthSource      string
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

	dbUserID, err := s.resolveUserID(w, r)
	if err != nil {
		auditErr = err
		return
	}

	authSource := ""
	if p := PrincipalFromContext(r.Context()); p != nil {
		authSource = p.AuthSource()
	}
	metricProviderName, connectionMode, auditTarget, err = s.startIntegrationOAuthForOwner(w, r, req, oauthStartTarget{
		OwnerKind:       core.IntegrationTokenOwnerKindUser,
		OwnerID:         dbUserID,
		InitiatorUserID: dbUserID,
		AuthSource:      authSource,
	})
	if err != nil {
		auditErr = err
		return
	}
	providerName = metricProviderName

	auditAllowed = true
	auditErr = nil
}

func (s *Server) startManagedIdentityOAuth(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	auditAllowed := false
	auditErr := errors.New("identity oauth start failed")
	auditTarget := auditTarget{Kind: auditTargetKindConnection}
	providerName := ""
	metricProviderName := metricutil.UnknownAttrValue
	connectionMode := metricutil.UnknownAttrValue
	defer func() {
		metricutil.RecordConnectionAuthMetrics(r.Context(), startedAt, metricProviderName, "oauth", "start", connectionMode, auditErr != nil)
		s.auditHTTPEventWithTarget(r.Context(), PrincipalFromContext(r.Context()), providerName, "identity.connection.oauth.start", auditAllowed, auditErr, auditTarget)
	}()

	actor, ok := s.resolveManagedIdentityActor(w, r, managedIdentityRoleEditor)
	if !ok {
		return
	}

	var req startOAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auditErr = errors.New("invalid JSON body")
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	providerName = req.Integration
	if !s.managedIdentityGrantPluginVisible(req.Integration, PrincipalFromContext(r.Context())) {
		auditErr = errors.New("integration not found")
		writeError(w, http.StatusNotFound, "integration not found")
		return
	}

	authSource := ""
	if p := PrincipalFromContext(r.Context()); p != nil {
		authSource = p.AuthSource()
	}
	metricProviderName, connectionMode, auditTarget, err := s.startIntegrationOAuthForOwner(w, r, req, oauthStartTarget{
		OwnerKind:       core.IntegrationTokenOwnerKindManagedIdentity,
		OwnerID:         actor.Identity.ID,
		InitiatorUserID: actor.UserID,
		AuthSource:      authSource,
	})
	if err != nil {
		auditErr = err
		return
	}
	providerName = metricProviderName
	auditAllowed = true
	auditErr = nil
}

func (s *Server) startIntegrationOAuthForOwner(w http.ResponseWriter, r *http.Request, req startOAuthRequest, target oauthStartTarget) (string, string, auditTarget, error) {
	prov, ok := s.getProvider(w, req.Integration)
	if !ok {
		return metricutil.UnknownAttrValue, metricutil.UnknownAttrValue, auditTarget{Kind: auditTargetKindConnection}, errors.New("integration not found")
	}
	connectionMode := metricutil.NormalizeConnectionMode(prov.ConnectionMode())

	connection, ok := s.resolveRequestedConnection(w, req.Integration, req.Connection)
	if !ok {
		return req.Integration, connectionMode, auditTarget{Kind: auditTargetKindConnection}, errors.New("invalid connection")
	}
	if target.OwnerKind == core.IntegrationTokenOwnerKindManagedIdentity && !s.managedIdentityConnectionVisible(req.Integration, connection) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("integration %q not found", req.Integration))
		return req.Integration, connectionMode, auditTarget{Kind: auditTargetKindConnection}, errors.New("integration not found")
	}

	handler, ok := s.requireOAuthHandler(w, req.Integration, connection)
	if !ok {
		return req.Integration, connectionMode, auditTarget{Kind: auditTargetKindConnection}, errors.New("oauth is not configured")
	}

	if s.stateCodec == nil {
		writeError(w, http.StatusInternalServerError, "oauth state encryption is not configured")
		return req.Integration, connectionMode, auditTarget{Kind: auditTargetKindConnection}, errors.New("oauth state encryption is not configured")
	}

	instance, ok := resolveRequestedInstance(w, req.Instance)
	if !ok {
		return req.Integration, connectionMode, auditTarget{Kind: auditTargetKindConnection}, errors.New("invalid instance")
	}
	auditTarget := connectionAuditTarget(req.Integration, connection, instance)

	connParams, ok := resolveConnectionParams(w, prov, req.ConnectionParams)
	if !ok {
		return req.Integration, connectionMode, auditTarget, errors.New("invalid connection parameters")
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

	legacyUserID := target.InitiatorUserID
	if target.OwnerKind == core.IntegrationTokenOwnerKindUser {
		legacyUserID = target.OwnerID
	}
	viewerScopes, viewerPerms := viewerCeilingForConnectionState(PrincipalFromContext(r.Context()))
	encodedState, err := s.stateCodec.Encode(integrationOAuthState{
		UserID:           legacyUserID,
		OwnerKind:        target.OwnerKind,
		OwnerID:          target.OwnerID,
		InitiatorUserID:  target.InitiatorUserID,
		AuthSource:       target.AuthSource,
		ViewerScopes:     viewerScopes,
		ViewerPerms:      viewerPerms,
		Integration:      req.Integration,
		Connection:       connection,
		Instance:         instance,
		Verifier:         verifier,
		ConnectionParams: connParams,
		ExpiresAt:        s.now().Add(integrationOAuthStateTTL).Unix(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode oauth state")
		return req.Integration, connectionMode, auditTarget, errors.New("failed to encode oauth state")
	}

	authURL, err = setURLQueryParam(authURL, "state", encodedState)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to prepare oauth URL")
		return req.Integration, connectionMode, auditTarget, errors.New("failed to prepare oauth URL")
	}

	writeJSON(w, http.StatusOK, map[string]string{"url": authURL, "state": encodedState})
	return req.Integration, connectionMode, auditTarget, nil
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
	auditUserID = state.InitiatorUserID
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
	writeManagedIdentityCallbackError := func(err error) bool {
		switch {
		case errors.Is(err, core.ErrNotFound):
			auditErr = errors.New("identity not found")
			writeCallbackError(
				http.StatusNotFound,
				"identity not found",
				providerName+" connection expired",
				"This identity is no longer available. Start the connection again from Integrations.",
			)
		case errors.Is(err, errManagedIdentityAccessDenied):
			auditErr = errManagedIdentityAccessDenied
			writeCallbackError(
				http.StatusForbidden,
				"identity access denied",
				providerName+" connection expired",
				"You no longer have access to finish this identity connection. Start the connection again from Integrations.",
			)
		case errors.Is(err, errManagedIdentityIntegrationNotFound):
			auditErr = errors.New("integration not found")
			writeCallbackError(
				http.StatusNotFound,
				"integration not found",
				providerName+" connection expired",
				"You no longer have access to finish this identity connection. Start the connection again from Integrations.",
			)
		default:
			return false
		}
		return true
	}
	if state.OwnerKind == core.IntegrationTokenOwnerKindManagedIdentity {
		if err := s.validateManagedIdentityConnectionWrite(r.Context(), tokenMaterial{
			OwnerKind:       state.OwnerKind,
			OwnerID:         state.OwnerID,
			InitiatorUserID: state.InitiatorUserID,
			AuthSource:      state.AuthSource,
			ViewerScopes:    append([]string(nil), state.ViewerScopes...),
			ViewerPerms:     append([]core.AccessPermission(nil), state.ViewerPerms...),
			Integration:     providerName,
			Connection:      state.Connection,
			Instance:        state.Instance,
		}); err != nil {
			if writeManagedIdentityCallbackError(err) {
				return
			}
			auditErr = errors.New("connection setup failed")
			writeCallbackError(
				http.StatusInternalServerError,
				"connection setup failed",
				providerName+" connection failed",
				"Gestalt could not finish saving this connection. Start the connection again from Integrations.",
			)
			return
		}
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
		OwnerKind:       state.OwnerKind,
		OwnerID:         state.OwnerID,
		InitiatorUserID: state.InitiatorUserID,
		AuthSource:      state.AuthSource,
		ViewerScopes:    append([]string(nil), state.ViewerScopes...),
		ViewerPerms:     append([]core.AccessPermission(nil), state.ViewerPerms...),
		Integration:     providerName,
		Connection:      state.Connection,
		Instance:        callbackInstance,
		AccessToken:     tokenResp.AccessToken,
		RefreshToken:    tokenResp.RefreshToken,
		TokenExpiresAt:  tokenExpiresAt,
		MetadataJSON:    metadata,
	}

	result, err := s.runPostConnect(r.Context(), prov, tm)
	if err != nil {
		if state.OwnerKind == core.IntegrationTokenOwnerKindManagedIdentity {
			if writeManagedIdentityCallbackError(err) {
				return
			}
		}
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
	redirectURL, err := setURLQueryParam(integrationOAuthRedirectPath(state), "connected", providerName)
	if err != nil {
		auditAllowed = false
		auditErr = errors.New("failed to prepare redirect URL")
		writeCallbackError(
			http.StatusInternalServerError,
			"failed to prepare redirect URL",
			providerName+" connection failed",
			"Gestalt saved the connection but could not redirect you back to the app.",
		)
		return
	}
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}
