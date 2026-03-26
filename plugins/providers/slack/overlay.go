package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
)

const (
	opFindUserMentions      = "find_user_mentions"
	opGetThreadParticipants = "get_thread_participants"
	opCreateCanvas          = "create_canvas"

	overlayProviderName = "slack"
	overlayDisplayName  = "Slack"
	overlayDescription  = "Slack overlay operations"

	slackAPIBaseURL = "https://slack.com/api"

	defaultHistoryLimit = 100
	maxThreadReplies    = 1000
)

var userMentionPattern = regexp.MustCompile(`<@([UW][A-Z0-9]+)>`)

type OverlayProvider struct {
	httpClient *http.Client
}

var (
	_ core.Provider        = (*OverlayProvider)(nil)
	_ core.CatalogProvider = (*OverlayProvider)(nil)
)

func NewOverlayProvider() *OverlayProvider {
	return &OverlayProvider{
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *OverlayProvider) Name() string                        { return overlayProviderName }
func (p *OverlayProvider) DisplayName() string                 { return overlayDisplayName }
func (p *OverlayProvider) Description() string                 { return overlayDescription }
func (p *OverlayProvider) ConnectionMode() core.ConnectionMode { return core.ConnectionModeUser }

func (p *OverlayProvider) ListOperations() []core.Operation {
	return []core.Operation{
		{
			Name:        opFindUserMentions,
			Description: "Find user mentions in channel messages",
			Method:      http.MethodPost,
			Parameters: []core.Parameter{
				{Name: "channel", Type: "string", Required: true, Description: "Channel ID to scan"},
				{Name: "user_id", Type: "string", Description: "Filter to specific user's mentions"},
				{Name: "limit", Type: "integer", Description: "Number of messages to scan"},
				{Name: "oldest", Type: "string", Description: "Unix timestamp, only messages after this"},
				{Name: "latest", Type: "string", Description: "Unix timestamp, only messages before this"},
				{Name: "include_bots", Type: "boolean", Description: "Include bot mentions"},
			},
		},
		{
			Name:        opGetThreadParticipants,
			Description: "Get unique participants in a thread",
			Method:      http.MethodPost,
			Parameters: []core.Parameter{
				{Name: "channel", Type: "string", Required: true, Description: "Channel ID containing the thread"},
				{Name: "ts", Type: "string", Required: true, Description: "Timestamp of the parent message"},
				{Name: "include_user_info", Type: "boolean", Description: "Fetch full user profiles"},
				{Name: "include_bots", Type: "boolean", Description: "Include bot users"},
			},
		},
		{
			Name:        opCreateCanvas,
			Description: "Create a new Slack canvas with a title and optional markdown content",
			Method:      http.MethodPost,
			Parameters: []core.Parameter{
				{Name: "title", Type: "string", Required: true, Description: "Canvas title"},
				{Name: "document_content", Type: "string", Description: "Markdown content for the canvas"},
			},
		},
	}
}

func (p *OverlayProvider) Catalog() *catalog.Catalog {
	ops := p.ListOperations()
	catOps := make([]catalog.CatalogOperation, len(ops))
	for i, op := range ops {
		params := make([]catalog.CatalogParameter, len(op.Parameters))
		for j, param := range op.Parameters {
			params[j] = catalog.CatalogParameter{
				Name:        param.Name,
				Type:        param.Type,
				Description: param.Description,
				Required:    param.Required,
			}
		}
		catOps[i] = catalog.CatalogOperation{
			ID:          op.Name,
			Method:      op.Method,
			Path:        "/",
			Description: op.Description,
			Parameters:  params,
		}
	}
	return &catalog.Catalog{
		Name:        overlayProviderName,
		DisplayName: overlayDisplayName,
		Description: overlayDescription,
		Operations:  catOps,
	}
}

func (p *OverlayProvider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	switch operation {
	case opFindUserMentions:
		return p.findUserMentions(ctx, params, token)
	case opGetThreadParticipants:
		return p.getThreadParticipants(ctx, params, token)
	case opCreateCanvas:
		return p.createCanvas(ctx, params, token)
	default:
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}
}

func (p *OverlayProvider) findUserMentions(ctx context.Context, params map[string]any, token string) (*core.OperationResult, error) {
	channel, _ := params["channel"].(string)
	if channel == "" {
		return nil, fmt.Errorf("channel is required")
	}

	limit := intParamOr(params, "limit", defaultHistoryLimit)
	filterUserID, _ := params["user_id"].(string)
	includeBots, _ := params["include_bots"].(bool)

	historyParams := map[string]any{
		"channel": channel,
		"limit":   limit,
	}
	if v, ok := params["oldest"]; ok {
		historyParams["oldest"] = v
	}
	if v, ok := params["latest"]; ok {
		historyParams["latest"] = v
	}

	body, err := p.slackPost(ctx, "/conversations.history", historyParams, token)
	if err != nil {
		return nil, fmt.Errorf("fetching channel history: %w", err)
	}

	messages, _ := body["messages"].([]any)

	type mention struct {
		UserID      string `json:"user_id"`
		MessageTS   string `json:"message_ts"`
		MentionedBy string `json:"mentioned_by"`
		Text        string `json:"text"`
		Channel     string `json:"channel"`
	}

	var mentions []mention
	mentionedUserIDs := map[string]struct{}{}

	for _, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if !includeBots {
			if _, hasBotID := msg["bot_id"]; hasBotID {
				continue
			}
		}
		text, _ := msg["text"].(string)
		found := userMentionPattern.FindAllStringSubmatch(text, -1)
		for _, match := range found {
			userID := match[1]
			if filterUserID != "" && userID != filterUserID {
				continue
			}
			mentionedUserIDs[userID] = struct{}{}
			ts, _ := msg["ts"].(string)
			by, _ := msg["user"].(string)
			mentions = append(mentions, mention{
				UserID:      userID,
				MessageTS:   ts,
				MentionedBy: by,
				Text:        text,
				Channel:     channel,
			})
		}
	}

	ids := make([]string, 0, len(mentionedUserIDs))
	for id := range mentionedUserIDs {
		ids = append(ids, id)
	}

	return jsonResult(map[string]any{
		"mentions":           mentions,
		"mentioned_user_ids": ids,
		"total_mentions":     len(mentions),
		"messages_scanned":   len(messages),
	})
}

func (p *OverlayProvider) getThreadParticipants(ctx context.Context, params map[string]any, token string) (*core.OperationResult, error) {
	channel, _ := params["channel"].(string)
	if channel == "" {
		return nil, fmt.Errorf("channel is required")
	}
	ts, _ := params["ts"].(string)
	if ts == "" {
		return nil, fmt.Errorf("ts is required")
	}

	includeBots := true
	if v, ok := params["include_bots"].(bool); ok {
		includeBots = v
	}
	includeUserInfo, _ := params["include_user_info"].(bool)

	body, err := p.slackPost(ctx, "/conversations.replies", map[string]any{
		"channel": channel,
		"ts":      ts,
		"limit":   maxThreadReplies,
	}, token)
	if err != nil {
		return nil, fmt.Errorf("fetching thread replies: %w", err)
	}

	messages, _ := body["messages"].([]any)

	type participant struct {
		UserID          string `json:"user_id"`
		MessageCount    int    `json:"message_count"`
		FirstReplyTS    string `json:"first_reply_ts"`
		IsThreadStarter bool   `json:"is_thread_starter"`
		DisplayName     string `json:"display_name,omitempty"`
		RealName        string `json:"real_name,omitempty"`
		IsBot           bool   `json:"is_bot,omitempty"`
	}

	var threadStarter string
	if len(messages) > 0 {
		if first, ok := messages[0].(map[string]any); ok {
			threadStarter, _ = first["user"].(string)
		}
	}

	participantMap := map[string]*participant{}

	for _, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		userID, _ := msg["user"].(string)
		if userID == "" {
			continue
		}
		if !includeBots {
			if _, hasBotID := msg["bot_id"]; hasBotID {
				continue
			}
		}
		if _, exists := participantMap[userID]; !exists {
			msgTS, _ := msg["ts"].(string)
			participantMap[userID] = &participant{
				UserID:          userID,
				FirstReplyTS:    msgTS,
				IsThreadStarter: userID == threadStarter,
			}
		}
		participantMap[userID].MessageCount++
	}

	if includeUserInfo {
		for userID, pt := range participantMap {
			userBody, err := p.slackPost(ctx, "/users.info", map[string]any{"user": userID}, token)
			if err != nil {
				continue
			}
			user, _ := userBody["user"].(map[string]any)
			if user == nil {
				continue
			}
			profile, _ := user["profile"].(map[string]any)
			if profile != nil {
				pt.DisplayName, _ = profile["display_name"].(string)
			}
			pt.RealName, _ = user["real_name"].(string)
			isBot, _ := user["is_bot"].(bool)
			pt.IsBot = isBot
		}
	}

	participants := make([]*participant, 0, len(participantMap))
	for _, p := range participantMap {
		participants = append(participants, p)
	}

	totalReplies := len(messages) - 1
	if totalReplies < 0 {
		totalReplies = 0
	}

	return jsonResult(map[string]any{
		"participants":      participants,
		"participant_count": len(participants),
		"total_replies":     totalReplies,
	})
}

func (p *OverlayProvider) createCanvas(ctx context.Context, params map[string]any, token string) (*core.OperationResult, error) {
	title, _ := params["title"].(string)
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}

	payload := map[string]any{"title": title}
	if content, ok := params["document_content"].(string); ok && content != "" {
		payload["document_content"] = map[string]any{
			"type":     "markdown",
			"markdown": content,
		}
	}

	body, err := p.slackPost(ctx, "/canvases.create", payload, token)
	if err != nil {
		return nil, fmt.Errorf("creating canvas: %w", err)
	}

	return jsonResult(map[string]any{"canvas": body})
}

func (p *OverlayProvider) slackPost(ctx context.Context, path string, params map[string]any, token string) (map[string]any, error) {
	payload, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return p.doSlackRequest(ctx, path, payload, token)
}

const slackContentType = "application/json; charset=utf-8"

func (p *OverlayProvider) doSlackRequest(ctx context.Context, path string, body []byte, token string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, slackAPIBaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", slackContentType)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	ok, _ := result["ok"].(bool)
	if !ok {
		errMsg, _ := result["error"].(string)
		if errMsg == "" {
			errMsg = "unknown error"
		}
		return nil, fmt.Errorf("slack API error: %s", errMsg)
	}

	return result, nil
}

func jsonResult(data map[string]any) (*core.OperationResult, error) {
	body, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("marshaling result: %w", err)
	}
	return &core.OperationResult{
		Status: http.StatusOK,
		Body:   string(body),
	}, nil
}

func intParamOr(params map[string]any, key string, defaultVal int) int {
	v, ok := params[key]
	if !ok {
		return defaultVal
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return defaultVal
	}
}
