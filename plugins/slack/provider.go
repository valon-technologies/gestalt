package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	pluginsdk "github.com/valon-technologies/gestalt/sdk/pluginsdk"
)

const (
	defaultBaseURL = "https://slack.com"

	opListChannels    = "slack_list_channels"
	opSendMessage     = "slack_send_message"
	opListUsers       = "slack_list_users"
	opReadHistory     = "slack_read_history"
	opSearchMessages  = "slack_search_messages"

	methodConversationsList    = "conversations.list"
	methodChatPostMessage      = "chat.postMessage"
	methodUsersList            = "users.list"
	methodConversationsHistory = "conversations.history"
	methodSearchMessages       = "search.messages"

	contentTypeJSON = "application/json"
)

type slackProvider struct {
	httpClient *http.Client
	baseURL    string
}

func (p *slackProvider) Name() string        { return "slack" }
func (p *slackProvider) DisplayName() string { return "Slack" }
func (p *slackProvider) Description() string {
	return "Send messages, search conversations, and manage channels."
}
func (p *slackProvider) ConnectionMode() pluginsdk.ConnectionMode {
	return pluginsdk.ConnectionModeUser
}

func (p *slackProvider) ListOperations() []pluginsdk.Operation {
	return []pluginsdk.Operation{
		{
			Name:        opListChannels,
			Description: "List conversations/channels",
			Method:      http.MethodGet,
			Parameters: []pluginsdk.Parameter{
				{Name: "limit", Type: "int", Description: "Maximum results to return"},
				{Name: "cursor", Type: "string", Description: "Pagination cursor"},
			},
		},
		{
			Name:        opSendMessage,
			Description: "Send a message to a channel",
			Method:      http.MethodPost,
			Parameters: []pluginsdk.Parameter{
				{Name: "channel", Type: "string", Description: "Channel ID", Required: true},
				{Name: "text", Type: "string", Description: "Message text", Required: true},
			},
		},
		{
			Name:        opListUsers,
			Description: "List workspace users",
			Method:      http.MethodGet,
			Parameters: []pluginsdk.Parameter{
				{Name: "limit", Type: "int", Description: "Maximum results to return"},
				{Name: "cursor", Type: "string", Description: "Pagination cursor"},
			},
		},
		{
			Name:        opReadHistory,
			Description: "Read message history for a channel",
			Method:      http.MethodGet,
			Parameters: []pluginsdk.Parameter{
				{Name: "channel", Type: "string", Description: "Channel ID", Required: true},
				{Name: "limit", Type: "int", Description: "Maximum results to return"},
				{Name: "cursor", Type: "string", Description: "Pagination cursor"},
			},
		},
		{
			Name:        opSearchMessages,
			Description: "Search messages across the workspace",
			Method:      http.MethodGet,
			Parameters: []pluginsdk.Parameter{
				{Name: "query", Type: "string", Description: "Search query", Required: true},
				{Name: "count", Type: "int", Description: "Number of results to return"},
			},
		},
	}
}

func (p *slackProvider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*pluginsdk.OperationResult, error) {
	switch operation {
	case opListChannels:
		return p.listChannels(ctx, params, token)
	case opSendMessage:
		return p.sendMessage(ctx, params, token)
	case opListUsers:
		return p.listUsers(ctx, params, token)
	case opReadHistory:
		return p.readHistory(ctx, params, token)
	case opSearchMessages:
		return p.searchMessages(ctx, params, token)
	default:
		return &pluginsdk.OperationResult{Status: http.StatusNotFound, Body: `{"error":"unknown operation"}`}, nil
	}
}

func (p *slackProvider) listChannels(ctx context.Context, params map[string]any, token string) (*pluginsdk.OperationResult, error) {
	q := make(map[string]string)
	if v := intParam(params, "limit"); v > 0 {
		q["limit"] = strconv.Itoa(v)
	}
	if v := stringParam(params, "cursor"); v != "" {
		q["cursor"] = v
	}
	return p.doGet(ctx, methodConversationsList, q, token)
}

func (p *slackProvider) sendMessage(ctx context.Context, params map[string]any, token string) (*pluginsdk.OperationResult, error) {
	body := map[string]string{
		"channel": stringParam(params, "channel"),
		"text":    stringParam(params, "text"),
	}
	return p.doPost(ctx, methodChatPostMessage, body, token)
}

func (p *slackProvider) listUsers(ctx context.Context, params map[string]any, token string) (*pluginsdk.OperationResult, error) {
	q := make(map[string]string)
	if v := intParam(params, "limit"); v > 0 {
		q["limit"] = strconv.Itoa(v)
	}
	if v := stringParam(params, "cursor"); v != "" {
		q["cursor"] = v
	}
	return p.doGet(ctx, methodUsersList, q, token)
}

func (p *slackProvider) readHistory(ctx context.Context, params map[string]any, token string) (*pluginsdk.OperationResult, error) {
	q := map[string]string{
		"channel": stringParam(params, "channel"),
	}
	if v := intParam(params, "limit"); v > 0 {
		q["limit"] = strconv.Itoa(v)
	}
	if v := stringParam(params, "cursor"); v != "" {
		q["cursor"] = v
	}
	return p.doGet(ctx, methodConversationsHistory, q, token)
}

func (p *slackProvider) searchMessages(ctx context.Context, params map[string]any, token string) (*pluginsdk.OperationResult, error) {
	q := map[string]string{
		"query": stringParam(params, "query"),
	}
	if v := intParam(params, "count"); v > 0 {
		q["count"] = strconv.Itoa(v)
	}
	return p.doGet(ctx, methodSearchMessages, q, token)
}

func (p *slackProvider) doGet(ctx context.Context, method string, queryParams map[string]string, token string) (*pluginsdk.OperationResult, error) {
	url := p.apiURL(method)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	q := req.URL.Query()
	for k, v := range queryParams {
		q.Set(k, v)
	}
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Authorization", "Bearer "+token)
	return p.do(req)
}

func (p *slackProvider) doPost(ctx context.Context, method string, body any, token string) (*pluginsdk.OperationResult, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshalling body: %w", err)
	}
	url := p.apiURL(method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", contentTypeJSON)
	return p.do(req)
}

const maxResponseBytes = 10 << 20 // 10 MB

func (p *slackProvider) do(req *http.Request) (*pluginsdk.OperationResult, error) {
	client := p.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	return &pluginsdk.OperationResult{Status: resp.StatusCode, Body: string(data)}, nil
}

func (p *slackProvider) apiURL(method string) string {
	base := p.baseURL
	if base == "" {
		base = defaultBaseURL
	}
	return base + "/api/" + method
}

func stringParam(params map[string]any, key string) string {
	v, _ := params[key].(string)
	return v
}

func intParam(params map[string]any, key string) int {
	switch v := params[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	}
	return 0
}
