package mcpoauth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/apps/oauth"
)

const ensureTimeout = 30 * time.Second

type HandlerConfig struct {
	MCPURL      string
	Store       *Store
	RedirectURL string

	// Static overrides: if set, skip DCR and use these directly.
	ClientID     string
	ClientSecret string
}

// Handler is stateless: every call discovers the authorization server and
// resolves the registered client from the shared Store, so all server
// instances present the same client_id across an OAuth flow.
type Handler struct {
	cfg HandlerConfig
}

func NewHandler(cfg HandlerConfig) *Handler {
	return &Handler{cfg: cfg}
}

func (h *Handler) IsManual() bool { return false }

func (h *Handler) ensure(ctx context.Context) (*oauth.UpstreamHandler, error) {
	ctx, cancel := context.WithTimeout(ctx, ensureTimeout)
	defer cancel()

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

	authMethod := oauth.ClientAuthNone
	switch md.PreferredAuthMethod() {
	case "client_secret_post":
		authMethod = oauth.ClientAuthBody
	case "client_secret_basic":
		authMethod = oauth.ClientAuthHeader
	}

	// If a client secret is available but the selected auth method would
	// not send it, upgrade to client_secret_post so the secret is included
	// in token exchange requests.
	if clientSecret != "" && authMethod == oauth.ClientAuthNone {
		authMethod = oauth.ClientAuthBody
	}

	return oauth.NewUpstream(oauth.UpstreamConfig{
		ClientID:         clientID,
		ClientSecret:     clientSecret,
		AuthorizationURL: md.AuthorizationEndpoint,
		TokenURL:         md.TokenEndpoint,
		RedirectURL:      h.cfg.RedirectURL,
		ClientAuthMethod: authMethod,
		PKCE:             md.SupportsPKCE(),
		DefaultScopes:    md.ScopesSupported,
	}), nil
}

func (h *Handler) resolveRegistration(ctx context.Context, md *DiscoveredMetadata) (*Registration, error) {
	existing, err := h.cfg.Store.Get(ctx, md.AuthServerURL, h.cfg.RedirectURL)
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

	reg, err := RegisterClient(ctx, md.RegistrationEndpoint, h.cfg.RedirectURL, "Gestalt", md.PreferredAuthMethod())
	if err != nil {
		return nil, fmt.Errorf("mcp oauth DCR for %s: %w", h.cfg.MCPURL, err)
	}

	if existing != nil {
		if err := h.cfg.Store.Put(ctx, md.AuthServerURL, h.cfg.RedirectURL, reg); err != nil {
			return nil, err
		}
		return reg, nil
	}
	if err := h.cfg.Store.Add(ctx, md.AuthServerURL, h.cfg.RedirectURL, reg); err != nil {
		if errors.Is(err, idb.ErrAlreadyExists) {
			// Lost the registration race: discard our client and adopt the
			// winner's so concurrent flows use one client end-to-end.
			winner, getErr := h.cfg.Store.Get(ctx, md.AuthServerURL, h.cfg.RedirectURL)
			if getErr == nil && winner != nil {
				return winner, nil
			}
		}
		return nil, err
	}
	return reg, nil
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

func (h *Handler) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	upstream, err := h.ensure(ctx)
	if err != nil {
		return nil, err
	}
	return upstream.ExchangeCode(ctx, code)
}

func (h *Handler) ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...oauth.ExchangeOption) (*core.TokenResponse, error) {
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

func (h *Handler) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	upstream, err := h.ensure(ctx)
	if err != nil {
		return nil, err
	}
	return upstream.RefreshToken(ctx, refreshToken)
}

func (h *Handler) RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error) {
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
