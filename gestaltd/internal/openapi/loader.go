package openapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/provider"
)

const maxSpecSize = 100 << 20 // 100 MB

var defaultClient = &http.Client{Timeout: 30 * time.Second}

func LoadDefinition(ctx context.Context, name, specURL string, allowedOps map[string]*config.OperationOverride) (*provider.Definition, error) {
	body, err := fetch(ctx, specURL)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", specURL, err)
	}

	doc, err := libopenapi.NewDocument(body)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", specURL, err)
	}

	model, _ := doc.BuildV3Model()
	if model == nil {
		return nil, fmt.Errorf("could not build model for %s", specURL)
	}

	def := &provider.Definition{Provider: name}

	if info := model.Model.Info; info != nil {
		def.DisplayName = info.Title
		def.Description = provider.TruncateDescription(info.Description)
	}

	if len(model.Model.Servers) > 0 {
		serverURL := model.Model.Servers[0].URL
		if !strings.HasPrefix(serverURL, "http://") && !strings.HasPrefix(serverURL, "https://") {
			if base, err := url.Parse(specURL); err == nil {
				if ref, err := url.Parse(serverURL); err == nil {
					serverURL = base.ResolveReference(ref).String()
				}
			}
		}
		def.BaseURL = strings.TrimRight(serverURL, "/")
	}

	extractAuth(&model.Model, def)
	extractOperations(&model.Model, def, allowedOps)

	if len(def.Auth.Scopes) == 0 {
		def.Auth.Scopes = collectOperationScopes(&model.Model, allowedOps)
	}

	return def, nil
}

func fetch(ctx context.Context, specURL string) ([]byte, error) {
	if !strings.HasPrefix(specURL, "http://") && !strings.HasPrefix(specURL, "https://") {
		f, err := os.Open(strings.TrimPrefix(specURL, "file://"))
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		return io.ReadAll(io.LimitReader(f, maxSpecSize))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, specURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := defaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, specURL)
	}

	return io.ReadAll(io.LimitReader(resp.Body, maxSpecSize))
}

func extractScopes(scopes *orderedmap.Map[string, string]) []string {
	if scopes == nil {
		return nil
	}
	var result []string
	for pair := scopes.First(); pair != nil; pair = pair.Next() {
		result = append(result, pair.Key())
	}
	return result
}

func collectOperationScopes(model *v3high.Document, allowedOps map[string]*config.OperationOverride) []string {
	seen := make(map[string]struct{})
	collect := func(reqs []*base.SecurityRequirement) {
		for _, req := range reqs {
			if req.Requirements == nil {
				continue
			}
			for pair := req.Requirements.First(); pair != nil; pair = pair.Next() {
				for _, scope := range pair.Value() {
					seen[scope] = struct{}{}
				}
			}
		}
	}

	collect(model.Security)

	if model.Paths != nil && model.Paths.PathItems != nil {
		for pair := model.Paths.PathItems.First(); pair != nil; pair = pair.Next() {
			for _, op := range pair.Value().GetOperations().FromOldest() {
				if op.OperationId == "" {
					continue
				}
				if allowedOps != nil {
					if _, ok := allowedOps[op.OperationId]; !ok {
						continue
					}
				}
				collect(op.Security)
			}
		}
	}

	if len(seen) == 0 {
		return nil
	}
	result := make([]string, 0, len(seen))
	for scope := range seen {
		result = append(result, scope)
	}
	return result
}

func extractOperations(model *v3high.Document, def *provider.Definition, allowedOps map[string]*config.OperationOverride) {
	def.Operations = make(map[string]provider.OperationDef)

	if model.Paths == nil || model.Paths.PathItems == nil {
		return
	}

	for pair := model.Paths.PathItems.First(); pair != nil; pair = pair.Next() {
		path := pair.Key()
		pathItem := pair.Value()

		for method, op := range pathItem.GetOperations().FromOldest() {
			if op.OperationId == "" {
				continue
			}

			if allowedOps != nil {
				if _, ok := allowedOps[op.OperationId]; !ok {
					continue
				}
			}

			desc := op.Summary
			if desc == "" {
				desc = op.Description
			}
			opID := op.OperationId
			var allowedRoles []string
			if override := allowedOps[op.OperationId]; override != nil {
				if override.Description != "" {
					desc = override.Description
				}
				if override.Alias != "" {
					opID = override.Alias
				}
				allowedRoles = override.AllowedRoles
			}

			var params []provider.ParameterDef
			for _, p := range op.Parameters {
				params = append(params, definitionParamFromOpenAPI(p))
			}

			if op.RequestBody != nil && op.RequestBody.Content != nil {
				seen := make(map[string]struct{}, len(params))
				for _, p := range params {
					seen[p.Name] = struct{}{}
				}
				for contentPair := op.RequestBody.Content.First(); contentPair != nil; contentPair = contentPair.Next() {
					mt := contentPair.Value()
					if mt.Schema == nil || mt.Schema.Schema() == nil {
						continue
					}
					schema := mt.Schema.Schema()
					if schema.Properties == nil {
						continue
					}
					requiredSet := make(map[string]bool, len(schema.Required))
					for _, r := range schema.Required {
						requiredSet[r] = true
					}
					for propPair := schema.Properties.First(); propPair != nil; propPair = propPair.Next() {
						if _, exists := seen[propPair.Key()]; exists {
							continue
						}
						propSchema := propPair.Value().Schema()
						if propSchema == nil {
							continue
						}
						pType := "string"
						if len(propSchema.Type) > 0 {
							pType = propSchema.Type[0]
						}
						params = append(params, provider.ParameterDef{
							Name:        propPair.Key(),
							Type:        pType,
							Location:    "body",
							Description: propSchema.Description,
							Required:    requiredSet[propPair.Key()],
						})
					}
					break
				}
			}

			def.Operations[opID] = provider.OperationDef{
				Description:  desc,
				Method:       strings.ToUpper(method),
				Path:         path,
				AllowedRoles: allowedRoles,
				Parameters:   params,
			}
		}
	}
}

func definitionParamFromOpenAPI(p *v3high.Parameter) provider.ParameterDef {
	paramType := "string"
	if p.Schema != nil && p.Schema.Schema() != nil {
		if types := p.Schema.Schema().Type; len(types) > 0 {
			paramType = types[0]
		}
	}
	name, wireName := normalizeParamName(p.Name)
	return provider.ParameterDef{
		Name:        name,
		WireName:    wireName,
		Type:        paramType,
		Location:    p.In,
		Description: p.Description,
		Required:    p.Required != nil && *p.Required,
	}
}

func normalizeParamName(raw string) (name, wireName string) {
	if !strings.ContainsAny(raw, "[]") {
		return raw, ""
	}
	normalized := strings.ReplaceAll(strings.ReplaceAll(raw, "[", "_"), "]", "")
	return normalized, raw
}
