package openapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	highbase "github.com/pb33f/libopenapi/datamodel/high/base"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/valon-technologies/toolshed/core/catalog"
	ci "github.com/valon-technologies/toolshed/core/integration"

	"github.com/pb33f/libopenapi"
)

const (
	contentTypeJSON = "application/json"
)

// LoadCatalog produces a *Catalog directly from an OpenAPI spec, preserving
// nested JSON Schema for request bodies instead of flattening to parameters.
func LoadCatalog(ctx context.Context, name, specURL string, allowedOps map[string]string) (*catalog.Catalog, error) {
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

	cat := &catalog.Catalog{Name: name}

	if info := model.Model.Info; info != nil {
		cat.DisplayName = info.Title
		cat.Description = info.Description
	}
	if len(model.Model.Servers) > 0 {
		cat.BaseURL = strings.TrimRight(model.Model.Servers[0].URL, "/")
	}

	catalogExtractAuth(&model.Model, cat)
	catalogExtractOperations(&model.Model, cat, allowedOps)

	return cat, nil
}

func catalogExtractAuth(model *v3high.Document, cat *catalog.Catalog) {
	if model.Components == nil || model.Components.SecuritySchemes == nil {
		return
	}
	for pair := model.Components.SecuritySchemes.First(); pair != nil; pair = pair.Next() {
		ss := pair.Value()
		if ss.Type != "oauth2" || ss.Flows == nil {
			continue
		}
		cat.AuthStyle = "bearer"
		if flow := ss.Flows.AuthorizationCode; flow != nil {
			return
		}
	}
}

func catalogExtractOperations(model *v3high.Document, cat *catalog.Catalog, allowedOps map[string]string) {
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

			title := op.Summary
			desc := op.Description
			if desc == "" {
				desc = op.Summary
			}
			if override, ok := allowedOps[op.OperationId]; ok && override != "" {
				desc = override
			}

			upperMethod := strings.ToUpper(method)

			catOp := catalog.CatalogOperation{
				ID:          op.OperationId,
				Method:      upperMethod,
				Path:        path,
				Title:       title,
				Description: desc,
				Annotations: ci.AnnotationsFromMethod(upperMethod),
			}

			for _, p := range op.Parameters {
				pType := "string"
				if p.Schema != nil && p.Schema.Schema() != nil {
					if types := p.Schema.Schema().Type; len(types) > 0 {
						pType = types[0]
					}
				}
				loc := ""
				if p.In != "" {
					loc = p.In
				}
				catOp.Parameters = append(catOp.Parameters, catalog.CatalogParameter{
					Name:        p.Name,
					Type:        pType,
					Location:    loc,
					Description: p.Description,
					Required:    p.Required != nil && *p.Required,
				})
			}

			if op.RequestBody != nil && op.RequestBody.Content != nil {
				catOp.InputSchema = extractRequestBodySchema(op.RequestBody)
			}

			cat.Operations = append(cat.Operations, catOp)
		}
	}
}

func extractRequestBodySchema(rb *v3high.RequestBody) json.RawMessage {
	if rb.Content == nil {
		return nil
	}

	var schema *highbase.Schema
	for contentPair := rb.Content.First(); contentPair != nil; contentPair = contentPair.Next() {
		mt := contentPair.Value()
		if mt.Schema == nil || mt.Schema.Schema() == nil {
			continue
		}
		if contentPair.Key() == contentTypeJSON {
			schema = mt.Schema.Schema()
			break
		}
		if schema == nil {
			schema = mt.Schema.Schema()
		}
	}
	if schema == nil {
		return nil
	}

	return schemaToJSON(schema)
}

func schemaToJSON(s *highbase.Schema) json.RawMessage {
	if s == nil {
		return nil
	}

	m := make(map[string]any)

	if len(s.Type) > 0 {
		if len(s.Type) == 1 {
			m["type"] = s.Type[0]
		} else {
			m["type"] = s.Type
		}
	}

	if s.Description != "" {
		m["description"] = s.Description
	}

	if s.Properties != nil {
		props := make(map[string]json.RawMessage)
		for propPair := s.Properties.First(); propPair != nil; propPair = propPair.Next() {
			propSchema := propPair.Value().Schema()
			if propSchema == nil {
				continue
			}
			props[propPair.Key()] = schemaToJSON(propSchema)
		}
		if len(props) > 0 {
			m["properties"] = props
		}
	}

	if len(s.Required) > 0 {
		m["required"] = s.Required
	}

	if s.Items != nil && !s.Items.IsB() && s.Items.A != nil {
		itemSchema := s.Items.A.Schema()
		if itemSchema != nil {
			m["items"] = schemaToJSON(itemSchema)
		}
	}

	if len(s.Enum) > 0 {
		var enumVals []any
		for _, e := range s.Enum {
			if e != nil {
				enumVals = append(enumVals, e.Value)
			}
		}
		if len(enumVals) > 0 {
			m["enum"] = enumVals
		}
	}

	if s.Default != nil {
		m["default"] = s.Default.Value
	}

	if s.Format != "" {
		m["format"] = s.Format
	}

	data, _ := json.Marshal(m)
	return data
}
