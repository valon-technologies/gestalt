package publicclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	gestaltclient "github.com/valon-technologies/gestalt/sdk/go/client"
	"github.com/valon-technologies/gestalt/sdk/go/publicclient/generated"
	pb "google.golang.org/protobuf/proto"
)

type restUnaryTransport struct {
	baseURL string
	auth    Auth
	client  *http.Client
}

func (t *restUnaryTransport) Close() error {
	if t == nil || t.client == nil || t.client == http.DefaultClient {
		return nil
	}
	t.client.CloseIdleConnections()
	return nil
}

func (t *restUnaryTransport) Unary(
	ctx context.Context,
	method generated.Method,
	request, response pb.Message,
) error {
	if t == nil {
		return &generated.GestaltError{
			Code:    gestaltclient.GestaltErrorCodeInvalidArgument,
			Message: "publicclient: REST transport is nil",
		}
	}

	prepared, err := generated.PrepareRESTRequest(method, request)
	if err != nil {
		return err
	}
	target, err := joinURL(t.baseURL, prepared.Path)
	if err != nil {
		return err
	}
	if len(prepared.Query) > 0 {
		values := url.Values{}
		for _, pair := range prepared.Query {
			values.Add(pair.Name, pair.Value)
		}
		if encoded := values.Encode(); encoded != "" {
			target += "?" + encoded
		}
	}

	var body io.Reader
	if prepared.Body != nil {
		body = bytes.NewReader(prepared.Body)
	}

	req, err := http.NewRequestWithContext(ctx, prepared.Verb, target, body)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	meta := &Request{Headers: map[string]string{}}
	if t.auth != nil {
		if err := t.auth.Apply(ctx, meta); err != nil {
			return err
		}
	}
	for key, value := range meta.Headers {
		req.Header.Set(key, value)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			code := gestaltclient.GestaltErrorCodeCanceled
			if ctx.Err() == context.DeadlineExceeded {
				code = gestaltclient.GestaltErrorCodeDeadlineExceeded
			}
			return &generated.GestaltError{Code: code, Message: ctx.Err().Error()}
		}
		return &generated.GestaltError{
			Code:    gestaltclient.GestaltErrorCodeUnavailable,
			Message: err.Error(),
		}
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	headers := make([]generated.Header, 0, len(resp.Header))
	for key, values := range resp.Header {
		for _, value := range values {
			headers = append(headers, generated.Header{Name: key, Value: value})
		}
	}

	return generated.DecodeRESTResponse(method, response, generated.RawRESTResponse{
		Status:  resp.StatusCode,
		Headers: headers,
		Body:    raw,
	})
}

func joinURL(baseURL, path string) (string, error) {
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return "", fmt.Errorf("publicclient: base URL is required")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base.Scheme + "://" + base.Host + strings.TrimSuffix(base.Path, "/") + path, nil
}
