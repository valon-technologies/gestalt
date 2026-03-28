package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

const (
	testToken     = "xoxb-test-token-000"
	testChannel   = "C00TEST"
	testMessage   = "hello from tests"
	testQuery     = "search term"
	testCursor    = "dGVhbTpDMDY="
	testLimit     = 10
	testCount     = 5
)

func newTestProvider(t *testing.T, handler http.HandlerFunc) (*slackProvider, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &slackProvider{
		httpClient: srv.Client(),
		baseURL:    srv.URL,
	}, srv
}

func TestListChannels(t *testing.T) {
	t.Parallel()
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"ok":       true,
			"channels": []map[string]string{{"id": "C001", "name": "general"}},
		})
	})

	result, err := p.Execute(context.Background(), opListChannels, map[string]any{
		"limit":  float64(testLimit),
		"cursor": testCursor,
	}, testToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != 200 {
		t.Fatalf("expected status 200, got %d", result.Status)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(result.Body), &body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	channels, ok := body["channels"].([]any)
	if !ok || len(channels) == 0 {
		t.Fatal("expected non-empty channels array")
	}
}

func TestSendMessage(t *testing.T) {
	t.Parallel()
	var gotChannel, gotText string
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		var req map[string]string
		json.Unmarshal(data, &req)
		gotChannel = req["channel"]
		gotText = req["text"]
		json.NewEncoder(w).Encode(map[string]any{
			"ok":      true,
			"channel": gotChannel,
			"ts":      "1234567890.123456",
		})
	})

	result, err := p.Execute(context.Background(), opSendMessage, map[string]any{
		"channel": testChannel,
		"text":    testMessage,
	}, testToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != 200 {
		t.Fatalf("expected status 200, got %d", result.Status)
	}
	if gotChannel != testChannel {
		t.Errorf("expected channel %s, got %s", testChannel, gotChannel)
	}
	if gotText != testMessage {
		t.Errorf("expected text %q, got %q", testMessage, gotText)
	}
}

func TestListUsers(t *testing.T) {
	t.Parallel()
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"ok":      true,
			"members": []map[string]string{{"id": "U001", "name": "alice"}},
		})
	})

	result, err := p.Execute(context.Background(), opListUsers, map[string]any{
		"limit": float64(testLimit),
	}, testToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != 200 {
		t.Fatalf("expected status 200, got %d", result.Status)
	}
	var body map[string]any
	json.Unmarshal([]byte(result.Body), &body)
	members, ok := body["members"].([]any)
	if !ok || len(members) == 0 {
		t.Fatal("expected non-empty members array")
	}
}

func TestReadHistory(t *testing.T) {
	t.Parallel()
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"ok":       true,
			"messages": []map[string]string{{"text": "hey", "ts": "111.222"}},
		})
	})

	result, err := p.Execute(context.Background(), opReadHistory, map[string]any{
		"channel": testChannel,
		"limit":   float64(testLimit),
	}, testToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != 200 {
		t.Fatalf("expected status 200, got %d", result.Status)
	}
	var body map[string]any
	json.Unmarshal([]byte(result.Body), &body)
	messages, ok := body["messages"].([]any)
	if !ok || len(messages) == 0 {
		t.Fatal("expected non-empty messages array")
	}
}

func TestSearchMessages(t *testing.T) {
	t.Parallel()
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"messages": map[string]any{
				"matches": []map[string]string{{"text": "found it"}},
			},
		})
	})

	result, err := p.Execute(context.Background(), opSearchMessages, map[string]any{
		"query": testQuery,
		"count": float64(testCount),
	}, testToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != 200 {
		t.Fatalf("expected status 200, got %d", result.Status)
	}
	var body map[string]any
	json.Unmarshal([]byte(result.Body), &body)
	if body["messages"] == nil {
		t.Fatal("expected messages in response")
	}
}

func TestSlackErrorResponse(t *testing.T) {
	t.Parallel()
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"error": "channel_not_found",
		})
	})

	_, err := p.Execute(context.Background(), opSendMessage, map[string]any{
		"channel": "C00INVALID",
		"text":    testMessage,
	}, testToken)
	if err == nil {
		t.Fatal("expected error for Slack API failure")
	}
	expected := "slack API error: channel_not_found"
	if err.Error() != expected {
		t.Errorf("expected error %q, got %q", expected, err.Error())
	}
}

func TestAuthHeaderSent(t *testing.T) {
	t.Parallel()
	var gotAuth string
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "channels": []any{}})
	})

	_, err := p.Execute(context.Background(), opListChannels, map[string]any{}, testToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "Bearer " + testToken
	if gotAuth != expected {
		t.Errorf("expected Authorization %q, got %q", expected, gotAuth)
	}
}
