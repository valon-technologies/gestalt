package pluginapi

import (
	"context"
	"net/http"
	"slices"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
	"github.com/valon-technologies/gestalt/internal/oauth"
	"github.com/valon-technologies/gestalt/internal/paraminterp"
	pluginapiv1 "github.com/valon-technologies/gestalt/sdk/pluginapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ProviderServer struct {
	pluginapiv1.UnimplementedProviderPluginServer
	provider   core.Provider
	httpClient *http.Client
}

func NewProviderServer(provider core.Provider) *ProviderServer {
	return &ProviderServer{provider: provider, httpClient: http.DefaultClient}
}

func (s *ProviderServer) SetHTTPClient(client *http.Client) {
	s.httpClient = client
}

func (s *ProviderServer) GetMetadata(_ context.Context, _ *emptypb.Empty) (*pluginapiv1.ProviderMetadata, error) {
	var defs map[string]core.ConnectionParamDef
	if cpp, ok := s.provider.(core.ConnectionParamProvider); ok {
		defs = cpp.ConnectionParamDefs()
	}

	staticCatalog, err := catalogToJSON(staticCatalogForProvider(s.provider))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode static catalog: %v", err)
	}

	return &pluginapiv1.ProviderMetadata{
		Name:                   s.provider.Name(),
		DisplayName:            s.provider.DisplayName(),
		Description:            s.provider.Description(),
		ConnectionMode:         coreConnectionModeToProto(s.provider.ConnectionMode()),
		AuthTypes:              authTypesForProvider(s.provider),
		ConnectionParams:       connectionParamDefsToProto(defs),
		StaticCatalogJson:      staticCatalog,
		SupportsSessionCatalog: supportsSessionCatalog(s.provider),
		SupportsPostConnect:    supportsPostConnect(s.provider),
		AuthorizationBaseUrl:   authorizationBaseURLForProvider(s.provider),
		TokenUrl:               tokenURLForProvider(s.provider),
	}, nil
}

func (s *ProviderServer) ListOperations(_ context.Context, _ *emptypb.Empty) (*pluginapiv1.ListOperationsResponse, error) {
	ops, err := operationsToProto(s.provider.ListOperations())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode operations: %v", err)
	}
	return &pluginapiv1.ListOperationsResponse{Operations: ops}, nil
}

func (s *ProviderServer) Execute(ctx context.Context, req *pluginapiv1.ExecuteRequest) (*pluginapiv1.OperationResult, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if len(req.GetConnectionParams()) > 0 {
		ctx = core.WithConnectionParams(ctx, req.GetConnectionParams())
	}
	result, err := s.provider.Execute(ctx, req.GetOperation(), mapFromStruct(req.GetParams()), req.GetToken())
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "execute: %v", err)
	}
	return &pluginapiv1.OperationResult{
		Status: int32(result.Status),
		Body:   result.Body,
	}, nil
}

func (s *ProviderServer) AuthorizationURL(_ context.Context, req *pluginapiv1.AuthorizationURLRequest) (*pluginapiv1.AuthorizationURLResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	oauthProv, ok := s.provider.(core.OAuthProvider)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "provider does not support OAuth")
	}

	authBaseURL := resolveAuthorizationBaseURL(s.provider, req.GetAuthBaseUrl(), req.GetConnectionParams())
	if authBaseURL != "" {
		if ov, ok := s.provider.(interface {
			StartOAuthWithOverride(authBaseURL, state string, scopes []string) (string, string)
		}); ok {
			url, verifier := ov.StartOAuthWithOverride(authBaseURL, req.GetState(), slices.Clone(req.GetScopes()))
			return &pluginapiv1.AuthorizationURLResponse{Url: url, Verifier: verifier}, nil
		}
	}
	if req.GetIncludeVerifier() {
		if starter, ok := s.provider.(interface {
			StartOAuth(state string, scopes []string) (string, string)
		}); ok {
			url, verifier := starter.StartOAuth(req.GetState(), slices.Clone(req.GetScopes()))
			return &pluginapiv1.AuthorizationURLResponse{Url: url, Verifier: verifier}, nil
		}
	}
	return &pluginapiv1.AuthorizationURLResponse{
		Url: oauthProv.AuthorizationURL(req.GetState(), slices.Clone(req.GetScopes())),
	}, nil
}

func (s *ProviderServer) ExchangeCode(ctx context.Context, req *pluginapiv1.ExchangeCodeRequest) (*pluginapiv1.TokenResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	oauthProv, ok := s.provider.(core.OAuthProvider)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "provider does not support OAuth")
	}
	if len(req.GetConnectionParams()) > 0 {
		ctx = core.WithConnectionParams(ctx, req.GetConnectionParams())
	}

	tokenURL := resolveTokenURL(s.provider, req.GetTokenUrl(), req.GetConnectionParams())

	var resp *core.TokenResponse
	var err error
	if req.GetVerifier() != "" || tokenURL != "" {
		if exchanger, ok := s.provider.(interface {
			ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...oauth.ExchangeOption) (*core.TokenResponse, error)
		}); ok {
			var opts []oauth.ExchangeOption
			if tokenURL != "" {
				opts = append(opts, oauth.WithTokenURL(tokenURL))
			}
			resp, err = exchanger.ExchangeCodeWithVerifier(ctx, req.GetCode(), req.GetVerifier(), opts...)
		} else {
			resp, err = oauthProv.ExchangeCode(ctx, req.GetCode())
		}
	} else {
		resp, err = oauthProv.ExchangeCode(ctx, req.GetCode())
	}
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "exchange code: %v", err)
	}
	msg, err := tokenResponseToProto(resp)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode token response: %v", err)
	}
	return msg, nil
}

func (s *ProviderServer) RefreshToken(ctx context.Context, req *pluginapiv1.RefreshTokenRequest) (*pluginapiv1.TokenResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	oauthProv, ok := s.provider.(core.OAuthProvider)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "provider does not support OAuth")
	}
	if len(req.GetConnectionParams()) > 0 {
		ctx = core.WithConnectionParams(ctx, req.GetConnectionParams())
	}

	tokenURL := resolveTokenURL(s.provider, req.GetTokenUrl(), req.GetConnectionParams())

	var resp *core.TokenResponse
	var err error
	if tokenURL != "" {
		if refresher, ok := s.provider.(interface {
			RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error)
		}); ok {
			resp, err = refresher.RefreshTokenWithURL(ctx, req.GetRefreshToken(), tokenURL)
		} else {
			resp, err = oauthProv.RefreshToken(ctx, req.GetRefreshToken())
		}
	} else {
		resp, err = oauthProv.RefreshToken(ctx, req.GetRefreshToken())
	}
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "refresh token: %v", err)
	}
	msg, err := tokenResponseToProto(resp)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode token response: %v", err)
	}
	return msg, nil
}

func (s *ProviderServer) GetSessionCatalog(ctx context.Context, req *pluginapiv1.GetSessionCatalogRequest) (*pluginapiv1.GetSessionCatalogResponse, error) {
	scp, ok := s.provider.(core.SessionCatalogProvider)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "provider does not support session catalogs")
	}
	if len(req.GetConnectionParams()) > 0 {
		ctx = core.WithConnectionParams(ctx, req.GetConnectionParams())
	}
	cat, err := scp.CatalogForRequest(ctx, req.GetToken())
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "session catalog: %v", err)
	}
	raw, err := catalogToJSON(cat)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode session catalog: %v", err)
	}
	return &pluginapiv1.GetSessionCatalogResponse{CatalogJson: raw}, nil
}

func (s *ProviderServer) PostConnect(ctx context.Context, req *pluginapiv1.PostConnectRequest) (*pluginapiv1.PostConnectResponse, error) {
	pcp, ok := s.provider.(core.PostConnectProvider)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "provider does not support post-connect")
	}
	hook := pcp.PostConnectHook()
	if hook == nil {
		return nil, status.Error(codes.Unimplemented, "provider returned nil post-connect hook")
	}
	if req == nil || req.Token == nil {
		return nil, status.Error(codes.InvalidArgument, "token is required")
	}
	metadata, err := hook(ctx, integrationTokenFromProto(req.GetToken()), s.httpClient)
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "post-connect: %v", err)
	}
	return &pluginapiv1.PostConnectResponse{Metadata: metadata}, nil
}

func authTypesForProvider(prov core.Provider) []string {
	if atl, ok := prov.(core.AuthTypeLister); ok {
		return slices.Clone(atl.AuthTypes())
	}
	_, hasOAuth := prov.(core.OAuthProvider)
	hasManual := false
	if mp, ok := prov.(core.ManualProvider); ok {
		hasManual = mp.SupportsManualAuth()
	}
	switch {
	case hasOAuth && hasManual:
		return []string{"oauth", "manual"}
	case hasManual:
		return []string{"manual"}
	default:
		return []string{"oauth"}
	}
}

func staticCatalogForProvider(prov core.Provider) *catalog.Catalog {
	if cp, ok := prov.(core.CatalogProvider); ok {
		if cat := cp.Catalog(); cat != nil {
			return cat
		}
	}
	return nil
}

func supportsSessionCatalog(prov core.Provider) bool {
	_, ok := prov.(core.SessionCatalogProvider)
	return ok
}

func supportsPostConnect(prov core.Provider) bool {
	pcp, ok := prov.(core.PostConnectProvider)
	return ok && pcp.PostConnectHook() != nil
}

func authorizationBaseURLForProvider(prov core.Provider) string {
	if abu, ok := prov.(interface{ AuthorizationBaseURL() string }); ok {
		return abu.AuthorizationBaseURL()
	}
	return ""
}

func tokenURLForProvider(prov core.Provider) string {
	if tu, ok := prov.(interface{ TokenURL() string }); ok {
		return tu.TokenURL()
	}
	return ""
}

func resolveAuthorizationBaseURL(prov core.Provider, explicit string, connParams map[string]string) string {
	if explicit != "" {
		return explicit
	}
	raw := authorizationBaseURLForProvider(prov)
	if raw == "" || len(connParams) == 0 {
		return ""
	}
	resolved := paraminterp.Interpolate(raw, connParams)
	if resolved == raw {
		return ""
	}
	return resolved
}

func resolveTokenURL(prov core.Provider, explicit string, connParams map[string]string) string {
	if explicit != "" {
		return explicit
	}
	raw := tokenURLForProvider(prov)
	if raw == "" || len(connParams) == 0 {
		return ""
	}
	resolved := paraminterp.Interpolate(raw, connParams)
	if resolved == raw {
		return ""
	}
	return resolved
}
