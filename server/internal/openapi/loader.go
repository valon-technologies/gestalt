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
	"unicode"

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
	if extractSecurityRequirementsAuth(model, def) {
		return
	}
	for pair := model.Components.SecuritySchemes.First(); pair != nil; pair = pair.Next() {
		if applySecuritySchemeAuth(def, pair.Key(), pair.Value()) {
			return
		}
	}
}

func extractSecurityRequirementsAuth(model *v3high.Document, def *provider.Definition) bool {
	if len(model.Security) == 0 || model.Components == nil || model.Components.SecuritySchemes == nil {
		return false
	}

	securitySchemes := make(map[string]*v3high.SecurityScheme)
	for pair := model.Components.SecuritySchemes.First(); pair != nil; pair = pair.Next() {
		securitySchemes[pair.Key()] = pair.Value()
	}

	for _, req := range model.Security {
		namedSchemes := namedSecuritySchemes(req, securitySchemes)
		if len(namedSchemes) == 0 {
			continue
		}
		if applyNamedSecuritySchemesAuth(def, namedSchemes) {
			return true
		}
	}

	return false
}

type namedSecurityScheme struct {
	name   string
	scheme *v3high.SecurityScheme
}

func namedSecuritySchemes(req *base.SecurityRequirement, securitySchemes map[string]*v3high.SecurityScheme) []namedSecurityScheme {
	if req == nil || req.Requirements == nil {
		return nil
	}

	var named []namedSecurityScheme
	for pair := req.Requirements.First(); pair != nil; pair = pair.Next() {
		scheme := securitySchemes[pair.Key()]
		if scheme == nil {
			return nil
		}
		named = append(named, namedSecurityScheme{
			name:   pair.Key(),
			scheme: scheme,
		})
	}
	return named
}

func applyNamedSecuritySchemesAuth(def *provider.Definition, schemes []namedSecurityScheme) bool {
	if len(schemes) == 0 {
		return false
	}

	if applyAPIKeySecurityAuth(def, schemes) {
		return true
	}

	if len(schemes) != 1 {
		return false
	}

	return applySecuritySchemeAuth(def, schemes[0].name, schemes[0].scheme)
}

func applyAPIKeySecurityAuth(def *provider.Definition, schemes []namedSecurityScheme) bool {
	if len(schemes) == 0 {
		return false
	}

	authMapping := &provider.AuthMappingDef{Headers: make(map[string]string, len(schemes))}
	credentialFields := make([]provider.CredentialFieldDef, 0, len(schemes))
	for _, named := range schemes {
		if named.scheme == nil || named.scheme.Type != secTypeAPIKey || named.scheme.In != secInHeader {
			return false
		}
		field := credentialFieldForSecurityScheme(named.name, named.scheme)
		credentialFields = append(credentialFields, field)
		authMapping.Headers[named.scheme.Name] = field.Name
	}

	def.Auth.Type = "manual"
	if len(schemes) == 1 {
		def.AuthStyle = "raw"
		def.AuthHeader = schemes[0].scheme.Name
		def.CredentialFields = credentialFields
		return true
	}

	def.AuthMapping = authMapping
	def.CredentialFields = credentialFields
	return true
}

func applySecuritySchemeAuth(def *provider.Definition, schemeName string, ss *v3high.SecurityScheme) bool {
	if ss == nil {
		return false
	}

	switch ss.Type {
	case secTypeOAuth2:
		if ss.Flows == nil {
			return false
		}
		def.Auth.Type = "oauth2"
		if flow := ss.Flows.AuthorizationCode; flow != nil {
			def.Auth.AuthorizationURL = flow.AuthorizationUrl
			def.Auth.TokenURL = flow.TokenUrl
			def.Auth.Scopes = extractScopes(flow.Scopes)
			return true
		}
		if flow := ss.Flows.Implicit; flow != nil {
			def.Auth.AuthorizationURL = flow.AuthorizationUrl
			def.Auth.Scopes = extractScopes(flow.Scopes)
			return true
		}
		return false

	case secTypeAPIKey:
		switch ss.In {
		case secInHeader:
			def.Auth.Type = "manual"
			def.AuthStyle = "raw"
			def.AuthHeader = ss.Name
			def.CredentialFields = []provider.CredentialFieldDef{credentialFieldForSecurityScheme(schemeName, ss)}
			return true
		case secInQuery:
			def.Auth.Type = "manual"
			def.AuthStyle = "raw"
			def.CredentialFields = []provider.CredentialFieldDef{credentialFieldForSecurityScheme(schemeName, ss)}
			return true
		default:
			return false
		}

	case secTypeHTTP:
		def.Auth.Type = "manual"
		switch strings.ToLower(ss.Scheme) {
		case "basic":
			def.AuthStyle = "basic"
		case "bearer":
			def.AuthStyle = ""
		default:
			return false
		}
		def.CredentialFields = []provider.CredentialFieldDef{credentialFieldForSecurityScheme(schemeName, ss)}
		return true
	}

	return false
}

func credentialFieldForSecurityScheme(schemeName string, ss *v3high.SecurityScheme) provider.CredentialFieldDef {
	return provider.CredentialFieldDef{
		Name:        normalizeSecuritySchemeFieldName(schemeName),
		Label:       humanizeCredentialFieldLabel(schemeName),
		Description: provider.TruncateDescription(ss.Description),
	}
}

func normalizeSecuritySchemeFieldName(raw string) string {
	if raw == "" {
		return "token"
	}

	var b strings.Builder
	lastUnderscore := false
	for i, r := range raw {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if unicode.IsUpper(r) {
				if i > 0 && !lastUnderscore {
					b.WriteByte('_')
				}
				r = unicode.ToLower(r)
			}
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}

	name := strings.Trim(b.String(), "_")
	if name == "" {
		return "token"
	}
	return name
}

func humanizeCredentialFieldLabel(raw string) string {
	name := normalizeSecuritySchemeFieldName(raw)
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '_' || r == '-' })
	if len(parts) == 0 {
		return "Token"
	}

	for i, part := range parts {
		switch strings.ToLower(part) {
		case "api":
			parts[i] = "API"
		case "id":
			parts[i] = "ID"
		case "oauth":
			parts[i] = "OAuth"
		case "url":
			parts[i] = "URL"
		default:
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, " ")
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
			if override := allowedOps[op.OperationId]; override != nil {
				if override.Description != "" {
					desc = override.Description
				}
				if override.Alias != "" {
					opID = override.Alias
				}
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
				Description: desc,
				Method:      strings.ToUpper(method),
				Path:        path,
				Parameters:  params,
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
