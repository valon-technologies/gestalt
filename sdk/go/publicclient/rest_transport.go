package publicclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/sdk/go/publicclient/generated"
	gproto "google.golang.org/protobuf/encoding/protojson"
	pb "google.golang.org/protobuf/proto"
)

// RESTTransport performs protobuf-JSON calls against the public /api/v2 surface.
type RESTTransport struct {
	BaseURL string
	Auth    Auth
	Client  *http.Client
}

// CallUnary implements generated.RESTCaller.
func (t *RESTTransport) CallUnary(
	ctx context.Context,
	method generated.Method,
	request pb.Message,
	response pb.Message,
) (pb.Message, error) {
	if t == nil {
		return nil, &generated.GestaltError{
			Code:    generated.GestaltErrorCodeInvalidArgument,
			Message: "publicclient: REST transport is nil",
		}
	}

	pathParams := pathParamNamesFromTemplate(method.HTTPPath)
	path, err := substitutePath(method.HTTPPath, request)
	if err != nil {
		return nil, err
	}
	target, err := joinURL(t.BaseURL, path)
	if err != nil {
		return nil, err
	}

	requestMap, err := messageToJSONMap(request)
	if err != nil {
		return nil, err
	}

	var body io.Reader
	switch strings.ToUpper(method.HTTPVerb) {
	case http.MethodGet, http.MethodDelete:
		query, err := buildQueryValues(requestMap, pathParams)
		if err != nil {
			return nil, err
		}
		if encoded := query.Encode(); encoded != "" {
			target += "?" + encoded
		}
	default:
		payload, err := buildBodyMap(requestMap, pathParams)
		if err != nil {
			return nil, err
		}
		if len(payload) > 0 {
			encoded, err := json.Marshal(payload)
			if err != nil {
				return nil, err
			}
			body = bytes.NewReader(encoded)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method.HTTPVerb, target, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	meta := &Request{Headers: map[string]string{}}
	if t.Auth != nil {
		if err := t.Auth.Apply(ctx, meta); err != nil {
			return nil, err
		}
	}
	for key, value := range meta.Headers {
		req.Header.Set(key, value)
	}

	client := t.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, &generated.GestaltError{
			Code:    generated.GestaltErrorCodeUnavailable,
			Message: err.Error(),
		}
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if IsOperationResult(resp.Header) {
		if response == nil {
			if resp.StatusCode >= 400 {
				return nil, DecodeGatewayError(resp.StatusCode, raw)
			}
			return nil, nil
		}
		return fillOperationResult(resp.StatusCode, raw, resp.Header, response)
	}

	if resp.StatusCode >= 400 {
		return nil, DecodeGatewayError(resp.StatusCode, raw)
	}
	if response == nil {
		return nil, nil
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return response, nil
	}
	if err := gproto.Unmarshal(raw, response); err != nil {
		return nil, err
	}
	return response, nil
}

func messageToJSONMap(msg pb.Message) (map[string]any, error) {
	if msg == nil {
		return map[string]any{}, nil
	}
	data, err := gproto.Marshal(msg)
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

func buildBodyMap(fields map[string]any, pathParamNames []string) (map[string]any, error) {
	pathSet := make(map[string]struct{}, len(pathParamNames)*2)
	for _, name := range pathParamNames {
		pathSet[name] = struct{}{}
		pathSet[toCamelCase(name)] = struct{}{}
	}
	body := make(map[string]any, len(fields))
	for key, value := range fields {
		if value == nil {
			continue
		}
		if _, ok := pathSet[key]; ok {
			continue
		}
		if _, ok := pathSet[toSnakeCase(key)]; ok {
			continue
		}
		body[key] = value
	}
	return body, nil
}

func substitutePath(pattern string, request pb.Message) (string, error) {
	fields, err := messageToJSONMap(request)
	if err != nil {
		return "", err
	}
	out := pattern
	for key, value := range fields {
		if value == nil {
			continue
		}
		placeholder := "{" + key + "}"
		if strings.Contains(out, placeholder) {
			out = strings.ReplaceAll(out, placeholder, url.PathEscape(fmt.Sprint(value)))
		}
		snake := toSnakeCase(key)
		placeholder = "{" + snake + "}"
		if strings.Contains(out, placeholder) {
			out = strings.ReplaceAll(out, placeholder, url.PathEscape(fmt.Sprint(value)))
		}
	}
	if strings.Contains(out, "{") {
		return "", &generated.GestaltError{
			Code:    generated.GestaltErrorCodeInvalidArgument,
			Message: fmt.Sprintf("missing path parameter in %q", out),
		}
	}
	return out, nil
}

func fillOperationResult(
	statusCode int,
	body []byte,
	headers http.Header,
	response pb.Message,
) (pb.Message, error) {
	wire, ok := response.(*proto.OperationResult)
	if !ok {
		if len(bytes.TrimSpace(body)) == 0 {
			return response, nil
		}
		if err := gproto.Unmarshal(body, response); err != nil {
			return nil, err
		}
		return response, nil
	}
	wire.Status = int32(statusCode)
	wire.Body = append([]byte(nil), body...)
	wire.Headers = flattenHeadersToProto(headers)
	return wire, nil
}

func flattenHeadersToProto(headers http.Header) map[string]*proto.StringList {
	out := make(map[string]*proto.StringList, len(headers))
	for key, values := range headers {
		if strings.EqualFold(key, responseKindHeader) {
			continue
		}
		if len(values) == 0 {
			continue
		}
		out[key] = &proto.StringList{Values: append([]string(nil), values...)}
	}
	return out
}

func joinURL(baseURL, path string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return "", fmt.Errorf("publicclient: base URL is required")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u, err := url.Parse(base + path)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func normalizeAddress(address string) (string, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", fmt.Errorf("publicclient: address is required")
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("publicclient: invalid address %q", address)
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func toSnakeCase(name string) string {
	var b strings.Builder
	for i, r := range name {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r + ('a' - 'A'))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func toCamelCase(name string) string {
	parts := strings.Split(name, "_")
	if len(parts) == 0 {
		return name
	}
	out := parts[0]
	for _, part := range parts[1:] {
		if part == "" {
			continue
		}
		out += strings.ToUpper(part[:1]) + part[1:]
	}
	return out
}

func pathParamNamesFromTemplate(pathTemplate string) []string {
	var names []string
	start := 0
	for {
		open := strings.Index(pathTemplate[start:], "{")
		if open < 0 {
			break
		}
		open += start
		close := strings.Index(pathTemplate[open:], "}")
		if close < 0 {
			break
		}
		close += open
		names = append(names, pathTemplate[open+1:close])
		start = close + 1
	}
	return names
}

func buildQueryValues(msg map[string]any, pathParamNames []string) (url.Values, error) {
	pathSet := make(map[string]struct{}, len(pathParamNames)*2)
	for _, name := range pathParamNames {
		pathSet[name] = struct{}{}
		pathSet[toCamelCase(name)] = struct{}{}
	}
	values := url.Values{}
	for key, value := range msg {
		if value == nil {
			continue
		}
		if _, ok := pathSet[key]; ok {
			continue
		}
		if _, ok := pathSet[toSnakeCase(key)]; ok {
			continue
		}
		encodeQueryValue(values, key, value)
	}
	return values, nil
}

func encodeQueryValue(values url.Values, key string, value any) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			encodeQueryValue(values, key, item)
		}
	case []string:
		for _, item := range typed {
			values.Add(key, item)
		}
	case map[string]any:
		for nestedKey, nestedValue := range typed {
			encodeQueryValue(values, key+"."+nestedKey, nestedValue)
		}
	default:
		values.Add(key, fmt.Sprint(value))
	}
}
