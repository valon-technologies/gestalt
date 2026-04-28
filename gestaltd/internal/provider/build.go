package provider

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreintegration "github.com/valon-technologies/gestalt/server/core/integration"
	"github.com/valon-technologies/gestalt/server/internal/apiexec"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/oauth"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

// BuildOption configures optional aspects of provider construction.
type BuildOption func(*buildOptions)

type buildOptions struct {
	authOverride coreintegration.AuthHandler
	egressCheck  func(string) error
}

// WithAuthHandler injects a pre-built auth handler, bypassing buildAuth.
func WithAuthHandler(h coreintegration.AuthHandler) BuildOption {
	return func(o *buildOptions) { o.authOverride = h }
}

// WithEgressCheck injects a host-level egress check function.
func WithEgressCheck(fn func(string) error) BuildOption {
	return func(o *buildOptions) { o.egressCheck = fn }
}

// Build constructs a provider from a spec Definition and a ConnectionDef that
// owns auth configuration.
func Build(def *Definition, conn config.ConnectionDef, opts ...BuildOption) (core.Provider, error) {
	var bo buildOptions
	for _, opt := range opts {
		opt(&bo)
	}

	d := *def
	def = &d
	ApplyConnectionAuth(def, conn)

	baseURL := def.BaseURL

	client := &http.Client{Timeout: 10 * time.Second}

	var auth coreintegration.AuthHandler
	var err error
	if bo.authOverride != nil {
		auth = bo.authOverride
	} else {
		auth, err = buildAuth(def, conn, baseURL, client)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", def.Provider, err)
	}

	cat := CatalogFromDefinition(def)
	cat.BaseURL = baseURL
	coreintegration.CompileSchemas(cat)

	base := &coreintegration.Base{
		IntegrationName:    def.Provider,
		IntegrationDisplay: def.DisplayName,
		IntegrationDesc:    def.Description,
		Auth:               auth,
		BaseURL:            baseURL,
		HTTPClient:         client,
		Headers:            def.Headers,
		Pagination:         buildPaginationConfigs(def),
		CheckEgress:        bo.egressCheck,
	}

	connMode := conn.Mode
	if connMode == "" {
		connMode = providermanifestv1.ConnectionMode(def.ConnectionMode)
	}
	switch connMode {
	case "", providermanifestv1.ConnectionModeNone, providermanifestv1.ConnectionModeUser:
		if connMode != "" {
			base.ConnMode = core.NormalizeConnectionMode(core.ConnectionMode(connMode))
		}
	default:
		return nil, fmt.Errorf("%s: unknown connectionMode %q", def.Provider, connMode)
	}

	switch def.AuthStyle {
	case "", "bearer":
	case "raw":
		base.AuthStyle = coreintegration.AuthStyleRaw
	case "none":
		base.AuthStyle = coreintegration.AuthStyleNone
	case "basic":
		base.AuthStyle = coreintegration.AuthStyleBasic
	default:
		return nil, fmt.Errorf("%s: unknown authStyle %q", def.Provider, def.AuthStyle)
	}

	switch {
	case def.ResponseCheck != nil:
		base.CheckResponse = buildResponseChecker(def.ResponseCheck)
	case def.ErrorMessagePath != "":
		msgPath := def.ErrorMessagePath
		base.CheckResponse = func(status int, body []byte) error {
			if status < http.StatusBadRequest {
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
	case def.AuthMapping != nil && (len(def.AuthMapping.Headers) > 0 || def.AuthMapping.Basic != nil):
		if base.AuthStyle != coreintegration.AuthStyleBasic {
			if def.AuthMapping.Basic != nil {
				return nil, fmt.Errorf("%s: authMapping.basic requires authStyle basic", def.Provider)
			}
		}
		base.TokenParser = MappedCredentialParser(def.AuthMapping)
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

	if def.Discovery != nil {
		base.DiscoveryDef = def.Discovery.ToCore()
	}

	base.ManualAuthEnabled = def.ManualAuth

	if def.ResponseMapping != nil {
		rm := &coreintegration.ResponseMappingConfig{
			DataPath: def.ResponseMapping.DataPath,
		}
		if def.ResponseMapping.Pagination != nil {
			rm.Pagination = &coreintegration.PaginationProjectionConfig{
				HasMore: buildValueSelector(def.ResponseMapping.Pagination.HasMore),
				Cursor:  buildValueSelector(def.ResponseMapping.Pagination.Cursor),
			}
		}
		base.ResponseMapping = rm
	}

	if len(def.CredentialFields) > 0 {
		base.CredentialFieldDefs = make([]core.CredentialFieldDef, len(def.CredentialFields))
		for i, cf := range def.CredentialFields {
			base.CredentialFieldDefs[i] = core.CredentialFieldDef{
				Name:        cf.Name,
				Label:       cf.Label,
				Description: cf.Description,
			}
		}
	}

	if len(conn.ConnectionParams) > 0 {
		base.ConnectionDefs = make(map[string]core.ConnectionParamDef, len(conn.ConnectionParams))
		for name, cpd := range conn.ConnectionParams {
			base.ConnectionDefs[name] = core.ConnectionParamDef{
				Required:    cpd.Required,
				Description: cpd.Description,
				From:        cpd.From,
			}
		}
	} else if len(def.Connection) > 0 {
		base.ConnectionDefs = make(map[string]core.ConnectionParamDef, len(def.Connection))
		for name, cpd := range def.Connection {
			base.ConnectionDefs[name] = core.ConnectionParamDef{
				Required:    cpd.Required,
				Description: cpd.Description,
				Default:     cpd.Default,
				From:        cpd.From,
				Field:       cpd.Field,
			}
		}
	}

	base.SetCatalog(cat)
	return base, nil
}

func ReadIconFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// ApplyConnectionAuth merges connection auth overrides into the Definition.
func ApplyConnectionAuth(def *Definition, conn config.ConnectionDef) {
	o := conn.Auth
	if o.Type == providermanifestv1.AuthTypeOAuth2 && def.Auth.Type != string(providermanifestv1.AuthTypeOAuth2) {
		clearManualAuthMaterialization(def)
	}
	setStr(&def.Auth.Type, string(o.Type))
	setStr(&def.Auth.AuthorizationURL, o.AuthorizationURL)
	setStr(&def.Auth.TokenURL, o.TokenURL)
	setStr(&def.Auth.ClientAuth, o.ClientAuth)
	setStr(&def.Auth.TokenExchange, o.TokenExchange)
	if o.Scopes != nil {
		def.Auth.Scopes = o.Scopes
	}
	setStr(&def.Auth.ScopeParam, o.ScopeParam)
	setStr(&def.Auth.ScopeSeparator, o.ScopeSeparator)
	setStr(&def.Auth.AcceptHeader, o.AcceptHeader)
	setStr(&def.Auth.AccessTokenPath, o.AccessTokenPath)
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
	if len(o.Credentials) > 0 {
		def.CredentialFields = make([]CredentialFieldDef, len(o.Credentials))
		copy(def.CredentialFields, o.Credentials)
	}
	if o.AuthMapping != nil {
		def.AuthMapping = config.CloneAuthMapping(o.AuthMapping)
	}
}

func clearManualAuthMaterialization(def *Definition) {
	def.AuthStyle = ""
	def.AuthHeader = ""
	def.TokenPrefix = ""
	def.AuthMapping = nil
	def.CredentialFields = nil
}

func parseMappedToken(token string) (map[string]any, error) {
	var tokenData map[string]any
	if err := json.Unmarshal([]byte(token), &tokenData); err != nil {
		return nil, fmt.Errorf("parsing token as JSON for authMapping: %w", err)
	}
	return tokenData, nil
}

func MappedCredentialParser(mapping *AuthMappingDef) func(string) (string, map[string]string, error) {
	return func(token string) (string, map[string]string, error) {
		var (
			tokenData map[string]any
			headers   map[string]string
		)
		if len(mapping.Headers) > 0 {
			headers = make(map[string]string, len(mapping.Headers))
			for headerName, value := range mapping.Headers {
				resolved, err := resolveAuthValue(value, token, &tokenData)
				if err != nil {
					return "", nil, fmt.Errorf("authMapping.headers[%q]: %w", headerName, err)
				}
				headers[headerName] = resolved
			}
		}
		authToken := ""
		if mapping.Basic != nil {
			username, err := resolveAuthValue(mapping.Basic.Username, token, &tokenData)
			if err != nil {
				return "", nil, fmt.Errorf("authMapping.basic.username: %w", err)
			}
			password, err := resolveAuthValue(mapping.Basic.Password, token, &tokenData)
			if err != nil {
				return "", nil, fmt.Errorf("authMapping.basic.password: %w", err)
			}
			credential := fmt.Sprintf("%s:%s", username, password)
			authToken = "Basic " + base64.StdEncoding.EncodeToString([]byte(credential))
		}
		if len(headers) == 0 {
			headers = nil
		}
		return authToken, headers, nil
	}
}

func resolveAuthValue(value AuthValueDef, token string, tokenData *map[string]any) (string, error) {
	hasValue := value.Value != ""
	hasValueFrom := value.ValueFrom != nil
	if hasValue == hasValueFrom {
		return "", fmt.Errorf("must set exactly one of value or valueFrom.credentialFieldRef")
	}
	if hasValue {
		return value.Value, nil
	}
	if value.ValueFrom == nil || value.ValueFrom.CredentialFieldRef == nil {
		return "", fmt.Errorf("must set exactly one of value or valueFrom.credentialFieldRef")
	}
	if *tokenData == nil {
		parsed, err := parseMappedToken(token)
		if err != nil {
			return "", err
		}
		*tokenData = parsed
	}
	fieldName := value.ValueFrom.CredentialFieldRef.Name
	val, ok := (*tokenData)[fieldName]
	if !ok || val == nil {
		return "", fmt.Errorf("token field %q is missing or null", fieldName)
	}
	return fmt.Sprintf("%v", val), nil
}

func setStr(dst *string, val string) {
	if val != "" {
		*dst = val
	}
}

func buildAuth(def *Definition, conn config.ConnectionDef, baseURL string, client *http.Client) (coreintegration.AuthHandler, error) {
	if def.Auth.Type == "manual" || (def.Auth.Type == "" && def.Auth.AuthorizationURL == "") {
		return oauth.ManualAuthHandler{}, nil
	}

	upstream, err := BuildOAuthUpstream(def, conn, baseURL, client)
	if err != nil {
		return nil, err
	}
	return coreintegration.UpstreamAuth{UpstreamHandler: upstream}, nil
}

// BuildOAuthUpstream creates an oauth.UpstreamHandler from a provider
// Definition and ConnectionDef. This preserves all fields: token_exchange,
// token_params, refresh_params, scope_separator, accept_header, and response
// hooks. Callers outside the provider build pipeline (e.g. the factory building
// per-connection OAuth handlers) use this to get a standalone handler.
func BuildOAuthUpstream(def *Definition, conn config.ConnectionDef, baseURL string, client *http.Client) (*oauth.UpstreamHandler, error) {
	var tokenExchange oauth.TokenExchangeFormat
	switch def.Auth.TokenExchange {
	case "", "form":
		tokenExchange = oauth.TokenExchangeForm
	case "json":
		tokenExchange = oauth.TokenExchangeJSON
	default:
		return nil, fmt.Errorf("unknown tokenExchange %q", def.Auth.TokenExchange)
	}

	authURL := resolveURL(baseURL, def.Auth.AuthorizationURL)
	tokenURL := resolveURL(baseURL, def.Auth.TokenURL)

	oauthCfg := oauth.UpstreamConfig{
		ClientID:            conn.Auth.ClientID,
		ClientSecret:        conn.Auth.ClientSecret,
		AuthorizationURL:    authURL,
		TokenURL:            tokenURL,
		RedirectURL:         conn.Auth.RedirectURL,
		PKCE:                def.Auth.PKCE,
		DefaultScopes:       def.Auth.Scopes,
		ScopeParam:          def.Auth.ScopeParam,
		ScopeSeparator:      def.Auth.ScopeSeparator,
		AuthorizationParams: def.Auth.AuthorizationParams,
		TokenParams:         def.Auth.TokenParams,
		RefreshParams:       def.Auth.RefreshParams,
		TokenExchange:       tokenExchange,
		AcceptHeader:        def.Auth.AcceptHeader,
		AccessTokenPath:     def.Auth.AccessTokenPath,
	}

	if def.Auth.ClientAuth == "header" {
		oauthCfg.ClientAuthMethod = oauth.ClientAuthHeader
	}

	var opts []oauth.Option
	if client != nil {
		opts = append(opts, oauth.WithHTTPClient(client))
	}

	if def.Auth.ResponseCheck != nil {
		rcd := def.Auth.ResponseCheck
		opts = append(opts, oauth.WithResponseHook(func(body []byte) error {
			var data map[string]any
			if err := json.Unmarshal(body, &data); err != nil {
				return fmt.Errorf("failed to parse auth response: %w", err)
			}
			if len(rcd.SuccessBodyMatch) > 0 {
				return checkBodyMatch(data, rcd)
			}
			if rcd.ErrorMessagePath != "" {
				if msg, ok := extractJSONPath(data, rcd.ErrorMessagePath); ok && msg != "" {
					return fmt.Errorf("%s", msg)
				}
			}
			return nil
		}))
	}

	return oauth.NewUpstream(oauthCfg, opts...), nil
}

func buildResponseChecker(rcd *ResponseCheckDef) apiexec.ResponseChecker {
	return func(status int, body []byte) error {
		var data map[string]any
		if err := json.Unmarshal(body, &data); err != nil {
			if status >= http.StatusBadRequest {
				return fmt.Errorf("HTTP %d: %s", status, body)
			}
			return nil
		}
		if len(rcd.SuccessBodyMatch) > 0 {
			if err := checkBodyMatch(data, rcd); err != nil {
				return fmt.Errorf("response check failed: %s", err)
			}
			return nil
		}
		if status >= http.StatusBadRequest && rcd.ErrorMessagePath != "" {
			if msg, ok := extractJSONPath(data, rcd.ErrorMessagePath); ok {
				return fmt.Errorf("HTTP %d: %s", status, msg)
			}
			return fmt.Errorf("HTTP %d: %s", status, body)
		}
		return nil
	}
}

func checkBodyMatch(data map[string]any, rcd *ResponseCheckDef) error {
	for key, expected := range rcd.SuccessBodyMatch {
		actual, ok := data[key]
		if !ok || !matchValue(actual, expected) {
			errMsg := "unknown error"
			if rcd.ErrorMessagePath != "" {
				if msg, found := extractJSONPath(data, rcd.ErrorMessagePath); found {
					errMsg = msg
				}
			}
			return fmt.Errorf("%s", errMsg)
		}
	}
	return nil
}

func matchValue(actual, expected any) bool {
	switch e := expected.(type) {
	case bool:
		a, ok := actual.(bool)
		return ok && a == e
	case string:
		a, ok := actual.(string)
		return ok && a == e
	case float64:
		a, ok := actual.(float64)
		return ok && a == e
	case int:
		a, ok := actual.(float64)
		return ok && a == float64(e)
	default:
		return fmt.Sprintf("%v", actual) == fmt.Sprintf("%v", expected)
	}
}

func extractJSONPath(data map[string]any, path string) (string, bool) {
	v, ok := apiexec.ExtractJSONPath(data, path)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("%v", v), true
}

func buildPaginationConfigs(def *Definition) map[string]apiexec.PaginationConfig {
	var configs map[string]apiexec.PaginationConfig
	for name := range def.Operations {
		opDef := def.Operations[name] //nolint:gocritic // map values not addressable
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
			Cursor:       buildValueSelector(p.Cursor),
			LimitParam:   p.LimitParam,
			DefaultLimit: p.DefaultLimit,
			ResultsPath:  p.ResultsPath,
			MaxPages:     p.MaxPages,
		}
	}
	return configs
}

func buildValueSelector(def *ValueSelectorDef) *apiexec.ValueSelector {
	if def == nil {
		return nil
	}
	return &apiexec.ValueSelector{
		Source: def.Source,
		Path:   def.Path,
	}
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
