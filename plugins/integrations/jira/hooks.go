package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/provider"
)

func init() {
	provider.RegisterPostConnectHook("jira_discovery", jiraDiscovery)
}

type accessibleResource struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	URL       string   `json:"url"`
	Scopes    []string `json:"scopes"`
	AvatarURL string   `json:"avatarUrl"`
}

func jiraDiscovery(ctx context.Context, _ *core.IntegrationToken, client *http.Client) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.atlassian.com/oauth/token/accessible-resources", nil)
	if err != nil {
		return nil, fmt.Errorf("building accessible-resources request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching accessible resources: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading accessible-resources response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("accessible-resources returned %d: %s", resp.StatusCode, body)
	}

	var resources []accessibleResource
	if err := json.Unmarshal(body, &resources); err != nil {
		return nil, fmt.Errorf("decoding accessible-resources response: %w", err)
	}

	if len(resources) == 0 {
		return nil, fmt.Errorf("no Jira sites accessible with this token")
	}

	return map[string]string{"cloud_id": resources[0].ID}, nil
}
