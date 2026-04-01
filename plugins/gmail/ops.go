package gmail

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

func (p *Provider) sendMessage(ctx context.Context, params map[string]any, token string) (*gestalt.OperationResult, error) {
	to := stringParam(params, "to")
	subject := stringParam(params, "subject")
	body := stringParam(params, "body")
	if to == "" || subject == "" || body == "" {
		return nil, fmt.Errorf("to, subject, and body are required")
	}

	raw := buildMIME(mimeParams{
		To:       to,
		Subject:  subject,
		Body:     body,
		Cc:       stringParam(params, "cc"),
		Bcc:      stringParam(params, "bcc"),
		HtmlBody: stringParam(params, "html_body"),
	})

	resp, err := p.doPost(ctx, gmailBaseURL+"/messages/send", map[string]string{"raw": raw}, token)
	if err != nil {
		return nil, err
	}
	return jsonResult(map[string]any{
		"data": map[string]json.RawMessage{"message": resp},
	})
}

func (p *Provider) createDraft(ctx context.Context, params map[string]any, token string) (*gestalt.OperationResult, error) {
	to := stringParam(params, "to")
	subject := stringParam(params, "subject")
	body := stringParam(params, "body")
	if to == "" || subject == "" || body == "" {
		return nil, fmt.Errorf("to, subject, and body are required")
	}

	raw := buildMIME(mimeParams{
		To:       to,
		Subject:  subject,
		Body:     body,
		Cc:       stringParam(params, "cc"),
		Bcc:      stringParam(params, "bcc"),
		HtmlBody: stringParam(params, "html_body"),
	})

	resp, err := p.doPost(ctx, gmailBaseURL+"/drafts", map[string]any{
		"message": map[string]string{"raw": raw},
	}, token)
	if err != nil {
		return nil, err
	}
	return jsonResult(map[string]any{
		"data": map[string]json.RawMessage{"draft": resp},
	})
}

func (p *Provider) replyToMessage(ctx context.Context, params map[string]any, token string) (*gestalt.OperationResult, error) {
	messageID := stringParam(params, "message_id")
	replyBody := stringParam(params, "body")
	if messageID == "" || replyBody == "" {
		return nil, fmt.Errorf("message_id and body are required")
	}

	origURL := gmailBaseURL + "/messages/" + url.PathEscape(messageID) + "?format=metadata&metadataHeaders=From&metadataHeaders=To&metadataHeaders=Cc&metadataHeaders=Subject&metadataHeaders=Message-ID&metadataHeaders=References&metadataHeaders=Delivered-To"
	origBody, err := p.doGet(ctx, origURL, token)
	if err != nil {
		return nil, fmt.Errorf("fetching original message: %w", err)
	}

	var orig struct {
		ThreadID string `json:"threadId"`
		Payload  struct {
			Headers []map[string]string `json:"headers"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(origBody, &orig); err != nil {
		return nil, fmt.Errorf("parsing original message: %w", err)
	}

	origFrom := getHeader(orig.Payload.Headers, "From")
	origTo := getHeader(orig.Payload.Headers, "To")
	origCc := getHeader(orig.Payload.Headers, "Cc")
	origSubject := getHeader(orig.Payload.Headers, "Subject")
	origMessageID := getHeader(orig.Payload.Headers, "Message-ID")
	origReferences := getHeader(orig.Payload.Headers, "References")

	references := origMessageID
	if origReferences != "" {
		references = origReferences + " " + origMessageID
	}

	selfEmail := getHeader(orig.Payload.Headers, "Delivered-To")

	to := origFrom
	cc := stringParam(params, "cc")
	if boolParamOr(params, "reply_all", false) {
		var allCC []string
		for _, v := range []string{origTo, origCc, cc} {
			if v != "" {
				allCC = append(allCC, v)
			}
		}
		cc = filterSelfFromRecipients(strings.Join(allCC, ", "), selfEmail)
	}

	raw := buildMIME(mimeParams{
		To:         to,
		Subject:    ensureReplyPrefix(origSubject),
		Body:       replyBody,
		Cc:         cc,
		HtmlBody:   stringParam(params, "html_body"),
		InReplyTo:  origMessageID,
		References: references,
	})

	resp, err := p.doPost(ctx, gmailBaseURL+"/messages/send", map[string]any{
		"raw":      raw,
		"threadId": orig.ThreadID,
	}, token)
	if err != nil {
		return nil, err
	}
	return jsonResult(map[string]any{
		"data": map[string]json.RawMessage{"message": resp},
	})
}

func (p *Provider) forwardMessage(ctx context.Context, params map[string]any, token string) (*gestalt.OperationResult, error) {
	messageID := stringParam(params, "message_id")
	to := stringParam(params, "to")
	if messageID == "" || to == "" {
		return nil, fmt.Errorf("message_id and to are required")
	}

	origURL := gmailBaseURL + "/messages/" + url.PathEscape(messageID) + "?format=full"
	origBody, err := p.doGet(ctx, origURL, token)
	if err != nil {
		return nil, fmt.Errorf("fetching original message: %w", err)
	}

	var orig struct {
		Payload struct {
			Headers []map[string]string `json:"headers"`
			Body    struct {
				Data string `json:"data"`
			} `json:"body"`
			Parts []struct {
				MimeType string `json:"mimeType"`
				Body     struct {
					Data string `json:"data"`
				} `json:"body"`
			} `json:"parts"`
			MimeType string `json:"mimeType"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(origBody, &orig); err != nil {
		return nil, fmt.Errorf("parsing original message: %w", err)
	}

	origSubject := getHeader(orig.Payload.Headers, "Subject")
	origFrom := getHeader(orig.Payload.Headers, "From")
	origDate := getHeader(orig.Payload.Headers, "Date")

	origText := extractPlainText(orig.Payload.Parts, orig.Payload.Body.Data, orig.Payload.MimeType)

	forwardedBody := ""
	if additional := stringParam(params, "additional_text"); additional != "" {
		forwardedBody = additional + "\r\n\r\n"
	}
	forwardedBody += fmt.Sprintf("---------- Forwarded message ----------\r\nFrom: %s\r\nDate: %s\r\nSubject: %s\r\n\r\n%s",
		origFrom, origDate, origSubject, origText)

	raw := buildMIME(mimeParams{
		To:      to,
		Subject: ensureForwardPrefix(origSubject),
		Body:    forwardedBody,
		Cc:      stringParam(params, "cc"),
	})

	resp, err := p.doPost(ctx, gmailBaseURL+"/messages/send", map[string]string{"raw": raw}, token)
	if err != nil {
		return nil, err
	}
	return jsonResult(map[string]any{
		"data": map[string]json.RawMessage{"message": resp},
	})
}

func extractPlainText(parts []struct {
	MimeType string `json:"mimeType"`
	Body     struct {
		Data string `json:"data"`
	} `json:"body"`
}, bodyData, mimeType string) string {
	for _, part := range parts {
		if part.MimeType == "text/plain" && part.Body.Data != "" {
			if decoded, err := b64.DecodeString(part.Body.Data); err == nil {
				return string(decoded)
			}
		}
	}
	if bodyData != "" && !strings.Contains(mimeType, "html") {
		if decoded, err := b64.DecodeString(bodyData); err == nil {
			return string(decoded)
		}
	}
	return ""
}
