package pluginhost

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/integration"
	"github.com/valon-technologies/gestalt/server/internal/apiexec"
	"github.com/valon-technologies/gestalt/server/internal/paraminterp"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

const declarativeHTTPTimeout = 30 * time.Second

type DeclarativeProviderOption func(*DeclarativeProvider)

func WithDeclarativeMetadataOverrides(displayName, description, iconSVG string) DeclarativeProviderOption {
	return func(p *DeclarativeProvider) {
		if displayName != "" {
			p.catalog.DisplayName = displayName
		}
		if description != "" {
			p.catalog.Description = description
		}
		if iconSVG != "" {
			p.catalog.IconSVG = iconSVG
		}
	}
}

type DeclarativeProvider struct {
	catalog              *catalog.Catalog
	opsByName            map[string]*catalog.CatalogOperation
	baseURL              string
	auth                 *pluginmanifestv1.ProviderAuth
	httpClient           *http.Client
	postConnectDiscovery *pluginmanifestv1.ProviderPostConnectDiscovery
	connectionDefs       map[string]pluginmanifestv1.ProviderConnectionParam
}

func NewDeclarativeProvider(manifest *pluginmanifestv1.Manifest, httpClient *http.Client, opts ...DeclarativeProviderOption) (*DeclarativeProvider, error) {
	if manifest == nil {
		return nil, fmt.Errorf("manifest is required")
	}
	if manifest.Provider == nil || !manifest.Provider.IsDeclarative() {
		return nil, fmt.Errorf("manifest is not a declarative provider")
	}

	if httpClient == nil {
		httpClient = &http.Client{Timeout: declarativeHTTPTimeout}
	}

	p := &DeclarativeProvider{
		catalog: &catalog.Catalog{
			Name:        manifest.Source,
			DisplayName: manifest.DisplayName,
			Description: manifest.Description,
			Headers:     maps.Clone(manifest.Provider.Headers),
			Operations:  make([]catalog.CatalogOperation, 0, len(manifest.Provider.Operations)),
		},
		opsByName:            make(map[string]*catalog.CatalogOperation, len(manifest.Provider.Operations)),
		baseURL:              manifest.Provider.BaseURL,
		auth:                 manifest.Provider.Auth,
		httpClient:           httpClient,
		postConnectDiscovery: manifest.Provider.PostConnectDiscovery,
		connectionDefs:       manifest.Provider.Connection,
	}

	for i := range manifest.Provider.Operations {
		mop := &manifest.Provider.Operations[i]
		catOp := catalog.CatalogOperation{
			ID:          mop.Name,
			Method:      mop.Method,
			Path:        mop.Path,
			Description: mop.Description,
			Transport:   catalog.TransportREST,
			Parameters:  make([]catalog.CatalogParameter, 0, len(mop.Parameters)),
		}
		for _, mp := range mop.Parameters {
			catOp.Parameters = append(catOp.Parameters, catalog.CatalogParameter{
				Name:        mp.Name,
				Type:        mp.Type,
				Location:    mp.In,
				Description: mp.Description,
				Required:    mp.Required,
			})
		}
		p.catalog.Operations = append(p.catalog.Operations, catOp)
	}

	integration.CompileSchemas(p.catalog)
	for i := range p.catalog.Operations {
		op := &p.catalog.Operations[i]
		p.opsByName[op.ID] = op
	}
	for _, opt := range opts {
		opt(p)
	}

	return p, nil
}

func (p *DeclarativeProvider) Name() string        { return p.catalog.Name }
func (p *DeclarativeProvider) DisplayName() string { return p.catalog.DisplayName }
func (p *DeclarativeProvider) Description() string { return p.catalog.Description }

func (p *DeclarativeProvider) Catalog() *catalog.Catalog { return p.catalog.Clone() }

func (p *DeclarativeProvider) ConnectionMode() core.ConnectionMode {
	return core.ConnectionModeUser
}

func (p *DeclarativeProvider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	op, ok := p.opsByName[operation]
	if !ok {
		return &core.OperationResult{
			Status: http.StatusNotFound,
			Body:   `{"error":"unknown operation"}`,
		}, nil
	}

	queryParams := make(map[string]any)
	bodyParams := make(map[string]any)

	isBodyMethod := op.Method == http.MethodPost || op.Method == http.MethodPut || op.Method == http.MethodPatch

	for k, v := range params {
		loc, declared := declarativeParamLocation(op, k)
		if !declared {
			if isBodyMethod {
				loc = "body"
			} else {
				loc = "query"
			}
		}
		switch loc {
		case "query":
			queryParams[k] = v
		case "body", "path":
			bodyParams[k] = v
		}
	}

	baseURL := p.baseURL
	headers := maps.Clone(p.catalog.Headers)
	if cp := core.ConnectionParams(ctx); cp != nil {
		baseURL = paraminterp.Interpolate(baseURL, cp)
		for k, v := range headers {
			headers[k] = paraminterp.Interpolate(v, cp)
		}
	}

	req := apiexec.Request{
		Method:        op.Method,
		BaseURL:       baseURL,
		Path:          op.Path,
		Params:        bodyParams,
		QueryParams:   queryParams,
		CustomHeaders: headers,
		Token:         token,
		NoRetry:       true,
	}

	return apiexec.Do(ctx, p.httpClient, req)
}

func declarativeParamLocation(op *catalog.CatalogOperation, name string) (string, bool) {
	for _, param := range op.Parameters {
		if param.Name == name {
			return param.Location, true
		}
	}
	return "", false
}

func (p *DeclarativeProvider) SupportsManualAuth() bool {
	if p.auth == nil {
		return false
	}
	return p.auth.Type == pluginmanifestv1.AuthTypeManual || p.auth.Type == pluginmanifestv1.AuthTypeBearer
}

func (p *DeclarativeProvider) CredentialFields() []core.CredentialFieldDef {
	if p.auth == nil || len(p.auth.Credentials) == 0 {
		return nil
	}
	fields := make([]core.CredentialFieldDef, len(p.auth.Credentials))
	for i, cf := range p.auth.Credentials {
		fields[i] = core.CredentialFieldDef{
			Name:        cf.Name,
			Label:       cf.Label,
			Description: cf.Description,
			HelpURL:     cf.HelpURL,
		}
	}
	return fields
}

func (p *DeclarativeProvider) AuthTypes() []string {
	if p.auth == nil {
		return nil
	}
	switch p.auth.Type {
	case pluginmanifestv1.AuthTypeOAuth2:
		return []string{"oauth2"}
	case pluginmanifestv1.AuthTypeBearer:
		return []string{"manual"}
	case pluginmanifestv1.AuthTypeManual:
		return []string{"manual"}
	case pluginmanifestv1.AuthTypeNone:
		return nil
	}
	return nil
}

func (p *DeclarativeProvider) AuthorizationURL(state string, scopes []string) string {
	if p.auth == nil || p.auth.Type != pluginmanifestv1.AuthTypeOAuth2 {
		return ""
	}
	return p.auth.AuthorizationURL
}

func (p *DeclarativeProvider) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	if p.auth == nil || p.auth.Type != pluginmanifestv1.AuthTypeOAuth2 {
		return nil, fmt.Errorf("provider does not support OAuth")
	}

	return nil, fmt.Errorf("declarative provider OAuth exchange not implemented")
}

func (p *DeclarativeProvider) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	if p.auth == nil || p.auth.Type != pluginmanifestv1.AuthTypeOAuth2 {
		return nil, fmt.Errorf("provider does not support OAuth")
	}

	return nil, fmt.Errorf("declarative provider OAuth refresh not implemented")
}

func (p *DeclarativeProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	if len(p.connectionDefs) == 0 {
		return nil
	}
	defs := make(map[string]core.ConnectionParamDef, len(p.connectionDefs))
	for name, cp := range p.connectionDefs {
		defs[name] = core.ConnectionParamDef{
			Required:    cp.Required,
			Description: cp.Description,
			From:        cp.From,
		}
	}
	return defs
}

func (p *DeclarativeProvider) DiscoveryConfig() *core.DiscoveryConfig {
	if p.postConnectDiscovery == nil {
		return nil
	}
	return &core.DiscoveryConfig{
		URL:             p.postConnectDiscovery.URL,
		IDPath:          p.postConnectDiscovery.IDPath,
		NamePath:        p.postConnectDiscovery.NamePath,
		MetadataMapping: p.postConnectDiscovery.MetadataMapping,
	}
}
