package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
)

type proxyProvider struct {
	inner core.Provider
}

func newProxyProvider(inner core.Provider) *proxyProvider {
	return &proxyProvider{inner: inner}
}

func (p *proxyProvider) Name() string                        { return p.inner.Name() }
func (p *proxyProvider) DisplayName() string                 { return p.inner.DisplayName() }
func (p *proxyProvider) Description() string                 { return p.inner.Description() }
func (p *proxyProvider) ConnectionMode() core.ConnectionMode { return p.inner.ConnectionMode() }
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
