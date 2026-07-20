package publicclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	gestaltclient "github.com/valon-technologies/gestalt/sdk/go/client"
	"github.com/valon-technologies/gestalt/sdk/go/publicclient/generated"
	"google.golang.org/grpc/codes"
	gproto "google.golang.org/protobuf/encoding/protojson"
	pb "google.golang.org/protobuf/proto"
)

var protoJSONMarshal = gproto.MarshalOptions{}
var protoJSONUnmarshal = gproto.UnmarshalOptions{DiscardUnknown: true}

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

type gatewayErrorBody struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

func decodeGatewayError(status int, body []byte) error {
	code := gestaltclient.HTTPStatusToGestaltCode(int32(status))
	message := fmt.Sprintf("request failed with status %d", status)
	if len(body) > 0 {
		var payload gatewayErrorBody
		if json.Unmarshal(body, &payload) == nil {
			if payload.Error != "" {
				message = payload.Error
			}
			if payload.Code != "" {
				if parsed := codeFromString(payload.Code); parsed != gestaltclient.GestaltErrorCodeUnknown {
					code = parsed
				}
			}
		}
	}
	return &generated.GestaltError{Code: code, Message: message}
}

func codeFromString(raw string) generated.GestaltErrorCode {
	for c := codes.OK; c <= codes.Unauthenticated; c++ {
		if c.String() == raw {
			return generated.GestaltErrorCode(c)
		}
	}
	return gestaltclient.GestaltErrorCodeUnknown
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
	if method.HTTPVerb == "" || method.HTTPPath == "" {
		return &generated.GestaltError{
			Code:    gestaltclient.GestaltErrorCodeInvalidArgument,
			Message: fmt.Sprintf("publicclient: method %s has no REST binding", method.FullMethod),
		}
	}

	requestMap, err := messageToJSONMap(request)
	if err != nil {
		return err
	}
	path, err := buildRestPath(method, requestMap)
	if err != nil {
		return err
	}
	target, err := joinURL(t.baseURL, path)
	if err != nil {
		return err
	}
	if query := buildRestQuery(method, requestMap).Encode(); query != "" {
		target += "?" + query
	}

	var body io.Reader
	switch strings.ToUpper(method.HTTPVerb) {
	case http.MethodGet, http.MethodDelete:
	default:
		payload := buildRestBody(method, requestMap)
		if payload != nil {
			encoded, err := json.Marshal(payload)
			if err != nil {
				return err
			}
			body = bytes.NewReader(encoded)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method.HTTPVerb, target, body)
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

	if resp.StatusCode >= 400 {
		return decodeGatewayError(resp.StatusCode, raw)
	}
	if response == nil {
		return nil
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := protoJSONUnmarshal.Unmarshal(raw, response); err != nil {
		return &generated.GestaltError{
			Code:    gestaltclient.GestaltErrorCodeInternal,
			Message: "response body does not match the expected schema",
		}
	}
	return nil
}

func messageToJSONMap(msg pb.Message) (map[string]any, error) {
	if msg == nil {
		return map[string]any{}, nil
	}
	data, err := protoJSONMarshal.Marshal(msg)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	if out == nil {
		return map[string]any{}, nil
	}
	return out, nil
}

// ServerStream is not supported by the REST transport; streaming public
// methods require the gRPC transport. Callers that construct a REST-only
// client never see streaming methods because REST-only clients expose only
// REST-bound methods. This stub exists so *restUnaryTransport satisfies the
// generated.UnaryTransport interface.
func (t *restUnaryTransport) ServerStream(
	_ context.Context,
	method generated.Method,
	_ pb.Message,
) (generated.ServerStreamRecvCloser, error) {
	return nil, &generated.GestaltError{
		Code:    gestaltclient.GestaltErrorCodeUnimplemented,
		Message: fmt.Sprintf("publicclient: method %s is not available on the REST transport; use a gRPC client", method.FullMethod),
	}
}
