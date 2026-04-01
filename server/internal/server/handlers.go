package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/apiexec"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/discovery"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/oauth"
	"github.com/valon-technologies/gestalt/server/internal/paraminterp"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const defaultTokenInstance = "default"

const cliStatePrefix = "cli:"
const maxPort = 65535

const sessionCookieName = "session_token"
const defaultSessionCookieTTL = 24 * time.Hour

func (s *Server) resolveUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	user := UserFromContext(r.Context())
	if user == nil || user.Email == "" {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return "", false
	}
	if id := UserIDFromContext(r.Context()); id != "" {
		return id, true
	}
	dbUser, err := s.datastore.FindOrCreateUser(r.Context(), user.Email)
	if err != nil || dbUser == nil || dbUser.ID == "" {
		writeError(w, http.StatusInternalServerError, "failed to resolve user")
		return "", false
	}
	return dbUser.ID, true
}

func (s *Server) healthCheck(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) readinessCheck(w http.ResponseWriter, _ *http.Request) {
	if s.readiness != nil {
		if reason := s.readiness(); reason != "" {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": reason})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type instanceInfo struct {
	Name       string `json:"name"`
	Connection string `json:"connection,omitempty"`
}

type credentialFieldInfo struct {
	Name        string `json:"name"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	HelpURL     string `json:"help_url,omitempty"`
}

type connectionDefInfo struct {
	Name             string                `json:"name"`
	AuthType         string                `json:"auth_type"`
	CredentialFields []credentialFieldInfo `json:"credential_fields,omitempty"`
}

type integrationInfo struct {
	Name             string                         `json:"name"`
	DisplayName      string                         `json:"display_name,omitempty"`
	Description      string                         `json:"description,omitempty"`
	IconSVG          string                         `json:"icon_svg,omitempty"`
	Connected        bool                           `json:"connected"`
	Instances        []instanceInfo                 `json:"instances,omitempty"`
	AuthTypes        []string                       `json:"auth_types"`
	ConnectionParams map[string]connectionParamInfo `json:"connection_params,omitempty"`
	Connections      []connectionDefInfo            `json:"connections,omitempty"`
	CredentialFields []credentialFieldInfo          `json:"credential_fields,omitempty"`
}

type connectionParamInfo struct {
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description,omitempty"`
	Default     string `json:"default,omitempty"`
}

func (s *Server) listIntegrations(w http.ResponseWriter, r *http.Request) {
	connected, err := s.userConnectedIntegrations(r)
	if err != nil {
		slog.ErrorContext(r.Context(), "listing integrations", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to check integration status")
		return
	}

	names := s.providers.List()
	out := make([]integrationInfo, 0, len(names))
	for _, name := range names {
		prov, err := s.providers.Get(name)
		if err != nil {
			continue
		}
		var authTypes []string
		if atl, ok := prov.(core.AuthTypeLister); ok {
			authTypes = atl.AuthTypes()
		} else if mp, ok := prov.(core.ManualProvider); ok && mp.SupportsManualAuth() {
			authTypes = []string{"manual"}
		} else {
			authTypes = []string{"oauth"}
		}
		instances := connected[name]
		info := integrationInfo{
			Name:        name,
			DisplayName: prov.DisplayName(),
			Description: prov.Description(),
			Connected:   len(instances) > 0,
			Instances:   instances,
			AuthTypes:   authTypes,
		}
		if cat := prov.Catalog(); cat != nil {
			info.IconSVG = cat.IconSVG
		}
		if cpp, ok := prov.(core.ConnectionParamProvider); ok {
			defs := cpp.ConnectionParamDefs()
			userParams := make(map[string]connectionParamInfo)
			for name, def := range defs {
				if def.From == "" {
					userParams[name] = connectionParamInfo{
						Required:    def.Required,
						Description: def.Description,
						Default:     def.Default,
					}
				}
			}
			if len(userParams) > 0 {
				info.ConnectionParams = userParams
			}
		}
		if cfp, ok := prov.(core.CredentialFieldsProvider); ok {
			if fields := cfp.CredentialFields(); len(fields) > 0 {
				cfInfos := make([]credentialFieldInfo, len(fields))
				for i, f := range fields {
					cfInfos[i] = credentialFieldInfo{
						Name:        f.Name,
						Label:       f.Label,
						Description: f.Description,
						HelpURL:     f.HelpURL,
					}
				}
				info.CredentialFields = cfInfos
			}
		}
		out = append(out, info)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) userConnectedIntegrations(r *http.Request) (map[string][]instanceInfo, error) {
	user := UserFromContext(r.Context())
	if user == nil || user.Email == "" {
		return nil, nil
	}
	userID := UserIDFromContext(r.Context())
	if userID == "" {
		dbUser, err := s.datastore.FindOrCreateUser(r.Context(), user.Email)
		if err != nil {
			return nil, fmt.Errorf("resolving user: %w", err)
		}
		if dbUser == nil || dbUser.ID == "" {
			return nil, fmt.Errorf("resolving user: empty result")
		}
		userID = dbUser.ID
	}
	tokens, err := s.datastore.ListTokens(r.Context(), userID)
	if err != nil {
		return nil, fmt.Errorf("listing tokens: %w", err)
	}
	m := make(map[string][]instanceInfo, len(tokens))
	for _, tok := range tokens {
		m[tok.Integration] = append(m[tok.Integration], instanceInfo{Name: tok.Instance, Connection: tok.Connection})
	}
	return m, nil
}

func (s *Server) disconnectIntegration(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.resolveUserID(w, r)
	if !ok {
		return
	}

	name := chi.URLParam(r, "name")
	if _, ok := s.getProvider(w, name); !ok {
		return
	}

	requestedInstance := r.URL.Query().Get("instance")
	requestedConnection := r.URL.Query().Get("connection")

	tokens, err := s.datastore.ListTokensForIntegration(r.Context(), userID, name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list tokens")
		return
	}

	var matched []*core.IntegrationToken
	for _, tok := range tokens {
		if requestedConnection != "" && tok.Connection != requestedConnection {
			continue
		}
		matched = append(matched, tok)
	}

	if len(matched) == 0 {
		writeError(w, http.StatusNotFound, fmt.Sprintf("no connection found for integration %q", name))
		return
	}

	if requestedInstance != "" {
		var instanceMatched []*core.IntegrationToken
		for _, tok := range matched {
			if tok.Instance == requestedInstance {
				instanceMatched = append(instanceMatched, tok)
			}
		}
		matched = instanceMatched
	}

	if len(matched) == 0 {
		writeError(w, http.StatusNotFound, fmt.Sprintf("no connection found for integration %q instance %q", name, requestedInstance))
		return
	}
	if len(matched) > 1 {
		labels := make([]string, len(matched))
		for i, t := range matched {
			labels[i] = fmt.Sprintf("%s/%s", t.Connection, t.Instance)
		}
		hint := "?instance=NAME"
		if requestedInstance != "" {
			hint = "?connection=NAME"
		}
		writeError(w, http.StatusConflict, fmt.Sprintf("multiple connections exist for %q (%v); specify %s", name, labels, hint))
		return
	}

	tokenID := matched[0].ID
	if tokenID == "" {
		writeError(w, http.StatusNotFound, fmt.Sprintf("no connection found for integration %q", name))
		return
	}

	if err := s.datastore.DeleteToken(r.Context(), tokenID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to disconnect integration")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
}

func (s *Server) getProvider(w http.ResponseWriter, name string) (core.Provider, bool) {
	prov, err := s.providers.Get(name)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, fmt.Sprintf("integration %q not found", name))
			return nil, false
		}
		writeError(w, http.StatusInternalServerError, "failed to look up integration")
		return nil, false
	}
	return prov, true
}

func (s *Server) requireOAuthHandler(w http.ResponseWriter, integration, connection string) (bootstrap.OAuthHandler, bool) {
	if s.connectionAuth == nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("integration %q has no OAuth connections configured", integration))
		return nil, false
	}
	connMap := s.connectionAuth()[integration]
	if connMap == nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("integration %q has no OAuth connections configured", integration))
		return nil, false
	}
	handler, ok := connMap[connection]
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("connection %q on integration %q does not support OAuth", connection, integration))
		return nil, false
	}
	return handler, true
}

func (s *Server) listOperations(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	prov, ok := s.getProvider(w, name)
	if !ok {
		return
	}
	p := PrincipalFromContext(r.Context())
	var resolver tokenResolver
	if tr, ok := s.invoker.(tokenResolver); ok {
		resolver = tr
	}
	catalogConnection := s.catalogConnection[name]
	if catalogConnection == "" {
		catalogConnection = s.defaultConnection[name]
	}
	cat, err := resolveCatalog(r.Context(), prov, name, resolver, p, catalogConnection)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve catalog")
		return
	}
	writeJSON(w, http.StatusOK, cat.Operations)
}

func (s *Server) listRuntimes(w http.ResponseWriter, _ *http.Request) {
	if s.runtimes == nil {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	writeJSON(w, http.StatusOK, s.runtimes.List())
}

func (s *Server) listBindings(w http.ResponseWriter, _ *http.Request) {
	if s.bindings == nil {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	writeJSON(w, http.StatusOK, s.bindings.List())
}

func (s *Server) executeOperation(w http.ResponseWriter, r *http.Request) {
	providerName := chi.URLParam(r, "integration")
	operationName := chi.URLParam(r, "operation")

	p := PrincipalFromContext(r.Context())

	params := make(map[string]any)
	if r.Method == http.MethodPost {
		if r.Body != nil {
			defer func() { _ = r.Body.Close() }()
			if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON body")
				return
			}
		}
	} else {
		for key, values := range r.URL.Query() {
			if len(values) > 0 {
				params[key] = values[0]
			}
		}
	}

	instance, _ := params["_instance"].(string)
	delete(params, "_instance")

	result, err := s.invoker.Invoke(r.Context(), p, providerName, instance, operationName, params)
	if err != nil {
		var upstreamErr *apiexec.UpstreamHTTPError
		switch {
		case errors.Is(err, invocation.ErrProviderNotFound):
			writeError(w, http.StatusNotFound, fmt.Sprintf("integration %q not found", providerName))
		case errors.Is(err, invocation.ErrOperationNotFound):
			writeError(w, http.StatusNotFound, fmt.Sprintf("operation %q not found on integration %q", operationName, providerName))
		case errors.Is(err, invocation.ErrNotAuthenticated):
			writeError(w, http.StatusUnauthorized, "not authenticated")
		case errors.Is(err, invocation.ErrScopeDenied):
			writeError(w, http.StatusForbidden, err.Error())
		case errors.Is(err, invocation.ErrNoToken):
			writeError(w, http.StatusPreconditionFailed, fmt.Sprintf("no token stored for integration %q; connect via OAuth first", providerName))
		case errors.Is(err, invocation.ErrAmbiguousInstance):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, invocation.ErrUserResolution):
			writeError(w, http.StatusInternalServerError, "failed to resolve user")
		case errors.Is(err, invocation.ErrInternal):
			writeError(w, http.StatusInternalServerError, "internal error")
		case errors.Is(err, core.ErrMCPOnly):
			writeError(w, http.StatusBadRequest, "this integration is accessible only via MCP")
		case errors.Is(err, apiexec.ErrMissingPathParam):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.As(err, &upstreamErr):
			writeOperationResult(w, &core.OperationResult{
				Status:  upstreamErr.Status,
				Headers: upstreamErr.Headers,
				Body:    upstreamErr.Body,
			})
		default:
			if message, ok := safeOperationErrorMessage(err); ok {
				slog.WarnContext(
					r.Context(),
					"operation failed with user-facing error",
					"provider",
					providerName,
					"operation",
					operationName,
					"error",
					err,
				)
				writeError(w, http.StatusBadGateway, message)
				return
			}
			slog.ErrorContext(r.Context(), "operation failed", "provider", providerName, "operation", operationName, "error", err)
			writeError(w, http.StatusBadGateway, "operation failed")
		}
		return
	}

	writeOperationResult(w, result)
}

func safeOperationErrorMessage(err error) (string, bool) {
	var userErr *apiexec.UserMessageError
	if errors.As(err, &userErr) && userErr.Message != "" {
		return userErr.Message, true
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return "operation timed out", true
	}

	status, ok := grpcstatus.FromError(err)
	if !ok {
		return "", false
	}

	switch status.Code() {
	case codes.DeadlineExceeded:
		return "operation timed out", true
	case codes.Unavailable:
		return "integration runtime unavailable", true
	default:
		return "", false
	}
}

type loginRequest struct {
	State        string `json:"state"`
	CallbackPort int    `json:"callback_port,omitempty"`
}

type authInfoResponse struct {
	Provider    string `json:"provider"`
	DisplayName string `json:"display_name"`
}

func (s *Server) authInfo(w http.ResponseWriter, _ *http.Request) {
	provider := s.auth.Name()
	displayName := provider
	if dn, ok := s.auth.(AuthProviderDisplayName); ok {
		displayName = dn.DisplayName()
	}
	writeJSON(w, http.StatusOK, authInfoResponse{
		Provider:    provider,
		DisplayName: displayName,
	})
}

func (s *Server) startLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	state := req.State
	if req.CallbackPort > 0 && req.CallbackPort <= maxPort {
		state = fmt.Sprintf("%s%d:%s", cliStatePrefix, req.CallbackPort, req.State)
	}
	url, err := s.auth.LoginURL(state)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate login URL")
		return
	}
	if s.encryptor != nil {
		encoded, encErr := encodeLoginState(s.encryptor, loginState{
			State:     req.State,
			ExpiresAt: s.now().Add(loginStateTTL).Unix(),
		})
		if encErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to encode login state")
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     loginStateCookieName,
			Value:    encoded,
			Path:     "/",
			MaxAge:   int(loginStateTTL.Seconds()),
			HttpOnly: true,
			Secure:   s.secureCookies,
			SameSite: http.SameSiteLaxMode,
		})
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

// AuthProviderDisplayName is an optional interface for a human-readable login label.
type AuthProviderDisplayName interface {
	DisplayName() string
}

// SessionTokenIssuer is an optional interface that auth providers can implement
// to issue session tokens after login.
type SessionTokenIssuer interface {
	IssueSessionToken(identity *core.UserIdentity) (string, error)
}

// SessionTokenTTLProvider is an optional interface that auth providers can
// implement to expose their configured session TTL for cookie MaxAge.
type SessionTokenTTLProvider interface {
	SessionTokenTTL() time.Duration
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string) {
	maxAge := int(defaultSessionCookieTTL.Seconds())
	if p, ok := s.auth.(SessionTokenTTLProvider); ok {
		maxAge = int(p.SessionTokenTTL().Seconds())
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

// StatefulCallbackHandler is an optional interface for auth providers that need
// the OAuth state parameter during callback (e.g., for PKCE where the
// code_verifier is encrypted in the state).
type StatefulCallbackHandler interface {
	HandleCallbackWithState(ctx context.Context, code, state string) (*core.UserIdentity, string, error)
}

func (s *Server) loginCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "missing code parameter")
		return
	}

	var identity *core.UserIdentity
	var originalState string
	var err error

	if stateful, ok := s.auth.(StatefulCallbackHandler); ok {
		state := r.URL.Query().Get("state")
		identity, originalState, err = stateful.HandleCallbackWithState(r.Context(), code, state)
	} else {
		originalState = r.URL.Query().Get("state")
		identity, err = s.auth.HandleCallback(r.Context(), code)
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "login callback failed", "error", err)
		writeError(w, http.StatusUnauthorized, "login failed")
		return
	}

	if csrfErr := s.validateLoginState(r, originalState); csrfErr != nil {
		slog.ErrorContext(r.Context(), "login state validation failed", "error", csrfErr)
		writeError(w, http.StatusForbidden, "login state validation failed")
		return
	}
	if s.encryptor != nil {
		s.clearLoginStateCookie(w)
	}

	resp := map[string]any{
		"email":        identity.Email,
		"display_name": identity.DisplayName,
	}

	issuer, ok := s.auth.(SessionTokenIssuer)
	if !ok {
		writeError(w, http.StatusInternalServerError, "auth provider does not support session tokens")
		return
	}
	token, err := issuer.IssueSessionToken(identity)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue session token")
		return
	}
	s.setSessionCookie(w, token)

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) validateLoginState(r *http.Request, originalState string) error {
	if s.encryptor == nil {
		return nil
	}
	cookie, err := r.Cookie(loginStateCookieName)
	if err != nil {
		return fmt.Errorf("missing login state cookie")
	}
	expected, err := decodeLoginState(s.encryptor, cookie.Value, s.now())
	if err != nil {
		return fmt.Errorf("invalid login state cookie: %w", err)
	}
	if expected.State != originalState {
		return fmt.Errorf("login state mismatch")
	}
	return nil
}

func (s *Server) clearLoginStateCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     loginStateCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

type startOAuthRequest struct {
	Integration      string            `json:"integration"`
	Connection       string            `json:"connection"`
	Instance         string            `json:"instance"`
	Scopes           []string          `json:"scopes"`
	ConnectionParams map[string]string `json:"connection_params"`
}

func (s *Server) startIntegrationOAuth(w http.ResponseWriter, r *http.Request) {
	var req startOAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if _, ok := s.getProvider(w, req.Integration); !ok {
		return
	}

	connection := req.Connection
	if connection == "" {
		connection = s.defaultConnection[req.Integration]
	}
	if connection == "" {
		connection = config.PluginConnectionName
	}
	if !safeParamValue.MatchString(connection) {
		writeError(w, http.StatusBadRequest, "connection name contains invalid characters")
		return
	}

	handler, ok := s.requireOAuthHandler(w, req.Integration, connection)
	if !ok {
		return
	}

	if s.stateCodec == nil {
		writeError(w, http.StatusInternalServerError, "oauth state encryption is not configured")
		return
	}

	dbUserID, ok := s.resolveUserID(w, r)
	if !ok {
		return
	}

	instance := req.Instance
	if instance == "" {
		instance = "default"
	}
	if !safeParamValue.MatchString(instance) {
		writeError(w, http.StatusBadRequest, "instance name contains invalid characters")
		return
	}

	prov, _ := s.providers.Get(req.Integration)

	var connParams map[string]string
	if cpp, ok := prov.(core.ConnectionParamProvider); ok {
		var valErr error
		connParams, valErr = validateConnectionParams(cpp.ConnectionParamDefs(), req.ConnectionParams)
		if valErr != nil {
			writeError(w, http.StatusBadRequest, valErr.Error())
			return
		}
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

	state, err := s.stateCodec.Encode(integrationOAuthState{
		UserID:           dbUserID,
		Integration:      req.Integration,
		Connection:       connection,
		Instance:         instance,
		Verifier:         verifier,
		ConnectionParams: connParams,
		ExpiresAt:        s.now().Add(integrationOAuthStateTTL).Unix(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode oauth state")
		return
	}

	authURL, err = setURLQueryParam(authURL, "state", state)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to prepare oauth URL")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"url": authURL, "state": state})
}

func (s *Server) integrationOAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	encodedState := r.URL.Query().Get("state")
	if code == "" || encodedState == "" {
		writeError(w, http.StatusBadRequest, "missing code or state parameter")
		return
	}

	if s.stateCodec == nil {
		writeError(w, http.StatusInternalServerError, "oauth state encryption is not configured")
		return
	}

	state, err := s.stateCodec.Decode(encodedState, s.now())
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid or expired oauth state")
		return
	}

	providerName := state.Integration
	handler, ok := s.requireOAuthHandler(w, providerName, state.Connection)
	if !ok {
		return
	}

	prov, _ := s.providers.Get(providerName)

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
		slog.ErrorContext(r.Context(), "token exchange failed", "provider", providerName, "error", err)
		writeError(w, http.StatusBadGateway, "token exchange failed")
		return
	}

	metadata, metaErr := buildConnectionMetadata(prov, connParams, tokenResp)
	if metaErr != nil {
		slog.ErrorContext(r.Context(), "connection metadata extraction failed", "provider", providerName, "error", metaErr)
		writeError(w, http.StatusBadGateway, "failed to extract connection metadata from token response")
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
		slog.ErrorContext(r.Context(), "post_connect failed", "provider", providerName, "error", err)
		writeError(w, http.StatusBadGateway, "connection setup failed")
		return
	}

	if result.Status == "selection_required" {
		http.Redirect(w, r, "/integrations?pending="+url.QueryEscape(result.StagedID), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/integrations?connected="+url.QueryEscape(providerName), http.StatusSeeOther)
}

type connectManualRequest struct {
	Integration      string            `json:"integration"`
	Connection       string            `json:"connection"`
	Instance         string            `json:"instance"`
	Credential       string            `json:"credential"`
	Credentials      map[string]string `json:"credentials"`
	ConnectionParams map[string]string `json:"connection_params"`
}

func (s *Server) connectManual(w http.ResponseWriter, r *http.Request) {
	var req connectManualRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	var effectiveCredential string
	if len(req.Credentials) > 0 {
		for k, v := range req.Credentials {
			if v == "" {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("credential %q must not be empty", k))
				return
			}
		}
		b, err := json.Marshal(req.Credentials)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid credentials map")
			return
		}
		effectiveCredential = string(b)
	} else {
		effectiveCredential = req.Credential
	}

	if req.Integration == "" || effectiveCredential == "" {
		writeError(w, http.StatusBadRequest, "integration and credential are required")
		return
	}

	prov, ok := s.getProvider(w, req.Integration)
	if !ok {
		return
	}

	mp, ok := prov.(core.ManualProvider)
	if !ok || !mp.SupportsManualAuth() {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("integration %q does not support manual auth; use OAuth connect instead", req.Integration))
		return
	}

	user := UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	dbUser, err := s.datastore.FindOrCreateUser(r.Context(), user.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve user")
		return
	}

	manualInstance := req.Instance
	if manualInstance == "" {
		manualInstance = "default"
	}
	if !safeParamValue.MatchString(manualInstance) {
		writeError(w, http.StatusBadRequest, "instance name contains invalid characters")
		return
	}

	manualConnection := req.Connection
	if manualConnection == "" {
		manualConnection = s.defaultConnection[req.Integration]
	}
	if manualConnection == "" {
		manualConnection = config.PluginConnectionName
	}
	if !safeParamValue.MatchString(manualConnection) {
		writeError(w, http.StatusBadRequest, "connection name contains invalid characters")
		return
	}

	var connParams map[string]string
	if cpp, ok := prov.(core.ConnectionParamProvider); ok {
		var valErr error
		connParams, valErr = validateConnectionParams(cpp.ConnectionParamDefs(), req.ConnectionParams)
		if valErr != nil {
			writeError(w, http.StatusBadRequest, valErr.Error())
			return
		}
	}

	manualMeta, metaErr := buildConnectionMetadata(prov, connParams, nil)
	if metaErr != nil {
		writeError(w, http.StatusBadRequest, metaErr.Error())
		return
	}

	tm := tokenMaterial{
		UserID:       dbUser.ID,
		Integration:  req.Integration,
		Connection:   manualConnection,
		Instance:     manualInstance,
		AccessToken:  effectiveCredential,
		MetadataJSON: manualMeta,
	}

	result, err := s.runPostConnect(r.Context(), prov, tm)
	if err != nil {
		slog.ErrorContext(r.Context(), "post_connect failed", "provider", req.Integration, "error", err)
		writeError(w, http.StatusBadGateway, "connection setup failed")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

type createTokenRequest struct {
	Name      string `json:"name"`
	Scopes    string `json:"scopes"`
	ExpiresIn string `json:"expires_in"`
}

type createTokenResponse struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Token     string     `json:"token"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

func (s *Server) createAPIToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.resolveUserID(w, r)
	if !ok {
		return
	}

	var req createTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	if req.Scopes != "" {
		for _, scope := range strings.Fields(req.Scopes) {
			if _, err := s.providers.Get(scope); err != nil {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown scope %q", scope))
				return
			}
		}
	}

	issued, err := s.issueTokenWithTypeAndExpiryHint(principal.TokenTypeAPI, req.ExpiresIn)
	if err != nil {
		if strings.HasPrefix(err.Error(), "invalid expires_in:") {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	apiToken := &core.APIToken{
		ID:          uuid.NewString(),
		UserID:      userID,
		Name:        req.Name,
		HashedToken: issued.Hashed,
		Scopes:      req.Scopes,
		ExpiresAt:   issued.ExpiresAt,
		CreatedAt:   issued.CreatedAt,
		UpdatedAt:   issued.UpdatedAt,
	}

	if err := s.datastore.StoreAPIToken(r.Context(), apiToken); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store API token")
		return
	}

	writeJSON(w, http.StatusCreated, createTokenResponse{
		ID:        apiToken.ID,
		Name:      apiToken.Name,
		Token:     issued.Plaintext,
		ExpiresAt: apiToken.ExpiresAt,
	})
}

type apiTokenInfo struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Scopes    string     `json:"scopes"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

func (s *Server) listAPITokens(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.resolveUserID(w, r)
	if !ok {
		return
	}

	tokens, err := s.datastore.ListAPITokens(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list tokens")
		return
	}

	out := make([]apiTokenInfo, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, apiTokenInfoFromCore(t))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) revokeAPIToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.resolveUserID(w, r)
	if !ok {
		return
	}

	id := chi.URLParam(r, "id")
	if err := s.datastore.RevokeAPIToken(r.Context(), userID, id); err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, "token not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to revoke token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func (s *Server) revokeAllAPITokens(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.resolveUserID(w, r)
	if !ok {
		return
	}

	count, err := s.datastore.RevokeAllAPITokens(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke tokens")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "revoked", "count": count})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

var (
	safeParamValue         = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	safeTokenResponseValue = regexp.MustCompile(`^[a-zA-Z0-9._:/-]+$`)
)

func validateConnectionParams(defs map[string]core.ConnectionParamDef, provided map[string]string) (map[string]string, error) {
	if len(defs) == 0 {
		return nil, nil
	}
	for key := range provided {
		if _, ok := defs[key]; !ok {
			return nil, fmt.Errorf("unknown connection parameter: %s", key)
		}
	}
	result := make(map[string]string)
	for name, def := range defs {
		if def.From != "" {
			continue
		}
		if v, ok := provided[name]; ok && v != "" {
			if !safeParamValue.MatchString(v) {
				return nil, fmt.Errorf("connection parameter %q contains invalid characters (allowed: letters, digits, hyphens, dots, underscores)", name)
			}
			result[name] = v
		} else if def.Default != "" {
			result[name] = def.Default
		} else if def.Required {
			return nil, fmt.Errorf("missing required connection parameter: %s", name)
		}
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

func buildConnectionMetadata(prov core.Provider, userParams map[string]string, tokenResp *core.TokenResponse) (string, error) {
	metadata := make(map[string]string)
	for k, v := range userParams {
		metadata[k] = v
	}

	if cpp, ok := prov.(core.ConnectionParamProvider); ok && tokenResp != nil && tokenResp.Extra != nil {
		for name, def := range cpp.ConnectionParamDefs() {
			if def.From == "token_response" {
				field := def.Field
				if field == "" {
					field = name
				}
				val, ok := tokenResp.Extra[field]
				if !ok {
					if def.Required {
						return "", fmt.Errorf("token response missing required field %q for connection param %q", field, name)
					}
					continue
				}
				s := fmt.Sprintf("%v", val)
				if !safeTokenResponseValue.MatchString(s) {
					return "", fmt.Errorf("token response field %q for connection param %q contains invalid characters", field, name)
				}
				metadata[name] = s
			}
		}
	}

	if len(metadata) == 0 {
		return "", nil
	}
	b, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("marshal connection metadata: %w", err)
	}
	return string(b), nil
}

type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("Authorization") == "" {
		req = req.Clone(req.Context())
		req.Header.Set("Authorization", core.BearerScheme+t.token)
	}
	return t.base.RoundTrip(req)
}

const stagedConnectionTTL = 30 * time.Minute

type tokenMaterial struct {
	UserID         string
	Integration    string
	Connection     string
	Instance       string
	AccessToken    string
	RefreshToken   string
	TokenExpiresAt *time.Time
	MetadataJSON   string
}

type postConnectResult struct {
	Status     string                    `json:"status"`
	StagedID   string                    `json:"staged_id,omitempty"`
	Candidates []core.DiscoveryCandidate `json:"candidates,omitempty"`
}

func (s *Server) storeTokenFromMaterial(ctx context.Context, tm tokenMaterial) (*core.IntegrationToken, error) {
	now := s.now().UTC().Truncate(time.Second)
	tok := &core.IntegrationToken{
		ID:              uuid.NewString(),
		UserID:          tm.UserID,
		Integration:     tm.Integration,
		Connection:      tm.Connection,
		Instance:        tm.Instance,
		AccessToken:     tm.AccessToken,
		RefreshToken:    tm.RefreshToken,
		ExpiresAt:       tm.TokenExpiresAt,
		LastRefreshedAt: &now,
		MetadataJSON:    tm.MetadataJSON,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := s.datastore.StoreToken(ctx, tok); err != nil {
		return nil, err
	}
	return tok, nil
}

func validateDiscoveryMetadata(metadata map[string]string) error {
	for k, v := range metadata {
		if !safeParamValue.MatchString(k) || !safeTokenResponseValue.MatchString(v) {
			return fmt.Errorf("discovery returned invalid key or value for %q", k)
		}
	}
	return nil
}
func mergeMetadataJSON(existing string, extra map[string]string) (string, error) {
	m := make(map[string]string)
	if existing != "" {
		if err := json.Unmarshal([]byte(existing), &m); err != nil {
			return "", fmt.Errorf("corrupt MetadataJSON: %w", err)
		}
	}
	for k, v := range extra {
		m[k] = v
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("marshal merged metadata: %w", err)
	}
	return string(b), nil
}

func (s *Server) runPostConnect(ctx context.Context, prov core.Provider, tm tokenMaterial) (*postConnectResult, error) {
	if dcp, ok := prov.(core.DiscoveryConfigProvider); ok {
		if cfg := dcp.DiscoveryConfig(); cfg != nil {
			client := &http.Client{
				Timeout:   30 * time.Second,
				Transport: &bearerTransport{token: tm.AccessToken, base: http.DefaultTransport},
			}
			candidates, err := discovery.Run(ctx, cfg, client)
			if err != nil {
				return nil, fmt.Errorf("discovery: %w", err)
			}
			if len(candidates) == 0 {
				return nil, fmt.Errorf("no resources discovered")
			}
			if len(candidates) == 1 {
				if err := validateDiscoveryMetadata(candidates[0].Metadata); err != nil {
					return nil, err
				}
				merged, err := mergeMetadataJSON(tm.MetadataJSON, candidates[0].Metadata)
				if err != nil {
					return nil, err
				}
				tm.MetadataJSON = merged
				if _, err := s.storeTokenFromMaterial(ctx, tm); err != nil {
					return nil, err
				}
				return &postConnectResult{Status: "connected"}, nil
			}

			scs, err := s.stagedConnectionStore()
			if err != nil {
				return nil, err
			}
			candidatesJSON, err := json.Marshal(candidates)
			if err != nil {
				return nil, fmt.Errorf("marshal discovery candidates: %w", err)
			}
			now := s.now().UTC().Truncate(time.Second)
			sc := &core.StagedConnection{
				ID:             uuid.NewString(),
				UserID:         tm.UserID,
				Integration:    tm.Integration,
				Connection:     tm.Connection,
				Instance:       tm.Instance,
				AccessToken:    tm.AccessToken,
				RefreshToken:   tm.RefreshToken,
				TokenExpiresAt: tm.TokenExpiresAt,
				MetadataJSON:   tm.MetadataJSON,
				CandidatesJSON: string(candidatesJSON),
				CreatedAt:      now,
				ExpiresAt:      now.Add(stagedConnectionTTL),
			}
			if err := scs.StoreStagedConnection(ctx, sc); err != nil {
				return nil, fmt.Errorf("storing staged connection: %w", err)
			}
			return &postConnectResult{
				Status:     "selection_required",
				StagedID:   sc.ID,
				Candidates: candidates,
			}, nil
		}
	}

	if _, err := s.storeTokenFromMaterial(ctx, tm); err != nil {
		return nil, err
	}
	return &postConnectResult{Status: "connected"}, nil
}

func (s *Server) loadStagedConnection(w http.ResponseWriter, r *http.Request, userID string, scs core.StagedConnectionStore) (*core.StagedConnection, bool) {
	id := chi.URLParam(r, "id")
	sc, err := scs.GetStagedConnection(r.Context(), id)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, "staged connection not found")
			return nil, false
		}
		writeError(w, http.StatusInternalServerError, "failed to get staged connection")
		return nil, false
	}
	if sc.UserID != userID {
		writeError(w, http.StatusNotFound, "staged connection not found")
		return nil, false
	}
	if s.now().After(sc.ExpiresAt) {
		_ = scs.DeleteStagedConnection(r.Context(), id)
		writeError(w, http.StatusGone, "staged connection has expired")
		return nil, false
	}
	return sc, true
}

func (s *Server) getStagedConnection(w http.ResponseWriter, r *http.Request) {
	scs, err := s.stagedConnectionStore()
	if err != nil {
		writeError(w, http.StatusNotImplemented, err.Error())
		return
	}
	userID, ok := s.resolveUserID(w, r)
	if !ok {
		return
	}
	sc, ok := s.loadStagedConnection(w, r, userID, scs)
	if !ok {
		return
	}

	var candidates []core.DiscoveryCandidate
	if err := json.Unmarshal([]byte(sc.CandidatesJSON), &candidates); err != nil {
		writeError(w, http.StatusInternalServerError, "corrupt candidates data")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":          sc.ID,
		"integration": sc.Integration,
		"instance":    sc.Instance,
		"candidates":  candidates,
	})
}

func (s *Server) selectStagedConnection(w http.ResponseWriter, r *http.Request) {
	scs, err := s.stagedConnectionStore()
	if err != nil {
		writeError(w, http.StatusNotImplemented, err.Error())
		return
	}
	userID, ok := s.resolveUserID(w, r)
	if !ok {
		return
	}

	var req struct {
		CandidateID string `json:"candidate_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.CandidateID == "" {
		writeError(w, http.StatusBadRequest, "candidate_id is required")
		return
	}

	sc, ok := s.loadStagedConnection(w, r, userID, scs)
	if !ok {
		return
	}

	var candidates []core.DiscoveryCandidate
	if err := json.Unmarshal([]byte(sc.CandidatesJSON), &candidates); err != nil {
		writeError(w, http.StatusInternalServerError, "corrupt candidates data")
		return
	}

	var selected *core.DiscoveryCandidate
	for i := range candidates {
		if candidates[i].ID == req.CandidateID {
			selected = &candidates[i]
			break
		}
	}
	if selected == nil {
		writeError(w, http.StatusBadRequest, "candidate not found")
		return
	}

	if err := validateDiscoveryMetadata(selected.Metadata); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	merged, err := mergeMetadataJSON(sc.MetadataJSON, selected.Metadata)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to merge metadata")
		return
	}

	tm := tokenMaterial{
		UserID:         sc.UserID,
		Integration:    sc.Integration,
		Connection:     sc.Connection,
		Instance:       sc.Instance,
		AccessToken:    sc.AccessToken,
		RefreshToken:   sc.RefreshToken,
		TokenExpiresAt: sc.TokenExpiresAt,
		MetadataJSON:   merged,
	}
	if _, err := s.storeTokenFromMaterial(r.Context(), tm); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store connection")
		return
	}

	if err := scs.DeleteStagedConnection(r.Context(), sc.ID); err != nil {
		slog.ErrorContext(r.Context(), "failed to delete staged connection", "staged_connection_id", sc.ID, "error", err)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "connected"})
}

func (s *Server) cancelStagedConnection(w http.ResponseWriter, r *http.Request) {
	scs, err := s.stagedConnectionStore()
	if err != nil {
		writeError(w, http.StatusNotImplemented, err.Error())
		return
	}
	userID, ok := s.resolveUserID(w, r)
	if !ok {
		return
	}

	sc, ok := s.loadStagedConnection(w, r, userID, scs)
	if !ok {
		return
	}

	if err := scs.DeleteStagedConnection(r.Context(), sc.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to cancel staged connection")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "canceled"})
}
