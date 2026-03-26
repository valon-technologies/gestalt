package gmail

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
)

const (
	overlayProviderName        = "gmail"
	overlayProviderDisplayName = "Gmail"
	overlayProviderDescription = "Gmail overlay operations requiring MIME message building"
	gmailAPIBase               = "https://gmail.googleapis.com/gmail/v1"

	opSendMessage    = "send_message"
	opCreateDraft    = "create_draft"
	opReplyToMessage = "reply_to_message"
	opForwardMessage = "forward_message"
	opUpdateLabel    = "update_label"

	paramTo                    = "to"
	paramSubject               = "subject"
	paramBody                  = "body"
	paramCC                    = "cc"
	paramBCC                   = "bcc"
	paramHTMLBody              = "html_body"
	paramMessageID             = "message_id"
	paramReplyAll              = "reply_all"
	paramAdditionalText        = "additional_text"
	paramLabelID               = "label_id"
	paramName                  = "name"
	paramLabelListVisibility   = "label_list_visibility"
	paramMessageListVisibility = "message_list_visibility"
	paramBackgroundColor       = "background_color"
	paramTextColor             = "text_color"

	contentTypeJSON = "application/json"
)

var overlayOps = []core.Operation{
	{
		Name:        opSendMessage,
		Description: "Send an email message",
		Method:      http.MethodPost,
		Parameters: []core.Parameter{
			{Name: paramTo, Type: "string", Required: true, Description: "Recipient email address"},
			{Name: paramSubject, Type: "string", Required: true, Description: "Email subject line"},
			{Name: paramBody, Type: "string", Required: true, Description: "Plain text email body"},
			{Name: paramCC, Type: "string", Description: "CC recipients (comma-separated)"},
			{Name: paramBCC, Type: "string", Description: "BCC recipients (comma-separated)"},
			{Name: paramHTMLBody, Type: "string", Description: "HTML email body"},
		},
	},
	{
		Name:        opCreateDraft,
		Description: "Create an email draft",
		Method:      http.MethodPost,
		Parameters: []core.Parameter{
			{Name: paramTo, Type: "string", Required: true, Description: "Recipient email address"},
			{Name: paramSubject, Type: "string", Required: true, Description: "Email subject line"},
			{Name: paramBody, Type: "string", Required: true, Description: "Plain text email body"},
			{Name: paramCC, Type: "string", Description: "CC recipients (comma-separated)"},
			{Name: paramBCC, Type: "string", Description: "BCC recipients (comma-separated)"},
			{Name: paramHTMLBody, Type: "string", Description: "HTML email body"},
		},
	},
	{
		Name:        opReplyToMessage,
		Description: "Reply to an existing email message",
		Method:      http.MethodPost,
		Parameters: []core.Parameter{
			{Name: paramMessageID, Type: "string", Required: true, Description: "ID of the message to reply to"},
			{Name: paramBody, Type: "string", Required: true, Description: "Plain text reply body"},
			{Name: paramCC, Type: "string", Description: "CC recipients (comma-separated)"},
			{Name: paramReplyAll, Type: "boolean", Description: "Reply to all recipients (default false)"},
			{Name: paramHTMLBody, Type: "string", Description: "HTML reply body"},
		},
	},
	{
		Name:        opForwardMessage,
		Description: "Forward an email message to new recipients",
		Method:      http.MethodPost,
		Parameters: []core.Parameter{
			{Name: paramMessageID, Type: "string", Required: true, Description: "ID of the message to forward"},
			{Name: paramTo, Type: "string", Required: true, Description: "Recipient email address to forward to"},
			{Name: paramAdditionalText, Type: "string", Description: "Additional text to prepend to forwarded message"},
			{Name: paramCC, Type: "string", Description: "CC recipients (comma-separated)"},
		},
	},
	{
		Name:        opUpdateLabel,
		Description: "Update a Gmail label (rename, move in hierarchy, or change visibility/color)",
		Method:      http.MethodPatch,
		Parameters: []core.Parameter{
			{Name: paramLabelID, Type: "string", Required: true, Description: "Label ID to update"},
			{Name: paramName, Type: "string", Description: "New label name"},
			{Name: paramLabelListVisibility, Type: "string", Description: "Visibility: labelShow, labelShowIfUnread, or labelHide"},
			{Name: paramMessageListVisibility, Type: "string", Description: "Visibility: show or hide"},
			{Name: paramBackgroundColor, Type: "string", Description: "Background color hex code"},
			{Name: paramTextColor, Type: "string", Description: "Text color hex code"},
		},
	},
}

var _ core.Provider = (*OverlayProvider)(nil)
var _ core.CatalogProvider = (*OverlayProvider)(nil)

type OverlayProvider struct {
	client *http.Client
}

func NewOverlayProvider() *OverlayProvider {
	return &OverlayProvider{client: http.DefaultClient}
}

func (p *OverlayProvider) Name() string                        { return overlayProviderName }
func (p *OverlayProvider) DisplayName() string                 { return overlayProviderDisplayName }
func (p *OverlayProvider) Description() string                 { return overlayProviderDescription }
func (p *OverlayProvider) ConnectionMode() core.ConnectionMode { return core.ConnectionModeUser }
func (p *OverlayProvider) ListOperations() []core.Operation    { return overlayOps }

func (p *OverlayProvider) Catalog() *catalog.Catalog {
	ops := make([]catalog.CatalogOperation, len(overlayOps))
	for i, op := range overlayOps {
		params := make([]catalog.CatalogParameter, len(op.Parameters))
		for j, param := range op.Parameters {
			params[j] = catalog.CatalogParameter{
				Name:        param.Name,
				Type:        param.Type,
				Description: param.Description,
				Required:    param.Required,
			}
		}
		ops[i] = catalog.CatalogOperation{
			ID:          op.Name,
			Method:      op.Method,
			Description: op.Description,
			Parameters:  params,
		}
	}
	return &catalog.Catalog{
		Name:        overlayProviderName,
		DisplayName: overlayProviderDisplayName,
		Description: overlayProviderDescription,
		Operations:  ops,
	}
}

func (p *OverlayProvider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	switch operation {
	case opSendMessage:
		return p.sendMessage(ctx, params, token)
	case opCreateDraft:
		return p.createDraft(ctx, params, token)
	case opReplyToMessage:
		return p.replyToMessage(ctx, params, token)
	case opForwardMessage:
		return p.forwardMessage(ctx, params, token)
	case opUpdateLabel:
		return p.updateLabel(ctx, params, token)
	default:
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}
}

func (p *OverlayProvider) sendMessage(ctx context.Context, params map[string]any, token string) (*core.OperationResult, error) {
	raw, err := buildRawFromParams(params)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(map[string]string{"raw": raw})
	return p.doPost(ctx, gmailAPIBase+"/users/me/messages/send", payload, token)
}

func (p *OverlayProvider) createDraft(ctx context.Context, params map[string]any, token string) (*core.OperationResult, error) {
	raw, err := buildRawFromParams(params)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(map[string]any{
		"message": map[string]string{"raw": raw},
	})
	return p.doPost(ctx, gmailAPIBase+"/users/me/drafts", payload, token)
}

func buildRawFromParams(params map[string]any) (string, error) {
	to := stringParam(params, paramTo)
	subject := stringParam(params, paramSubject)
	body := stringParam(params, paramBody)
	if to == "" || subject == "" || body == "" {
		return "", fmt.Errorf("%s, %s, and %s are required", paramTo, paramSubject, paramBody)
	}
	return buildMIMEMessage(to, subject, body,
		stringParam(params, paramCC),
		stringParam(params, paramBCC),
		stringParam(params, paramHTMLBody),
		"", ""), nil
}

func (p *OverlayProvider) replyToMessage(ctx context.Context, params map[string]any, token string) (*core.OperationResult, error) {
	messageID := stringParam(params, paramMessageID)
	body := stringParam(params, paramBody)
	if messageID == "" || body == "" {
		return nil, fmt.Errorf("%s and %s are required", paramMessageID, paramBody)
	}

	original, err := p.getMessage(ctx, messageID, "metadata", token)
	if err != nil {
		return nil, fmt.Errorf("fetching original message: %w", err)
	}

	threadID, _ := original["threadId"].(string)
	headers := extractHeaders(original)
	origFrom := headers["from"]
	origTo := headers["to"]
	origSubject := headers["subject"]
	origMessageID := headers["message-id"]

	replyAll := boolParam(params, paramReplyAll, false)
	to := origFrom
	cc := stringParam(params, paramCC)
	if replyAll {
		ccParts := []string{}
		if origTo != "" {
			ccParts = append(ccParts, origTo)
		}
		if origCC := headers["cc"]; origCC != "" {
			ccParts = append(ccParts, origCC)
		}
		if cc != "" {
			ccParts = append(ccParts, cc)
		}
		if len(ccParts) > 0 {
			cc = strings.Join(ccParts, ", ")
		}
	}

	subject := origSubject
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}

	raw := buildMIMEMessage(to, subject, body, cc, "",
		stringParam(params, paramHTMLBody),
		origMessageID, origMessageID)

	payload, _ := json.Marshal(map[string]string{"raw": raw, "threadId": threadID})
	return p.doPost(ctx, gmailAPIBase+"/users/me/messages/send", payload, token)
}

func (p *OverlayProvider) forwardMessage(ctx context.Context, params map[string]any, token string) (*core.OperationResult, error) {
	messageID := stringParam(params, paramMessageID)
	to := stringParam(params, paramTo)
	if messageID == "" || to == "" {
		return nil, fmt.Errorf("%s and %s are required", paramMessageID, paramTo)
	}

	original, err := p.getMessage(ctx, messageID, "full", token)
	if err != nil {
		return nil, fmt.Errorf("fetching original message: %w", err)
	}

	headers := extractHeaders(original)
	origSubject := headers["subject"]
	origFrom := headers["from"]
	origDate := headers["date"]
	origBody := extractBody(original)

	subject := origSubject
	if !strings.HasPrefix(strings.ToLower(subject), "fwd:") {
		subject = "Fwd: " + subject
	}

	forwardHeader := fmt.Sprintf(
		"\n\n---------- Forwarded message ----------\nFrom: %s\nDate: %s\nSubject: %s\n\n",
		origFrom, origDate, origSubject)

	additionalText := stringParam(params, paramAdditionalText)
	body := forwardHeader + origBody
	if additionalText != "" {
		body = additionalText + body
	}

	raw := buildMIMEMessage(to, subject, body,
		stringParam(params, paramCC), "", "", "", "")

	payload, _ := json.Marshal(map[string]string{"raw": raw})
	return p.doPost(ctx, gmailAPIBase+"/users/me/messages/send", payload, token)
}

func (p *OverlayProvider) updateLabel(ctx context.Context, params map[string]any, token string) (*core.OperationResult, error) {
	labelID := stringParam(params, paramLabelID)
	if labelID == "" {
		return nil, fmt.Errorf("%s is required", paramLabelID)
	}

	body := map[string]any{}
	if v := stringParam(params, paramName); v != "" {
		body["name"] = v
	}
	if v := stringParam(params, paramLabelListVisibility); v != "" {
		body["labelListVisibility"] = v
	}
	if v := stringParam(params, paramMessageListVisibility); v != "" {
		body["messageListVisibility"] = v
	}

	bgColor := stringParam(params, paramBackgroundColor)
	textColor := stringParam(params, paramTextColor)
	if bgColor != "" || textColor != "" {
		colorMap := map[string]string{}
		if bgColor != "" {
			colorMap["backgroundColor"] = bgColor
		}
		if textColor != "" {
			colorMap["textColor"] = textColor
		}
		body["color"] = colorMap
	}

	if len(body) == 0 {
		return nil, fmt.Errorf("at least one field to update must be provided")
	}

	payload, _ := json.Marshal(body)
	url := fmt.Sprintf("%s/users/me/labels/%s", gmailAPIBase, labelID)
	return p.doPatch(ctx, url, payload, token)
}

func (p *OverlayProvider) getMessage(ctx context.Context, messageID, format, token string) (map[string]any, error) {
	url := fmt.Sprintf("%s/users/me/messages/%s?format=%s", gmailAPIBase, messageID, format)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("gmail API returned %d: %s", resp.StatusCode, string(data))
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (p *OverlayProvider) doPost(ctx context.Context, url string, payload []byte, token string) (*core.OperationResult, error) {
	return p.doRequest(ctx, http.MethodPost, url, payload, token)
}

func (p *OverlayProvider) doPatch(ctx context.Context, url string, payload []byte, token string) (*core.OperationResult, error) {
	return p.doRequest(ctx, http.MethodPatch, url, payload, token)
}

func (p *OverlayProvider) doRequest(ctx context.Context, method, url string, payload []byte, token string) (*core.OperationResult, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", contentTypeJSON)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return &core.OperationResult{
		Status: resp.StatusCode,
		Body:   string(data),
	}, nil
}

func buildMIMEMessage(to, subject, body, cc, bcc, htmlBody, inReplyTo, references string) string {
	var buf strings.Builder

	if htmlBody != "" {
		w := multipart.NewWriter(&buf)
		boundary := w.Boundary()

		header := buildRawHeaders(to, subject, cc, bcc, inReplyTo, references)
		header += fmt.Sprintf("MIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=%q\r\n\r\n", boundary)
		buf.Reset()
		buf.WriteString(header)

		plainHeader := make(textproto.MIMEHeader)
		plainHeader.Set("Content-Type", "text/plain; charset=utf-8")
		pw, _ := w.CreatePart(plainHeader)
		_, _ = fmt.Fprint(pw, body)

		htmlHeader := make(textproto.MIMEHeader)
		htmlHeader.Set("Content-Type", "text/html; charset=utf-8")
		hw, _ := w.CreatePart(htmlHeader)
		_, _ = fmt.Fprint(hw, htmlBody)

		_ = w.Close()
	} else {
		header := buildRawHeaders(to, subject, cc, bcc, inReplyTo, references)
		header += "MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n"
		buf.WriteString(header)
		buf.WriteString(body)
	}

	return base64.RawURLEncoding.EncodeToString([]byte(buf.String()))
}

func buildRawHeaders(to, subject, cc, bcc, inReplyTo, references string) string {
	var h strings.Builder
	h.WriteString("To: " + to + "\r\n")
	h.WriteString("Subject: " + subject + "\r\n")
	if cc != "" {
		h.WriteString("Cc: " + cc + "\r\n")
	}
	if bcc != "" {
		h.WriteString("Bcc: " + bcc + "\r\n")
	}
	if inReplyTo != "" {
		h.WriteString("In-Reply-To: " + inReplyTo + "\r\n")
	}
	if references != "" {
		h.WriteString("References: " + references + "\r\n")
	}
	return h.String()
}

func extractHeaders(msg map[string]any) map[string]string {
	result := map[string]string{}
	payload, _ := msg["payload"].(map[string]any)
	if payload == nil {
		return result
	}
	rawHeaders, _ := payload["headers"].([]any)
	for _, h := range rawHeaders {
		hMap, _ := h.(map[string]any)
		if hMap == nil {
			continue
		}
		name, _ := hMap["name"].(string)
		value, _ := hMap["value"].(string)
		result[strings.ToLower(name)] = value
	}
	return result
}

func extractBody(msg map[string]any) string {
	payload, _ := msg["payload"].(map[string]any)
	if payload == nil {
		return ""
	}
	if bodyData, ok := payload["body"].(map[string]any); ok {
		if data, ok := bodyData["data"].(string); ok && data != "" {
			decoded, err := base64.RawURLEncoding.DecodeString(data)
			if err == nil {
				return string(decoded)
			}
		}
	}
	parts, _ := payload["parts"].([]any)
	for _, part := range parts {
		p, _ := part.(map[string]any)
		if p == nil {
			continue
		}
		mimeType, _ := p["mimeType"].(string)
		if mimeType == "text/plain" {
			if bodyData, ok := p["body"].(map[string]any); ok {
				if data, ok := bodyData["data"].(string); ok && data != "" {
					decoded, err := base64.RawURLEncoding.DecodeString(data)
					if err == nil {
						return string(decoded)
					}
				}
			}
		}
	}
	return ""
}

func stringParam(params map[string]any, key string) string {
	v, _ := params[key].(string)
	return v
}

func boolParam(params map[string]any, key string, defaultVal bool) bool {
	v, ok := params[key].(bool)
	if !ok {
		return defaultVal
	}
	return v
}
