package gmail

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
	if p.Name() != "gmail" {
		t.Fatalf("Name() = %q, want %q", p.Name(), "gmail")
	}
	catalog := p.Catalog()
	if catalog == nil {
		t.Fatal("Catalog() returned nil")
	}
	if len(catalog.Operations) != 4 {
		t.Fatalf("Catalog().Operations returned %d ops, want 4", len(catalog.Operations))
	}
	wantIDs := []string{
		"messages.send",
		"messages.createDraft",
		"messages.reply",
		"messages.forward",
	}
	for i, want := range wantIDs {
		if got := catalog.Operations[i].ID; got != want {
			t.Fatalf("Catalog().Operations[%d].ID = %q, want %q", i, got, want)
		}
	}
}

func TestExecuteRequiresToken(t *testing.T) {
	p := NewProvider()
	_, err := p.Execute(context.Background(), "messages.send", map[string]any{
		"to": "a@b.com", "subject": "Hi", "body": "Hi",
	}, "")
	if err == nil {
		t.Fatal("Execute with empty token should error")
	}
}

func TestSendMessage(t *testing.T) {
	p := NewProvider()
	p.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(r.URL.Path, "/messages/send") {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["raw"] == "" {
			t.Fatal("expected non-empty raw field")
		}
		decoded, err := b64.DecodeString(body["raw"])
		if err != nil {
			t.Fatalf("base64 decode error: %v", err)
		}
		mime := string(decoded)
		if !strings.Contains(mime, "To: bob@example.com") {
			t.Fatalf("MIME missing To header: %s", mime)
		}
		if !strings.Contains(mime, "Subject: Hello") {
			t.Fatalf("MIME missing Subject: %s", mime)
		}
		if !strings.Contains(mime, "MIME-Version: 1.0") {
			t.Fatalf("MIME missing MIME-Version: %s", mime)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg-new","threadId":"t-1"}`)),
		}, nil
	})

	result, err := p.Execute(context.Background(), "messages.send", map[string]any{
		"to":      "bob@example.com",
		"subject": "Hello",
		"body":    "Hi Bob",
	}, "test-token")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(result.Body, `"message"`) {
		t.Fatalf("expected message in body: %s", result.Body)
	}
}

func TestSendMessageWithHTML(t *testing.T) {
	p := NewProvider()
	p.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		decoded, _ := b64.DecodeString(body["raw"])
		mime := string(decoded)
		if !strings.Contains(mime, "multipart/alternative") {
			t.Fatalf("expected multipart/alternative: %s", mime)
		}
		if !strings.Contains(mime, "text/html") {
			t.Fatalf("expected text/html part: %s", mime)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg-new"}`)),
		}, nil
	})

	_, err := p.Execute(context.Background(), "messages.send", map[string]any{
		"to":        "bob@example.com",
		"subject":   "Hello",
		"body":      "Hi Bob",
		"html_body": "<p>Hi Bob</p>",
	}, "test-token")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
}

func TestCreateDraft(t *testing.T) {
	p := NewProvider()
	p.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(r.URL.Path, "/drafts") {
			t.Fatalf("path = %s, want /drafts", r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		msg, ok := body["message"].(map[string]any)
		if !ok || msg["raw"] == "" {
			t.Fatal("expected message.raw in body")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"id":"draft-1","message":{"id":"msg-1"}}`)),
		}, nil
	})

	result, err := p.Execute(context.Background(), "messages.createDraft", map[string]any{
		"to":      "bob@example.com",
		"subject": "Draft",
		"body":    "Draft body",
	}, "test-token")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(result.Body, `"draft"`) {
		t.Fatalf("expected draft in body: %s", result.Body)
	}
}

func TestReplyToMessage(t *testing.T) {
	p := NewProvider()
	callCount := 0
	p.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		callCount++
		if callCount == 1 {
			if !strings.Contains(r.URL.Path, "/messages/orig-1") {
				t.Fatalf("call 1 path = %s", r.URL.Path)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`{
					"threadId": "t-1",
					"payload": {
						"headers": [
							{"name": "From", "value": "alice@example.com"},
							{"name": "To", "value": "me@example.com"},
							{"name": "Subject", "value": "Original Subject"},
							{"name": "Message-ID", "value": "<msg-id-123@example.com>"},
							{"name": "References", "value": "<older@example.com>"}
						]
					}
				}`)),
			}, nil
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["threadId"] != "t-1" {
			t.Fatalf("threadId = %v, want t-1", body["threadId"])
		}
		raw, _ := body["raw"].(string)
		decoded, _ := b64.DecodeString(raw)
		mime := string(decoded)
		if !strings.Contains(mime, "Re: Original Subject") {
			t.Fatalf("expected Re: prefix in subject: %s", mime)
		}
		if !strings.Contains(mime, "In-Reply-To: <msg-id-123@example.com>") {
			t.Fatalf("expected In-Reply-To header: %s", mime)
		}
		if !strings.Contains(mime, "<older@example.com> <msg-id-123@example.com>") {
			t.Fatalf("expected full References chain: %s", mime)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"id":"reply-1","threadId":"t-1"}`)),
		}, nil
	})

	_, err := p.Execute(context.Background(), "messages.reply", map[string]any{
		"message_id": "orig-1",
		"body":       "Thanks!",
	}, "test-token")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 API calls, got %d", callCount)
	}
}

func TestMIMEHeaderInjectionPrevented(t *testing.T) {
	raw := buildMIME(mimeParams{
		To:      "user@example.com\r\nBcc: hidden@attacker.com",
		Subject: "Test\r\nX-Injected: true",
		Body:    "Hello",
	})
	decoded, _ := b64.DecodeString(raw)
	mime := string(decoded)
	for _, line := range strings.Split(mime, "\r\n") {
		if strings.HasPrefix(line, "Bcc:") {
			t.Fatalf("injected Bcc header found: %s", line)
		}
		if strings.HasPrefix(line, "X-Injected:") {
			t.Fatalf("injected X-Injected header found: %s", line)
		}
	}
}

func TestMIMEBuildPlainText(t *testing.T) {
	raw := buildMIME(mimeParams{
		To:      "bob@example.com",
		Subject: "Test",
		Body:    "Hello",
	})
	decoded, err := b64.DecodeString(raw)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	mime := string(decoded)
	if !strings.Contains(mime, "MIME-Version: 1.0") {
		t.Fatalf("missing MIME-Version: %s", mime)
	}
	if !strings.Contains(mime, "Content-Type: text/plain") {
		t.Fatalf("missing content type: %s", mime)
	}
	if strings.Contains(mime, "multipart") {
		t.Fatalf("should not be multipart for plain text: %s", mime)
	}
}

func TestEnsureReplyPrefix(t *testing.T) {
	if got := ensureReplyPrefix("Hello"); got != "Re: Hello" {
		t.Fatalf("got %q", got)
	}
	if got := ensureReplyPrefix("Re: Hello"); got != "Re: Hello" {
		t.Fatalf("got %q, should not double-prefix", got)
	}
}
