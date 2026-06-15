package mcpoauth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/apps/oauth"
)

const (
	ensureTimeout = 30 * time.Second

	// resourceParam is the RFC 8707 resource indicator.
	resourceParam = "resource"

	clientAuthMethodPost  = "client_secret_post"
	clientAuthMethodBasic = "client_secret_basic"
)

type HandlerConfig struct {
	MCPURL      string
	Credentials core.ExternalCredentialProvider
	RedirectURL string

	// Static overrides: if set, skip DCR and use these directly.
	ClientID     string
	ClientSecret string
}

// Handler is stateless: every call discovers the authorization server and
// resolves the registered client from the external credentials provider, so
// all server instances present the same client_id across an OAuth flow.
type Handler struct {
	cfg HandlerConfig
}

func NewHandler(cfg HandlerConfig) *Handler {
	return &Handler{cfg: cfg}
}

func (h *Handler) IsManual() bool { return false }

type resolvedClient struct {
	md           *DiscoveredMetadata
	clientID     string
	clientSecret string
}

func (h *Handler) resolveClient(ctx context.Context) (*resolvedClient, error) {
	md, err := Discover(ctx, h.cfg.MCPURL)
	if err != nil {
		return nil, fmt.Errorf("mcp oauth discovery failed for %s: %w", h.cfg.MCPURL, err)
	}

	clientID := h.cfg.ClientID
	clientSecret := h.cfg.ClientSecret
	if clientID == "" {
		reg, err := h.resolveRegistration(ctx, md)
		if err != nil {
			return nil, err
		}
		clientID = reg.ClientID
		clientSecret = reg.ClientSecret
	}
	return &resolvedClient{md: md, clientID: clientID, clientSecret: clientSecret}, nil
}

func (h *Handler) ensure(ctx context.Context) (*oauth.UpstreamHandler, error) {
	ctx, cancel := context.WithTimeout(ctx, ensureTimeout)
	defer cancel()

	resolved, err := h.resolveClient(ctx)
	if err != nil {
		return nil, err
	}
	md := resolved.md

	authMethod := oauth.ClientAuthNone
	switch md.PreferredAuthMethod() {
	case clientAuthMethodPost:
		authMethod = oauth.ClientAuthBody
	case clientAuthMethodBasic:
		authMethod = oauth.ClientAuthHeader
	}

	// If a client secret is available but the selected auth method would
	// not send it, upgrade to client_secret_post so the secret is included
	// in token exchange requests.
	if resolved.clientSecret != "" && authMethod == oauth.ClientAuthNone {
		authMethod = oauth.ClientAuthBody
	}

	resource := map[string]string{resourceParam: md.Resource}
	return oauth.NewUpstream(oauth.UpstreamConfig{
		ClientID:            resolved.clientID,
		ClientSecret:        resolved.clientSecret,
		AuthorizationURL:    md.AuthorizationEndpoint,
		TokenURL:            md.TokenEndpoint,
		RedirectURL:         h.cfg.RedirectURL,
		ClientAuthMethod:    authMethod,
		PKCE:                md.SupportsPKCE(),
		DefaultScopes:       md.ScopesSupported,
		AuthorizationParams: resource,
		TokenParams:         resource,
		RefreshParams:       resource,
	}), nil
}

// AuthConfig returns the refresh-capable auth config for this MCP connection:
// the discovered token endpoint plus the client identity (static config or
// the shared registration), so the external credentials provider can refresh
// stored grants.
func (h *Handler) AuthConfig(ctx context.Context) (core.ExternalCredentialAuthConfig, error) {
	ctx, cancel := context.WithTimeout(ctx, ensureTimeout)
	defer cancel()

	resolved, err := h.resolveClient(ctx)
	if err != nil {
		return core.ExternalCredentialAuthConfig{}, err
	}

	clientAuth := ""
	if resolved.md.PreferredAuthMethod() == clientAuthMethodBasic {
		clientAuth = "header"
	}
	return core.ExternalCredentialAuthConfig{
		Type:          "oauth2",
		TokenURL:      resolved.md.TokenEndpoint,
		ClientID:      resolved.clientID,
		ClientSecret:  resolved.clientSecret,
		ClientAuth:    clientAuth,
		RefreshParams: map[string]string{resourceParam: resolved.md.Resource},
	}, nil
}

func (h *Handler) resolveRegistration(ctx context.Context, md *DiscoveredMetadata) (*Registration, error) {
	existingID, existing, err := h.storedRegistration(ctx, md)
	if err != nil {
		return nil, err
	}
	if existing != nil && !existing.Expired() {
		return existing, nil
	}

	if md.RegistrationEndpoint == "" {
		if existing != nil {
			return existing, nil
		}
		return nil, fmt.Errorf("mcp oauth: no client_id available for %s (no static config and no registration endpoint)", h.cfg.MCPURL)
	}

	// Expired: replace, accepting the small delete/create race window.
	if existing != nil {
		if err := h.cfg.Credentials.DeleteCredential(ctx, existingID); err != nil {
			return nil, err
		}
	}

	reg, err := RegisterClient(ctx, md.RegistrationEndpoint, h.cfg.RedirectURL, "Gestalt", md.PreferredAuthMethod())
	if err != nil {
		return nil, fmt.Errorf("mcp oauth DCR for %s: %w", h.cfg.MCPURL, err)
	}

	credential := &core.ExternalCredential{
		Subject:   core.GestaltdSubjectID,
		Audience:  md.AuthServerURL,
		Qualifier: h.cfg.RedirectURL,
		Client: &core.ExternalCredentialClientInfo{
			ClientID:              reg.ClientID,
			ClientSecret:          reg.ClientSecret,
			ClientSecretExpiresAt: reg.ExpiresAt,
		},
	}
	if err := h.cfg.Credentials.CreateCredential(ctx, credential); err != nil {
		if errors.Is(err, core.ErrAlreadyExists) {
			// Lost the registration race: discard our client and adopt the
			// winner's so concurrent flows use one client end-to-end.
			_, winner, getErr := h.storedRegistration(ctx, md)
			if getErr == nil && winner != nil {
				return winner, nil
			}
		}
		return nil, err
	}
	return reg, nil
}

func (h *Handler) storedRegistration(ctx context.Context, md *DiscoveredMetadata) (string, *Registration, error) {
	credential, err := h.cfg.Credentials.GetCredential(ctx, core.GestaltdSubjectID, md.AuthServerURL, h.cfg.RedirectURL)
	if errors.Is(err, core.ErrNotFound) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	if credential == nil || credential.Client == nil {
		return "", nil, fmt.Errorf("mcp oauth: stored credential for %s is not a client registration", md.AuthServerURL)
	}
	return credential.ID, &Registration{
		ClientID:     credential.Client.ClientID,
		ClientSecret: credential.Client.ClientSecret,
		ExpiresAt:    credential.Client.ClientSecretExpiresAt,
	}, nil
}

// AuthorizationURL discards the PKCE verifier. Use StartOAuth when the
// verifier is needed for code exchange.
func (h *Handler) AuthorizationURL(state string, scopes []string) string {
	upstream, err := h.ensure(context.Background())
	if err != nil {
		slog.Error("mcpoauth: ensure failed", "method", "AuthorizationURL", "error", err)
		return ""
	}
	authURL, _ := upstream.AuthorizationURLWithPKCE(state, scopes)
	return authURL
}

func (h *Handler) StartOAuth(state string, scopes []string) (string, string) {
	upstream, err := h.ensure(context.Background())
	if err != nil {
		slog.Error("mcpoauth: ensure failed", "method", "StartOAuth", "error", err)
		return "", ""
	}
	return upstream.AuthorizationURLWithPKCE(state, scopes)
}

func (h *Handler) StartOAuthWithOverride(authBaseURL, state string, scopes []string) (string, string) {
	upstream, err := h.ensure(context.Background())
	if err != nil {
		slog.Error("mcpoauth: ensure failed", "method", "StartOAuthWithOverride", "error", err)
		return "", ""
	}
	return upstream.AuthorizationURLWithOverride(authBaseURL, state, scopes)
}

func (h *Handler) ExchangeCode(ctx context.Context, code string) (*core.OAuthTokenResponse, error) {
	upstream, err := h.ensure(ctx)
	if err != nil {
		return nil, err
	}
	return upstream.ExchangeCode(ctx, code)
}

func (h *Handler) ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...oauth.ExchangeOption) (*core.OAuthTokenResponse, error) {
	upstream, err := h.ensure(ctx)
	if err != nil {
		return nil, err
	}
	var opts []oauth.ExchangeOption
	if verifier != "" {
		opts = append(opts, oauth.WithPKCEVerifier(verifier))
	}
	opts = append(opts, extraOpts...)
	return upstream.ExchangeCode(ctx, code, opts...)
}

func (h *Handler) RefreshToken(ctx context.Context, refreshToken string) (*core.OAuthTokenResponse, error) {
	upstream, err := h.ensure(ctx)
	if err != nil {
		return nil, err
	}
	return upstream.RefreshToken(ctx, refreshToken)
}

func (h *Handler) RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.OAuthTokenResponse, error) {
	upstream, err := h.ensure(ctx)
	if err != nil {
		return nil, err
	}
	return upstream.RefreshTokenWithURL(ctx, refreshToken, tokenURL)
}

func (h *Handler) TokenURL() string {
	upstream, err := h.ensure(context.Background())
	if err != nil {
		return ""
	}
	return upstream.TokenURL()
}

func (h *Handler) AuthorizationBaseURL() string {
	upstream, err := h.ensure(context.Background())
	if err != nil {
		return ""
	}
	return upstream.AuthorizationBaseURL()
}
