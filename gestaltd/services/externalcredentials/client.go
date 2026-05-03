package externalcredentials

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type ExecConfig struct {
	Command      string
	Args         []string
	Env          map[string]string
	Config       map[string]any
	Egress       egress.Policy
	HostBinary   string
	Cleanup      func()
	HostServices []runtimehost.HostService
	Name         string
}

type remoteExternalCredentialProvider struct {
	client proto.ExternalCredentialProviderClient
	closer io.Closer
}

func NewExecutable(ctx context.Context, cfg ExecConfig) (core.ExternalCredentialProvider, error) {
	proc, err := runtimehost.StartPluginProcess(ctx, runtimehost.ProcessConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
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
	client := proto.NewExternalCredentialProviderClient(proc.Conn())
	if _, err := runtimehost.ConfigureRuntimeProvider(ctx, runtimeClient, proto.ProviderKind_PROVIDER_KIND_EXTERNAL_CREDENTIAL, cfg.Name, cfg.Config); err != nil {
		_ = proc.Close()
		return nil, err
	}

	return &remoteExternalCredentialProvider{client: client, closer: proc}, nil
}

func (r *remoteExternalCredentialProvider) PutCredential(ctx context.Context, credential *core.ExternalCredential) error {
	value, err := r.upsertCredential(ctx, credential, false)
	if err != nil {
		return err
	}
	*credential = *value
	return nil
}

func (r *remoteExternalCredentialProvider) RestoreCredential(ctx context.Context, credential *core.ExternalCredential) error {
	value, err := r.upsertCredential(ctx, credential, true)
	if err != nil {
		return err
	}
	*credential = *value
	return nil
}

func (r *remoteExternalCredentialProvider) GetCredential(ctx context.Context, subjectID, connectionID, instance string) (*core.ExternalCredential, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()

	resp, err := r.client.GetCredential(ctx, &proto.GetExternalCredentialRequest{
		Lookup: &proto.ExternalCredentialLookup{
			SubjectId:    strings.TrimSpace(subjectID),
			ConnectionId: strings.TrimSpace(connectionID),
			Instance:     strings.TrimSpace(instance),
		},
	})
	if err != nil {
		return nil, externalCredentialRPCError("get external credential", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("get external credential: provider returned nil credential")
	}
	return externalCredentialFromProto(resp), nil
}

func (r *remoteExternalCredentialProvider) ListCredentials(ctx context.Context, subjectID string) ([]*core.ExternalCredential, error) {
	return r.listCredentials(ctx, &proto.ListExternalCredentialsRequest{
		SubjectId: strings.TrimSpace(subjectID),
	})
}

func (r *remoteExternalCredentialProvider) ListCredentialsForConnection(ctx context.Context, subjectID, connectionID string) ([]*core.ExternalCredential, error) {
	return r.listCredentials(ctx, &proto.ListExternalCredentialsRequest{
		SubjectId:    strings.TrimSpace(subjectID),
		ConnectionId: strings.TrimSpace(connectionID),
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

func (r *remoteExternalCredentialProvider) upsertCredential(ctx context.Context, credential *core.ExternalCredential, preserveTimestamps bool) (*core.ExternalCredential, error) {
	if credential == nil {
		return nil, fmt.Errorf("external credential is required")
	}
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()

	resp, err := r.client.UpsertCredential(ctx, &proto.UpsertExternalCredentialRequest{
		Credential:         externalCredentialToProto(credential),
		PreserveTimestamps: preserveTimestamps,
	})
	if err != nil {
		return nil, fmt.Errorf("upsert external credential: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("upsert external credential: provider returned nil credential")
	}
	return externalCredentialFromProto(resp), nil
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
	return &proto.ExternalCredential{
		Id:                credential.ID,
		SubjectId:         strings.TrimSpace(credential.SubjectID),
		ConnectionId:      strings.TrimSpace(credential.ConnectionID),
		Instance:          strings.TrimSpace(credential.Instance),
		AccessToken:       credential.AccessToken,
		RefreshToken:      credential.RefreshToken,
		Scopes:            credential.Scopes,
		ExpiresAt:         timeToProto(credential.ExpiresAt),
		LastRefreshedAt:   timeToProto(credential.LastRefreshedAt),
		RefreshErrorCount: int32(credential.RefreshErrorCount),
		MetadataJson:      credential.MetadataJSON,
		CreatedAt:         timeToProto(nonZeroTimePtr(credential.CreatedAt)),
		UpdatedAt:         timeToProto(nonZeroTimePtr(credential.UpdatedAt)),
	}
}

func validateCredentialConfigToProto(req *core.ValidateExternalCredentialConfigRequest) *proto.ValidateExternalCredentialConfigRequest {
	if req == nil {
		return nil
	}
	return &proto.ValidateExternalCredentialConfigRequest{
		Provider:         strings.TrimSpace(req.Provider),
		Connection:       strings.TrimSpace(req.Connection),
		ConnectionId:     strings.TrimSpace(req.ConnectionID),
		Mode:             string(req.Mode),
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
		Mode:                string(req.Mode),
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
	return &core.ExternalCredential{
		ID:                strings.TrimSpace(credential.GetId()),
		SubjectID:         strings.TrimSpace(credential.GetSubjectId()),
		ConnectionID:      strings.TrimSpace(credential.GetConnectionId()),
		Instance:          strings.TrimSpace(credential.GetInstance()),
		AccessToken:       credential.GetAccessToken(),
		RefreshToken:      credential.GetRefreshToken(),
		Scopes:            credential.GetScopes(),
		ExpiresAt:         timeFromProto(credential.GetExpiresAt()),
		LastRefreshedAt:   timeFromProto(credential.GetLastRefreshedAt()),
		RefreshErrorCount: int(credential.GetRefreshErrorCount()),
		MetadataJSON:      credential.GetMetadataJson(),
		CreatedAt:         derefTime(timeFromProto(credential.GetCreatedAt())),
		UpdatedAt:         derefTime(timeFromProto(credential.GetUpdatedAt())),
	}
}

func externalCredentialRPCError(operation string, err error) error {
	switch status.Code(err) {
	case codes.NotFound:
		return core.ErrNotFound
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
