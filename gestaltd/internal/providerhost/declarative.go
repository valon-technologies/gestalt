package providerhost

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/integration"
	"github.com/valon-technologies/gestalt/server/internal/provider"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

const declarativeHTTPTimeout = 30 * time.Second
const declarativeJSONContentType = "application/json; charset=utf-8"

type declarativeOptions struct {
	displayName    string
	description    string
	iconSVG        string
	connectionMode core.ConnectionMode
}

type DeclarativeProviderOption func(*declarativeOptions)

func WithDeclarativeMetadataOverrides(displayName, description, iconSVG string) DeclarativeProviderOption {
	return func(opts *declarativeOptions) {
		if displayName != "" {
			opts.displayName = displayName
		}
		if description != "" {
			opts.description = description
		}
		if iconSVG != "" {
			opts.iconSVG = iconSVG
		}
	}
}

func WithDeclarativeConnectionMode(mode core.ConnectionMode) DeclarativeProviderOption {
	return func(opts *declarativeOptions) { opts.connectionMode = mode }
}

type DeclarativeProvider struct {
	*integration.Base
	authType         providermanifestv1.AuthType
	authorizationURL string
}

func NewDeclarativeProvider(manifest *providermanifestv1.Manifest, httpClient *http.Client, opts ...DeclarativeProviderOption) (*DeclarativeProvider, error) {
	if manifest == nil {
		return nil, fmt.Errorf("manifest is required")
	}
	if manifest.Spec == nil || !manifest.Spec.IsDeclarative() {
		return nil, fmt.Errorf("manifest is not a declarative provider")
	}

	if httpClient == nil {
		httpClient = &http.Client{Timeout: declarativeHTTPTimeout}
	}

	options := declarativeOptions{}
	for _, opt := range opts {
		opt(&options)
	}
	auth := manifest.Spec.Auth
	cat := declarativeCatalog(manifest, options)
	authType := providermanifestv1.AuthType("")
	authorizationURL := ""
	base := &integration.Base{
		IntegrationName:             cat.Name,
		IntegrationDisplay:          cat.DisplayName,
		IntegrationDesc:             cat.Description,
		ConnMode:                    declarativeConnectionMode(options.connectionMode, auth),
		BaseURL:                     manifest.Spec.RESTBaseURL(),
		Headers:                     maps.Clone(manifest.Spec.Headers),
		HTTPClient:                  httpClient,
		MethodDefaultParamLocations: true,
		RequestContentType:          declarativeJSONContentType,
		ConnectionDefs:              ConnectionParamDefsFromManifest(manifest.Spec.ConnectionParams),
		DiscoveryDef:                DiscoveryConfigFromManifest(manifest.Spec.Discovery),
		CredentialFieldDefs:         declarativeCredentialFields(auth),
		NoRetry:                     true,
	}
	if auth != nil {
		authType = auth.Type
		authorizationURL = auth.AuthorizationURL
		if auth.AuthMapping != nil && (len(auth.AuthMapping.Headers) > 0 || auth.AuthMapping.Basic != nil) {
			base.TokenParser = provider.MappedCredentialParser(auth.AuthMapping)
		}
	}
	base.SetCatalog(cat)

	return &DeclarativeProvider{
		Base:             base,
		authType:         authType,
		authorizationURL: authorizationURL,
	}, nil
}

func (p *DeclarativeProvider) Catalog() *catalog.Catalog {
	return p.Base.Catalog().Clone()
}

func (p *DeclarativeProvider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	if !declarativeHasOperation(p.Base.Catalog(), operation) {
		return &core.OperationResult{Status: http.StatusNotFound, Body: `{"error":"unknown operation"}`}, nil
	}
	return p.Base.Execute(ctx, operation, params, token)
}

func (p *DeclarativeProvider) AuthTypes() []string {
	switch p.authType {
	case providermanifestv1.AuthTypeOAuth2:
		return []string{"oauth"}
	case providermanifestv1.AuthTypeBearer, providermanifestv1.AuthTypeManual:
		return []string{"manual"}
	case providermanifestv1.AuthTypeNone:
		return nil
	default:
		return nil
	}
}

func (p *DeclarativeProvider) AuthorizationURL(state string, scopes []string) string {
	if p.authType != providermanifestv1.AuthTypeOAuth2 {
		return ""
	}
	return p.authorizationURL
}

func (p *DeclarativeProvider) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	if p.authType != providermanifestv1.AuthTypeOAuth2 {
		return nil, fmt.Errorf("provider does not support OAuth")
	}
	return nil, fmt.Errorf("declarative provider OAuth exchange not implemented")
}

func (p *DeclarativeProvider) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	if p.authType != providermanifestv1.AuthTypeOAuth2 {
		return nil, fmt.Errorf("provider does not support OAuth")
	}
	return nil, fmt.Errorf("declarative provider OAuth refresh not implemented")
}

func declarativeCatalog(manifest *providermanifestv1.Manifest, opts declarativeOptions) *catalog.Catalog {
	ops := manifest.Spec.RESTOperations()
	cat := &catalog.Catalog{
		Name:        manifest.Source,
		DisplayName: manifest.DisplayName,
		Description: manifest.Description,
		Headers:     maps.Clone(manifest.Spec.Headers),
		Operations:  make([]catalog.CatalogOperation, 0, len(ops)),
	}
	if opts.displayName != "" {
		cat.DisplayName = opts.displayName
	}
	if opts.description != "" {
		cat.Description = opts.description
	}
	if opts.iconSVG != "" {
		cat.IconSVG = opts.iconSVG
	}
	for i := range ops {
		mop := &ops[i]
		catOp := catalog.CatalogOperation{
			ID:           mop.Name,
			Method:       mop.Method,
			Path:         mop.Path,
			Description:  mop.Description,
			AllowedRoles: mop.AllowedRoles,
			Transport:    catalog.TransportREST,
			Parameters:   make([]catalog.CatalogParameter, 0, len(mop.Parameters)),
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
		cat.Operations = append(cat.Operations, catOp)
	}
	integration.CompileSchemas(cat)
	return cat
}

func declarativeConnectionMode(override core.ConnectionMode, auth *providermanifestv1.ProviderAuth) core.ConnectionMode {
	if override != "" {
		return override
	}
	if auth == nil || auth.Type == "" || auth.Type == providermanifestv1.AuthTypeNone {
		return core.ConnectionModeNone
	}
	return core.ConnectionModeUser
}

func declarativeCredentialFields(auth *providermanifestv1.ProviderAuth) []core.CredentialFieldDef {
	if auth == nil {
		return nil
	}
	return CredentialFieldsFromManifest(auth.Credentials)
}

func declarativeHasOperation(cat *catalog.Catalog, operation string) bool {
	if cat == nil {
		return false
	}
	for i := range cat.Operations {
		if cat.Operations[i].ID == operation {
			return true
		}
	}
	return false
}
