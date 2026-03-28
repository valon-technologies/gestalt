package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

const (
	testToken   = "xoxb-test-token-000"
	testChannel = "C00TEST"
	testMessage = "hello from tests"
	testQuery   = "search term"
	testCursor  = "dGVhbTpDMDY="
	testLimit   = 10
	testCount   = 5
)

type requestLog struct {
	method      string
	path        string
	query       map[string]string
	headers     map[string]string
	body        map[string]string
	contentType string
}

func newTestProvider(t *testing.T, handler http.HandlerFunc) *slackProvider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &slackProvider{
		httpClient: srv.Client(),
		baseURL:    srv.URL,
	}
}

func captureRequest(t *testing.T, respond map[string]any) (*requestLog, *slackProvider) {
	t.Helper()
	log := &requestLog{query: map[string]string{}, headers: map[string]string{}}
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		log.method = r.Method
		log.path = r.URL.Path
		log.contentType = r.Header.Get("Content-Type")
		log.headers["Authorization"] = r.Header.Get("Authorization")
		for k, v := range r.URL.Query() {
			log.query[k] = v[0]
		}
		if r.Body != nil {
			data, _ := io.ReadAll(r.Body)
			if len(data) > 0 {
				log.body = map[string]string{}
				json.Unmarshal(data, &log.body)
			}
		}
		json.NewEncoder(w).Encode(respond)
	})
	return log, p
}

func TestListChannels(t *testing.T) {
	t.Parallel()
	log, p := captureRequest(t, map[string]any{
		"ok":       true,
		"channels": []map[string]string{{"id": "C001", "name": "general"}},
	})

	result, err := p.Execute(context.Background(), opListChannels, map[string]any{
		"limit":  float64(testLimit),
		"cursor": testCursor,
	}, testToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if log.method != "GET" {
		t.Errorf("method = %s, want GET", log.method)
	}
	if log.path != "/api/"+methodConversationsList {
		t.Errorf("path = %s, want /api/%s", log.path, methodConversationsList)
	}
	if log.query["limit"] != strconv.Itoa(testLimit) {
		t.Errorf("query limit = %s, want %d", log.query["limit"], testLimit)
	}
	if log.query["cursor"] != testCursor {
		t.Errorf("query cursor = %s, want %s", log.query["cursor"], testCursor)
	}
	if log.headers["Authorization"] != "Bearer "+testToken {
		t.Errorf("auth = %s, want Bearer %s", log.headers["Authorization"], testToken)
	}
	if result.Status != 200 {
		t.Errorf("status = %d, want 200", result.Status)
	}
}

func TestSendMessage(t *testing.T) {
	t.Parallel()
	log, p := captureRequest(t, map[string]any{
		"ok":      true,
		"channel": testChannel,
		"ts":      "1234567890.123456",
	})

	result, err := p.Execute(context.Background(), opSendMessage, map[string]any{
		"channel": testChannel,
		"text":    testMessage,
	}, testToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if log.method != "POST" {
		t.Errorf("method = %s, want POST", log.method)
	}
	if log.path != "/api/"+methodChatPostMessage {
		t.Errorf("path = %s, want /api/%s", log.path, methodChatPostMessage)
	}
	if log.contentType != "application/json" {
		t.Errorf("content-type = %s, want application/json", log.contentType)
	}
	if log.body["channel"] != testChannel {
		t.Errorf("body channel = %s, want %s", log.body["channel"], testChannel)
	}
	if log.body["text"] != testMessage {
		t.Errorf("body text = %s, want %s", log.body["text"], testMessage)
	}
	if result.Status != 200 {
		t.Errorf("status = %d, want 200", result.Status)
	}
}

func TestListUsers(t *testing.T) {
	t.Parallel()
	log, p := captureRequest(t, map[string]any{
		"ok":      true,
		"members": []map[string]string{{"id": "U001", "name": "testuser"}},
	})

	result, err := p.Execute(context.Background(), opListUsers, map[string]any{
		"limit":  float64(testLimit),
		"cursor": testCursor,
	}, testToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if log.method != "GET" {
		t.Errorf("method = %s, want GET", log.method)
	}
	if log.path != "/api/"+methodUsersList {
		t.Errorf("path = %s, want /api/%s", log.path, methodUsersList)
	}
	if log.query["limit"] != strconv.Itoa(testLimit) {
		t.Errorf("query limit = %s, want %d", log.query["limit"], testLimit)
	}
	if log.query["cursor"] != testCursor {
		t.Errorf("query cursor = %s, want %s", log.query["cursor"], testCursor)
	}
	if result.Status != 200 {
		t.Errorf("status = %d, want 200", result.Status)
	}
}

func TestReadHistory(t *testing.T) {
	t.Parallel()
	log, p := captureRequest(t, map[string]any{
		"ok":       true,
		"messages": []map[string]string{{"text": "hey", "ts": "111.222"}},
	})

	result, err := p.Execute(context.Background(), opReadHistory, map[string]any{
		"channel": testChannel,
		"limit":   float64(testLimit),
	}, testToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if log.method != "GET" {
		t.Errorf("method = %s, want GET", log.method)
	}
	if log.path != "/api/"+methodConversationsHistory {
		t.Errorf("path = %s, want /api/%s", log.path, methodConversationsHistory)
	}
	if log.query["channel"] != testChannel {
		t.Errorf("query channel = %s, want %s", log.query["channel"], testChannel)
	}
	if log.query["limit"] != strconv.Itoa(testLimit) {
		t.Errorf("query limit = %s, want %d", log.query["limit"], testLimit)
	}
	if result.Status != 200 {
		t.Errorf("status = %d, want 200", result.Status)
	}
}

func TestSearchMessages(t *testing.T) {
	t.Parallel()
	log, p := captureRequest(t, map[string]any{
		"ok": true,
		"messages": map[string]any{
			"matches": []map[string]string{{"text": "found it"}},
		},
	})

	result, err := p.Execute(context.Background(), opSearchMessages, map[string]any{
		"query": testQuery,
		"count": float64(testCount),
	}, testToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if log.method != "GET" {
		t.Errorf("method = %s, want GET", log.method)
	}
	if log.path != "/api/"+methodSearchMessages {
		t.Errorf("path = %s, want /api/%s", log.path, methodSearchMessages)
	}
	if log.query["query"] != testQuery {
		t.Errorf("query = %s, want %s", log.query["query"], testQuery)
	}
	if log.query["count"] != strconv.Itoa(testCount) {
		t.Errorf("count = %s, want %d", log.query["count"], testCount)
	}
	if result.Status != 200 {
		t.Errorf("status = %d, want 200", result.Status)
	}
}

func TestSlackAPIError(t *testing.T) {
	t.Parallel()
	_, p := captureRequest(t, map[string]any{
		"ok":    false,
		"error": "channel_not_found",
	})

	result, err := p.Execute(context.Background(), opSendMessage, map[string]any{
		"channel": "C00INVALID",
		"text":    testMessage,
	}, testToken)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if result.Status != 200 {
		t.Errorf("status = %d, want 200 (Slack returns 200 for API errors)", result.Status)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(result.Body), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["ok"] != false {
		t.Error("expected ok=false")
	}
	if body["error"] != "channel_not_found" {
		t.Errorf("error = %v, want channel_not_found", body["error"])
	}
}

func TestHTTPErrorPassthrough(t *testing.T) {
	t.Parallel()
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "ratelimited"})
	})

	result, err := p.Execute(context.Background(), opListChannels, map[string]any{}, testToken)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if result.Status != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", result.Status, http.StatusTooManyRequests)
	}
}

func TestUnknownOperation(t *testing.T) {
	t.Parallel()
	p := &slackProvider{}
	result, err := p.Execute(context.Background(), "nonexistent", map[string]any{}, testToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != 404 {
		t.Errorf("status = %d, want 404", result.Status)
	}
}
