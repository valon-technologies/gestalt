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

func TestProviderMetadata(t *testing.T) {
	p := NewProvider()
	if p.Name() != "slack" {
		t.Fatalf("Name() = %q, want %q", p.Name(), "slack")
	}
	catalog := p.Catalog()
	if catalog == nil {
		t.Fatal("Catalog() returned nil")
	}
	if len(catalog.Operations) != 4 {
		t.Fatalf("Catalog().Operations returned %d ops, want 4", len(catalog.Operations))
	}
}

func TestExecuteRequiresToken(t *testing.T) {
	p := NewProvider()
	_, err := p.Execute(context.Background(), "slack_get_message", map[string]any{
		"channel": "C123", "ts": "1234567890.123456",
	}, "")
	if err == nil {
		t.Fatal("Execute with empty token should error")
	}
}

func TestGetMessageByURL(t *testing.T) {
	p := NewProvider()
	p.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/conversations.history" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("channel"); got != "C123ABC456" {
			t.Fatalf("channel = %q", got)
		}
		if got := r.URL.Query().Get("oldest"); got != "1712161829.000300" {
			t.Fatalf("oldest = %q", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(`{
				"ok": true,
				"messages": [{"ts": "1712161829.000300", "text": "hello"}]
			}`)),
		}, nil
	})

	result, err := p.Execute(context.Background(), "slack_get_message", map[string]any{
		"url": "https://valon.slack.com/archives/C123ABC456/p1712161829000300",
	}, "test-token")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(result.Body, `"message"`) {
		t.Fatalf("expected message in body: %s", result.Body)
	}
}

func TestFindUserMentionsSkipsBotsAndFiltersUser(t *testing.T) {
	p := NewProvider()
	p.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/conversations.history" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(`{
				"ok": true,
				"messages": [
					{"ts":"1.0","user":"UPOSTER1","text":"hey <@UKEEP123> and <@USKIP999>"},
					{"ts":"2.0","user":"UPOSTER2","bot_id":"B123","text":"bot ping <@UKEEP123>"},
					{"ts":"3.0","user":"UPOSTER3","text":"no mentions here"}
				]
			}`)),
		}, nil
	})

	result, err := p.Execute(context.Background(), "slack_find_user_mentions", map[string]any{
		"channel":      "C123",
		"user_id":      "UKEEP123",
		"include_bots": false,
	}, "test-token")
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(result.Body), &payload); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	data := payload["data"].(map[string]any)
	if got := int(data["total_mentions"].(float64)); got != 1 {
		t.Fatalf("total_mentions = %d, want 1", got)
	}
	userIDs := data["mentioned_user_ids"].([]any)
	if len(userIDs) != 1 || userIDs[0].(string) != "UKEEP123" {
		t.Fatalf("mentioned_user_ids = %v", userIDs)
	}
}

func TestGetThreadParticipantsIncludesUserInfo(t *testing.T) {
	p := NewProvider()
	callCount := 0
	p.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		callCount++
		switch {
		case r.URL.Path == "/api/conversations.replies":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`{
					"ok": true,
					"messages": [
						{"ts":"1.0","user":"U1","text":"parent"},
						{"ts":"2.0","user":"U2","text":"reply"},
						{"ts":"3.0","user":"U2","text":"reply again"},
						{"ts":"4.0","user":"U3","bot_id":"B3","text":"bot reply"}
					]
				}`)),
			}, nil
		case r.URL.Path == "/api/users.info" && r.URL.Query().Get("user") == "U1":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`{
					"ok": true,
					"user": {"real_name":"Alice","is_bot":false,"profile":{"display_name":"alice"}}
				}`)),
			}, nil
		case r.URL.Path == "/api/users.info" && r.URL.Query().Get("user") == "U2":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`{
					"ok": true,
					"user": {"real_name":"Bob","is_bot":false,"profile":{"display_name":"bob"}}
				}`)),
			}, nil
		default:
			t.Fatalf("unexpected request %s?%s", r.URL.Path, r.URL.RawQuery)
			return nil, nil
		}
	})

	result, err := p.Execute(context.Background(), "slack_get_thread_participants", map[string]any{
		"channel":           "C123",
		"ts":                "1.0",
		"include_user_info": true,
		"include_bots":      false,
	}, "test-token")
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	if callCount != 3 {
		t.Fatalf("expected 3 API calls, got %d", callCount)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(result.Body), &payload); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	data := payload["data"].(map[string]any)
	if got := int(data["participant_count"].(float64)); got != 2 {
		t.Fatalf("participant_count = %d, want 2", got)
	}
	participants := data["participants"].([]any)
	second := participants[1].(map[string]any)
	if got := int(second["message_count"].(float64)); got != 2 {
		t.Fatalf("message_count = %d, want 2", got)
	}
	if second["display_name"].(string) != "bob" {
		t.Fatalf("display_name = %q", second["display_name"].(string))
	}
}

func TestCreateCanvasWrapsMarkdownPayload(t *testing.T) {
	p := NewProvider()
	p.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/canvases.create" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		doc, ok := body["document_content"].(map[string]any)
		if !ok {
			t.Fatal("expected document_content object")
		}
		if doc["type"] != "markdown" || doc["markdown"] != "# Title" {
			t.Fatalf("unexpected document_content = %#v", doc)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"ok": true, "canvas_id": "F123"}`)),
		}, nil
	})

	result, err := p.Execute(context.Background(), "slack_create_canvas", map[string]any{
		"title":            "Test canvas",
		"document_content": "# Title",
	}, "test-token")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(result.Body, `"canvas"`) {
		t.Fatalf("expected canvas in body: %s", result.Body)
	}
}
