package plugins

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	integration "github.com/valon-technologies/gestalt/server/services/apps/declarative"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

const declarativeHTTPTimeout = 30 * time.Second
const declarativeJSONContentType = "application/json; charset=utf-8"
const declarativeDefaultConnection = "default"

type declarativeOptions struct {
	displayName          string
	description          string
	iconSVG              string
	connectionMode       core.ConnectionMode
	operationConnections map[string]string
	connectionSelectors  map[string]core.OperationConnectionSelector
	operationLocks       map[string]bool
	egressCheck          func(string) error
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

// WithDeclarativeOperationConnections binds declarative REST operations to
// effective connection names and optional parameter-based selectors.
func WithDeclarativeOperationConnections(connections map[string]string, selectors map[string]core.OperationConnectionSelector, locks map[string]bool) DeclarativeProviderOption {
	return func(opts *declarativeOptions) {
		opts.operationConnections = maps.Clone(connections)
		opts.connectionSelectors = cloneOperationConnectionSelectors(selectors)
		opts.operationLocks = maps.Clone(locks)
	}
}

func WithDeclarativeEgressCheck(fn func(string) error) DeclarativeProviderOption {
	return func(opts *declarativeOptions) { opts.egressCheck = fn }
}

type DeclarativeProvider struct {
	*integration.Base
	authType             providermanifestv1.AuthType
	authorizationURL     string
	operationConnections map[string]string
	connectionSelectors  map[string]core.OperationConnectionSelector
	operationLocks       map[string]bool
	tokenParsers         map[string]egress.TokenParser
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
	var (
		auth      *providermanifestv1.ProviderAuth
		params    map[string]providermanifestv1.ProviderConnectionParam
		discovery *providermanifestv1.ProviderDiscovery
	)
	if def := manifest.Spec.DefaultConnectionDef(); def != nil {
		auth = def.Auth
		params = def.Params
		discovery = def.Discovery
	}
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
		ConnectionDefs:              ConnectionParamDefsFromManifest(params),
		DiscoveryDef:                DiscoveryConfigFromManifest(discovery),
		CredentialFieldDefs:         declarativeCredentialFields(auth),
		CheckEgress:                 options.egressCheck,
		NoRetry:                     true,
	}
	if auth != nil {
		authType = auth.Type
		authorizationURL = auth.AuthorizationURL
	}
	base.SetCatalog(cat)

	return &DeclarativeProvider{
		Base:                 base,
		authType:             authType,
		authorizationURL:     authorizationURL,
		operationConnections: maps.Clone(options.operationConnections),
		connectionSelectors:  cloneOperationConnectionSelectors(options.connectionSelectors),
		operationLocks:       maps.Clone(options.operationLocks),
		tokenParsers:         connectionTokenParsers(manifest.Spec.Connections),
	}, nil
}

func (p *DeclarativeProvider) Catalog() *catalog.Catalog {
	return p.Base.Catalog().Clone()
}

func (p *DeclarativeProvider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	if !declarativeHasOperation(p.Base.Catalog(), operation) {
		return &core.OperationResult{Status: http.StatusNotFound, Body: []byte(`{"error":"unknown operation"}`)}, nil
	}
	parser, err := p.tokenParserForExecution(ctx, operation, params)
	if err != nil {
		return nil, err
	}
	return p.ExecuteWithTokenParser(ctx, operation, params, token, parser)
}

func (p *DeclarativeProvider) ConnectionForOperation(operation string) string {
	return p.operationConnections[operation]
}

func (p *DeclarativeProvider) ResolveConnectionForOperation(operation string, params map[string]any) (string, error) {
	selector, ok := p.connectionSelectors[operation]
	if !ok {
		return p.ConnectionForOperation(operation), nil
	}
	if connection, selected, err := selectorConnection(selector, params); selected || err != nil {
		return connection, err
	}
	return p.ConnectionForOperation(operation), nil
}

func (p *DeclarativeProvider) OperationConnectionOverrideAllowed(operation string, params map[string]any) bool {
	if selector, ok := p.connectionSelectors[operation]; ok {
		if _, selected, _ := selectorConnection(selector, params); selected {
			return false
		}
	}
	return !p.operationLocks[operation]
}

func (p *DeclarativeProvider) tokenParserForExecution(ctx context.Context, operation string, params map[string]any) (egress.TokenParser, error) {
	connection, resolved, err := p.connectionForExecution(ctx, operation, params)
	if err != nil {
		return nil, err
	}
	if !resolved {
		connection = declarativeDefaultConnection
	}
	if parser, ok := p.tokenParsers[connection]; ok {
		return parser, nil
	}
	return p.TokenParser, nil
}

func (p *DeclarativeProvider) connectionForExecution(ctx context.Context, operation string, params map[string]any) (string, bool, error) {
	for _, connection := range []string{
		invocation.CredentialContextFromContext(ctx).Connection,
		invocation.ConnectionFromContext(ctx),
	} {
		connection = core.ResolveConnectionAlias(strings.TrimSpace(connection))
		if connection == "" {
			continue
		}
		return connection, true, nil
	}
	connection, err := p.ResolveConnectionForOperation(operation, params)
	if err != nil {
		return "", false, err
	}
	connection = core.ResolveConnectionAlias(strings.TrimSpace(connection))
	if connection == "" {
		return "", false, nil
	}
	return connection, true, nil
}

func connectionTokenParsers(connections map[string]*providermanifestv1.ManifestConnectionDef) map[string]egress.TokenParser {
	if len(connections) == 0 {
		return nil
	}
	parsers := make(map[string]egress.TokenParser)
	for name, conn := range connections {
		if conn == nil {
			continue
		}
		name = core.ResolveConnectionAlias(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		if conn.Auth != nil {
			parsers[name] = integration.AuthMappingTokenParser(conn.Auth.AuthMapping)
		} else {
			parsers[name] = nil
		}
	}
	if len(parsers) == 0 {
		return nil
	}
	return parsers
}

func selectorConnection(selector core.OperationConnectionSelector, params map[string]any) (string, bool, error) {
	selected := ""
	if params != nil {
		if value, ok := params[selector.Parameter]; ok && value != nil {
			selected = strings.TrimSpace(fmt.Sprint(value))
		}
	}
	if selected == "" {
		selected = strings.TrimSpace(selector.Default)
	}
	if selected == "" {
		return "", false, nil
	}
	connection, ok := selector.Values[selected]
	if !ok {
		values := make([]string, 0, len(selector.Values))
		for value := range selector.Values {
			values = append(values, value)
		}
		slices.Sort(values)
		return "", true, fmt.Errorf("connection selector parameter %q must be one of [%s]", selector.Parameter, strings.Join(values, ", "))
	}
	return connection, true, nil
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
			Tags:         catalog.MergeTags(mop.Tags),
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
				Internal:    mp.Internal,
			})
		}
		cat.Operations = append(cat.Operations, catOp)
	}
	integration.CompileSchemas(cat)
	return cat
}

func cloneOperationConnectionSelectors(src map[string]core.OperationConnectionSelector) map[string]core.OperationConnectionSelector {
	if src == nil {
		return nil
	}
	cloned := make(map[string]core.OperationConnectionSelector, len(src))
	for operation, selector := range src {
		selector.Values = maps.Clone(selector.Values)
		cloned[operation] = selector
	}
	return cloned
}

func declarativeConnectionMode(override core.ConnectionMode, auth *providermanifestv1.ProviderAuth) core.ConnectionMode {
	if override != "" {
		return core.NormalizeConnectionMode(override)
	}
	if auth == nil || auth.Type == "" || auth.Type == providermanifestv1.AuthTypeNone {
		return core.ConnectionModeNone
	}
	return core.ConnectionModeSubject
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
