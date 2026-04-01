package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"regexp"
	"sort"
	"strconv"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

const slackAPIBaseURL = "https://slack.com/api"

var (
	slackMessageURLPattern = regexp.MustCompile(`https?://[^/]+\.slack\.com/archives/([A-Z0-9]+)/p(\d{10})(\d+)`)
	userMentionPattern     = regexp.MustCompile(`<@(U[A-Z0-9]+)>`)
)

type Provider struct {
	httpClient *http.Client
	baseURL    string
}

type mention struct {
	UserID      string `json:"user_id"`
	MessageTS   string `json:"message_ts"`
	MentionedBy string `json:"mentioned_by"`
	Text        string `json:"text"`
	Channel     string `json:"channel"`
}

type threadParticipant struct {
	UserID          string `json:"user_id"`
	MessageCount    int    `json:"message_count"`
	FirstReplyTS    string `json:"first_reply_ts"`
	IsThreadStarter bool   `json:"is_thread_starter"`
	DisplayName     string `json:"display_name,omitempty"`
	RealName        string `json:"real_name,omitempty"`
	IsBot           *bool  `json:"is_bot,omitempty"`
}

var _ gestalt.Provider = (*Provider)(nil)

func NewProvider() *Provider {
	return &Provider{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    slackAPIBaseURL,
	}
}

func (p *Provider) Name() string        { return "slack" }
func (p *Provider) DisplayName() string { return "Slack" }
func (p *Provider) Description() string {
	return "Slack helper operations for message lookup, mention scans, and thread participants."
}
func (p *Provider) ConnectionMode() gestalt.ConnectionMode { return gestalt.ConnectionModeUser }

func (p *Provider) Catalog() *gestalt.Catalog {
	return &gestalt.Catalog{
		Name:        p.Name(),
		DisplayName: p.DisplayName(),
		Description: p.Description(),
		Operations: []gestalt.CatalogOperation{
			{
				ID:          "conversations.getMessage",
				Description: "Fetch a single message by Slack URL or channel and timestamp",
				Method:      http.MethodPost,
				Parameters: []gestalt.CatalogParameter{
					{Name: "url", Type: "string", Description: "Slack message URL"},
					{Name: "channel", Type: "string", Description: "Channel ID"},
					{Name: "ts", Type: "string", Description: "Message timestamp"},
				},
			},
			{
				ID:          "conversations.findUserMentions",
				Description: "Find Slack user mentions in channel messages",
				Method:      http.MethodPost,
				Parameters: []gestalt.CatalogParameter{
					{Name: "channel", Type: "string", Required: true, Description: "Channel ID to scan"},
					{Name: "user_id", Type: "string", Description: "Optional user ID to filter mentions to"},
					{Name: "limit", Type: "int", Description: "Number of messages to scan"},
					{Name: "oldest", Type: "string", Description: "Only include messages after this Unix timestamp"},
					{Name: "latest", Type: "string", Description: "Only include messages before this Unix timestamp"},
					{Name: "include_bots", Type: "bool", Description: "Include bot messages in the scan"},
				},
			},
			{
				ID:          "conversations.getThreadParticipants",
				Description: "Get unique participants in a Slack thread",
				Method:      http.MethodPost,
				Parameters: []gestalt.CatalogParameter{
					{Name: "channel", Type: "string", Required: true, Description: "Channel ID containing the thread"},
					{Name: "ts", Type: "string", Required: true, Description: "Parent message timestamp"},
					{Name: "include_user_info", Type: "bool", Description: "Fetch user profile details for participants"},
					{Name: "include_bots", Type: "bool", Description: "Include bot users in the participant list"},
				},
			},
		},
	}
}

func (p *Provider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*gestalt.OperationResult, error) {
	if token == "" {
		return nil, fmt.Errorf("token is required")
	}

	switch operation {
	case "conversations.getMessage":
		return p.getMessage(ctx, params, token)
	case "conversations.findUserMentions":
		return p.findUserMentions(ctx, params, token)
	case "conversations.getThreadParticipants":
		return p.getThreadParticipants(ctx, params, token)
	default:
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}
}

func (p *Provider) getMessage(ctx context.Context, params map[string]any, token string) (*gestalt.OperationResult, error) {
	channel := stringParam(params, "channel")
	ts := stringParam(params, "ts")

	if messageURL := stringParam(params, "url"); messageURL != "" {
		match := slackMessageURLPattern.FindStringSubmatch(messageURL)
		if match == nil {
			return nil, fmt.Errorf("invalid Slack message URL: %s", messageURL)
		}
		channel = match[1]
		ts = match[2] + "." + match[3]
	}

	if channel == "" || ts == "" {
		return nil, fmt.Errorf("either url or both channel and ts are required")
	}

	query := neturl.Values{
		"channel":   []string{channel},
		"oldest":    []string{ts},
		"latest":    []string{ts},
		"inclusive": []string{"true"},
		"limit":     []string{"1"},
	}
	data, err := p.slackGET(ctx, "/conversations.history", query, token)
	if err != nil {
		return nil, err
	}

	messages := mapSlice(data["messages"])
	if len(messages) == 0 {
		return nil, fmt.Errorf("no message found at timestamp %s", ts)
	}

	return jsonResult(map[string]any{
		"data": map[string]any{
			"message": messages[0],
		},
	})
}

func (p *Provider) findUserMentions(ctx context.Context, params map[string]any, token string) (*gestalt.OperationResult, error) {
	channel := stringParam(params, "channel")
	if channel == "" {
		return nil, fmt.Errorf("channel is required")
	}

	query := neturl.Values{
		"channel": []string{channel},
		"limit":   []string{strconv.Itoa(intParamOr(params, "limit", 100))},
	}
	if oldest := stringParam(params, "oldest"); oldest != "" {
		query.Set("oldest", oldest)
	}
	if latest := stringParam(params, "latest"); latest != "" {
		query.Set("latest", latest)
	}

	data, err := p.slackGET(ctx, "/conversations.history", query, token)
	if err != nil {
		return nil, err
	}

	includeBots := boolParamOr(params, "include_bots", false)
	filterUserID := stringParam(params, "user_id")
	messages := mapSlice(data["messages"])
	mentions := make([]mention, 0)
	mentionedUserIDs := make(map[string]struct{})

	for _, message := range messages {
		if !includeBots && stringField(message, "bot_id") != "" {
			continue
		}

		text := stringField(message, "text")
		for _, match := range userMentionPattern.FindAllStringSubmatch(text, -1) {
			userID := match[1]
			if filterUserID != "" && userID != filterUserID {
				continue
			}
			mentionedUserIDs[userID] = struct{}{}
			mentions = append(mentions, mention{
				UserID:      userID,
				MessageTS:   stringField(message, "ts"),
				MentionedBy: stringField(message, "user"),
				Text:        text,
				Channel:     channel,
			})
		}
	}

	userIDs := make([]string, 0, len(mentionedUserIDs))
	for userID := range mentionedUserIDs {
		userIDs = append(userIDs, userID)
	}
	sort.Strings(userIDs)

	return jsonResult(map[string]any{
		"data": map[string]any{
			"mentions":           mentions,
			"mentioned_user_ids": userIDs,
			"total_mentions":     len(mentions),
			"messages_scanned":   len(messages),
		},
	})
}

func (p *Provider) getThreadParticipants(ctx context.Context, params map[string]any, token string) (*gestalt.OperationResult, error) {
	channel := stringParam(params, "channel")
	if channel == "" {
		return nil, fmt.Errorf("channel is required")
	}
	ts := stringParam(params, "ts")
	if ts == "" {
		return nil, fmt.Errorf("ts is required")
	}

	query := neturl.Values{
		"channel": []string{channel},
		"ts":      []string{ts},
		"limit":   []string{"1000"},
	}
	data, err := p.slackGET(ctx, "/conversations.replies", query, token)
	if err != nil {
		return nil, err
	}

	messages := mapSlice(data["messages"])
	includeBots := boolParamOr(params, "include_bots", true)
	includeUserInfo := boolParamOr(params, "include_user_info", false)
	threadStarter := ""
	if len(messages) > 0 {
		threadStarter = stringField(messages[0], "user")
	}

	participantsByUser := make(map[string]*threadParticipant)
	for _, message := range messages {
		userID := stringField(message, "user")
		if userID == "" {
			continue
		}
		if !includeBots && stringField(message, "bot_id") != "" {
			continue
		}

		participant := participantsByUser[userID]
		if participant == nil {
			participant = &threadParticipant{
				UserID:          userID,
				MessageCount:    0,
				FirstReplyTS:    stringField(message, "ts"),
				IsThreadStarter: userID == threadStarter,
			}
			participantsByUser[userID] = participant
		}
		participant.MessageCount++
	}

	participants := make([]threadParticipant, 0, len(participantsByUser))
	for _, participant := range participantsByUser {
		if includeUserInfo {
			userData, err := p.slackGET(ctx, "/users.info", neturl.Values{
				"user": []string{participant.UserID},
			}, token)
			if err == nil {
				user := mapField(userData, "user")
				profile := mapField(user, "profile")
				participant.DisplayName = stringField(profile, "display_name")
				participant.RealName = stringField(user, "real_name")
				if isBot, ok := boolField(user, "is_bot"); ok {
					participant.IsBot = &isBot
				}
			}
		}
		participants = append(participants, *participant)
	}

	sort.Slice(participants, func(i, j int) bool {
		if participants[i].FirstReplyTS == participants[j].FirstReplyTS {
			return participants[i].UserID < participants[j].UserID
		}
		return participants[i].FirstReplyTS < participants[j].FirstReplyTS
	})

	totalReplies := 0
	if len(messages) > 0 {
		totalReplies = len(messages) - 1
	}

	return jsonResult(map[string]any{
		"data": map[string]any{
			"participants":      participants,
			"participant_count": len(participants),
			"total_replies":     totalReplies,
		},
	})
}

func (p *Provider) slackGET(ctx context.Context, endpoint string, query neturl.Values, token string) (map[string]any, error) {
	return p.slackRequest(ctx, http.MethodGet, endpoint, query, nil, token)
}

func (p *Provider) slackRequest(ctx context.Context, method, endpoint string, query neturl.Values, payload any, token string) (map[string]any, error) {
	requestURL := p.baseURL + endpoint
	if len(query) > 0 {
		requestURL += "?" + query.Encode()
	}

	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal Slack payload: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var data map[string]any
	if err := json.Unmarshal(responseBody, &data); err != nil {
		return nil, fmt.Errorf("decode Slack response: %w", err)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("slack API error (status %d): %s", resp.StatusCode, string(responseBody))
	}
	if ok, exists := boolField(data, "ok"); exists && !ok {
		if slackErr := stringField(data, "error"); slackErr != "" {
			return nil, fmt.Errorf("slack API error: %s", slackErr)
		}
		return nil, fmt.Errorf("slack API error")
	}

	return data, nil
}

func jsonResult(data any) (*gestalt.OperationResult, error) {
	body, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}
	return &gestalt.OperationResult{
		Status: http.StatusOK,
		Body:   string(body),
	}, nil
}

func stringParam(params map[string]any, key string) string {
	switch v := params[key].(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case fmt.Stringer:
		return v.String()
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return ""
	}
}

func intParamOr(params map[string]any, key string, defaultValue int) int {
	switch v := params[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		i, err := v.Int64()
		if err == nil {
			return int(i)
		}
	case string:
		i, err := strconv.Atoi(v)
		if err == nil {
			return i
		}
	}
	return defaultValue
}

func boolParamOr(params map[string]any, key string, defaultValue bool) bool {
	switch v := params[key].(type) {
	case bool:
		return v
	case string:
		b, err := strconv.ParseBool(v)
		if err == nil {
			return b
		}
	}
	return defaultValue
}

func mapSlice(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if object, ok := item.(map[string]any); ok {
			out = append(out, object)
		}
	}
	return out
}

func mapField(data map[string]any, key string) map[string]any {
	if value, ok := data[key].(map[string]any); ok {
		return value
	}
	return nil
}

func stringField(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	value, ok := data[key]
	if !ok {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return ""
	}
}

func boolField(data map[string]any, key string) (bool, bool) {
	if data == nil {
		return false, false
	}
	value, ok := data[key]
	if !ok {
		return false, false
	}
	b, ok := value.(bool)
	return b, ok
}
