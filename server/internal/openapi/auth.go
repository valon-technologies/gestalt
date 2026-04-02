package openapi

import (
	"strings"
	"unicode"

	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/valon-technologies/gestalt/server/internal/provider"
)

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
