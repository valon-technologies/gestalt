package oauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/valon-technologies/toolshed/core"
)

const defaultTimeout = 10 * time.Second

type ClientAuthMethod int

const (
	ClientAuthBody ClientAuthMethod = iota
	ClientAuthHeader
)

type TokenExchangeFormat string

const (
	TokenExchangeForm TokenExchangeFormat = "form"
	TokenExchangeJSON TokenExchangeFormat = "json"
)

type UpstreamConfig struct {
	ClientID         string
	ClientSecret     string
	AuthorizationURL string
	TokenURL         string
	RedirectURL      string
	ClientAuthMethod ClientAuthMethod
	PKCE             bool

	DefaultScopes       []string
	ScopeSeparator      string
	AuthorizationParams map[string]string
	TokenParams         map[string]string
	RefreshParams       map[string]string

	TokenExchange TokenExchangeFormat
	AcceptHeader  string
}

type ResponseHook func(body []byte) error

type Option func(*UpstreamHandler)

type UpstreamHandler struct {
	cfg        UpstreamConfig
	httpClient *http.Client
	hook       ResponseHook
}

func WithHTTPClient(c *http.Client) Option {
	return func(h *UpstreamHandler) {
		h.httpClient = c
	}
}

func WithResponseHook(hook ResponseHook) Option {
	return func(h *UpstreamHandler) {
		h.hook = hook
	}
}

func NewUpstream(cfg UpstreamConfig, opts ...Option) *UpstreamHandler {
	h := &UpstreamHandler{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

type ExchangeOption func(*exchangeOptions)

type exchangeOptions struct {
	verifier string
}

func WithPKCEVerifier(verifier string) ExchangeOption {
	return func(o *exchangeOptions) {
		o.verifier = verifier
	}
}

// AuthorizationURL returns only the URL. Callers that need PKCE must use
// AuthorizationURLWithPKCE to obtain the verifier for the token exchange.
func (h *UpstreamHandler) AuthorizationURL(state string, scopes []string) string {
	if h.cfg.PKCE {
		panic("oauth: AuthorizationURL called with PKCE enabled; use AuthorizationURLWithPKCE instead")
	}
	authURL, _ := h.AuthorizationURLWithPKCE(state, scopes)
	return authURL
}

func (h *UpstreamHandler) AuthorizationURLWithPKCE(state string, scopes []string) (string, string) {
	u, err := url.Parse(h.cfg.AuthorizationURL)
	if err != nil {
		u = &url.URL{RawQuery: ""}
	}
	q := u.Query()
	q.Set("client_id", h.cfg.ClientID)
	q.Set("redirect_uri", h.cfg.RedirectURL)
	q.Set("response_type", "code")
	q.Set("state", state)
	effective := scopes
	if len(effective) == 0 {
		effective = h.cfg.DefaultScopes
	}
	if len(effective) > 0 {
		sep := " "
		if h.cfg.ScopeSeparator != "" {
			sep = h.cfg.ScopeSeparator
		}
		q.Set("scope", strings.Join(effective, sep))
	}

	for k, v := range h.cfg.AuthorizationParams {
		q.Set(k, v)
	}

	var verifier string
	if h.cfg.PKCE {
		verifier = GenerateVerifier()
		q.Set("code_challenge", ComputeS256Challenge(verifier))
		q.Set("code_challenge_method", "S256")
	}

	u.RawQuery = q.Encode()
	return u.String(), verifier
}

func (h *UpstreamHandler) ExchangeCode(ctx context.Context, code string, opts ...ExchangeOption) (*core.TokenResponse, error) {
	var eo exchangeOptions
	for _, opt := range opts {
		opt(&eo)
	}

	data := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {h.cfg.RedirectURL},
	}

	if h.cfg.ClientAuthMethod == ClientAuthBody {
		data.Set("client_id", h.cfg.ClientID)
		data.Set("client_secret", h.cfg.ClientSecret)
	}

	if eo.verifier != "" {
		data.Set("code_verifier", eo.verifier)
	}

	for k, v := range h.cfg.TokenParams {
		data.Set(k, v)
	}

	return h.tokenRequest(ctx, data)
}

func (h *UpstreamHandler) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}

	if h.cfg.ClientAuthMethod == ClientAuthBody {
		data.Set("client_id", h.cfg.ClientID)
		data.Set("client_secret", h.cfg.ClientSecret)
	}

	for k, v := range h.cfg.RefreshParams {
		data.Set(k, v)
	}

	return h.tokenRequest(ctx, data)
}

func (h *UpstreamHandler) tokenRequest(ctx context.Context, data url.Values) (*core.TokenResponse, error) {
	var reader io.Reader
	var contentType string

	if h.cfg.TokenExchange == TokenExchangeJSON {
		m := make(map[string]string, len(data))
		for k, vs := range data {
			if len(vs) > 0 {
				m[k] = vs[0]
			}
		}
		b, err := json.Marshal(m)
		if err != nil {
			return nil, fmt.Errorf("encoding token request as JSON: %w", err)
		}
		reader = bytes.NewReader(b)
		contentType = "application/json"
	} else {
		reader = strings.NewReader(data.Encode())
		contentType = "application/x-www-form-urlencoded"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.cfg.TokenURL, reader)
	if err != nil {
		return nil, fmt.Errorf("creating token request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)

	if h.cfg.AcceptHeader != "" {
		req.Header.Set("Accept", h.cfg.AcceptHeader)
	}

	if h.cfg.ClientAuthMethod == ClientAuthHeader {
		// RFC 6749 §2.3.1: URL-encode client credentials before base64.
		req.SetBasicAuth(url.QueryEscape(h.cfg.ClientID), url.QueryEscape(h.cfg.ClientSecret))
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading token response: %w", err)
	}

	if h.hook != nil {
		if err := h.hook(body); err != nil {
			return nil, err
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, body)
	}

	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, fmt.Errorf("decoding token response: %w", err)
	}
	if tok.AccessToken == "" {
		return nil, fmt.Errorf("token response missing access_token")
	}

	return &core.TokenResponse{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresIn:    tok.ExpiresIn,
		TokenType:    tok.TokenType,
	}, nil
}

func GenerateVerifier() string {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"
	const length = 43
	max := big.NewInt(int64(len(charset)))
	b := make([]byte, length)
	for i := range b {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic(fmt.Sprintf("crypto/rand failed: %v", err))
		}
		b[i] = charset[n.Int64()]
	}
	return string(b)
}

func ComputeS256Challenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
