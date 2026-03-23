package openapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/valon-technologies/gestalt/internal/provider"
)

const maxSpecSize = 100 << 20 // 100 MB

var defaultClient = &http.Client{Timeout: 30 * time.Second}

func LoadDefinition(ctx context.Context, name, specURL string, allowedOps map[string]string) (*provider.Definition, error) {
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
		def.BaseURL = strings.TrimRight(model.Model.Servers[0].URL, "/")
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

const (
	secTypeOAuth2 = "oauth2"
	secTypeAPIKey = "apiKey"
	secTypeHTTP   = "http"

	secInHeader = "header"
	secInQuery  = "query"
)

func extractAuth(model *v3high.Document, def *provider.Definition) {
	if model.Components == nil || model.Components.SecuritySchemes == nil {
		return
	}
	for pair := model.Components.SecuritySchemes.First(); pair != nil; pair = pair.Next() {
		ss := pair.Value()

		switch ss.Type {
		case secTypeOAuth2:
			if ss.Flows == nil {
				continue
			}
			def.Auth.Type = "oauth2"
			if flow := ss.Flows.AuthorizationCode; flow != nil {
				def.Auth.AuthorizationURL = flow.AuthorizationUrl
				def.Auth.TokenURL = flow.TokenUrl
				def.Auth.Scopes = extractScopes(flow.Scopes)
				return
			}
			if flow := ss.Flows.Implicit; flow != nil {
				def.Auth.AuthorizationURL = flow.AuthorizationUrl
				def.Auth.Scopes = extractScopes(flow.Scopes)
				return
			}

		case secTypeAPIKey:
			switch ss.In {
			case secInHeader:
				def.Auth.Type = "manual"
				def.AuthStyle = "raw"
				def.AuthHeader = ss.Name
			case secInQuery:
				def.Auth.Type = "manual"
				def.AuthStyle = "raw"
			default:
				continue
			}
			return

		case secTypeHTTP:
			def.Auth.Type = "manual"
			return
		}
	}
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

func collectOperationScopes(model *v3high.Document, allowedOps map[string]string) []string {
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

func extractOperations(model *v3high.Document, def *provider.Definition, allowedOps map[string]string) {
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
			if override, ok := allowedOps[op.OperationId]; ok && override != "" {
				desc = override
			}

			var params []provider.ParameterDef
			for _, p := range op.Parameters {
				pType := "string"
				if p.Schema != nil && p.Schema.Schema() != nil {
					if types := p.Schema.Schema().Type; len(types) > 0 {
						pType = types[0]
					}
				}
				params = append(params, provider.ParameterDef{
					Name:        p.Name,
					Type:        pType,
					Description: p.Description,
					Required:    p.Required != nil && *p.Required,
				})
			}

			if op.RequestBody != nil && op.RequestBody.Content != nil {
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
							Description: propSchema.Description,
							Required:    requiredSet[propPair.Key()],
						})
					}
					break
				}
			}

			def.Operations[op.OperationId] = provider.OperationDef{
				Description: desc,
				Method:      strings.ToUpper(method),
				Path:        path,
				Parameters:  params,
			}
		}
	}
}
