package slack

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func newContractTestProvider(fn roundTripFunc) *Provider {
	p := NewProvider()
	p.httpClient.Transport = fn
	return p
}

func jsonHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func decodeResultBody(t *testing.T, body string) map[string]any {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("unmarshal result body: %v", err)
	}
	return payload
}

func TestExecuteConversationsGetMessageUsesHistoryLookupContract(t *testing.T) {
	callCount := 0
	p := newContractTestProvider(func(r *http.Request) (*http.Response, error) {
		callCount++
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.URL.Path; got != "/api/conversations.history" {
			t.Fatalf("path = %q", got)
		}

		query := r.URL.Query()
		expected := map[string]string{
			"channel":   "C123ABC456",
			"oldest":    "1712161829.000300",
			"latest":    "1712161829.000300",
			"inclusive": "true",
			"limit":     "1",
		}
		if len(query) != len(expected) {
			t.Fatalf("query = %#v", query)
		}
		for key, want := range expected {
			if got := query.Get(key); got != want {
				t.Fatalf("query[%s] = %q, want %q", key, got, want)
			}
		}

		return jsonHTTPResponse(http.StatusOK, `{
			"ok": true,
			"messages": [
				{"ts": "1712161829.000300", "text": "hello", "user": "U123"}
			]
		}`), nil
	})

	result, err := p.Execute(context.Background(), "conversations.getMessage", map[string]any{
		"url": "https://valon.slack.com/archives/C123ABC456/p1712161829000300",
	}, "test-token")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if callCount != 1 {
		t.Fatalf("callCount = %d, want 1", callCount)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", result.Status, http.StatusOK)
	}

	payload := decodeResultBody(t, result.Body)
	data := payload["data"].(map[string]any)
	message := data["message"].(map[string]any)
	if got := message["ts"].(string); got != "1712161829.000300" {
		t.Fatalf("message.ts = %q", got)
	}
	if got := message["text"].(string); got != "hello" {
		t.Fatalf("message.text = %q", got)
	}
}

func TestExecuteConversationsFindUserMentionsUsesHistoryContract(t *testing.T) {
	callCount := 0
	p := newContractTestProvider(func(r *http.Request) (*http.Response, error) {
		callCount++
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.URL.Path; got != "/api/conversations.history" {
			t.Fatalf("path = %q", got)
		}

		query := r.URL.Query()
		expected := map[string]string{
			"channel": "C123",
			"limit":   "25",
			"oldest":  "100.0",
			"latest":  "200.0",
		}
		if len(query) != len(expected) {
			t.Fatalf("query = %#v", query)
		}
		for key, want := range expected {
			if got := query.Get(key); got != want {
				t.Fatalf("query[%s] = %q, want %q", key, got, want)
			}
		}

		return jsonHTTPResponse(http.StatusOK, `{
			"ok": true,
			"messages": [
				{"ts":"101.0","user":"UPOSTER1","text":"hello <@UKEEP123>"},
				{"ts":"102.0","user":"UPOSTER2","text":"again <@UKEEP123> <@UOTHER999>"},
				{"ts":"103.0","user":"UPOSTER3","bot_id":"B123","text":"bot <@UKEEP123>"},
				{"ts":"104.0","user":"UPOSTER4","text":"no mention"}
			]
		}`), nil
	})

	result, err := p.Execute(context.Background(), "conversations.findUserMentions", map[string]any{
		"channel":      "C123",
		"user_id":      "UKEEP123",
		"limit":        25,
		"oldest":       "100.0",
		"latest":       "200.0",
		"include_bots": false,
	}, "test-token")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if callCount != 1 {
		t.Fatalf("callCount = %d, want 1", callCount)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", result.Status, http.StatusOK)
	}

	payload := decodeResultBody(t, result.Body)
	data := payload["data"].(map[string]any)
	if got := int(data["total_mentions"].(float64)); got != 2 {
		t.Fatalf("total_mentions = %d, want 2", got)
	}
	if got := int(data["messages_scanned"].(float64)); got != 4 {
		t.Fatalf("messages_scanned = %d, want 4", got)
	}

	userIDs := data["mentioned_user_ids"].([]any)
	if len(userIDs) != 1 || userIDs[0].(string) != "UKEEP123" {
		t.Fatalf("mentioned_user_ids = %#v", userIDs)
	}

	mentions := data["mentions"].([]any)
	if len(mentions) != 2 {
		t.Fatalf("mentions = %#v", mentions)
	}
	first := mentions[0].(map[string]any)
	if got := first["message_ts"].(string); got != "101.0" {
		t.Fatalf("first.message_ts = %q", got)
	}
	if got := first["mentioned_by"].(string); got != "UPOSTER1" {
		t.Fatalf("first.mentioned_by = %q", got)
	}
}

func TestExecuteConversationsGetThreadParticipantsUsesRepliesAndUsersInfoContract(t *testing.T) {
	callCount := 0
	p := newContractTestProvider(func(r *http.Request) (*http.Response, error) {
		callCount++
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization = %q", got)
		}
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}

		switch callCount {
		case 1:
			if got := r.URL.Path; got != "/api/conversations.replies" {
				t.Fatalf("path = %q", got)
			}
			expected := map[string]string{
				"channel": "C123",
				"ts":      "1.0",
				"limit":   "1000",
			}
			query := r.URL.Query()
			if len(query) != len(expected) {
				t.Fatalf("query = %#v", query)
			}
			for key, want := range expected {
				if got := query.Get(key); got != want {
					t.Fatalf("query[%s] = %q, want %q", key, got, want)
				}
			}
			return jsonHTTPResponse(http.StatusOK, `{
				"ok": true,
				"messages": [
					{"ts":"1.0","user":"U1","text":"parent"},
					{"ts":"2.0","user":"U2","text":"reply"},
					{"ts":"3.0","user":"U2","text":"reply again"},
					{"ts":"4.0","user":"U3","bot_id":"B3","text":"bot reply"}
				]
			}`), nil
		case 2:
			if got := r.URL.Path; got != "/api/users.info" {
				t.Fatalf("path = %q", got)
			}
			if got := r.URL.Query().Get("user"); got != "U1" {
				t.Fatalf("user = %q", got)
			}
			return jsonHTTPResponse(http.StatusOK, `{
				"ok": true,
				"user": {"real_name":"Alice","is_bot":false,"profile":{"display_name":"alice"}}
			}`), nil
		case 3:
			if got := r.URL.Path; got != "/api/users.info" {
				t.Fatalf("path = %q", got)
			}
			if got := r.URL.Query().Get("user"); got != "U2" {
				t.Fatalf("user = %q", got)
			}
			return jsonHTTPResponse(http.StatusOK, `{
				"ok": true,
				"user": {"real_name":"Bob","is_bot":false,"profile":{"display_name":"bob"}}
			}`), nil
		default:
			t.Fatalf("unexpected request %s?%s", r.URL.Path, r.URL.RawQuery)
			return nil, nil
		}
	})

	result, err := p.Execute(context.Background(), "conversations.getThreadParticipants", map[string]any{
		"channel":           "C123",
		"ts":                "1.0",
		"include_user_info": true,
		"include_bots":      false,
	}, "test-token")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if callCount != 3 {
		t.Fatalf("callCount = %d, want 3", callCount)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", result.Status, http.StatusOK)
	}

	payload := decodeResultBody(t, result.Body)
	data := payload["data"].(map[string]any)
	if got := int(data["participant_count"].(float64)); got != 2 {
		t.Fatalf("participant_count = %d, want 2", got)
	}
	if got := int(data["total_replies"].(float64)); got != 3 {
		t.Fatalf("total_replies = %d, want 3", got)
	}

	participants := data["participants"].([]any)
	if len(participants) != 2 {
		t.Fatalf("participants = %#v", participants)
	}

	first := participants[0].(map[string]any)
	if got := first["user_id"].(string); got != "U1" {
		t.Fatalf("first.user_id = %q", got)
	}
	if got := first["is_thread_starter"].(bool); !got {
		t.Fatalf("first.is_thread_starter = %v, want true", got)
	}

	second := participants[1].(map[string]any)
	if got := int(second["message_count"].(float64)); got != 2 {
		t.Fatalf("second.message_count = %d, want 2", got)
	}
	if got := second["display_name"].(string); got != "bob" {
		t.Fatalf("second.display_name = %q", got)
	}
}

func TestExecutePropagatesSlackAPIHTTPError(t *testing.T) {
	callCount := 0
	p := newContractTestProvider(func(r *http.Request) (*http.Response, error) {
		callCount++
		if got := r.URL.Path; got != "/api/conversations.history" {
			t.Fatalf("path = %q", got)
		}
		return jsonHTTPResponse(http.StatusTooManyRequests, `{"ok": false, "error": "rate_limited"}`), nil
	})

	_, err := p.Execute(context.Background(), "conversations.getMessage", map[string]any{
		"channel": "C123",
		"ts":      "1234567890.123456",
	}, "test-token")
	if err == nil {
		t.Fatal("expected error")
	}
	if callCount != 1 {
		t.Fatalf("callCount = %d, want 1", callCount)
	}
	if got := err.Error(); got != `slack API error (status 429): {"ok": false, "error": "rate_limited"}` {
		t.Fatalf("error = %q", got)
	}
}
