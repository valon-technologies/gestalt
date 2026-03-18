package provider

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/valon-technologies/toolshed/core"
	ci "github.com/valon-technologies/toolshed/core/integration"
	"github.com/valon-technologies/toolshed/internal/config"
	"github.com/valon-technologies/toolshed/internal/integration"
	"github.com/valon-technologies/toolshed/internal/oauth"
)

func Build(def *Definition, intg config.IntegrationDef) (core.Integration, error) {
	d := *def // shallow copy so we don't mutate the caller's definition
	def = &d
	applyOverrides(def, intg)

	baseURL := def.BaseURL
	if intg.BaseURL != "" {
		baseURL = intg.BaseURL
	}

	client := &http.Client{Timeout: 10 * time.Second}

	auth, err := buildAuth(def, intg, baseURL, client)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", def.Provider, err)
	}

	operations, endpoints := buildCatalog(def)

	base := &ci.Base{
		IntegrationName:    def.Provider,
		IntegrationDisplay: def.DisplayName,
		IntegrationDesc:    def.Description,
		Auth:               auth,
		BaseURL:            baseURL,
		HTTPClient:         client,
		Operations:         operations,
		Endpoints:          endpoints,
		Headers:            def.Headers,
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
		checker, ok := responseCheckers[def.ResponseCheck]
		if !ok {
			return nil, fmt.Errorf("%s: unknown response_check %q", def.Provider, def.ResponseCheck)
		}
		base.CheckResponse = checker
	}

	if def.TokenParser != "" {
		parser, ok := tokenParsers[def.TokenParser]
		if !ok {
			return nil, fmt.Errorf("%s: unknown token_parser %q", def.Provider, def.TokenParser)
		}
		base.TokenParser = parser
	} else if def.TokenPrefix != "" {
		prefix := def.TokenPrefix
		base.TokenParser = func(token string) (string, map[string]string, error) {
			return prefix + token, nil, nil
		}
	}

	if def.RequestMutator != "" {
		mutator, ok := requestMutators[def.RequestMutator]
		if !ok {
			return nil, fmt.Errorf("%s: unknown request_mutator %q", def.Provider, def.RequestMutator)
		}
		base.RequestMutator = mutator
	}

	var result core.Integration = base

	if ops := intg.AllowedOperations; ops != nil {
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
			}
			allowed = append(allowed, opName)
		}
		result = integration.NewRestricted(result, allowed)
	}

	return result, nil
}

func applyOverrides(def *Definition, intg config.IntegrationDef) {
	o := intg.Auth
	setStr(&def.Auth.Type, o.Type)
	setStr(&def.Auth.AuthorizationURL, o.AuthorizationURL)
	setStr(&def.Auth.TokenURL, o.TokenURL)
	setStr(&def.Auth.ClientAuth, o.ClientAuth)
	setStr(&def.Auth.TokenExchange, o.TokenExchange)
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

	setStr(&def.ResponseCheck, intg.ResponseCheck)
	setStr(&def.TokenParser, intg.TokenParser)
	setStr(&def.RequestMutator, intg.RequestMutator)
	setStr(&def.TokenPrefix, intg.TokenPrefix)
	setStr(&def.AuthStyle, intg.AuthStyle)
	if intg.Headers != nil {
		def.Headers = intg.Headers
	}
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

	authURL := resolveURL(baseURL, def.Auth.AuthorizationURL)
	tokenURL := resolveURL(baseURL, def.Auth.TokenURL)

	oauthCfg := oauth.UpstreamConfig{
		ClientID:            intg.ClientID,
		ClientSecret:        intg.ClientSecret,
		AuthorizationURL:    authURL,
		TokenURL:            tokenURL,
		RedirectURL:         intg.RedirectURL,
		PKCE:                def.Auth.PKCE,
		ScopeSeparator:      def.Auth.ScopeSeparator,
		AuthorizationParams: def.Auth.AuthorizationParams,
		TokenParams:         def.Auth.TokenParams,
		RefreshParams:       def.Auth.RefreshParams,
	}

	if def.Auth.ClientAuth == "header" {
		oauthCfg.ClientAuthMethod = oauth.ClientAuthHeader
	}

	var opts []oauth.Option
	opts = append(opts, oauth.WithHTTPClient(client))

	if def.Auth.ResponseHook != "" {
		hook, ok := responseHooks[def.Auth.ResponseHook]
		if !ok {
			return nil, fmt.Errorf("unknown auth response_hook %q", def.Auth.ResponseHook)
		}
		opts = append(opts, oauth.WithResponseHook(hook))
	}

	upstream := oauth.NewUpstream(oauthCfg, opts...)
	return ci.UpstreamAuth{Handler: upstream}, nil
}

func buildCatalog(def *Definition) ([]core.Operation, map[string]ci.Endpoint) {
	ops := make([]core.Operation, 0, len(def.Operations))
	eps := make(map[string]ci.Endpoint, len(def.Operations))

	for name, opDef := range def.Operations {
		method := strings.ToUpper(opDef.Method)

		params := make([]core.Parameter, len(opDef.Parameters))
		for i, p := range opDef.Parameters {
			params[i] = core.Parameter{
				Name:        p.Name,
				Type:        p.Type,
				Description: p.Description,
				Required:    p.Required,
				Default:     p.Default,
			}
		}

		ops = append(ops, core.Operation{
			Name:        name,
			Description: opDef.Description,
			Method:      method,
			Parameters:  params,
		})
		eps[name] = ci.Endpoint{
			Method: method,
			Path:   opDef.Path,
		}
	}

	sort.Slice(ops, func(i, j int) bool {
		return ops[i].Name < ops[j].Name
	})

	return ops, eps
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
