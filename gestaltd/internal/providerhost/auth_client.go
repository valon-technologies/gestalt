package providerhost

import (
	"context"
	"fmt"
	"io"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/session"
	"github.com/valon-technologies/gestalt/server/internal/authcallback"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/known/emptypb"
)

type authenticationRPCClient interface {
	BeginAuthentication(context.Context, *proto.BeginAuthenticationRequest, ...grpc.CallOption) (*proto.BeginAuthenticationResponse, error)
	CompleteAuthentication(context.Context, *proto.CompleteAuthenticationRequest, ...grpc.CallOption) (*proto.AuthenticatedUser, error)
	Authenticate(context.Context, *proto.AuthenticateRequest, ...grpc.CallOption) (*proto.AuthenticatedUser, error)
	GetSessionSettings(context.Context, *emptypb.Empty, ...grpc.CallOption) (*proto.AuthSessionSettings, error)
}

type AuthenticationExecConfig struct {
	Command      string
	Args         []string
	Env          map[string]string
	Config       map[string]any
	AllowedHosts []string
	HostBinary   string
	Cleanup      func()
	Name         string
	CallbackURL  string
	SessionKey   []byte
}

const defaultSessionTokenTTL = 24 * time.Hour

type remoteAuthenticationProvider struct {
	runtime     proto.ProviderLifecycleClient
	client      authenticationRPCClient
	conn        grpc.ClientConnInterface
	name        string
	displayName string
	description string
	callbackURL string
	sessionTTL  time.Duration
	sessionKey  []byte
	closer      io.Closer
}

func NewExecutableAuthenticationProvider(ctx context.Context, cfg AuthenticationExecConfig) (core.AuthenticationProvider, error) {
	execCfg := ExecConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Env:          cfg.Env,
		Config:       cfg.Config,
		AllowedHosts: cfg.AllowedHosts,
		HostBinary:   cfg.HostBinary,
		Cleanup:      cfg.Cleanup,
	}
	proc, err := startProviderProcess(ctx, execCfg.processConfig())
	if err != nil {
		return nil, err
	}

	runtimeClient := proto.NewProviderLifecycleClient(proc.conn)
	client := proto.NewAuthenticationProviderClient(proc.conn)
	provider, err := newRemoteAuthenticationProvider(ctx, runtimeClient, client, proc.conn, cfg)
	if err != nil {
		_ = proc.Close()
		return nil, err
	}
	provider.closer = proc
	return provider, nil
}

func newRemoteAuthenticationProvider(ctx context.Context, runtimeClient proto.ProviderLifecycleClient, client authenticationRPCClient, conn grpc.ClientConnInterface, cfg AuthenticationExecConfig) (*remoteAuthenticationProvider, error) {
	provider := &remoteAuthenticationProvider{
		runtime:     runtimeClient,
		client:      client,
		conn:        conn,
		name:        cfg.Name,
		callbackURL: cfg.CallbackURL,
		sessionKey:  append([]byte(nil), cfg.SessionKey...),
		sessionTTL:  defaultSessionTokenTTL,
	}
	if err := provider.configure(ctx, cfg.Name, cfg.Config); err != nil {
		return nil, err
	}
	return provider, nil
}

func (p *remoteAuthenticationProvider) configure(ctx context.Context, name string, config map[string]any) error {
	meta, err := configureRuntimeProvider(ctx, p.runtime, proto.ProviderKind_PROVIDER_KIND_AUTHENTICATION, name, config)
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
	if ttl := getAuthenticationSessionTTL(ctx, p.client); ttl > 0 {
		p.sessionTTL = ttl
	}
	return nil
}

func (p *remoteAuthenticationProvider) Name() string {
	return p.name
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

func (p *remoteAuthenticationProvider) SessionTokenTTL() time.Duration {
	return p.sessionTTL
}

func (p *remoteAuthenticationProvider) BeginAuthentication(ctx context.Context, req *core.BeginAuthenticationRequest) (*core.BeginAuthenticationResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("begin authentication: request is required")
	}
	ctx, cancel := providerCallContext(ctx)
	defer cancel()

	callbackURL := p.callbackURL
	if req.CallbackURL != "" {
		callbackURL = req.CallbackURL
	}
	resp, err := p.client.BeginAuthentication(ctx, &proto.BeginAuthenticationRequest{
		CallbackUrl: callbackURL,
		HostState:   req.HostState,
		Scopes:      append([]string(nil), req.Scopes...),
		Options:     cloneStringMap(req.Options),
	})
	if err != nil {
		if status.Code(err) != codes.Unimplemented {
			return nil, fmt.Errorf("begin authentication: %w", err)
		}
		legacyResp, legacyErr := p.beginLoginCompat(ctx, &proto.BeginAuthenticationRequest{
			CallbackUrl: callbackURL,
			HostState:   req.HostState,
			Scopes:      append([]string(nil), req.Scopes...),
			Options:     cloneStringMap(req.Options),
		})
		if legacyErr != nil {
			return nil, fmt.Errorf("begin authentication: %w", legacyErr)
		}
		resp = &proto.BeginAuthenticationResponse{
			AuthorizationUrl: legacyResp.GetAuthorizationUrl(),
			ProviderState:    append([]byte(nil), legacyResp.GetProviderState()...),
		}
	}
	if resp == nil {
		return nil, fmt.Errorf("begin authentication: provider returned nil response")
	}
	return &core.BeginAuthenticationResponse{
		AuthorizationURL: resp.GetAuthorizationUrl(),
		ProviderState:    append([]byte(nil), resp.GetProviderState()...),
	}, nil
}

func (p *remoteAuthenticationProvider) CompleteAuthentication(ctx context.Context, req *core.CompleteAuthenticationRequest) (*core.UserIdentity, error) {
	if req == nil {
		return nil, fmt.Errorf("complete authentication: request is required")
	}
	hostState, providerState, normalizedQuery, err := decodeAndNormalizeAuthenticationQuery(req.Query, req.ProviderState)
	if err != nil {
		return nil, err
	}

	ctx, cancel := providerCallContext(ctx)
	defer cancel()

	callbackURL := p.callbackURL
	if req.CallbackURL != "" {
		callbackURL = req.CallbackURL
	}
	resp, err := p.client.CompleteAuthentication(ctx, &proto.CompleteAuthenticationRequest{
		Query:         cloneStringMap(normalizedQuery),
		ProviderState: append([]byte(nil), providerState...),
		CallbackUrl:   callbackURL,
	})
	if err != nil {
		if status.Code(err) != codes.Unimplemented {
			return nil, fmt.Errorf("complete authentication: %w", err)
		}
		resp, err = p.completeLoginCompat(ctx, &proto.CompleteAuthenticationRequest{
			Query:         cloneStringMap(normalizedQuery),
			ProviderState: append([]byte(nil), providerState...),
			CallbackUrl:   callbackURL,
		})
		if err != nil {
			return nil, fmt.Errorf("complete authentication: %w", err)
		}
	}
	user := authenticatedUserFromProto(resp)
	if user == nil {
		return nil, fmt.Errorf("complete authentication: provider returned nil user")
	}
	_ = hostState
	return user, nil
}

func (p *remoteAuthenticationProvider) Authenticate(ctx context.Context, req *core.AuthenticateRequest) (*core.UserIdentity, error) {
	if req == nil {
		return nil, fmt.Errorf("authenticate: request is required")
	}
	ctx, cancel := providerCallContext(ctx)
	defer cancel()

	var (
		user   *proto.AuthenticatedUser
		err    error
		jwtErr error
	)

	switch {
	case req.Token != nil:
		identity, sessionErr := p.validateSessionToken(req.Token.Token)
		jwtErr = sessionErr
		if jwtErr == nil {
			return identity, nil
		}

		user, err = p.client.Authenticate(ctx, &proto.AuthenticateRequest{
			Input: &proto.AuthenticateRequest_Token{
				Token: &proto.TokenAuthInput{Token: req.Token.Token},
			},
			Options: cloneStringMap(req.Options),
		})
		if err == nil {
			break
		}
		if status.Code(err) != codes.Unimplemented {
			return nil, fmt.Errorf("authenticate: %w", err)
		}

		user, err = p.validateExternalTokenCompat(ctx, req.Token.Token)
		if err != nil {
			if status.Code(err) == codes.Unimplemented {
				if jwtErr != nil && jwtErr != session.ErrNotJWT {
					return nil, jwtErr
				}
				return nil, fmt.Errorf("authenticate: authentication provider does not support external authentication")
			}
			return nil, fmt.Errorf("authenticate: %w", err)
		}
	case req.HTTP != nil:
		user, err = p.client.Authenticate(ctx, &proto.AuthenticateRequest{
			Input: &proto.AuthenticateRequest_Http{
				Http: &proto.HTTPRequestAuthInput{
					Method:  req.HTTP.Method,
					Url:     req.HTTP.URL,
					Headers: cloneStringMap(req.HTTP.Headers),
					Query:   cloneStringMap(req.HTTP.Query),
				},
			},
			Options: cloneStringMap(req.Options),
		})
		if err != nil {
			if status.Code(err) == codes.Unimplemented {
				return nil, fmt.Errorf("authenticate: authentication provider does not support external authentication")
			}
			return nil, fmt.Errorf("authenticate: %w", err)
		}
	default:
		return nil, fmt.Errorf("authenticate: input is required")
	}
	authenticated := authenticatedUserFromProto(user)
	if authenticated == nil {
		return nil, fmt.Errorf("authenticate: provider returned nil user")
	}
	return authenticated, nil
}

func (p *remoteAuthenticationProvider) Close() error {
	if p == nil || p.closer == nil {
		return nil
	}
	return p.closer.Close()
}

func (p *remoteAuthenticationProvider) validateSessionToken(token string) (*core.UserIdentity, error) {
	if len(p.sessionKey) == 0 {
		return nil, session.ErrNotJWT
	}
	return session.ValidateToken(token, p.sessionKey)
}

func (p *remoteAuthenticationProvider) beginLoginCompat(ctx context.Context, req *proto.BeginAuthenticationRequest) (*proto.BeginAuthenticationResponse, error) {
	if p.conn == nil {
		return nil, status.Error(codes.Unimplemented, "legacy begin login RPC unavailable")
	}
	resp := new(proto.BeginAuthenticationResponse)
	if err := p.conn.Invoke(ctx, proto.AuthenticationProvider_BeginLogin_FullMethodName, req, resp, grpc.StaticMethod()); err != nil {
		return nil, err
	}
	return resp, nil
}

func (p *remoteAuthenticationProvider) completeLoginCompat(ctx context.Context, req *proto.CompleteAuthenticationRequest) (*proto.AuthenticatedUser, error) {
	if p.conn == nil {
		return nil, status.Error(codes.Unimplemented, "legacy complete login RPC unavailable")
	}
	resp := new(proto.AuthenticatedUser)
	if err := p.conn.Invoke(ctx, proto.AuthenticationProvider_CompleteLogin_FullMethodName, req, resp, grpc.StaticMethod()); err != nil {
		return nil, err
	}
	return resp, nil
}

func (p *remoteAuthenticationProvider) validateExternalTokenCompat(ctx context.Context, token string) (*proto.AuthenticatedUser, error) {
	if p.conn == nil {
		return nil, status.Error(codes.Unimplemented, "legacy validate external token RPC unavailable")
	}
	resp := new(proto.AuthenticatedUser)
	req, err := legacyValidateExternalTokenRequest(token)
	if err != nil {
		return nil, err
	}
	if err := p.conn.Invoke(ctx, proto.AuthenticationProvider_ValidateExternalToken_FullMethodName, req, resp, grpc.StaticMethod()); err != nil {
		return nil, err
	}
	return resp, nil
}

func getAuthenticationSessionTTL(ctx context.Context, client authenticationRPCClient) time.Duration {
	if client == nil {
		return 0
	}
	ctx, cancel := providerCallContext(ctx)
	defer cancel()

	resp, err := client.GetSessionSettings(ctx, &emptypb.Empty{})
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return 0
		}
		return 0
	}
	if resp == nil || resp.GetSessionTtlSeconds() <= 0 {
		return 0
	}
	return time.Duration(resp.GetSessionTtlSeconds()) * time.Second
}

func authenticatedUserFromProto(user *proto.AuthenticatedUser) *core.UserIdentity {
	if user == nil {
		return nil
	}
	return &core.UserIdentity{
		Email:       user.GetEmail(),
		DisplayName: user.GetDisplayName(),
		AvatarURL:   user.GetAvatarUrl(),
	}
}

func decodeAndNormalizeAuthenticationQuery(query map[string]string, providerState []byte) (string, []byte, map[string]string, error) {
	hostState, wrappedProviderState, upstreamState, err := authcallback.DecodeState(query["state"])
	if err != nil {
		return "", nil, nil, err
	}
	normalizedQuery := cloneStringMap(query)
	if upstreamState != "" {
		normalizedQuery["state"] = upstreamState
	} else {
		delete(normalizedQuery, "state")
	}
	if len(providerState) > 0 {
		wrappedProviderState = append([]byte(nil), providerState...)
	}
	return hostState, wrappedProviderState, normalizedQuery, nil
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func legacyValidateExternalTokenRequest(token string) (*dynamicpb.Message, error) {
	messageDesc := proto.File_v1_authentication_proto.Messages().ByName("ValidateExternalTokenRequest")
	if messageDesc == nil {
		return nil, status.Error(codes.Internal, "legacy validate external token descriptor unavailable")
	}
	fieldDesc := messageDesc.Fields().ByName(protoreflect.Name("token"))
	if fieldDesc == nil {
		return nil, status.Error(codes.Internal, "legacy validate external token field unavailable")
	}
	message := dynamicpb.NewMessage(messageDesc)
	message.Set(fieldDesc, protoreflect.ValueOfString(token))
	return message, nil
}

var (
	_ core.AuthenticationProvider = (*remoteAuthenticationProvider)(nil)
	_ interface {
		DisplayName() string
		Description() string
		SessionTokenTTL() time.Duration
	} = (*remoteAuthenticationProvider)(nil)
	_ core.Authenticator         = (*remoteAuthenticationProvider)(nil)
	_ interface{ Close() error } = (*remoteAuthenticationProvider)(nil)
)
