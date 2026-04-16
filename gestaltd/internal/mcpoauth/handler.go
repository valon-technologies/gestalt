package mcpoauth

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/oauth"
)

const discoveryCacheTTL = 24 * time.Hour

type HandlerConfig struct {
	MCPURL      string
	Store       RegistrationStore // nil for non-SQL deployments
	RedirectURL string

	// Static overrides: if set, skip DCR and use these directly.
	ClientID     string
	ClientSecret string
}

type Handler struct {
	cfg HandlerConfig

	mu           sync.Mutex
	upstream     *oauth.UpstreamHandler
	metadata     *DiscoveredMetadata
	discoveredAt time.Time
}

func NewHandler(cfg HandlerConfig) *Handler {
	return &Handler{cfg: cfg}
}

func (h *Handler) ensure() (*oauth.UpstreamHandler, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.upstream != nil && time.Since(h.discoveredAt) < discoveryCacheTTL {
		return h.upstream, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	md, err := Discover(ctx, h.cfg.MCPURL)
	if err != nil {
		if h.upstream != nil {
			return h.upstream, nil
		}
		return nil, fmt.Errorf("mcp oauth discovery failed for %s: %w", h.cfg.MCPURL, err)
	}

	clientID := h.cfg.ClientID
	clientSecret := h.cfg.ClientSecret

	if clientID == "" {
		reg, err := h.resolveRegistration(ctx, md)
		if err != nil {
			if h.upstream != nil {
				return h.upstream, nil
			}
			return nil, err
		}
		if reg != nil {
			clientID = reg.ClientID
			clientSecret = reg.ClientSecret
		}
	}

	if clientID == "" {
		if h.upstream != nil {
			return h.upstream, nil
		}
		return nil, fmt.Errorf("mcp oauth: no client_id available for %s (no static config and DCR failed or unavailable)", h.cfg.MCPURL)
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

	upstream := oauth.NewUpstream(oauth.UpstreamConfig{
		ClientID:         clientID,
		ClientSecret:     clientSecret,
		AuthorizationURL: md.AuthorizationEndpoint,
		TokenURL:         md.TokenEndpoint,
		RedirectURL:      h.cfg.RedirectURL,
		ClientAuthMethod: authMethod,
		PKCE:             md.SupportsPKCE(),
		DefaultScopes:    md.ScopesSupported,
	})

	h.upstream = upstream
	h.metadata = md
	h.discoveredAt = time.Now()
	return upstream, nil
}

func (h *Handler) resolveRegistration(ctx context.Context, md *DiscoveredMetadata) (*Registration, error) {
	authServerURL := md.AuthServerURL
	var existing *Registration
	if h.cfg.Store != nil {
		var err error
		existing, err = h.cfg.Store.GetRegistration(ctx, authServerURL, h.cfg.RedirectURL)
		if err != nil {
			slog.Warn("mcpoauth: reading registration failed", "auth_server", authServerURL, "error", err)
		}
	}

	if existing != nil && !existing.Expired() {
		return existing, nil
	}

	if md.RegistrationEndpoint == "" {
		if existing != nil {
			return existing, nil
		}
		return nil, nil
	}

	if existing != nil {
		slog.Info("mcpoauth: re-registering (expired)", "auth_server", authServerURL)
	}

	reg, err := RegisterClient(ctx, md.RegistrationEndpoint, h.cfg.RedirectURL, "Gestalt", md.PreferredAuthMethod())
	if err != nil {
		if existing != nil {
			slog.Warn("mcpoauth: re-registration failed, using existing", "auth_server", authServerURL, "error", err)
			return existing, nil
		}
		return nil, fmt.Errorf("mcp oauth DCR for %s: %w", h.cfg.MCPURL, err)
	}

	reg.AuthServerURL = authServerURL
	reg.RedirectURI = h.cfg.RedirectURL
	reg.AuthorizationEndpoint = md.AuthorizationEndpoint
	reg.TokenEndpoint = md.TokenEndpoint

	if scopesJSON, err := json.Marshal(md.ScopesSupported); err == nil {
		reg.ScopesSupported = string(scopesJSON)
	}

	if h.cfg.Store != nil {
		if err := h.cfg.Store.StoreRegistration(ctx, reg); err != nil {
			slog.Warn("mcpoauth: storing registration failed", "auth_server", authServerURL, "error", err)
		}
	}

	return reg, nil
}

func (h *Handler) ClearRegistration() {
	h.mu.Lock()
	defer h.mu.Unlock()

	var authServerURL string
	if h.metadata != nil {
		authServerURL = h.metadata.AuthServerURL
	}
	h.upstream = nil
	h.metadata = nil
	h.discoveredAt = time.Time{}

	if h.cfg.Store == nil || authServerURL == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := h.cfg.Store.DeleteRegistration(ctx, authServerURL, h.cfg.RedirectURL); err != nil {
		slog.Warn("mcpoauth: deleting registration failed", "auth_server", authServerURL, "error", err)
	}
}

// AuthorizationURL discards the PKCE verifier. Use StartOAuth when the
// verifier is needed for code exchange.
func (h *Handler) AuthorizationURL(state string, scopes []string) string {
	upstream, err := h.ensure()
	if err != nil {
		slog.Error("mcpoauth: ensure failed", "method", "AuthorizationURL", "error", err)
		return ""
	}
	authURL, _ := upstream.AuthorizationURLWithPKCE(state, scopes)
	return authURL
}

func (h *Handler) StartOAuth(state string, scopes []string) (string, string) {
	upstream, err := h.ensure()
	if err != nil {
		slog.Error("mcpoauth: ensure failed", "method", "StartOAuth", "error", err)
		return "", ""
	}
	return upstream.AuthorizationURLWithPKCE(state, scopes)
}

func (h *Handler) StartOAuthWithOverride(authBaseURL, state string, scopes []string) (string, string) {
	upstream, err := h.ensure()
	if err != nil {
		slog.Error("mcpoauth: ensure failed", "method", "StartOAuthWithOverride", "error", err)
		return "", ""
	}
	return upstream.AuthorizationURLWithOverride(authBaseURL, state, scopes)
}

func (h *Handler) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	upstream, err := h.ensure()
	if err != nil {
		return nil, err
	}
	return upstream.ExchangeCode(ctx, code)
}

func (h *Handler) ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...oauth.ExchangeOption) (*core.TokenResponse, error) {
	upstream, err := h.ensure()
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
	upstream, err := h.ensure()
	if err != nil {
		return nil, err
	}
	return upstream.RefreshToken(ctx, refreshToken)
}

func (h *Handler) RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error) {
	upstream, err := h.ensure()
	if err != nil {
		return nil, err
	}
	return upstream.RefreshTokenWithURL(ctx, refreshToken, tokenURL)
}

func (h *Handler) TokenURL() string {
	upstream, err := h.ensure()
	if err != nil {
		return ""
	}
	return upstream.TokenURL()
}

func (h *Handler) AuthorizationBaseURL() string {
	upstream, err := h.ensure()
	if err != nil {
		return ""
	}
	return upstream.AuthorizationBaseURL()
}
