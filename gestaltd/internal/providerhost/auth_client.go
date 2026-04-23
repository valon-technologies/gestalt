package providerhost

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/session"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type authenticationRPCClient interface {
	BeginLogin(context.Context, *proto.BeginLoginRequest, ...grpc.CallOption) (*proto.BeginLoginResponse, error)
	CompleteLogin(context.Context, *proto.CompleteLoginRequest, ...grpc.CallOption) (*proto.AuthenticatedUser, error)
	ValidateExternalToken(context.Context, *proto.ValidateExternalTokenRequest, ...grpc.CallOption) (*proto.AuthenticatedUser, error)
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
		ProviderName: cfg.Name,
	}
	proc, err := startProviderProcess(ctx, execCfg.processConfig())
	if err != nil {
		return nil, err
	}

	runtimeClient := proto.NewProviderLifecycleClient(proc.conn)
	client := proto.NewAuthenticationProviderClient(proc.conn)
	provider, err := newRemoteAuthenticationProvider(ctx, runtimeClient, client, cfg)
	if err != nil {
		_ = proc.Close()
		return nil, err
	}
	provider.closer = proc
	return provider, nil
}

func newRemoteAuthenticationProvider(ctx context.Context, runtimeClient proto.ProviderLifecycleClient, client authenticationRPCClient, cfg AuthenticationExecConfig) (*remoteAuthenticationProvider, error) {
	provider := &remoteAuthenticationProvider{
		runtime:     runtimeClient,
		client:      client,
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
	meta, err := ConfigureRuntimeProvider(ctx, p.runtime, proto.ProviderKind_PROVIDER_KIND_AUTHENTICATION, name, config)
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

func (p *remoteAuthenticationProvider) LoginURL(state string) (string, error) {
	return p.LoginURLContext(context.Background(), state)
}

func (p *remoteAuthenticationProvider) LoginURLContext(ctx context.Context, state string) (string, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()

	resp, err := p.client.BeginLogin(ctx, &proto.BeginLoginRequest{
		CallbackUrl: p.callbackURL,
		HostState:   state,
	})
	if err != nil {
		return "", fmt.Errorf("begin login: %w", err)
	}
	if resp == nil {
		return "", fmt.Errorf("begin login: provider returned nil response")
	}
	rewrittenURL, upstreamState, err := withWrappedStateParam(resp.GetAuthorizationUrl(), "")
	if err != nil {
		return "", err
	}
	encodedState, err := encodeAuthCallbackState(state, resp.GetProviderState(), upstreamState)
	if err != nil {
		return "", err
	}
	wrappedURL, _, err := withWrappedStateParam(rewrittenURL, encodedState)
	if err != nil {
		return "", err
	}
	return wrappedURL, nil
}

func (p *remoteAuthenticationProvider) HandleCallback(ctx context.Context, code string) (*core.UserIdentity, error) {
	identity, _, err := p.HandleCallbackWithState(ctx, code, "")
	return identity, err
}

func (p *remoteAuthenticationProvider) HandleCallbackWithState(ctx context.Context, code, rawState string) (*core.UserIdentity, string, error) {
	values := url.Values{}
	if code != "" {
		values.Set("code", code)
	}
	if rawState != "" {
		values.Set("state", rawState)
	}
	return p.HandleCallbackRequest(ctx, values)
}

func (p *remoteAuthenticationProvider) HandleCallbackRequest(ctx context.Context, query url.Values) (*core.UserIdentity, string, error) {
	if query == nil {
		query = url.Values{}
	}
	hostState, providerState, upstreamState, err := decodeAuthCallbackState(query.Get("state"))
	if err != nil {
		return nil, "", err
	}
	normalizedQuery := cloneQueryValues(query)
	if upstreamState != "" {
		normalizedQuery.Set("state", upstreamState)
	} else {
		normalizedQuery.Del("state")
	}

	ctx, cancel := providerCallContext(ctx)
	defer cancel()

	resp, err := p.client.CompleteLogin(ctx, &proto.CompleteLoginRequest{
		Query:         firstQueryValues(normalizedQuery),
		ProviderState: append([]byte(nil), providerState...),
		CallbackUrl:   p.callbackURL,
	})
	if err != nil {
		return nil, "", fmt.Errorf("complete login: %w", err)
	}
	user := authenticatedUserFromProto(resp)
	if user == nil {
		return nil, "", fmt.Errorf("complete login: provider returned nil user")
	}
	return user, hostState, nil
}

func (p *remoteAuthenticationProvider) ValidateToken(ctx context.Context, token string) (*core.UserIdentity, error) {
	identity, jwtErr := p.validateSessionToken(token)
	if jwtErr == nil {
		return identity, nil
	}

	ctx, cancel := providerCallContext(ctx)
	defer cancel()

	user, err := p.client.ValidateExternalToken(ctx, &proto.ValidateExternalTokenRequest{Token: token})
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			if jwtErr != nil && jwtErr != session.ErrNotJWT {
				return nil, jwtErr
			}
			return nil, fmt.Errorf("validate token: authentication provider does not support external token validation")
		}
		return nil, fmt.Errorf("validate token: %w", err)
	}
	authenticated := authenticatedUserFromProto(user)
	if authenticated == nil {
		return nil, fmt.Errorf("validate token: provider returned nil user")
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

type authCallbackState struct {
	HostState     string `json:"host_state"`
	ProviderState string `json:"provider_state,omitempty"`
	UpstreamState string `json:"upstream_state,omitempty"`
}

func encodeAuthCallbackState(hostState string, providerState []byte, upstreamState string) (string, error) {
	payload := authCallbackState{HostState: hostState}
	if len(providerState) > 0 {
		payload.ProviderState = base64.RawURLEncoding.EncodeToString(providerState)
	}
	if upstreamState != "" {
		payload.UpstreamState = upstreamState
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode auth callback state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeAuthCallbackState(raw string) (string, []byte, string, error) {
	if raw == "" {
		return "", nil, "", nil
	}
	data, ok := decodeOptionalBase64URL(raw)
	if !ok {
		return raw, nil, raw, nil
	}
	payload, ok := decodeOptionalAuthCallbackState(data)
	if !ok {
		return raw, nil, raw, nil
	}
	if payload.ProviderState == "" {
		return payload.HostState, nil, payload.UpstreamState, nil
	}
	providerState, err := base64.RawURLEncoding.DecodeString(payload.ProviderState)
	if err != nil {
		return "", nil, "", fmt.Errorf("decode auth callback state: %w", err)
	}
	return payload.HostState, providerState, payload.UpstreamState, nil
}

func decodeOptionalBase64URL(raw string) ([]byte, bool) {
	data, err := base64.RawURLEncoding.DecodeString(raw)
	return data, err == nil
}

func decodeOptionalAuthCallbackState(data []byte) (authCallbackState, bool) {
	var payload authCallbackState
	if err := json.Unmarshal(data, &payload); err != nil || payload.HostState == "" {
		return authCallbackState{}, false
	}
	return payload, true
}

func withWrappedStateParam(rawURL, state string) (string, string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", "", fmt.Errorf("parse authorization URL: %w", err)
	}
	values := parsed.Query()
	originalState := values.Get("state")
	values.Set("state", state)
	parsed.RawQuery = values.Encode()
	return parsed.String(), originalState, nil
}

func firstQueryValues(values url.Values) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, candidates := range values {
		if len(candidates) > 0 {
			out[key] = candidates[0]
		}
	}
	return out
}

func cloneQueryValues(values url.Values) url.Values {
	if len(values) == 0 {
		return url.Values{}
	}
	cloned := make(url.Values, len(values))
	for key, candidates := range values {
		cloned[key] = append([]string(nil), candidates...)
	}
	return cloned
}

var (
	_ core.AuthenticationProvider = (*remoteAuthenticationProvider)(nil)
	_ interface {
		DisplayName() string
		Description() string
		SessionTokenTTL() time.Duration
		HandleCallbackWithState(context.Context, string, string) (*core.UserIdentity, string, error)
		HandleCallbackRequest(context.Context, url.Values) (*core.UserIdentity, string, error)
	} = (*remoteAuthenticationProvider)(nil)
	_ interface{ Close() error } = (*remoteAuthenticationProvider)(nil)
)
