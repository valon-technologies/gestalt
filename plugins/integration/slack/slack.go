package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/internal/oauth"
)

const defaultTimeout = 10 * time.Second

type Config struct {
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	RedirectURL  string `yaml:"redirect_url"`
	BaseURL      string `yaml:"base_url"`
}

type Integration struct {
	upstream *oauth.UpstreamHandler
	client   *http.Client
	baseURL  string
}

func New(cfg Config) *Integration {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://slack.com"
	}

	client := &http.Client{Timeout: defaultTimeout}

	upstream := oauth.NewUpstream(
		oauth.UpstreamConfig{
			ClientID:         cfg.ClientID,
			ClientSecret:     cfg.ClientSecret,
			AuthorizationURL: baseURL + "/oauth/v2/authorize",
			TokenURL:         baseURL + "/api/oauth.v2.access",
			RedirectURL:      cfg.RedirectURL,
		},
		oauth.WithHTTPClient(client),
		oauth.WithResponseHook(slackResponseHook),
	)

	return &Integration{
		upstream: upstream,
		client:   client,
		baseURL:  baseURL,
	}
}

func (s *Integration) Name() string {
	return "slack"
}

func (s *Integration) DisplayName() string {
	return "Slack"
}

func (s *Integration) Description() string {
	return "Slack workspace integration for messaging, channels, and user management"
}

func (s *Integration) AuthorizationURL(state string, scopes []string) string {
	return s.upstream.AuthorizationURL(state, scopes)
}

func (s *Integration) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	return s.upstream.ExchangeCode(ctx, code)
}

func (s *Integration) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	return s.upstream.RefreshToken(ctx, refreshToken)
}

func (s *Integration) ListOperations() []core.Operation {
	return []core.Operation{
		{
			Name:        "list_channels",
			Description: "List conversations the bot is a member of",
			Method:      "GET",
			Parameters: []core.Parameter{
				{Name: "limit", Type: "integer", Description: "Maximum number of channels to return", Default: 100},
				{Name: "cursor", Type: "string", Description: "Pagination cursor"},
			},
		},
		{
			Name:        "send_message",
			Description: "Send a message to a channel",
			Method:      "POST",
			Parameters: []core.Parameter{
				{Name: "channel", Type: "string", Description: "Channel ID to send the message to", Required: true},
				{Name: "text", Type: "string", Description: "Message text", Required: true},
			},
		},
		{
			Name:        "list_users",
			Description: "List all users in the workspace",
			Method:      "GET",
			Parameters: []core.Parameter{
				{Name: "limit", Type: "integer", Description: "Maximum number of users to return", Default: 100},
				{Name: "cursor", Type: "string", Description: "Pagination cursor"},
			},
		},
		{
			Name:        "search_messages",
			Description: "Search for messages matching a query",
			Method:      "GET",
			Parameters: []core.Parameter{
				{Name: "query", Type: "string", Description: "Search query", Required: true},
				{Name: "count", Type: "integer", Description: "Number of results per page", Default: 20},
			},
		},
		{
			Name:        "read_channel",
			Description: "Fetch message history from a channel",
			Method:      "GET",
			Parameters: []core.Parameter{
				{Name: "channel", Type: "string", Description: "Channel ID to read history from", Required: true},
				{Name: "limit", Type: "integer", Description: "Maximum number of messages to return", Default: 100},
			},
		},
	}
}

var operationEndpoints = map[string]struct {
	method string
	path   string
}{
	"list_channels":   {method: http.MethodGet, path: "/api/conversations.list"},
	"send_message":    {method: http.MethodPost, path: "/api/chat.postMessage"},
	"list_users":      {method: http.MethodGet, path: "/api/users.list"},
	"search_messages": {method: http.MethodGet, path: "/api/search.messages"},
	"read_channel":    {method: http.MethodGet, path: "/api/conversations.history"},
}

func (s *Integration) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	ep, ok := operationEndpoints[operation]
	if !ok {
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}

	url := s.baseURL + ep.path

	var req *http.Request
	var err error

	if ep.method == http.MethodPost {
		body, marshalErr := json.Marshal(params)
		if marshalErr != nil {
			return nil, fmt.Errorf("marshaling request body: %w", marshalErr)
		}
		req, err = http.NewRequestWithContext(ctx, ep.method, url, strings.NewReader(string(body)))
		if err != nil {
			return nil, fmt.Errorf("creating request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	} else {
		req, err = http.NewRequestWithContext(ctx, ep.method, url, nil)
		if err != nil {
			return nil, fmt.Errorf("creating request: %w", err)
		}
		q := req.URL.Query()
		for k, v := range params {
			q.Set(k, fmt.Sprintf("%v", v))
		}
		req.URL.RawQuery = q.Encode()
	}

	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing %s: %w", operation, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response for %s: %w", operation, err)
	}

	var slackResp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(respBody, &slackResp); err != nil {
		return nil, fmt.Errorf("decoding %s response: %w", operation, err)
	}
	if !slackResp.OK {
		return nil, fmt.Errorf("slack %s: %s", operation, slackResp.Error)
	}

	return &core.OperationResult{
		Status: http.StatusOK,
		Body:   string(respBody),
	}, nil
}

func slackResponseHook(body []byte) error {
	var resp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("decoding slack response: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("slack oauth: %s", resp.Error)
	}
	return nil
}
