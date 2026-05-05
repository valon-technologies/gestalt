package declarative

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"reflect"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/plugins/apiexec"
)

const maxPostConnectResponseSize = 5 * 1024 * 1024

func (b *Base) SupportsPostConnect() bool {
	return b != nil && len(b.PostConnectConfigs) > 0
}

func (b *Base) PostConnect(ctx context.Context, token *core.ExternalCredential) (map[string]string, error) {
	if b == nil || len(b.PostConnectConfigs) == 0 {
		return nil, core.ErrPostConnectUnsupported
	}
	if token == nil {
		return nil, fmt.Errorf("post-connect token is required")
	}
	cfg := b.PostConnectConfigs[strings.TrimSpace(token.Connection)]
	if cfg == nil {
		return nil, core.ErrPostConnectUnsupported
	}
	return b.runDeclarativePostConnect(ctx, cfg, token.AccessToken)
}

func (b *Base) runDeclarativePostConnect(ctx context.Context, cfg *core.PostConnectConfig, accessToken string) (map[string]string, error) {
	method := strings.ToUpper(strings.TrimSpace(cfg.Request.Method))
	switch method {
	case http.MethodGet, http.MethodPost:
	default:
		return nil, fmt.Errorf("post-connect request method %q is not supported", cfg.Request.Method)
	}
	rawURL := strings.TrimSpace(cfg.Request.URL)
	if rawURL == "" {
		return nil, fmt.Errorf("post-connect request URL is required")
	}
	if err := b.checkEgressHost(rawURL); err != nil {
		return nil, err
	}

	credential, err := b.materializeCredential(accessToken)
	if err != nil {
		return nil, err
	}
	staticHeaders := maps.Clone(b.Headers)
	for k, v := range cfg.Request.Headers {
		if staticHeaders == nil {
			staticHeaders = make(map[string]string, len(cfg.Request.Headers))
		}
		staticHeaders[k] = v
	}
	headers := egress.ApplyHeaderMutations(staticHeaders, credential.Headers)

	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create post-connect request: %w", err)
	}
	if credential.Authorization != "" {
		req.Header.Set("Authorization", credential.Authorization)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := b.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute post-connect request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPostConnectResponseSize))
	if err != nil {
		return nil, fmt.Errorf("read post-connect response: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("post-connect request failed: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse post-connect response: %w", err)
	}
	if err := validatePostConnectSuccess(raw, cfg.Success); err != nil {
		return nil, err
	}
	source, err := postConnectSource(raw, cfg.SourcePath)
	if err != nil {
		return nil, err
	}
	return renderPostConnectMetadata(source, cfg)
}

func validatePostConnectSuccess(raw any, success *core.PostConnectSuccessCheck) error {
	if success == nil {
		return nil
	}
	value, ok := apiexec.ExtractJSONPath(raw, strings.TrimSpace(success.Path))
	if !ok {
		return fmt.Errorf("post-connect success path %q not found", success.Path)
	}
	if !reflect.DeepEqual(value, success.Equals) {
		return fmt.Errorf("post-connect success check failed at %q", success.Path)
	}
	return nil
}

func postConnectSource(raw any, sourcePath string) (map[string]any, error) {
	source := raw
	if strings.TrimSpace(sourcePath) != "" {
		value, ok := apiexec.ExtractJSONPath(raw, strings.TrimSpace(sourcePath))
		if !ok {
			return nil, fmt.Errorf("post-connect source path %q not found", sourcePath)
		}
		source = value
	}
	obj, ok := source.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("post-connect source path %q is not an object", sourcePath)
	}
	return obj, nil
}

func renderPostConnectMetadata(source map[string]any, cfg *core.PostConnectConfig) (map[string]string, error) {
	metadata := make(map[string]string)
	if cfg.ExternalIdentity != nil {
		typ := strings.TrimSpace(cfg.ExternalIdentity.Type)
		id, err := renderPostConnectTemplate(cfg.ExternalIdentity.ID, source)
		if err != nil {
			return nil, fmt.Errorf("post-connect external identity id: %w", err)
		}
		if typ != "" {
			metadata["gestalt.external_identity.type"] = typ
		}
		if id != "" {
			metadata["gestalt.external_identity.id"] = id
		}
	}
	for key, expr := range cfg.Metadata {
		value, err := renderPostConnectMapping(expr, source)
		if err != nil {
			return nil, fmt.Errorf("post-connect metadata %q: %w", key, err)
		}
		metadata[key] = value
	}
	return metadata, nil
}

func renderPostConnectMapping(expr string, source map[string]any) (string, error) {
	if strings.Contains(expr, "{") || strings.Contains(expr, "}") {
		return renderPostConnectTemplate(expr, source)
	}
	value, ok := apiexec.ExtractJSONPath(source, strings.TrimSpace(expr))
	if !ok {
		return "", fmt.Errorf("path %q not found", expr)
	}
	return postConnectValueString(value)
}

func renderPostConnectTemplate(tmpl string, source map[string]any) (string, error) {
	var out strings.Builder
	for {
		start := strings.Index(tmpl, "{")
		if start < 0 {
			out.WriteString(tmpl)
			return out.String(), nil
		}
		end := strings.Index(tmpl[start+1:], "}")
		if end < 0 {
			return "", fmt.Errorf("unclosed template placeholder")
		}
		end += start + 1
		out.WriteString(tmpl[:start])
		path := strings.TrimSpace(tmpl[start+1 : end])
		if path == "" {
			return "", fmt.Errorf("empty template placeholder")
		}
		value, ok := apiexec.ExtractJSONPath(source, path)
		if !ok {
			return "", fmt.Errorf("path %q not found", path)
		}
		rendered, err := postConnectValueString(value)
		if err != nil {
			return "", err
		}
		out.WriteString(rendered)
		tmpl = tmpl[end+1:]
	}
}

func postConnectValueString(value any) (string, error) {
	switch v := value.(type) {
	case nil:
		return "", fmt.Errorf("value is null")
	case string:
		return v, nil
	case bool:
		return fmt.Sprintf("%t", v), nil
	case float64:
		return fmt.Sprintf("%v", v), nil
	default:
		return "", fmt.Errorf("value has unsupported type %T", value)
	}
}
