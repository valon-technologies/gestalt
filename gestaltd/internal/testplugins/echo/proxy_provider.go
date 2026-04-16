package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
)

type proxyProvider struct {
	inner core.Provider
}

type invokePluginInput struct {
	Plugin        string         `json:"plugin"`
	Operation     string         `json:"operation"`
	Connection    string         `json:"connection,omitempty"`
	Instance      string         `json:"instance,omitempty"`
	RequestHandle string         `json:"request_handle,omitempty"`
	Params        map[string]any `json:"params,omitempty"`
}

func newProxyProvider(inner core.Provider) *proxyProvider {
	return &proxyProvider{inner: inner}
}

func (p *proxyProvider) Name() string                        { return p.inner.Name() }
func (p *proxyProvider) DisplayName() string                 { return p.inner.DisplayName() }
func (p *proxyProvider) Description() string                 { return p.inner.Description() }
func (p *proxyProvider) ConnectionMode() core.ConnectionMode { return p.inner.ConnectionMode() }
func (p *proxyProvider) AuthTypes() []string                 { return p.inner.AuthTypes() }
func (p *proxyProvider) AuthorizationURL(state string, scopes []string) string {
	return p.inner.AuthorizationURL(state, scopes)
}
func (p *proxyProvider) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	return p.inner.ExchangeCode(ctx, code)
}
func (p *proxyProvider) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	return p.inner.RefreshToken(ctx, refreshToken)
}
func (p *proxyProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return p.inner.ConnectionParamDefs()
}
func (p *proxyProvider) CredentialFields() []core.CredentialFieldDef {
	return p.inner.CredentialFields()
}
func (p *proxyProvider) DiscoveryConfig() *core.DiscoveryConfig {
	return p.inner.DiscoveryConfig()
}
func (p *proxyProvider) ConnectionForOperation(operation string) string {
	return p.inner.ConnectionForOperation(operation)
}
func (p *proxyProvider) Catalog() *catalog.Catalog {
	var cat *catalog.Catalog
	if inner := p.inner.Catalog(); inner != nil {
		cat = inner.Clone()
	} else {
		cat = &catalog.Catalog{
			Name:        p.Name(),
			DisplayName: p.DisplayName(),
			Description: p.Description(),
		}
	}
	cat.Operations = append(cat.Operations,
		catalog.CatalogOperation{
			ID:        "read_env",
			Method:    http.MethodGet,
			Transport: catalog.TransportPlugin,
			Parameters: []catalog.CatalogParameter{
				{Name: "name", Type: "string", Required: true},
			},
		},
		catalog.CatalogOperation{
			ID:        "read_file",
			Method:    http.MethodGet,
			Transport: catalog.TransportPlugin,
			Parameters: []catalog.CatalogParameter{
				{Name: "path", Type: "string", Required: true},
			},
		},
		catalog.CatalogOperation{
			ID:        "make_http_request",
			Method:    http.MethodGet,
			Transport: catalog.TransportPlugin,
			Parameters: []catalog.CatalogParameter{
				{Name: "url", Type: "string", Required: true},
			},
		},
		catalog.CatalogOperation{
			ID:        "invoke_plugin",
			Method:    http.MethodPost,
			Transport: catalog.TransportPlugin,
			Parameters: []catalog.CatalogParameter{
				{Name: "plugin", Type: "string", Description: "Target plugin name", Required: true},
				{Name: "operation", Type: "string", Description: "Target operation id", Required: true},
				{Name: "connection", Type: "string", Description: "Optional connection override"},
				{Name: "instance", Type: "string", Description: "Optional target instance override"},
				{Name: "request_handle", Type: "string", Description: "Optional request handle override for raw-provider compatibility"},
				{Name: "params", Type: "object", Description: "Nested params forwarded to the target operation"},
			},
		},
		catalog.CatalogOperation{
			ID:        "indexeddb_roundtrip",
			Method:    http.MethodPost,
			Transport: catalog.TransportPlugin,
			Parameters: []catalog.CatalogParameter{
				{Name: "binding", Type: "string"},
				{Name: "store", Type: "string", Required: true},
				{Name: "id", Type: "string", Required: true},
				{Name: "value", Type: "string", Required: true},
			},
		},
		catalog.CatalogOperation{
			ID:        "s3_roundtrip",
			Method:    http.MethodPost,
			Transport: catalog.TransportPlugin,
			Parameters: []catalog.CatalogParameter{
				{Name: "binding", Type: "string"},
				{Name: "bucket", Type: "string", Required: true},
				{Name: "key", Type: "string", Required: true},
				{Name: "value", Type: "string", Required: true},
			},
		},
	)
	return cat
}

func (p *proxyProvider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	switch operation {
	case "invoke_plugin":
		input, err := decodeInvokePluginInput(params)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		if strings.TrimSpace(input.Plugin) == "" {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": "plugin is required"}), nil
		}
		if strings.TrimSpace(input.Operation) == "" {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": "operation is required"}), nil
		}

		envelope := map[string]any{
			"ok":                       false,
			"target_plugin":            input.Plugin,
			"target_operation":         input.Operation,
			"used_connection_override": strings.TrimSpace(input.Connection) != "",
		}

		requestHandle := input.RequestHandle
		if requestHandle == "" {
			requestHandle = providerhost.RequestHandleFromContext(ctx)
		}
		if requestHandle == "" {
			envelope["error"] = "request handle is not available"
			return jsonResult(http.StatusOK, envelope), nil
		}

		invoker, err := gestalt.Invoker(requestHandle)
		if err != nil {
			envelope["error"] = err.Error()
			return jsonResult(http.StatusOK, envelope), nil
		}
		defer func() { _ = invoker.Close() }()

		connection := strings.TrimSpace(input.Connection)
		instance := strings.TrimSpace(input.Instance)
		var opts *gestalt.InvokeOptions
		if connection != "" || instance != "" {
			opts = &gestalt.InvokeOptions{
				Connection: connection,
				Instance:   instance,
			}
		}
		result, err := invoker.Invoke(ctx, input.Plugin, input.Operation, input.Params, opts)
		if err != nil {
			envelope["error"] = err.Error()
			return jsonResult(http.StatusOK, envelope), nil
		}
		envelope["ok"] = true
		envelope["status"] = result.Status
		envelope["body"] = decodeResultBody(result.Body)
		return jsonResult(http.StatusOK, envelope), nil

	case "read_env":
		name, _ := params["name"].(string)
		val, ok := os.LookupEnv(name)
		body, _ := json.Marshal(map[string]any{"name": name, "value": val, "found": ok})
		return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil

	case "read_file":
		path, _ := params["path"].(string)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsPermission(err) {
				body, _ := json.Marshal(map[string]any{"error": err.Error()})
				return &core.OperationResult{Status: http.StatusForbidden, Body: string(body)}, nil
			}
			if os.IsNotExist(err) {
				body, _ := json.Marshal(map[string]any{"error": err.Error()})
				return &core.OperationResult{Status: http.StatusNotFound, Body: string(body)}, nil
			}
			body, _ := json.Marshal(map[string]any{"error": err.Error()})
			return &core.OperationResult{Status: http.StatusInternalServerError, Body: string(body)}, nil
		}
		body, _ := json.Marshal(map[string]any{"content": string(data)})
		return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil

	case "make_http_request":
		targetURL, _ := params["url"].(string)
		client := &http.Client{}
		if proxyURL := os.Getenv("HTTP_PROXY"); proxyURL != "" {
			parsed, err := url.Parse(proxyURL)
			if err == nil {
				client.Transport = &http.Transport{Proxy: http.ProxyURL(parsed)}
			}
		}
		resp, err := client.Get(targetURL)
		if err != nil {
			body, _ := json.Marshal(map[string]any{"error": err.Error()})
			return &core.OperationResult{Status: http.StatusBadGateway, Body: string(body)}, nil
		}
		defer func() { _ = resp.Body.Close() }()
		respBody, _ := io.ReadAll(resp.Body)
		body, _ := json.Marshal(map[string]any{
			"status": resp.StatusCode,
			"body":   string(respBody),
		})
		return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil

	case "indexeddb_roundtrip":
		binding, _ := params["binding"].(string)
		store, _ := params["store"].(string)
		id, _ := params["id"].(string)
		value, _ := params["value"].(string)

		var (
			db  *gestalt.IndexedDBClient
			err error
		)
		if binding != "" {
			db, err = gestalt.IndexedDB(binding)
		} else {
			db, err = gestalt.IndexedDB()
		}
		if err != nil {
			return nil, err
		}
		defer func() { _ = db.Close() }()

		if err := db.CreateObjectStore(ctx, store, gestalt.ObjectStoreSchema{}); err != nil && !errors.Is(err, gestalt.ErrAlreadyExists) {
			return nil, err
		}
		if err := db.ObjectStore(store).Put(ctx, map[string]any{"id": id, "value": value}); err != nil {
			return nil, err
		}
		record, err := db.ObjectStore(store).Get(ctx, id)
		if err != nil {
			return nil, err
		}
		body, _ := json.Marshal(record)
		return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil

	case "s3_roundtrip":
		binding, _ := params["binding"].(string)
		bucket, _ := params["bucket"].(string)
		key, _ := params["key"].(string)
		value, _ := params["value"].(string)

		var (
			client *gestalt.S3Client
			err    error
		)
		if binding != "" {
			client, err = gestalt.S3(binding)
		} else {
			client, err = gestalt.S3()
		}
		if err != nil {
			return nil, err
		}
		defer func() { _ = client.Close() }()

		obj := client.Object(bucket, key)
		if _, err := obj.WriteString(ctx, value, &gestalt.WriteOptions{ContentType: "text/plain"}); err != nil {
			return nil, err
		}
		text, err := obj.Text(ctx, nil)
		if err != nil {
			return nil, err
		}
		stat, err := obj.Stat(ctx)
		if err != nil {
			return nil, err
		}
		page, err := client.ListObjects(ctx, gestalt.ListOptions{Bucket: bucket, Prefix: key})
		if err != nil {
			return nil, err
		}
		keys := make([]string, 0, len(page.Objects))
		for i := range page.Objects {
			keys = append(keys, page.Objects[i].Ref.Key)
		}
		body, _ := json.Marshal(map[string]any{
			"body":  text,
			"key":   stat.Ref.Key,
			"size":  stat.Size,
			"keys":  keys,
			"type":  stat.ContentType,
			"etag":  stat.ETag,
			"found": len(page.Objects) > 0,
		})
		return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil

	default:
		return p.inner.Execute(ctx, operation, params, token)
	}
}

func (p *proxyProvider) Close() error {
	return nil
}

func decodeInvokePluginInput(params map[string]any) (invokePluginInput, error) {
	if params == nil {
		params = map[string]any{}
	}
	var input invokePluginInput
	data, err := json.Marshal(params)
	if err != nil {
		return invokePluginInput{}, err
	}
	if err := json.Unmarshal(data, &input); err != nil {
		return invokePluginInput{}, err
	}
	return input, nil
}

func decodeResultBody(body string) any {
	var decoded any
	if err := json.Unmarshal([]byte(body), &decoded); err == nil {
		return decoded
	}
	return body
}

func jsonResult(status int, body any) *core.OperationResult {
	data, _ := json.Marshal(body)
	return &core.OperationResult{Status: status, Body: string(data)}
}
