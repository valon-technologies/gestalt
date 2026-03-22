package provider

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/core"
	ci "github.com/valon-technologies/gestalt/core/integration"
	"github.com/valon-technologies/gestalt/internal/apiexec"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/integration"
	"github.com/valon-technologies/gestalt/internal/oauth"
)

func Build(def *Definition, intg config.IntegrationDef, allowedOperations map[string]string) (core.Provider, error) {
	d := *def // shallow copy so we don't mutate the caller's definition
	def = &d
	if err := applyOverrides(def, intg); err != nil {
		return nil, fmt.Errorf("%s: %w", def.Provider, err)
	}

	baseURL := def.BaseURL
	if intg.BaseURL != "" {
		baseURL = intg.BaseURL
	}

	client := &http.Client{Timeout: 10 * time.Second}

	auth, err := buildAuth(def, intg, baseURL, client)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", def.Provider, err)
	}

	cat := CatalogFromDefinition(def)
	cat.BaseURL = baseURL
	ci.CompileSchemas(cat)

	endpoints, err := ci.EndpointsMap(cat)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", def.Provider, err)
	}

	base := &ci.Base{
		IntegrationName:    def.Provider,
		IntegrationDisplay: def.DisplayName,
		IntegrationDesc:    def.Description,
		Auth:               auth,
		BaseURL:            baseURL,
		HTTPClient:         client,
		Operations:         ci.OperationsList(cat),
		Endpoints:          endpoints,
		Queries:            ci.QueriesMap(cat),
		Headers:            def.Headers,
		Pagination:         buildPaginationConfigs(def),
	}

	connMode := def.ConnectionMode
	if intg.ConnectionMode != "" {
		connMode = intg.ConnectionMode
	}
	switch connMode {
	case "", "none", "user", "identity", "either":
		if connMode != "" {
			base.ConnMode = core.ConnectionMode(connMode)
		}
	default:
		return nil, fmt.Errorf("%s: unknown connection_mode %q", def.Provider, connMode)
	}

	switch def.AuthStyle {
	case "", "bearer":
	case "raw":
		base.AuthStyle = ci.AuthStyleRaw
	case "none":
		base.AuthStyle = ci.AuthStyleNone
	default:
		return nil, fmt.Errorf("%s: unknown auth_style %q", def.Provider, def.AuthStyle)
	}

	if def.ResponseCheck != "" {
		checker, ok := lookupResponseChecker(def.ResponseCheck)
		if !ok {
			return nil, fmt.Errorf("%s: unknown response_check %q", def.Provider, def.ResponseCheck)
		}
		base.CheckResponse = checker
	} else if def.ErrorMessagePath != "" {
		msgPath := def.ErrorMessagePath
		base.CheckResponse = func(status int, body []byte) error {
			if status < 400 {
				return nil
			}
			var data map[string]any
			if err := json.Unmarshal(body, &data); err == nil {
				if msg, ok := extractJSONPath(data, msgPath); ok {
					return fmt.Errorf("HTTP %d: %s", status, msg)
				}
			}
			return fmt.Errorf("HTTP %d: %s", status, body)
		}
	}

	switch {
	case def.TokenParser != "":
		parser, ok := lookupTokenParser(def.TokenParser)
		if !ok {
			return nil, fmt.Errorf("%s: unknown token_parser %q", def.Provider, def.TokenParser)
		}
		base.TokenParser = parser
	case def.AuthMapping != nil && len(def.AuthMapping.Headers) > 0:
		mapping := def.AuthMapping.Headers
		base.TokenParser = func(token string) (string, map[string]string, error) {
			var tokenData map[string]any
			if err := json.Unmarshal([]byte(token), &tokenData); err != nil {
				return "", nil, fmt.Errorf("parsing token as JSON for auth_mapping: %w", err)
			}
			headers := make(map[string]string, len(mapping))
			for headerName, jsonField := range mapping {
				val, ok := tokenData[jsonField]
				if !ok || val == nil {
					return "", nil, fmt.Errorf("auth_mapping: token field %q for header %q is missing or null", jsonField, headerName)
				}
				headers[headerName] = fmt.Sprintf("%v", val)
			}
			return "", headers, nil
		}
	case def.AuthHeader != "":
		headerName := def.AuthHeader
		base.TokenParser = func(token string) (string, map[string]string, error) {
			return "", map[string]string{headerName: token}, nil
		}
	case def.TokenPrefix != "":
		prefix := def.TokenPrefix
		base.TokenParser = func(token string) (string, map[string]string, error) {
			return prefix + token, nil, nil
		}
	}

	if def.RequestMutator != "" {
		mutator, ok := lookupRequestMutator(def.RequestMutator)
		if !ok {
			return nil, fmt.Errorf("%s: unknown request_mutator %q", def.Provider, def.RequestMutator)
		}
		base.RequestMutator = mutator
	}

	base.SetCatalog(cat)

	var result core.Provider = base

	if ops := allowedOperations; ops != nil {
		if len(ops) == 0 {
			return nil, fmt.Errorf("%s: allowed_operations cannot be empty; omit the field to allow all", def.Provider)
		}
		opSet := make(map[string]struct{}, len(base.Operations))
		for _, op := range base.Operations {
			opSet[op.Name] = struct{}{}
		}
		allowed := make([]string, 0, len(ops))
		for opName, desc := range ops {
			if _, ok := opSet[opName]; !ok {
				return nil, fmt.Errorf("%s: allowed_operations contains unknown operation %q", def.Provider, opName)
			}
			if desc != "" {
				for i := range base.Operations {
					if base.Operations[i].Name == opName {
						base.Operations[i].Description = desc
						break
					}
				}
				for i := range cat.Operations {
					if cat.Operations[i].ID == opName {
						cat.Operations[i].Description = desc
						break
					}
				}
			}
			allowed = append(allowed, opName)
		}
		result = integration.NewRestricted(result, allowed)
	}

	return result, nil
}

func applyOverrides(def *Definition, intg config.IntegrationDef) error {
	setStr(&def.DisplayName, intg.DisplayName)
	setStr(&def.Description, intg.Description)
	o := intg.Auth
	setStr(&def.Auth.Type, o.Type)
	setStr(&def.Auth.AuthorizationURL, o.AuthorizationURL)
	setStr(&def.Auth.TokenURL, o.TokenURL)
	setStr(&def.Auth.ClientAuth, o.ClientAuth)
	setStr(&def.Auth.TokenExchange, o.TokenExchange)
	if o.Scopes != nil {
		def.Auth.Scopes = o.Scopes
	}
	setStr(&def.Auth.ScopeSeparator, o.ScopeSeparator)
	setStr(&def.Auth.AcceptHeader, o.AcceptHeader)
	setStr(&def.Auth.ResponseHook, o.ResponseHook)
	if o.PKCE {
		def.Auth.PKCE = true
	}
	if o.AuthorizationParams != nil {
		def.Auth.AuthorizationParams = o.AuthorizationParams
	}
	if o.TokenParams != nil {
		def.Auth.TokenParams = o.TokenParams
	}
	if o.RefreshParams != nil {
		def.Auth.RefreshParams = o.RefreshParams
	}
	if o.TokenMetadata != nil {
		def.Auth.TokenMetadata = o.TokenMetadata
	}

	setStr(&def.AuthHeader, intg.Auth.AuthHeader)
	setStr(&def.AuthHeader, intg.AuthHeader)
	if intg.AuthMapping != nil {
		def.AuthMapping = &AuthMappingDef{Headers: intg.AuthMapping.Headers}
	}
	setStr(&def.ErrorMessagePath, intg.ErrorMessagePath)
	setStr(&def.ResponseCheck, intg.ResponseCheck)
	setStr(&def.TokenParser, intg.TokenParser)
	setStr(&def.RequestMutator, intg.RequestMutator)
	setStr(&def.TokenPrefix, intg.TokenPrefix)
	setStr(&def.AuthStyle, intg.AuthStyle)
	if intg.IconFile != "" {
		data, err := os.ReadFile(intg.IconFile)
		if err != nil {
			log.Printf("WARNING: could not read icon_file %q: %v", intg.IconFile, err)
		} else {
			def.IconSVG = strings.TrimSpace(string(data))
		}
	}
	if intg.Headers != nil {
		def.Headers = intg.Headers
	}
	return nil
}

func setStr(dst *string, val string) {
	if val != "" {
		*dst = val
	}
}

func buildAuth(def *Definition, intg config.IntegrationDef, baseURL string, client *http.Client) (ci.AuthHandler, error) {
	if def.Auth.Type == "manual" || (def.Auth.Type == "" && def.Auth.AuthorizationURL == "") {
		return oauth.ManualAuthHandler{}, nil
	}

	var tokenExchange oauth.TokenExchangeFormat
	switch def.Auth.TokenExchange {
	case "", "form":
		tokenExchange = oauth.TokenExchangeForm
	case "json":
		tokenExchange = oauth.TokenExchangeJSON
	default:
		return nil, fmt.Errorf("unknown token_exchange %q", def.Auth.TokenExchange)
	}

	authURL := resolveURL(baseURL, def.Auth.AuthorizationURL)
	tokenURL := resolveURL(baseURL, def.Auth.TokenURL)

	oauthCfg := oauth.UpstreamConfig{
		ClientID:            intg.ClientID,
		ClientSecret:        intg.ClientSecret,
		AuthorizationURL:    authURL,
		TokenURL:            tokenURL,
		RedirectURL:         intg.RedirectURL,
		PKCE:                def.Auth.PKCE,
		DefaultScopes:       def.Auth.Scopes,
		ScopeSeparator:      def.Auth.ScopeSeparator,
		AuthorizationParams: def.Auth.AuthorizationParams,
		TokenParams:         def.Auth.TokenParams,
		RefreshParams:       def.Auth.RefreshParams,
		TokenExchange:       tokenExchange,
		AcceptHeader:        def.Auth.AcceptHeader,
	}

	if def.Auth.ClientAuth == "header" {
		oauthCfg.ClientAuthMethod = oauth.ClientAuthHeader
	}

	var opts []oauth.Option
	opts = append(opts, oauth.WithHTTPClient(client))

	if def.Auth.ResponseHook != "" {
		hook, ok := lookupResponseHook(def.Auth.ResponseHook)
		if !ok {
			return nil, fmt.Errorf("unknown auth response_hook %q", def.Auth.ResponseHook)
		}
		opts = append(opts, oauth.WithResponseHook(hook))
	}

	upstream := oauth.NewUpstream(oauthCfg, opts...)
	return ci.UpstreamAuth{Handler: upstream}, nil
}

func extractJSONPath(data map[string]any, path string) (string, bool) {
	parts := strings.Split(path, ".")
	var current any = data
	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return "", false
		}
		current, ok = m[part]
		if !ok {
			return "", false
		}
	}
	return fmt.Sprintf("%v", current), true
}

func buildPaginationConfigs(def *Definition) map[string]apiexec.PaginationConfig {
	var configs map[string]apiexec.PaginationConfig
	for name, opDef := range def.Operations {
		if opDef.Pagination == nil {
			continue
		}
		if configs == nil {
			configs = make(map[string]apiexec.PaginationConfig)
		}
		p := opDef.Pagination
		configs[name] = apiexec.PaginationConfig{
			Style:        p.Style,
			CursorParam:  p.CursorParam,
			CursorPath:   p.CursorPath,
			LimitParam:   p.LimitParam,
			DefaultLimit: p.DefaultLimit,
			ResultsPath:  p.ResultsPath,
			MaxPages:     p.MaxPages,
		}
	}
	return configs
}

func resolveURL(baseURL, u string) string {
	if u == "" {
		return ""
	}
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	return baseURL + u
}
