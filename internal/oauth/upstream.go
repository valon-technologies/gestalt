package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/valon-technologies/toolshed/core"
)

const defaultTimeout = 10 * time.Second

type UpstreamConfig struct {
	ClientID         string
	ClientSecret     string
	AuthorizationURL string
	TokenURL         string
	RedirectURL      string
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

func (h *UpstreamHandler) AuthorizationURL(state string, scopes []string) string {
	v := url.Values{
		"client_id":     {h.cfg.ClientID},
		"redirect_uri":  {h.cfg.RedirectURL},
		"response_type": {"code"},
		"state":         {state},
	}
	if len(scopes) > 0 {
		v.Set("scope", strings.Join(scopes, " "))
	}
	return h.cfg.AuthorizationURL + "?" + v.Encode()
}

func (h *UpstreamHandler) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {h.cfg.ClientID},
		"client_secret": {h.cfg.ClientSecret},
		"redirect_uri":  {h.cfg.RedirectURL},
	}
	return h.tokenRequest(ctx, data)
}

func (h *UpstreamHandler) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {h.cfg.ClientID},
		"client_secret": {h.cfg.ClientSecret},
	}
	return h.tokenRequest(ctx, data)
}

func (h *UpstreamHandler) tokenRequest(ctx context.Context, data url.Values) (*core.TokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.cfg.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("creating token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

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
