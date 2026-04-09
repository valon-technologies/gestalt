package pluginhost

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	corecrypto "github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/internal/mcpoauth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type DatastoreExecConfig struct {
	Command       string
	Args          []string
	Env           map[string]string
	Config        map[string]any
	AllowedHosts  []string
	HostBinary    string
	Cleanup       func()
	Name          string
	EncryptionKey []byte
}

type remoteDatastore struct {
	runtime     proto.ProviderLifecycleClient
	client      proto.DatastoreProviderClient
	name        string
	displayName string
	description string
	warnings    []string
	enc         *corecrypto.AESGCMEncryptor
	closer      io.Closer
}

type remoteDatastoreWithOAuth struct {
	*remoteDatastore
}

func NewExecutableDatastore(ctx context.Context, cfg DatastoreExecConfig) (core.Datastore, error) {
	enc, err := corecrypto.NewAESGCM(cfg.EncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("create datastore encryptor: %w", err)
	}
	proc, err := startPluginProcess(ctx, ExecConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Env:          cfg.Env,
		Config:       cfg.Config,
		AllowedHosts: cfg.AllowedHosts,
		HostBinary:   cfg.HostBinary,
		Cleanup:      cfg.Cleanup,
	}, nil, "")
	if err != nil {
		return nil, err
	}

	runtimeClient := proto.NewProviderLifecycleClient(proc.conn)
	datastoreClient := proto.NewDatastoreProviderClient(proc.conn)
	store, err := newRemoteDatastore(ctx, runtimeClient, datastoreClient, cfg.Name, cfg.Config, enc)
	if err != nil {
		_ = proc.Close()
		return nil, err
	}
	store.closer = proc
	if supports, err := store.supportsOAuthRegistration(ctx); err == nil && supports {
		return &remoteDatastoreWithOAuth{remoteDatastore: store}, nil
	}
	return store, nil
}

func newRemoteDatastore(ctx context.Context, runtimeClient proto.ProviderLifecycleClient, client proto.DatastoreProviderClient, name string, config map[string]any, enc *corecrypto.AESGCMEncryptor) (*remoteDatastore, error) {
	store := &remoteDatastore{
		runtime: runtimeClient,
		client:  client,
		enc:     enc,
	}
	if err := store.configure(ctx, name, config); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *remoteDatastore) configure(ctx context.Context, name string, config map[string]any) error {
	meta, err := configureRuntimePlugin(ctx, s.runtime, proto.PluginKind_PLUGIN_KIND_DATASTORE, name, config)
	if err != nil {
		return err
	}
	s.name = name
	if meta != nil && meta.Name != "" {
		s.name = meta.Name
	}
	if s.name == "" {
		s.name = "plugin"
	}
	if meta != nil {
		s.displayName = meta.DisplayName
		s.description = meta.Description
		s.warnings = append([]string(nil), meta.Warnings...)
	} else {
		s.displayName = ""
		s.description = ""
		s.warnings = nil
	}
	return nil
}

func (s *remoteDatastore) Name() string {
	return s.name
}

func (s *remoteDatastore) supportsOAuthRegistration(ctx context.Context) (bool, error) {
	ctx, cancel := pluginCallContext(ctx)
	defer cancel()

	_, err := s.client.GetOAuthRegistration(ctx, &proto.GetOAuthRegistrationRequest{})
	if err == nil {
		return true, nil
	}
	if status.Code(err) == codes.Unimplemented {
		return false, nil
	}
	return true, nil
}

func (s *remoteDatastore) GetUser(ctx context.Context, id string) (*core.User, error) {
	ctx, cancel := pluginCallContext(ctx)
	defer cancel()

	resp, err := s.client.GetUser(ctx, &proto.GetUserRequest{Id: id})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get user: %w", err)
	}
	return protoUserToCore(resp), nil
}

func (s *remoteDatastore) FindOrCreateUser(ctx context.Context, email string) (*core.User, error) {
	ctx, cancel := pluginCallContext(ctx)
	defer cancel()

	resp, err := s.client.FindOrCreateUser(ctx, &proto.FindOrCreateUserRequest{Email: email})
	if err != nil {
		return nil, fmt.Errorf("find or create user: %w", err)
	}
	return protoUserToCore(resp), nil
}

func (s *remoteDatastore) StoreToken(ctx context.Context, token *core.IntegrationToken) error {
	wire, err := s.coreTokenToProto(token)
	if err != nil {
		return err
	}
	ctx, cancel := pluginCallContext(ctx)
	defer cancel()

	_, err = s.client.PutStoredIntegrationToken(ctx, wire)
	if err != nil {
		return fmt.Errorf("store integration token: %w", err)
	}
	return nil
}

func (s *remoteDatastore) Token(ctx context.Context, userID, integration, connection, instance string) (*core.IntegrationToken, error) {
	ctx, cancel := pluginCallContext(ctx)
	defer cancel()

	resp, err := s.client.GetStoredIntegrationToken(ctx, &proto.GetStoredIntegrationTokenRequest{
		UserId:      userID,
		Integration: integration,
		Connection:  connection,
		Instance:    instance,
	})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get integration token: %w", err)
	}
	return s.protoTokenToCore(resp)
}

func (s *remoteDatastore) ListTokens(ctx context.Context, userID string) ([]*core.IntegrationToken, error) {
	return s.listTokens(ctx, userID, "", "")
}

func (s *remoteDatastore) ListTokensForIntegration(ctx context.Context, userID, integration string) ([]*core.IntegrationToken, error) {
	return s.listTokens(ctx, userID, integration, "")
}

func (s *remoteDatastore) ListTokensForConnection(ctx context.Context, userID, integration, connection string) ([]*core.IntegrationToken, error) {
	return s.listTokens(ctx, userID, integration, connection)
}

func (s *remoteDatastore) DeleteToken(ctx context.Context, id string) error {
	ctx, cancel := pluginCallContext(ctx)
	defer cancel()

	_, err := s.client.DeleteStoredIntegrationToken(ctx, &proto.DeleteStoredIntegrationTokenRequest{Id: id})
	if err != nil {
		return fmt.Errorf("delete integration token: %w", err)
	}
	return nil
}

func (s *remoteDatastore) StoreAPIToken(ctx context.Context, token *core.APIToken) error {
	ctx, cancel := pluginCallContext(ctx)
	defer cancel()

	_, err := s.client.PutAPIToken(ctx, coreAPITokenToProto(token))
	if err != nil {
		return fmt.Errorf("store api token: %w", err)
	}
	return nil
}

func (s *remoteDatastore) ValidateAPIToken(ctx context.Context, hashedToken string) (*core.APIToken, error) {
	ctx, cancel := pluginCallContext(ctx)
	defer cancel()

	resp, err := s.client.GetAPITokenByHash(ctx, &proto.GetAPITokenByHashRequest{HashedToken: hashedToken})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("validate api token: %w", err)
	}
	return protoAPITokenToCore(resp), nil
}

func (s *remoteDatastore) ListAPITokens(ctx context.Context, userID string) ([]*core.APIToken, error) {
	ctx, cancel := pluginCallContext(ctx)
	defer cancel()

	resp, err := s.client.ListAPITokens(ctx, &proto.ListAPITokensRequest{UserId: userID})
	if err != nil {
		return nil, fmt.Errorf("list api tokens: %w", err)
	}
	tokens := resp.GetTokens()
	out := make([]*core.APIToken, 0, len(tokens))
	for _, token := range tokens {
		out = append(out, protoAPITokenToCore(token))
	}
	return out, nil
}

func (s *remoteDatastore) RevokeAPIToken(ctx context.Context, userID, id string) error {
	ctx, cancel := pluginCallContext(ctx)
	defer cancel()

	_, err := s.client.RevokeAPIToken(ctx, &proto.RevokeAPITokenRequest{
		UserId: userID,
		Id:     id,
	})
	if err != nil {
		return fmt.Errorf("revoke api token: %w", err)
	}
	return nil
}

func (s *remoteDatastore) RevokeAllAPITokens(ctx context.Context, userID string) (int64, error) {
	ctx, cancel := pluginCallContext(ctx)
	defer cancel()

	resp, err := s.client.RevokeAllAPITokens(ctx, &proto.RevokeAllAPITokensRequest{UserId: userID})
	if err != nil {
		return 0, fmt.Errorf("revoke all api tokens: %w", err)
	}
	return resp.GetRevoked(), nil
}

func (s *remoteDatastore) Ping(ctx context.Context) error {
	ctx, cancel := pluginCallContext(ctx)
	defer cancel()
	return pingRuntimePlugin(ctx, s.runtime)
}

func (s *remoteDatastore) Migrate(ctx context.Context) error {
	ctx, cancel := pluginMigrateContext(ctx)
	defer cancel()
	_, err := s.client.Migrate(ctx, &emptypb.Empty{})
	if err != nil {
		return fmt.Errorf("migrate datastore: %w", err)
	}
	return nil
}

func (s *remoteDatastoreWithOAuth) GetRegistration(ctx context.Context, authServerURL, redirectURI string) (*mcpoauth.Registration, error) {
	ctx, cancel := pluginCallContext(ctx)
	defer cancel()

	resp, err := s.client.GetOAuthRegistration(ctx, &proto.GetOAuthRegistrationRequest{
		AuthServerUrl: authServerURL,
		RedirectUri:   redirectURI,
	})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		if status.Code(err) == codes.Unimplemented {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get oauth registration: %w", err)
	}
	return s.protoOAuthRegistrationToCore(resp)
}

func (s *remoteDatastoreWithOAuth) StoreRegistration(ctx context.Context, registration *mcpoauth.Registration) error {
	wire, err := s.coreOAuthRegistrationToProto(registration)
	if err != nil {
		return err
	}
	ctx, cancel := pluginCallContext(ctx)
	defer cancel()

	_, err = s.client.PutOAuthRegistration(ctx, wire)
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return fmt.Errorf("store oauth registration: unsupported")
		}
		return fmt.Errorf("store oauth registration: %w", err)
	}
	return nil
}

func (s *remoteDatastoreWithOAuth) DeleteRegistration(ctx context.Context, authServerURL, redirectURI string) error {
	ctx, cancel := pluginCallContext(ctx)
	defer cancel()

	_, err := s.client.DeleteOAuthRegistration(ctx, &proto.DeleteOAuthRegistrationRequest{
		AuthServerUrl: authServerURL,
		RedirectUri:   redirectURI,
	})
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return fmt.Errorf("delete oauth registration: unsupported")
		}
		return fmt.Errorf("delete oauth registration: %w", err)
	}
	return nil
}

func (s *remoteDatastore) Warnings() []string {
	if len(s.warnings) == 0 {
		return nil
	}
	return append([]string(nil), s.warnings...)
}

func (s *remoteDatastore) Close() error {
	if s == nil || s.closer == nil {
		return nil
	}
	return s.closer.Close()
}

func (s *remoteDatastore) listTokens(ctx context.Context, userID, integration, connection string) ([]*core.IntegrationToken, error) {
	ctx, cancel := pluginCallContext(ctx)
	defer cancel()

	resp, err := s.client.ListStoredIntegrationTokens(ctx, &proto.ListStoredIntegrationTokensRequest{
		UserId:      userID,
		Integration: integration,
		Connection:  connection,
	})
	if err != nil {
		return nil, fmt.Errorf("list integration tokens: %w", err)
	}
	tokens := resp.GetTokens()
	out := make([]*core.IntegrationToken, 0, len(tokens))
	for _, token := range tokens {
		converted, convErr := s.protoTokenToCore(token)
		if convErr != nil {
			return nil, convErr
		}
		out = append(out, converted)
	}
	return out, nil
}

func (s *remoteDatastore) coreTokenToProto(token *core.IntegrationToken) (*proto.StoredIntegrationToken, error) {
	if token == nil {
		return nil, nil
	}
	access, refresh, err := s.enc.EncryptTokenPair(token.AccessToken, token.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("encrypt token pair: %w", err)
	}
	params, err := metadataJSONToMap(token.MetadataJSON)
	if err != nil {
		return nil, fmt.Errorf("encode token metadata: %w", err)
	}
	return &proto.StoredIntegrationToken{
		Id:                 token.ID,
		UserId:             token.UserID,
		Integration:        token.Integration,
		Connection:         token.Connection,
		Instance:           token.Instance,
		AccessTokenSealed:  []byte(access),
		RefreshTokenSealed: []byte(refresh),
		Scopes:             token.Scopes,
		ExpiresAt:          timePtrToProto(token.ExpiresAt),
		LastRefreshedAt:    timePtrToProto(token.LastRefreshedAt),
		RefreshErrorCount:  int32(token.RefreshErrorCount),
		ConnectionParams:   params,
		CreatedAt:          timeToProto(token.CreatedAt),
		UpdatedAt:          timeToProto(token.UpdatedAt),
	}, nil
}

func (s *remoteDatastore) protoTokenToCore(token *proto.StoredIntegrationToken) (*core.IntegrationToken, error) {
	if token == nil {
		return nil, core.ErrNotFound
	}
	access, refresh, err := s.enc.DecryptTokenPair(string(token.GetAccessTokenSealed()), string(token.GetRefreshTokenSealed()))
	if err != nil {
		return nil, fmt.Errorf("decrypt token pair: %w", err)
	}
	metadataJSON, err := metadataMapToJSON(token.GetConnectionParams())
	if err != nil {
		return nil, fmt.Errorf("decode token metadata: %w", err)
	}
	return &core.IntegrationToken{
		ID:                token.GetId(),
		UserID:            token.GetUserId(),
		Integration:       token.GetIntegration(),
		Connection:        token.GetConnection(),
		Instance:          token.GetInstance(),
		AccessToken:       access,
		RefreshToken:      refresh,
		Scopes:            token.GetScopes(),
		ExpiresAt:         protoTimePtr(token.GetExpiresAt()),
		LastRefreshedAt:   protoTimePtr(token.GetLastRefreshedAt()),
		RefreshErrorCount: int(token.GetRefreshErrorCount()),
		MetadataJSON:      metadataJSON,
		CreatedAt:         protoTime(token.GetCreatedAt()),
		UpdatedAt:         protoTime(token.GetUpdatedAt()),
	}, nil
}

func (s *remoteDatastore) coreOAuthRegistrationToProto(registration *mcpoauth.Registration) (*proto.OAuthRegistration, error) {
	if registration == nil {
		return nil, nil
	}
	sealed, err := s.enc.Encrypt(registration.ClientSecret)
	if err != nil {
		return nil, fmt.Errorf("encrypt oauth client secret: %w", err)
	}
	return &proto.OAuthRegistration{
		AuthServerUrl:         registration.AuthServerURL,
		RedirectUri:           registration.RedirectURI,
		ClientId:              registration.ClientID,
		ClientSecretSealed:    []byte(sealed),
		ExpiresAt:             timePtrToProto(registration.ExpiresAt),
		AuthorizationEndpoint: registration.AuthorizationEndpoint,
		TokenEndpoint:         registration.TokenEndpoint,
		ScopesSupported:       registration.ScopesSupported,
		DiscoveredAt:          timeToProto(registration.DiscoveredAt),
	}, nil
}

func (s *remoteDatastore) protoOAuthRegistrationToCore(registration *proto.OAuthRegistration) (*mcpoauth.Registration, error) {
	if registration == nil {
		return nil, nil
	}
	secret, err := s.enc.Decrypt(string(registration.GetClientSecretSealed()))
	if err != nil {
		return nil, fmt.Errorf("decrypt oauth client secret: %w", err)
	}
	return &mcpoauth.Registration{
		AuthServerURL:         registration.GetAuthServerUrl(),
		RedirectURI:           registration.GetRedirectUri(),
		ClientID:              registration.GetClientId(),
		ClientSecret:          secret,
		ExpiresAt:             protoTimePtr(registration.GetExpiresAt()),
		AuthorizationEndpoint: registration.GetAuthorizationEndpoint(),
		TokenEndpoint:         registration.GetTokenEndpoint(),
		ScopesSupported:       registration.GetScopesSupported(),
		DiscoveredAt:          protoTime(registration.GetDiscoveredAt()),
	}, nil
}

func protoUserToCore(user *proto.StoredUser) *core.User {
	if user == nil {
		return nil
	}
	return &core.User{
		ID:          user.GetId(),
		Email:       user.GetEmail(),
		DisplayName: user.GetDisplayName(),
		CreatedAt:   protoTime(user.GetCreatedAt()),
		UpdatedAt:   protoTime(user.GetUpdatedAt()),
	}
}

func coreAPITokenToProto(token *core.APIToken) *proto.StoredAPIToken {
	if token == nil {
		return nil
	}
	return &proto.StoredAPIToken{
		Id:          token.ID,
		UserId:      token.UserID,
		Name:        token.Name,
		HashedToken: token.HashedToken,
		Scopes:      token.Scopes,
		ExpiresAt:   timePtrToProto(token.ExpiresAt),
		CreatedAt:   timeToProto(token.CreatedAt),
		UpdatedAt:   timeToProto(token.UpdatedAt),
	}
}

func protoAPITokenToCore(token *proto.StoredAPIToken) *core.APIToken {
	if token == nil {
		return nil
	}
	return &core.APIToken{
		ID:          token.GetId(),
		UserID:      token.GetUserId(),
		Name:        token.GetName(),
		HashedToken: token.GetHashedToken(),
		Scopes:      token.GetScopes(),
		ExpiresAt:   protoTimePtr(token.GetExpiresAt()),
		CreatedAt:   protoTime(token.GetCreatedAt()),
		UpdatedAt:   protoTime(token.GetUpdatedAt()),
	}
}

func metadataJSONToMap(raw string) (map[string]string, error) {
	if raw == "" {
		return nil, nil
	}
	var values map[string]string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, err
	}
	return values, nil
}

func metadataMapToJSON(values map[string]string) (string, error) {
	if len(values) == 0 {
		return "", nil
	}
	data, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

var (
	_ core.Datastore = (*remoteDatastore)(nil)
	_ interface {
		Warnings() []string
	} = (*remoteDatastore)(nil)
	_ mcpoauth.RegistrationStore = (*remoteDatastoreWithOAuth)(nil)
	_ interface{ Close() error } = (*remoteDatastore)(nil)
)
