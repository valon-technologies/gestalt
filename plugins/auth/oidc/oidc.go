package oidc

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/crypto"
	"github.com/valon-technologies/gestalt/core/session"
	"github.com/valon-technologies/gestalt/internal/oauth"
	"golang.org/x/oauth2"
)

const (
	defaultSessionTTL      = 24 * time.Hour
	defaultOIDCDisplayName = "SSO"
)

type Config struct {
	IssuerURL      string        `yaml:"issuer_url"`
	ClientID       string        `yaml:"client_id"`
	ClientSecret   string        `yaml:"client_secret"`
	RedirectURL    string        `yaml:"redirect_url"`
	AllowedDomains []string      `yaml:"allowed_domains"`
	Scopes         []string      `yaml:"scopes"`
	SessionSecret  string        `yaml:"session_secret"`
	SessionTTL     time.Duration `yaml:"session_ttl"`
	PKCE           bool          `yaml:"pkce"`
	DisplayName    string        `yaml:"display_name"`
}

type Provider struct {
	oauth2Cfg   *oauth2.Config
	discovery   *DiscoveryDocument
	httpClient  *http.Client
	allowed     map[string]bool
	secret      []byte
	encryptor   *crypto.AESGCMEncryptor
	ttl         time.Duration
	pkce        bool
	displayName string
}

type pkceState struct {
	State    string `json:"state"`
	Verifier string `json:"verifier"`
}

func New(cfg Config) (*Provider, error) {
	if cfg.IssuerURL == "" {
		return nil, fmt.Errorf("oidc auth: issuer URL is required")
	}
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("oidc auth: client ID is required")
	}
	if cfg.RedirectURL == "" {
		return nil, fmt.Errorf("oidc auth: redirect URL is required")
	}
	if cfg.SessionSecret == "" {
		return nil, fmt.Errorf("oidc auth: session secret is required")
	}

	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "email", "profile"}
	}

	ttl := cfg.SessionTTL
	if ttl == 0 {
		ttl = defaultSessionTTL
	}

	displayName := cfg.DisplayName
	if displayName == "" {
		displayName = defaultOIDCDisplayName
	}

	allowed := make(map[string]bool, len(cfg.AllowedDomains))
	for _, d := range cfg.AllowedDomains {
		allowed[strings.ToLower(d)] = true
	}

	httpClient := &http.Client{Timeout: 10 * time.Second}

	doc, err := Discover(context.Background(), cfg.IssuerURL, httpClient)
	if err != nil {
		return nil, fmt.Errorf("oidc auth: discovery failed: %w", err)
	}

	oauth2Cfg := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Scopes:       scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:  doc.AuthorizationEndpoint,
			TokenURL: doc.TokenEndpoint,
		},
	}

	encKey := sha256.Sum256([]byte(cfg.SessionSecret))
	enc, err := crypto.NewAESGCM(encKey[:])
	if err != nil {
		return nil, fmt.Errorf("oidc auth: init encryptor: %w", err)
	}

	return &Provider{
		oauth2Cfg:   oauth2Cfg,
		discovery:   doc,
		httpClient:  httpClient,
		allowed:     allowed,
		secret:      []byte(cfg.SessionSecret),
		encryptor:   enc,
		ttl:         ttl,
		pkce:        cfg.PKCE,
		displayName: displayName,
	}, nil
}

func (p *Provider) Name() string                   { return "oidc" }
func (p *Provider) DisplayName() string            { return p.displayName }
func (p *Provider) SessionTokenTTL() time.Duration { return p.ttl }

func (p *Provider) LoginURL(state string) (string, error) {
	if !p.pkce {
		return p.oauth2Cfg.AuthCodeURL(state, oauth2.AccessTypeOffline), nil
	}

	verifier := oauth.GenerateVerifier()
	challenge := oauth.ComputeS256Challenge(verifier)
	encoded, err := p.encodePKCEState(state, verifier)
	if err != nil {
		return "", fmt.Errorf("encode pkce state: %w", err)
	}

	return p.oauth2Cfg.AuthCodeURL(encoded,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	), nil
}

func (p *Provider) HandleCallback(ctx context.Context, code string) (*core.UserIdentity, error) {
	if p.pkce {
		return nil, fmt.Errorf("oidc: PKCE is enabled; use HandleCallbackWithState")
	}
	return p.exchangeAndFetchIdentity(ctx, code)
}

func (p *Provider) HandleCallbackWithState(ctx context.Context, code, encryptedState string) (identity *core.UserIdentity, originalState string, err error) {
	if !p.pkce {
		id, err := p.exchangeAndFetchIdentity(ctx, code)
		return id, encryptedState, err
	}

	origState, verifier, err := p.decodePKCEState(encryptedState)
	if err != nil {
		return nil, "", fmt.Errorf("decode pkce state: %w", err)
	}

	id, err := p.exchangeAndFetchIdentity(ctx, code, oauth2.SetAuthURLParam("code_verifier", verifier))
	if err != nil {
		return nil, "", err
	}

	return id, origState, nil
}

func (p *Provider) exchangeAndFetchIdentity(ctx context.Context, code string, opts ...oauth2.AuthCodeOption) (*core.UserIdentity, error) {
	if p.httpClient != nil {
		ctx = context.WithValue(ctx, oauth2.HTTPClient, p.httpClient)
	}

	tok, err := p.oauth2Cfg.Exchange(ctx, code, opts...)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.discovery.UserinfoEndpoint, nil)
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
		Email         string `json:"email"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
		EmailVerified any    `json:"email_verified"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode userinfo: %w", err)
	}

	switch v := info.EmailVerified.(type) {
	case bool:
		if !v {
			return nil, fmt.Errorf("oidc: email %s is not verified", info.Email)
		}
	case string:
		if strings.EqualFold(v, "false") {
			return nil, fmt.Errorf("oidc: email %s is not verified", info.Email)
		}
	}

	return &core.UserIdentity{
		Email:       info.Email,
		DisplayName: info.Name,
		AvatarURL:   info.Picture,
	}, nil
}

func (p *Provider) encodePKCEState(state, verifier string) (string, error) {
	payload, err := json.Marshal(pkceState{State: state, Verifier: verifier})
	if err != nil {
		return "", fmt.Errorf("oidc: marshal pkce state: %w", err)
	}
	return p.encryptor.EncryptURLSafe(string(payload))
}

func (p *Provider) decodePKCEState(encoded string) (state, verifier string, err error) {
	plaintext, err := p.encryptor.DecryptURLSafe(encoded)
	if err != nil {
		return "", "", fmt.Errorf("oidc: decrypt pkce state: %w", err)
	}
	var ps pkceState
	if err := json.Unmarshal([]byte(plaintext), &ps); err != nil {
		return "", "", fmt.Errorf("oidc: unmarshal pkce state: %w", err)
	}
	return ps.State, ps.Verifier, nil
}
