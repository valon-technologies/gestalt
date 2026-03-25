package pluginapi

import (
	"context"
	"io"
	"net/http"
	"slices"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
	"github.com/valon-technologies/gestalt/internal/oauth"
	pluginapiv1 "github.com/valon-technologies/gestalt/sdk/pluginapi/v1"
	"google.golang.org/protobuf/types/known/emptypb"
)

type remoteProviderBase struct {
	client   pluginapiv1.ProviderPluginClient
	metadata *pluginapiv1.ProviderMetadata
	ops      []core.Operation
	catalog  *catalog.Catalog
	closer   io.Closer
}

// RemoteProviderOption configures a remote provider returned by NewRemoteProvider.
type RemoteProviderOption func(*remoteProviderBase)

// WithCloser attaches a closer that is called when the provider is closed.
// This is used to tie process lifecycle to provider lifecycle.
func WithCloser(c io.Closer) RemoteProviderOption {
	return func(b *remoteProviderBase) { b.closer = c }
}

func (p *remoteProviderBase) Close() error {
	if p.closer != nil {
		return p.closer.Close()
	}
	return nil
}

func NewRemoteProvider(ctx context.Context, client pluginapiv1.ProviderPluginClient, opts ...RemoteProviderOption) (core.Provider, error) {
	meta, err := client.GetMetadata(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	opsResp, err := client.ListOperations(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	staticCatalog, err := catalogFromJSON(meta.GetStaticCatalogJson())
	if err != nil {
		return nil, err
	}

	base := &remoteProviderBase{
		client:   client,
		metadata: meta,
		ops:      operationsFromProto(opsResp.GetOperations()),
		catalog:  staticCatalog,
	}
	for _, opt := range opts {
		opt(base)
	}

	hasOAuth := slices.Contains(meta.GetAuthTypes(), "oauth")
	hasSessionCatalog := meta.GetSupportsSessionCatalog()
	hasPostConnect := meta.GetSupportsPostConnect()

	switch {
	case hasOAuth && hasSessionCatalog && hasPostConnect:
		return &remoteProviderWithOAuthSessionCatalogPostConnect{
			remoteProviderWithOAuth: &remoteProviderWithOAuth{remoteProviderBase: base},
		}, nil
	case hasOAuth && hasSessionCatalog:
		return &remoteProviderWithOAuthSessionCatalog{
			remoteProviderWithOAuth: &remoteProviderWithOAuth{remoteProviderBase: base},
		}, nil
	case hasOAuth && hasPostConnect:
		return &remoteProviderWithOAuthPostConnect{
			remoteProviderWithOAuth: &remoteProviderWithOAuth{remoteProviderBase: base},
		}, nil
	case hasSessionCatalog && hasPostConnect:
		return &remoteProviderWithSessionCatalogPostConnect{remoteProviderBase: base}, nil
	case hasOAuth:
		return &remoteProviderWithOAuth{remoteProviderBase: base}, nil
	case hasSessionCatalog:
		return &remoteProviderWithSessionCatalog{remoteProviderBase: base}, nil
	case hasPostConnect:
		return &remoteProviderWithPostConnect{remoteProviderBase: base}, nil
	default:
		return base, nil
	}
}

func (p *remoteProviderBase) Name() string { return p.metadata.GetName() }

func (p *remoteProviderBase) DisplayName() string { return p.metadata.GetDisplayName() }

func (p *remoteProviderBase) Description() string { return p.metadata.GetDescription() }

func (p *remoteProviderBase) ConnectionMode() core.ConnectionMode {
	return protoConnectionModeToCore(p.metadata.GetConnectionMode())
}

func (p *remoteProviderBase) ListOperations() []core.Operation {
	out := make([]core.Operation, len(p.ops))
	copy(out, p.ops)
	return out
}

func (p *remoteProviderBase) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	msg, err := structFromMap(params)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Execute(ctx, &pluginapiv1.ExecuteRequest{
		Operation:        operation,
		Params:           msg,
		Token:            token,
		ConnectionParams: core.ConnectionParams(ctx),
	})
	if err != nil {
		return nil, err
	}
	return &core.OperationResult{
		Status: int(resp.GetStatus()),
		Body:   resp.GetBody(),
	}, nil
}

func (p *remoteProviderBase) SupportsManualAuth() bool {
	return slices.Contains(p.metadata.GetAuthTypes(), "manual")
}

func (p *remoteProviderBase) Catalog() *catalog.Catalog {
	if p.catalog == nil {
		return nil
	}
	return p.catalog.Clone()
}

func (p *remoteProviderBase) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return connectionParamDefsFromProto(p.metadata.GetConnectionParams())
}

func (p *remoteProviderBase) AuthTypes() []string {
	return slices.Clone(p.metadata.GetAuthTypes())
}

func (p *remoteProviderBase) authorizationURL(state string, scopes []string, includeVerifier bool, authBaseURL string) (string, string) {
	resp, err := p.client.AuthorizationURL(context.Background(), &pluginapiv1.AuthorizationURLRequest{
		State:           state,
		Scopes:          slices.Clone(scopes),
		AuthBaseUrl:     authBaseURL,
		IncludeVerifier: includeVerifier,
	})
	if err != nil {
		return "", ""
	}
	return resp.GetUrl(), resp.GetVerifier()
}

func (p *remoteProviderBase) exchangeCode(ctx context.Context, code, verifier, tokenURL string) (*core.TokenResponse, error) {
	resp, err := p.client.ExchangeCode(ctx, &pluginapiv1.ExchangeCodeRequest{
		Code:             code,
		Verifier:         verifier,
		TokenUrl:         tokenURL,
		ConnectionParams: core.ConnectionParams(ctx),
	})
	if err != nil {
		return nil, err
	}
	return tokenResponseFromProto(resp), nil
}

func (p *remoteProviderBase) refreshToken(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error) {
	resp, err := p.client.RefreshToken(ctx, &pluginapiv1.RefreshTokenRequest{
		RefreshToken:     refreshToken,
		TokenUrl:         tokenURL,
		ConnectionParams: core.ConnectionParams(ctx),
	})
	if err != nil {
		return nil, err
	}
	return tokenResponseFromProto(resp), nil
}

func (p *remoteProviderBase) sessionCatalog(ctx context.Context, token string) (*catalog.Catalog, error) {
	resp, err := p.client.GetSessionCatalog(ctx, &pluginapiv1.GetSessionCatalogRequest{
		Token:            token,
		ConnectionParams: core.ConnectionParams(ctx),
	})
	if err != nil {
		return nil, err
	}
	return catalogFromJSON(resp.GetCatalogJson())
}

func (p *remoteProviderBase) postConnectHook() core.PostConnectHook {
	return func(ctx context.Context, tok *core.IntegrationToken, _ *http.Client) (map[string]string, error) {
		resp, err := p.client.PostConnect(ctx, &pluginapiv1.PostConnectRequest{
			Token: integrationTokenToProto(tok),
		})
		if err != nil {
			return nil, err
		}
		return resp.GetMetadata(), nil
	}
}

type remoteProviderWithOAuth struct{ *remoteProviderBase }

func (p *remoteProviderWithOAuth) AuthorizationURL(state string, scopes []string) string {
	url, _ := p.authorizationURL(state, scopes, false, "")
	return url
}

func (p *remoteProviderWithOAuth) StartOAuth(state string, scopes []string) (string, string) {
	return p.authorizationURL(state, scopes, true, "")
}

func (p *remoteProviderWithOAuth) StartOAuthWithOverride(authBaseURL, state string, scopes []string) (string, string) {
	return p.authorizationURL(state, scopes, true, authBaseURL)
}

func (p *remoteProviderWithOAuth) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	return p.exchangeCode(ctx, code, "", "")
}

func (p *remoteProviderWithOAuth) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	return p.refreshToken(ctx, refreshToken, "")
}

func (p *remoteProviderWithOAuth) ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...oauth.ExchangeOption) (*core.TokenResponse, error) {
	resolved := oauth.ResolveExchangeOptions(extraOpts...)
	if verifier == "" {
		verifier = resolved.Verifier
	}
	return p.exchangeCode(ctx, code, verifier, resolved.TokenURL)
}

func (p *remoteProviderWithOAuth) RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error) {
	return p.refreshToken(ctx, refreshToken, tokenURL)
}

func (p *remoteProviderWithOAuth) AuthorizationBaseURL() string {
	return p.metadata.GetAuthorizationBaseUrl()
}

func (p *remoteProviderWithOAuth) TokenURL() string {
	return p.metadata.GetTokenUrl()
}

type remoteProviderWithSessionCatalog struct{ *remoteProviderBase }

func (p *remoteProviderWithSessionCatalog) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	return p.sessionCatalog(ctx, token)
}

type remoteProviderWithPostConnect struct{ *remoteProviderBase }

func (p *remoteProviderWithPostConnect) PostConnectHook() core.PostConnectHook {
	return p.postConnectHook()
}

type remoteProviderWithOAuthSessionCatalog struct{ *remoteProviderWithOAuth }

func (p *remoteProviderWithOAuthSessionCatalog) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	return p.sessionCatalog(ctx, token)
}

type remoteProviderWithOAuthPostConnect struct{ *remoteProviderWithOAuth }

func (p *remoteProviderWithOAuthPostConnect) PostConnectHook() core.PostConnectHook {
	return p.postConnectHook()
}

type remoteProviderWithSessionCatalogPostConnect struct{ *remoteProviderBase }

func (p *remoteProviderWithSessionCatalogPostConnect) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	return p.sessionCatalog(ctx, token)
}

func (p *remoteProviderWithSessionCatalogPostConnect) PostConnectHook() core.PostConnectHook {
	return p.postConnectHook()
}

type remoteProviderWithOAuthSessionCatalogPostConnect struct{ *remoteProviderWithOAuth }

func (p *remoteProviderWithOAuthSessionCatalogPostConnect) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	return p.sessionCatalog(ctx, token)
}

func (p *remoteProviderWithOAuthSessionCatalogPostConnect) PostConnectHook() core.PostConnectHook {
	return p.postConnectHook()
}
