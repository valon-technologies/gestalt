package externalcredentials

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

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
}

type remoteExternalCredentialProvider struct {
	client proto.ExternalCredentialsClient
	closer io.Closer
}

func NewExecutable(ctx context.Context, cfg ExecConfig) (core.ExternalCredentialProvider, error) {
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
	client := proto.NewExternalCredentialsClient(proc.Conn())
	if _, err := runtimehost.ConfigureRuntimeProvider(ctx, runtimeClient, proto.ProviderKind_PROVIDER_KIND_EXTERNAL_CREDENTIAL, cfg.Name, cfg.Config); err != nil {
		_ = proc.Close()
		return nil, err
	}

	return &remoteExternalCredentialProvider{client: client, closer: proc}, nil
}

func (r *remoteExternalCredentialProvider) CreateCredential(ctx context.Context, credential *core.ExternalCredential) error {
	if credential == nil {
		return fmt.Errorf("external credential is required")
	}
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()

	resp, err := r.client.CreateCredential(ctx, &proto.CreateExternalCredentialRequest{
		Credential: externalCredentialToProto(credential),
	})
	if err != nil {
		return externalCredentialRPCError("create external credential", err)
	}
	if resp == nil {
		return fmt.Errorf("create external credential: provider returned nil credential")
	}
	*credential = *externalCredentialFromProto(resp)
	return nil
}

func (r *remoteExternalCredentialProvider) UpsertCredential(ctx context.Context, credential *core.ExternalCredential) error {
	if credential == nil {
		return fmt.Errorf("external credential is required")
	}
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()

	resp, err := r.client.UpsertCredential(ctx, &proto.UpsertExternalCredentialRequest{
		Credential: externalCredentialToProto(credential),
	})
	if err != nil {
		return fmt.Errorf("upsert external credential: %w", err)
	}
	if resp == nil {
		return fmt.Errorf("upsert external credential: provider returned nil credential")
	}
	*credential = *externalCredentialFromProto(resp)
	return nil
}

func (r *remoteExternalCredentialProvider) GetCredential(ctx context.Context, subject, audience, qualifier string) (*core.ExternalCredential, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()

	resp, err := r.client.GetCredential(ctx, &proto.GetExternalCredentialRequest{
		Subject:   strings.TrimSpace(subject),
		Audience:  strings.TrimSpace(audience),
		Qualifier: strings.TrimSpace(qualifier),
	})
	if err != nil {
		return nil, externalCredentialRPCError("get external credential", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("get external credential: provider returned nil credential")
	}
	return externalCredentialFromProto(resp), nil
}

func (r *remoteExternalCredentialProvider) ListCredentials(ctx context.Context, subject, audience string) ([]*core.ExternalCredential, error) {
	return r.listCredentials(ctx, &proto.ListExternalCredentialsRequest{
		Subject:  strings.TrimSpace(subject),
		Audience: strings.TrimSpace(audience),
	})
}

func (r *remoteExternalCredentialProvider) DeleteCredential(ctx context.Context, id string) error {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()

	_, err := r.client.DeleteCredential(ctx, &proto.DeleteExternalCredentialRequest{
		Id: strings.TrimSpace(id),
	})
	if status.Code(err) == codes.NotFound {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete external credential: %w", err)
	}
	return nil
}

func (r *remoteExternalCredentialProvider) ValidateCredentialConfig(ctx context.Context, req *core.ValidateExternalCredentialConfigRequest) error {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()

	_, err := r.client.ValidateCredentialConfig(ctx, validateCredentialConfigToProto(req))
	if err != nil {
		return externalCredentialRPCError("validate external credential config", err)
	}
	return nil
}

func (r *remoteExternalCredentialProvider) ResolveCredential(ctx context.Context, req *core.ResolveExternalCredentialRequest) (*core.ResolveExternalCredentialResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()

	resp, err := r.client.ResolveCredential(ctx, resolveCredentialRequestToProto(req))
	if err != nil {
		return nil, externalCredentialRPCError("resolve external credential", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("resolve external credential: provider returned nil response")
	}
	return resolveCredentialResponseFromProto(resp), nil
}

func (r *remoteExternalCredentialProvider) ExchangeCredential(ctx context.Context, req *core.ExchangeExternalCredentialRequest) (*core.ExchangeExternalCredentialResponse, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()

	resp, err := r.client.ExchangeCredential(ctx, exchangeCredentialRequestToProto(req))
	if err != nil {
		return nil, externalCredentialRPCError("exchange external credential", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("exchange external credential: provider returned nil response")
	}
	return exchangeCredentialResponseFromProto(resp), nil
}

func (r *remoteExternalCredentialProvider) Close() error {
	if r == nil || r.closer == nil {
		return nil
	}
	return r.closer.Close()
}

func (r *remoteExternalCredentialProvider) listCredentials(ctx context.Context, req *proto.ListExternalCredentialsRequest) ([]*core.ExternalCredential, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()

	resp, err := r.client.ListCredentials(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("list external credentials: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("list external credentials: provider returned nil response")
	}
	out := make([]*core.ExternalCredential, 0, len(resp.GetCredentials()))
	for _, credential := range resp.GetCredentials() {
		out = append(out, externalCredentialFromProto(credential))
	}
	return out, nil
}

func externalCredentialToProto(credential *core.ExternalCredential) *proto.ExternalCredential {
	if credential == nil {
		return nil
	}
	out := &proto.ExternalCredential{
		Id:           credential.ID,
		Subject:      strings.TrimSpace(credential.Subject),
		Audience:     strings.TrimSpace(credential.Audience),
		Qualifier:    strings.TrimSpace(credential.Qualifier),
		MetadataJson: credential.MetadataJSON,
		CreatedAt:    timeToProto(nonZeroTimePtr(credential.CreatedAt)),
		UpdatedAt:    timeToProto(nonZeroTimePtr(credential.UpdatedAt)),
	}
	switch {
	case credential.Grant != nil:
		out.Credential = &proto.ExternalCredential_Grant{Grant: &proto.ExternalCredentialGrant{
			AccessToken:       credential.Grant.AccessToken,
			RefreshToken:      credential.Grant.RefreshToken,
			Scope:             credential.Grant.Scope,
			ExpiresAt:         timeToProto(credential.Grant.ExpiresAt),
			LastRefreshedAt:   timeToProto(credential.Grant.LastRefreshedAt),
			RefreshErrorCount: int32(credential.Grant.RefreshErrorCount),
		}}
	case credential.Client != nil:
		out.Credential = &proto.ExternalCredential_Client{Client: &proto.ExternalCredentialClientInfo{
			ClientId:              credential.Client.ClientID,
			ClientSecret:          credential.Client.ClientSecret,
			ClientSecretExpiresAt: timeToProto(credential.Client.ClientSecretExpiresAt),
		}}
	case credential.Opaque != nil:
		out.Credential = &proto.ExternalCredential_Opaque{Opaque: &proto.ExternalCredentialOpaque{
			Fields: cloneStringMap(credential.Opaque.Fields),
		}}
	}
	return out
}

func validateCredentialConfigToProto(req *core.ValidateExternalCredentialConfigRequest) *proto.ValidateExternalCredentialConfigRequest {
	if req == nil {
		return nil
	}
	return &proto.ValidateExternalCredentialConfigRequest{
		Provider:         strings.TrimSpace(req.Provider),
		Connection:       strings.TrimSpace(req.Connection),
		ConnectionId:     strings.TrimSpace(req.ConnectionID),
		Mode:             string(core.NormalizeConnectionMode(req.Mode)),
		Auth:             externalCredentialAuthConfigToProto(req.Auth),
		ConnectionParams: cloneStringMap(req.ConnectionParams),
	}
}

func resolveCredentialRequestToProto(req *core.ResolveExternalCredentialRequest) *proto.ResolveExternalCredentialRequest {
	if req == nil {
		return nil
	}
	return &proto.ResolveExternalCredentialRequest{
		Provider:            strings.TrimSpace(req.Provider),
		Connection:          strings.TrimSpace(req.Connection),
		ConnectionId:        strings.TrimSpace(req.ConnectionID),
		Mode:                string(core.NormalizeConnectionMode(req.Mode)),
		CredentialSubjectId: strings.TrimSpace(req.CredentialSubjectID),
		ActorSubjectId:      strings.TrimSpace(req.ActorSubjectID),
		Instance:            strings.TrimSpace(req.Instance),
		Auth:                externalCredentialAuthConfigToProto(req.Auth),
		ConnectionParams:    cloneStringMap(req.ConnectionParams),
	}
}

func resolveCredentialResponseFromProto(resp *proto.ResolveExternalCredentialResponse) *core.ResolveExternalCredentialResponse {
	if resp == nil {
		return nil
	}
	return &core.ResolveExternalCredentialResponse{
		Token:        resp.GetToken(),
		ExpiresAt:    timeFromProto(resp.GetExpiresAt()),
		MetadataJSON: resp.GetMetadataJson(),
		Params:       cloneStringMap(resp.GetParams()),
		Credential:   externalCredentialFromProto(resp.GetCredential()),
	}
}

func exchangeCredentialRequestToProto(req *core.ExchangeExternalCredentialRequest) *proto.ExchangeExternalCredentialRequest {
	if req == nil {
		return nil
	}
	return &proto.ExchangeExternalCredentialRequest{
		Provider:            strings.TrimSpace(req.Provider),
		Connection:          strings.TrimSpace(req.Connection),
		ConnectionId:        strings.TrimSpace(req.ConnectionID),
		CredentialSubjectId: strings.TrimSpace(req.CredentialSubjectID),
		ActorSubjectId:      strings.TrimSpace(req.ActorSubjectID),
		Instance:            strings.TrimSpace(req.Instance),
		Auth:                externalCredentialAuthConfigToProto(req.Auth),
		CredentialJson:      req.CredentialJSON,
		ConnectionParams:    cloneStringMap(req.ConnectionParams),
	}
}

func exchangeCredentialResponseFromProto(resp *proto.ExchangeExternalCredentialResponse) *core.ExchangeExternalCredentialResponse {
	if resp == nil {
		return nil
	}
	return &core.ExchangeExternalCredentialResponse{
		TokenResponse: externalCredentialTokenResponseFromProto(resp.GetTokenResponse()),
	}
}

func externalCredentialTokenResponseToProto(resp *core.ExternalCredentialTokenResponse) *proto.ExternalCredentialTokenResponse {
	if resp == nil {
		return nil
	}
	extraJSON := ""
	if resp.Extra != nil {
		if data, err := json.Marshal(resp.Extra); err == nil {
			extraJSON = string(data)
		}
	}
	return &proto.ExternalCredentialTokenResponse{
		AccessToken:   resp.AccessToken,
		RefreshToken:  resp.RefreshToken,
		RefreshSource: resp.RefreshSource,
		ExpiresIn:     int32(resp.ExpiresIn),
		TokenType:     resp.TokenType,
		ExtraJson:     extraJSON,
	}
}

func externalCredentialTokenResponseFromProto(resp *proto.ExternalCredentialTokenResponse) *core.ExternalCredentialTokenResponse {
	if resp == nil {
		return nil
	}
	extra := map[string]any(nil)
	if resp.GetExtraJson() != "" {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(resp.GetExtraJson()), &decoded); err == nil {
			extra = decoded
		}
	}
	return &core.ExternalCredentialTokenResponse{
		AccessToken:   resp.GetAccessToken(),
		RefreshToken:  resp.GetRefreshToken(),
		RefreshSource: resp.GetRefreshSource(),
		ExpiresIn:     int(resp.GetExpiresIn()),
		TokenType:     resp.GetTokenType(),
		Extra:         extra,
	}
}

func externalCredentialAuthConfigToProto(auth core.ExternalCredentialAuthConfig) *proto.ExternalCredentialAuthConfig {
	drivers := make([]*proto.ExternalCredentialTokenExchangeDriver, 0, len(auth.TokenExchangeDrivers))
	for _, driver := range auth.TokenExchangeDrivers {
		drivers = append(drivers, &proto.ExternalCredentialTokenExchangeDriver{
			Type:            driver.Type,
			TargetPrincipal: driver.TargetPrincipal,
			Scopes:          append([]string(nil), driver.Scopes...),
			LifetimeSeconds: int32(driver.LifetimeSeconds),
			Endpoint:        driver.Endpoint,
			Params:          cloneStringMap(driver.Params),
		})
	}
	return &proto.ExternalCredentialAuthConfig{
		Type:                 auth.Type,
		Token:                auth.Token,
		TokenPrefix:          auth.TokenPrefix,
		GrantType:            auth.GrantType,
		RefreshToken:         auth.RefreshToken,
		TokenUrl:             auth.TokenURL,
		ClientId:             auth.ClientID,
		ClientSecret:         auth.ClientSecret,
		ClientAuth:           auth.ClientAuth,
		TokenExchange:        auth.TokenExchange,
		Scopes:               append([]string(nil), auth.Scopes...),
		ScopeParam:           auth.ScopeParam,
		ScopeSeparator:       auth.ScopeSeparator,
		TokenParams:          cloneStringMap(auth.TokenParams),
		RefreshParams:        cloneStringMap(auth.RefreshParams),
		AcceptHeader:         auth.AcceptHeader,
		AccessTokenPath:      auth.AccessTokenPath,
		TokenExchangeDrivers: drivers,
	}
}

func externalCredentialAuthConfigFromProto(auth *proto.ExternalCredentialAuthConfig) core.ExternalCredentialAuthConfig {
	if auth == nil {
		return core.ExternalCredentialAuthConfig{}
	}
	drivers := make([]core.ExternalCredentialTokenExchangeDriver, 0, len(auth.GetTokenExchangeDrivers()))
	for _, driver := range auth.GetTokenExchangeDrivers() {
		if driver == nil {
			continue
		}
		drivers = append(drivers, core.ExternalCredentialTokenExchangeDriver{
			Type:            driver.GetType(),
			TargetPrincipal: driver.GetTargetPrincipal(),
			Scopes:          append([]string(nil), driver.GetScopes()...),
			LifetimeSeconds: int(driver.GetLifetimeSeconds()),
			Endpoint:        driver.GetEndpoint(),
			Params:          cloneStringMap(driver.GetParams()),
		})
	}
	return core.ExternalCredentialAuthConfig{
		Type:                 auth.GetType(),
		Token:                auth.GetToken(),
		TokenPrefix:          auth.GetTokenPrefix(),
		GrantType:            auth.GetGrantType(),
		RefreshToken:         auth.GetRefreshToken(),
		TokenURL:             auth.GetTokenUrl(),
		ClientID:             auth.GetClientId(),
		ClientSecret:         auth.GetClientSecret(),
		ClientAuth:           auth.GetClientAuth(),
		TokenExchange:        auth.GetTokenExchange(),
		Scopes:               append([]string(nil), auth.GetScopes()...),
		ScopeParam:           auth.GetScopeParam(),
		ScopeSeparator:       auth.GetScopeSeparator(),
		TokenParams:          cloneStringMap(auth.GetTokenParams()),
		RefreshParams:        cloneStringMap(auth.GetRefreshParams()),
		AcceptHeader:         auth.GetAcceptHeader(),
		AccessTokenPath:      auth.GetAccessTokenPath(),
		TokenExchangeDrivers: drivers,
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func externalCredentialFromProto(credential *proto.ExternalCredential) *core.ExternalCredential {
	if credential == nil {
		return nil
	}
	out := &core.ExternalCredential{
		ID:           strings.TrimSpace(credential.GetId()),
		Subject:      strings.TrimSpace(credential.GetSubject()),
		Audience:     strings.TrimSpace(credential.GetAudience()),
		Qualifier:    strings.TrimSpace(credential.GetQualifier()),
		MetadataJSON: credential.GetMetadataJson(),
		CreatedAt:    derefTime(timeFromProto(credential.GetCreatedAt())),
		UpdatedAt:    derefTime(timeFromProto(credential.GetUpdatedAt())),
	}
	switch value := credential.GetCredential().(type) {
	case *proto.ExternalCredential_Grant:
		out.Grant = &core.ExternalCredentialGrant{
			AccessToken:       value.Grant.GetAccessToken(),
			RefreshToken:      value.Grant.GetRefreshToken(),
			Scope:             value.Grant.GetScope(),
			ExpiresAt:         timeFromProto(value.Grant.GetExpiresAt()),
			LastRefreshedAt:   timeFromProto(value.Grant.GetLastRefreshedAt()),
			RefreshErrorCount: int(value.Grant.GetRefreshErrorCount()),
		}
	case *proto.ExternalCredential_Client:
		out.Client = &core.ExternalCredentialClientInfo{
			ClientID:              value.Client.GetClientId(),
			ClientSecret:          value.Client.GetClientSecret(),
			ClientSecretExpiresAt: timeFromProto(value.Client.GetClientSecretExpiresAt()),
		}
	case *proto.ExternalCredential_Opaque:
		out.Opaque = &core.ExternalCredentialOpaque{
			Fields: cloneStringMap(value.Opaque.GetFields()),
		}
	}
	return out
}

func externalCredentialRPCError(operation string, err error) error {
	switch status.Code(err) {
	case codes.NotFound:
		return core.ErrNotFound
	case codes.AlreadyExists:
		return core.ErrAlreadyExists
	case codes.Unauthenticated:
		return fmt.Errorf("%w: %s", core.ErrReconnectRequired, status.Convert(err).Message())
	case codes.FailedPrecondition:
		if strings.Contains(strings.ToLower(status.Convert(err).Message()), "ambiguous") {
			return core.ErrAmbiguousCredential
		}
		return fmt.Errorf("%s: %w", operation, err)
	case codes.OK:
		return nil
	default:
		return fmt.Errorf("%s: %w", operation, err)
	}
}

func nonZeroTimePtr(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}

func timeToProto(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

func timeFromProto(t *timestamppb.Timestamp) *time.Time {
	if t == nil {
		return nil
	}
	value := t.AsTime()
	return &value
}

func derefTime(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}
