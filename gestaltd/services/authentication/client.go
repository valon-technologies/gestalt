package authentication

import (
	"context"
	"io"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type authenticationRPCClient interface {
	Authorize(context.Context, *proto.AuthorizeRequest, ...grpc.CallOption) (*proto.AuthorizeResponse, error)
	Token(context.Context, *proto.TokenRequest, ...grpc.CallOption) (*proto.TokenResponse, error)
	Introspect(context.Context, *proto.IntrospectRequest, ...grpc.CallOption) (*proto.IntrospectResponse, error)
	UserInfo(context.Context, *proto.UserInfoRequest, ...grpc.CallOption) (*proto.UserInfoResponse, error)
	ListGrants(context.Context, *proto.ListGrantsRequest, ...grpc.CallOption) (*proto.ListGrantsResponse, error)
	GetGrant(context.Context, *proto.GetGrantRequest, ...grpc.CallOption) (*proto.GetGrantResponse, error)
	RevokeGrant(context.Context, *proto.RevokeGrantRequest, ...grpc.CallOption) (*proto.RevokeGrantResponse, error)
}

type ExecConfig struct {
	Command      string
	Args         []string
	Workdir      string
	Env          map[string]string
	Config       map[string]any
	Egress       egress.Policy
	HostBinary   string
	Cleanup      func()
	HostServices []runtimehost.HostService
	Name         string
	CallbackURL  string
}

type remoteAuthenticationProvider struct {
	runtime     proto.ProviderLifecycleClient
	client      authenticationRPCClient
	name        string
	displayName string
	description string
	callbackURL string
	closer      io.Closer
}

func NewExecutable(ctx context.Context, cfg ExecConfig) (core.AuthenticationProvider, error) {
	proc, err := runtimehost.StartAppProcess(ctx, runtimehost.ProcessConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Workdir:      cfg.Workdir,
		Env:          cfg.Env,
		Egress:       cfg.Egress,
		HostBinary:   cfg.HostBinary,
		Cleanup:      cfg.Cleanup,
		HostServices: cfg.HostServices,
		ProviderName: cfg.Name,
	})
	if err != nil {
		return nil, err
	}

	runtimeClient := proc.Lifecycle()
	client := proto.NewAuthenticationClient(proc.Conn())
	provider, err := newRemoteAuthenticationProvider(ctx, runtimeClient, client, cfg)
	if err != nil {
		_ = proc.Close()
		return nil, err
	}
	provider.closer = proc
	return provider, nil
}

func newRemoteAuthenticationProvider(ctx context.Context, runtimeClient proto.ProviderLifecycleClient, client authenticationRPCClient, cfg ExecConfig) (*remoteAuthenticationProvider, error) {
	provider := &remoteAuthenticationProvider{
		runtime:     runtimeClient,
		client:      client,
		name:        cfg.Name,
		callbackURL: cfg.CallbackURL,
	}
	if err := provider.configure(ctx, cfg.Name, cfg.Config); err != nil {
		return nil, err
	}
	return provider, nil
}

func (p *remoteAuthenticationProvider) configure(ctx context.Context, name string, config map[string]any) error {
	meta, err := runtimehost.ConfigureRuntimeProvider(ctx, p.runtime, proto.ProviderKind_PROVIDER_KIND_AUTHENTICATION, name, config)
	if err != nil {
		return err
	}
	p.name = name
	if meta != nil && meta.Name != "" {
		p.name = meta.Name
	}
	if p.name == "" {
		p.name = "authentication"
	}
	if meta != nil {
		p.displayName = meta.DisplayName
		p.description = meta.Description
	}
	return nil
}

func (p *remoteAuthenticationProvider) DisplayName() string {
	if p.displayName == "" {
		return p.name
	}
	return p.displayName
}

func (p *remoteAuthenticationProvider) Description() string {
	return p.description
}

func (p *remoteAuthenticationProvider) Authorize(ctx context.Context, req *core.AuthorizeRequest) (*core.AuthorizeResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()

	resp, err := p.client.Authorize(ctx, authorizeRequestToProto(req))
	if err != nil {
		return nil, err
	}
	return authorizeResponseFromProto(resp), nil
}

func (p *remoteAuthenticationProvider) Token(ctx context.Context, req *core.TokenRequest) (*core.TokenResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()

	resp, err := p.client.Token(ctx, tokenRequestToProto(req))
	if err != nil {
		return nil, err
	}
	return tokenResponseFromProto(resp), nil
}

func (p *remoteAuthenticationProvider) Introspect(ctx context.Context, req *core.IntrospectRequest) (*core.IntrospectResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()

	resp, err := p.client.Introspect(ctx, introspectRequestToProto(req))
	if err != nil {
		return nil, err
	}
	return introspectResponseFromProto(resp), nil
}

func (p *remoteAuthenticationProvider) UserInfo(ctx context.Context, req *core.UserInfoRequest) (*core.UserInfoResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	ctx = p.outgoingAuthCallContext(ctx)

	resp, err := p.client.UserInfo(ctx, userInfoRequestToProto(req))
	if err != nil {
		return nil, mapAuthenticationProviderRPCError(err)
	}
	return userInfoResponseFromProto(resp), nil
}

func (p *remoteAuthenticationProvider) ListGrants(ctx context.Context, req *core.ListGrantsRequest) (*core.ListGrantsResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	ctx = p.outgoingAuthCallContext(ctx)

	resp, err := p.client.ListGrants(ctx, &proto.ListGrantsRequest{})
	if err != nil {
		return nil, mapAuthenticationProviderRPCError(err)
	}
	return listGrantsResponseFromProto(resp), nil
}

func (p *remoteAuthenticationProvider) GetGrant(ctx context.Context, req *core.GetGrantRequest) (*core.GetGrantResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	ctx = p.outgoingAuthCallContext(ctx)

	resp, err := p.client.GetGrant(ctx, getGrantRequestToProto(req))
	if err != nil {
		return nil, mapAuthenticationProviderRPCError(err)
	}
	return getGrantResponseFromProto(resp), nil
}

func (p *remoteAuthenticationProvider) RevokeGrant(ctx context.Context, req *core.RevokeGrantRequest) (*core.RevokeGrantResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	ctx = p.outgoingAuthCallContext(ctx)

	_, err := p.client.RevokeGrant(ctx, revokeGrantRequestToProto(req))
	if err != nil {
		return nil, mapAuthenticationProviderRPCError(err)
	}
	return &core.RevokeGrantResponse{}, nil
}

func (p *remoteAuthenticationProvider) Close() error {
	if p == nil || p.closer == nil {
		return nil
	}
	return p.closer.Close()
}

func (p *remoteAuthenticationProvider) outgoingAuthCallContext(ctx context.Context) context.Context {
	return gestalt.AppendAuthCallMetadata(ctx)
}

func authorizeRequestToProto(req *core.AuthorizeRequest) *proto.AuthorizeRequest {
	if req == nil {
		return nil
	}
	return &proto.AuthorizeRequest{
		ResponseType: req.ResponseType,
		ClientId:     req.ClientID,
		RedirectUri:  req.RedirectURI,
		Scope:        req.Scope,
		State:        req.State,
	}
}

func authorizeResponseFromProto(resp *proto.AuthorizeResponse) *core.AuthorizeResponse {
	if resp == nil {
		return nil
	}
	return &core.AuthorizeResponse{RedirectURI: resp.GetRedirectUri()}
}

func tokenRequestToProto(req *core.TokenRequest) *proto.TokenRequest {
	if req == nil {
		return nil
	}
	return &proto.TokenRequest{
		GrantType:        req.GrantType,
		Code:             req.Code,
		RedirectUri:      req.RedirectURI,
		ClientId:         req.ClientID,
		State:            req.State,
		Scope:            req.Scope,
		SubjectToken:     req.SubjectToken,
		SubjectTokenType: req.SubjectTokenType,
		RequestedTtl:     req.RequestedTTL,
	}
}

func tokenResponseFromProto(resp *proto.TokenResponse) *core.TokenResponse {
	if resp == nil {
		return nil
	}
	return &core.TokenResponse{
		AccessToken:  resp.GetAccessToken(),
		TokenType:    resp.GetTokenType(),
		ExpiresIn:    int(resp.GetExpiresIn()),
		RefreshToken: resp.GetRefreshToken(),
		Scope:        resp.GetScope(),
		GrantID:      resp.GetGrantId(),
	}
}

func introspectRequestToProto(req *core.IntrospectRequest) *proto.IntrospectRequest {
	if req == nil {
		return nil
	}
	return &proto.IntrospectRequest{
		Token:         req.Token,
		TokenTypeHint: req.TokenTypeHint,
	}
}

func introspectResponseFromProto(resp *proto.IntrospectResponse) *core.IntrospectResponse {
	if resp == nil {
		return nil
	}
	return &core.IntrospectResponse{
		Active:   resp.GetActive(),
		Subject:  resp.GetSubject(),
		Scope:    resp.GetScope(),
		ClientID: resp.GetClientId(),
		Audience: append([]string(nil), resp.GetAudience()...),
	}
}

func listGrantsResponseFromProto(resp *proto.ListGrantsResponse) *core.ListGrantsResponse {
	if resp == nil {
		return nil
	}
	return &core.ListGrantsResponse{GrantIDs: append([]string(nil), resp.GetGrantIds()...)}
}

func getGrantRequestToProto(req *core.GetGrantRequest) *proto.GetGrantRequest {
	if req == nil {
		return nil
	}
	return &proto.GetGrantRequest{GrantId: req.GrantID}
}

func getGrantResponseFromProto(resp *proto.GetGrantResponse) *core.GetGrantResponse {
	if resp == nil {
		return nil
	}
	out := &core.GetGrantResponse{
		CreatedAt: resp.GetCreatedAt(),
		ExpiresAt: resp.GetExpiresAt(),
	}
	for _, scope := range resp.GetScopes() {
		if scope == nil {
			continue
		}
		out.Scopes = append(out.Scopes, core.GrantScope{
			Scope:    scope.GetScope(),
			Resource: append([]string(nil), scope.GetResource()...),
		})
	}
	return out
}

func revokeGrantRequestToProto(req *core.RevokeGrantRequest) *proto.RevokeGrantRequest {
	if req == nil {
		return nil
	}
	return &proto.RevokeGrantRequest{GrantId: req.GrantID}
}

func userInfoRequestToProto(req *core.UserInfoRequest) *proto.UserInfoRequest {
	return &proto.UserInfoRequest{}
}

func userInfoResponseFromProto(resp *proto.UserInfoResponse) *core.UserInfoResponse {
	if resp == nil {
		return nil
	}
	return &core.UserInfoResponse{
		SubjectID: resp.GetSubjectId(),
		Email:     resp.GetEmail(),
		Name:      resp.GetName(),
	}
}

// WithCallerBearerToken attaches the caller bearer token for caller-relative RPCs.
func WithCallerBearerToken(ctx context.Context, token string) context.Context {
	ctx = gestalt.WithAuthCallContext(ctx, gestalt.AuthCallContext{CallerBearerToken: token})
	return metadata.AppendToOutgoingContext(ctx, gestalt.CallerBearerTokenMetadataKey, token)
}

func mapAuthenticationProviderRPCError(err error) error {
	if err == nil {
		return nil
	}
	if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
		return core.ErrNotFound
	}
	return err
}

var (
	_ core.AuthenticationProvider = (*remoteAuthenticationProvider)(nil)
	_ interface {
		DisplayName() string
		Description() string
	} = (*remoteAuthenticationProvider)(nil)
	_ interface{ Close() error } = (*remoteAuthenticationProvider)(nil)
)
