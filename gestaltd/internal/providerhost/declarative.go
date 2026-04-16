package providerhost

import (
	"fmt"
	"maps"
	"sort"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/integration"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/provider"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

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

func CatalogFromDeclarativeManifest(manifest *providermanifestv1.Manifest) (*catalog.Catalog, error) {
	def, err := DefinitionFromDeclarativeManifest(manifest)
	if err != nil {
		return nil, err
	}
	return CatalogFromDeclarativeDefinition(def, manifest.Spec.RESTOperations()), nil
}

func StaticProviderMetadata(name string, conn config.ConnectionDef, discovery *providermanifestv1.ProviderDiscovery) (StaticProviderSpec, error) {
	prov, err := provider.Build(&provider.Definition{
		Provider:  name,
		Discovery: declarativeDiscovery(discovery),
	}, conn)
	if err != nil {
		return StaticProviderSpec{}, err
	}
	return StaticProviderSpec{
		AuthTypes:        prov.AuthTypes(),
		ConnectionParams: prov.ConnectionParamDefs(),
		CredentialFields: prov.CredentialFields(),
		DiscoveryConfig:  prov.DiscoveryConfig(),
	}, nil
}

func CatalogFromDeclarativeDefinition(def *provider.Definition, ops []providermanifestv1.ProviderOperation) *catalog.Catalog {
	cat := provider.CatalogFromDefinition(def)
	order := make(map[string]int, len(ops))
	for i, op := range ops {
		order[op.Name] = i
	}
	sort.SliceStable(cat.Operations, func(i, j int) bool {
		return order[cat.Operations[i].ID] < order[cat.Operations[j].ID]
	})
	integration.CompileSchemas(cat)
	return cat
}

func DefinitionFromDeclarativeManifest(manifest *providermanifestv1.Manifest, opts ...DeclarativeProviderOption) (*provider.Definition, error) {
	if manifest == nil {
		return nil, fmt.Errorf("manifest is required")
	}
	if manifest.Spec == nil || !manifest.Spec.IsDeclarative() {
		return nil, fmt.Errorf("manifest is not a declarative provider")
	}

	options := declarativeOptions{}
	for _, opt := range opts {
		opt(&options)
	}

	def := &provider.Definition{
		Provider:       manifest.Source,
		DisplayName:    manifest.DisplayName,
		Description:    manifest.Description,
		Headers:        maps.Clone(manifest.Spec.Headers),
		BaseURL:        manifest.Spec.RESTBaseURL(),
		ConnectionMode: string(declarativeConnectionMode(options.connectionMode, manifest.Spec.Auth)),
		Connection:     declarativeConnection(manifest.Spec.ConnectionParams),
		Discovery:      declarativeDiscovery(manifest.Spec.Discovery),
		ResponseMapping: declarativeResponseMapping(
			manifest.Spec.ResponseMapping,
		),
		Operations: declarativeOperations(manifest.Spec.RESTOperations()),
	}
	if options.displayName != "" {
		def.DisplayName = options.displayName
	}
	if options.description != "" {
		def.Description = options.description
	}
	if options.iconSVG != "" {
		def.IconSVG = options.iconSVG
	}
	if auth := manifest.Spec.Auth; auth != nil {
		def.Auth = declarativeAuth(auth)
		if len(auth.Credentials) > 0 {
			def.CredentialFields = append([]provider.CredentialFieldDef(nil), auth.Credentials...)
		}
		if auth.AuthMapping != nil {
			def.AuthMapping = config.CloneAuthMapping(auth.AuthMapping)
			if auth.AuthMapping.Basic != nil {
				def.AuthStyle = "basic"
			}
		}
	}
	return def, nil
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

func declarativeConnection(defs map[string]providermanifestv1.ProviderConnectionParam) map[string]provider.ConnectionParamDef {
	if len(defs) == 0 {
		return nil
	}
	out := make(map[string]provider.ConnectionParamDef, len(defs))
	for name, def := range defs {
		out[name] = provider.ConnectionParamDef{
			Required:    def.Required,
			Description: def.Description,
			From:        def.From,
		}
	}
	return out
}

func declarativeDiscovery(discovery *providermanifestv1.ProviderDiscovery) *provider.DiscoveryDef {
	if discovery == nil {
		return nil
	}
	return &provider.DiscoveryDef{
		URL:      discovery.URL,
		IDPath:   discovery.IDPath,
		NamePath: discovery.NamePath,
		Metadata: maps.Clone(discovery.Metadata),
	}
}

func declarativeResponseMapping(mapping *providermanifestv1.ManifestResponseMapping) *provider.ResponseMappingDef {
	if mapping == nil {
		return nil
	}
	out := &provider.ResponseMappingDef{DataPath: mapping.DataPath}
	if mapping.Pagination != nil {
		out.Pagination = &provider.PaginationMappingDef{
			HasMore: declarativeValueSelector(mapping.Pagination.HasMore),
			Cursor:  declarativeValueSelector(mapping.Pagination.Cursor),
		}
	}
	return out
}

func declarativeValueSelector(in *providermanifestv1.ManifestValueSelector) *provider.ValueSelectorDef {
	if in == nil {
		return nil
	}
	return &provider.ValueSelectorDef{
		Source: in.Source,
		Path:   in.Path,
	}
}

func declarativeOperations(ops []providermanifestv1.ProviderOperation) map[string]provider.OperationDef {
	if len(ops) == 0 {
		return nil
	}
	out := make(map[string]provider.OperationDef, len(ops))
	for _, op := range ops {
		params := make([]provider.ParameterDef, 0, len(op.Parameters))
		for _, param := range op.Parameters {
			params = append(params, provider.ParameterDef{
				Name:        param.Name,
				Type:        param.Type,
				Location:    param.In,
				Description: param.Description,
				Required:    param.Required,
			})
		}
		out[op.Name] = provider.OperationDef{
			Description:  op.Description,
			Method:       op.Method,
			Path:         op.Path,
			AllowedRoles: append([]string(nil), op.AllowedRoles...),
			Parameters:   params,
		}
	}
	return out
}

func declarativeAuth(auth *providermanifestv1.ProviderAuth) provider.AuthDef {
	if auth == nil {
		return provider.AuthDef{}
	}
	return provider.AuthDef{
		Type:                string(auth.Type),
		AuthorizationURL:    auth.AuthorizationURL,
		TokenURL:            auth.TokenURL,
		ClientAuth:          auth.ClientAuth,
		TokenExchange:       auth.TokenExchange,
		Scopes:              append([]string(nil), auth.Scopes...),
		ScopeParam:          auth.ScopeParam,
		ScopeSeparator:      auth.ScopeSeparator,
		PKCE:                auth.PKCE,
		AuthorizationParams: maps.Clone(auth.AuthorizationParams),
		TokenParams:         maps.Clone(auth.TokenParams),
		RefreshParams:       maps.Clone(auth.RefreshParams),
		AcceptHeader:        auth.AcceptHeader,
		TokenMetadata:       append([]string(nil), auth.TokenMetadata...),
		AccessTokenPath:     auth.AccessTokenPath,
	}
}
