package slack

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/provider"
)

//go:embed provider.yaml
var definitionYAML []byte

const (
	slackAuthorizationURL = "https://slack.com/oauth/v2/authorize"
	slackTokenURL         = "https://slack.com/api/oauth.v2.access"
	scopeSeparator        = ","
)

func Factory(ctx context.Context, name string, intg config.IntegrationDef, deps bootstrap.Deps) (core.Provider, error) {
	var def provider.Definition
	if err := yaml.Unmarshal(definitionYAML, &def); err != nil {
		return nil, fmt.Errorf("slack: parsing embedded definition: %w", err)
	}

	auth := newSlackAuthHandler(intg.ClientID, intg.ClientSecret, intg.RedirectURL, def.Auth.Scopes)
	return provider.Build(&def, intg, nil,
		provider.WithAuthHandler(auth),
		provider.WithEgressResolver(deps.Egress.Resolver),
	)
}

type slackAuthHandler struct {
	clientID     string
	clientSecret string
	redirectURL  string
	scopes       []string
	httpClient   *http.Client
}

func newSlackAuthHandler(clientID, clientSecret, redirectURL string, scopes []string) *slackAuthHandler {
	return &slackAuthHandler{
		clientID:     clientID,
		clientSecret: clientSecret,
		redirectURL:  redirectURL,
		scopes:       scopes,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (h *slackAuthHandler) AuthorizationURL(state string, scopes []string) string {
	effective := scopes
	if len(effective) == 0 {
		effective = h.scopes
	}

	params := url.Values{
		"client_id":     {h.clientID},
		"redirect_uri":  {h.redirectURL},
		"response_type": {"code"},
		"state":         {state},
	}
	if len(effective) > 0 {
		params.Set("user_scope", strings.Join(effective, scopeSeparator))
	}

	return slackAuthorizationURL + "?" + params.Encode()
}

func (h *slackAuthHandler) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	return h.tokenRequest(ctx, url.Values{
		"code":          {code},
		"client_id":     {h.clientID},
		"client_secret": {h.clientSecret},
		"redirect_uri":  {h.redirectURL},
	})
}

func (h *slackAuthHandler) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	return h.tokenRequest(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {h.clientID},
		"client_secret": {h.clientSecret},
	})
}

func (h *slackAuthHandler) tokenRequest(ctx context.Context, data url.Values) (*core.TokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, slackTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("creating token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decoding token response: %w", err)
	}

	ok, _ := raw["ok"].(bool)
	if !ok {
		errMsg, _ := raw["error"].(string)
		if errMsg == "" {
			errMsg = "unknown error"
		}
		return nil, fmt.Errorf("slack oauth error: %s", errMsg)
	}

	var accessToken, refreshToken string

	if authedUser, _ := raw["authed_user"].(map[string]any); authedUser != nil {
		accessToken, _ = authedUser["access_token"].(string)
		refreshToken, _ = authedUser["refresh_token"].(string)
	}

	if accessToken == "" {
		accessToken, _ = raw["access_token"].(string)
	}
	if refreshToken == "" {
		refreshToken, _ = raw["refresh_token"].(string)
	}

	if accessToken == "" {
		return nil, fmt.Errorf("slack token response missing access_token")
	}

	return &core.TokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		Extra:        raw,
	}, nil
}
