package google

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/core/session"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const defaultSessionTTL = 24 * time.Hour

type Config struct {
	ClientID       string
	ClientSecret   string
	RedirectURL    string
	AllowedDomains []string
	SessionSecret  []byte
	SessionTTL     time.Duration
}

type Provider struct {
	oauth2Config *oauth2.Config
	httpClient   *http.Client
	userinfoURL  string
	allowed      map[string]bool
	secret       []byte
	ttl          time.Duration
}

func New(cfg Config) (*Provider, error) {
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("google auth: client ID is required")
	}
	if cfg.ClientSecret == "" {
		return nil, fmt.Errorf("google auth: client secret is required")
	}
	if cfg.RedirectURL == "" {
		return nil, fmt.Errorf("google auth: redirect URL is required")
	}
	if len(cfg.SessionSecret) == 0 {
		return nil, fmt.Errorf("google auth: session secret is required")
	}

	ttl := cfg.SessionTTL
	if ttl == 0 {
		ttl = defaultSessionTTL
	}

	allowed := make(map[string]bool, len(cfg.AllowedDomains))
	for _, d := range cfg.AllowedDomains {
		allowed[strings.ToLower(d)] = true
	}

	return &Provider{
		oauth2Config: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Scopes:       []string{"openid", "email", "profile"},
			Endpoint:     google.Endpoint,
		},
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		userinfoURL: "https://www.googleapis.com/oauth2/v3/userinfo",
		allowed:     allowed,
		secret:      cfg.SessionSecret,
		ttl:         ttl,
	}, nil
}

func (p *Provider) Name() string        { return "google" }
func (p *Provider) DisplayName() string { return "Google" }

func (p *Provider) LoginURL(state string) (string, error) {
	return p.oauth2Config.AuthCodeURL(state, oauth2.AccessTypeOffline), nil
}

func (p *Provider) HandleCallback(ctx context.Context, code string) (*core.UserIdentity, error) {
	client := p.httpClient
	if client != nil {
		ctx = context.WithValue(ctx, oauth2.HTTPClient, client)
	}

	tok, err := p.oauth2Config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("exchange code: %w", err)
	}

	identity, err := p.fetchUserInfo(ctx, tok.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("handle callback: %w", err)
	}

	if err := session.CheckDomain(p.allowed, identity.Email); err != nil {
		return nil, err
	}

	return identity, nil
}

func (p *Provider) IssueSessionToken(identity *core.UserIdentity) (string, error) {
	return session.IssueToken(identity, p.secret, p.ttl)
}

func (p *Provider) ValidateToken(ctx context.Context, token string) (*core.UserIdentity, error) {
	identity, err := session.ValidateToken(token, p.secret)
	if err == nil {
		if err := session.CheckDomain(p.allowed, identity.Email); err != nil {
			return nil, err
		}
		return identity, nil
	}

	if !errors.Is(err, session.ErrNotJWT) {
		return nil, err
	}

	identity, err = p.fetchUserInfo(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("validate token: %w", err)
	}

	if err := session.CheckDomain(p.allowed, identity.Email); err != nil {
		return nil, err
	}

	return identity, nil
}

func (p *Provider) fetchUserInfo(ctx context.Context, accessToken string) (*core.UserIdentity, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.userinfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create userinfo request: %w", err)
	}
	req.Header.Set("Authorization", core.BearerScheme+accessToken)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch userinfo: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("userinfo returned %d: %s", resp.StatusCode, body)
	}

	var info struct {
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode userinfo: %w", err)
	}

	return &core.UserIdentity{
		Email:       info.Email,
		DisplayName: info.Name,
		AvatarURL:   info.Picture,
	}, nil
}
