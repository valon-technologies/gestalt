package coretesting

import (
	"context"
	"errors"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
)

type StubSecretManager struct {
	Secrets map[string]string
}

func (s *StubSecretManager) GetSecret(_ context.Context, name string) (string, error) {
	if v, ok := s.Secrets[name]; ok {
		return v, nil
	}
	return "", core.ErrSecretNotFound
}

var _ core.StagedConnectionStore = (*StubDatastore)(nil)

// Set Fn fields to override individual methods; nil fields return zero values.
type StubDatastore struct {
	PingFn                     func(context.Context) error
	GetUserFn                  func(context.Context, string) (*core.User, error)
	FindOrCreateUserFn         func(context.Context, string) (*core.User, error)
	StoreTokenFn               func(context.Context, *core.IntegrationToken) error
	TokenFn                    func(context.Context, string, string, string, string) (*core.IntegrationToken, error)
	ListTokensFn               func(context.Context, string) ([]*core.IntegrationToken, error)
	ListTokensForIntegrationFn func(context.Context, string, string) ([]*core.IntegrationToken, error)
	ListTokensForConnectionFn  func(context.Context, string, string, string) ([]*core.IntegrationToken, error)
	DeleteTokenFn              func(context.Context, string) error
	StoreAPITokenFn            func(context.Context, *core.APIToken) error
	ValidateAPITokenFn         func(context.Context, string) (*core.APIToken, error)
	ListAPITokensFn            func(context.Context, string) ([]*core.APIToken, error)
	RevokeAPITokenFn           func(context.Context, string, string) error
	RevokeAllAPITokensFn       func(context.Context, string) (int64, error)
	StoreStagedConnectionFn    func(context.Context, *core.StagedConnection) error
	GetStagedConnectionFn      func(context.Context, string) (*core.StagedConnection, error)
	DeleteStagedConnectionFn   func(context.Context, string) error
}

func (s *StubDatastore) Ping(ctx context.Context) error {
	if s.PingFn != nil {
		return s.PingFn(ctx)
	}
	return nil
}

func (s *StubDatastore) GetUser(ctx context.Context, id string) (*core.User, error) {
	if s.GetUserFn != nil {
		return s.GetUserFn(ctx, id)
	}
	return nil, nil
}

func (s *StubDatastore) FindOrCreateUser(ctx context.Context, email string) (*core.User, error) {
	if s.FindOrCreateUserFn != nil {
		return s.FindOrCreateUserFn(ctx, email)
	}
	return nil, nil
}
func (s *StubDatastore) StoreToken(ctx context.Context, token *core.IntegrationToken) error {
	if s.StoreTokenFn != nil {
		return s.StoreTokenFn(ctx, token)
	}
	return nil
}
func (s *StubDatastore) Token(ctx context.Context, userID, integration, connection, instance string) (*core.IntegrationToken, error) {
	if s.TokenFn != nil {
		return s.TokenFn(ctx, userID, integration, connection, instance)
	}
	return nil, nil
}
func (s *StubDatastore) ListTokens(ctx context.Context, userID string) ([]*core.IntegrationToken, error) {
	if s.ListTokensFn != nil {
		return s.ListTokensFn(ctx, userID)
	}
	return nil, nil
}
func (s *StubDatastore) ListTokensForIntegration(ctx context.Context, userID, integration string) ([]*core.IntegrationToken, error) {
	if s.ListTokensForIntegrationFn != nil {
		return s.ListTokensForIntegrationFn(ctx, userID, integration)
	}
	if s.TokenFn != nil {
		tok, err := s.TokenFn(ctx, userID, integration, "", "default")
		if err != nil {
			if errors.Is(err, core.ErrNotFound) {
				return nil, nil
			}
			return nil, err
		}
		if tok == nil {
			return nil, nil
		}
		return []*core.IntegrationToken{tok}, nil
	}
	return nil, nil
}
func (s *StubDatastore) ListTokensForConnection(ctx context.Context, userID, integration, connection string) ([]*core.IntegrationToken, error) {
	if s.ListTokensForConnectionFn != nil {
		return s.ListTokensForConnectionFn(ctx, userID, integration, connection)
	}
	if s.TokenFn != nil {
		tok, err := s.TokenFn(ctx, userID, integration, connection, "default")
		if err != nil {
			if errors.Is(err, core.ErrNotFound) {
				return nil, nil
			}
			return nil, err
		}
		if tok == nil {
			return nil, nil
		}
		return []*core.IntegrationToken{tok}, nil
	}
	return nil, nil
}
func (s *StubDatastore) DeleteToken(ctx context.Context, id string) error {
	if s.DeleteTokenFn != nil {
		return s.DeleteTokenFn(ctx, id)
	}
	return nil
}
func (s *StubDatastore) StoreAPIToken(ctx context.Context, token *core.APIToken) error {
	if s.StoreAPITokenFn != nil {
		return s.StoreAPITokenFn(ctx, token)
	}
	return nil
}
func (s *StubDatastore) ValidateAPIToken(ctx context.Context, hashedToken string) (*core.APIToken, error) {
	if s.ValidateAPITokenFn != nil {
		return s.ValidateAPITokenFn(ctx, hashedToken)
	}
	return nil, nil
}
func (s *StubDatastore) ListAPITokens(ctx context.Context, userID string) ([]*core.APIToken, error) {
	if s.ListAPITokensFn != nil {
		return s.ListAPITokensFn(ctx, userID)
	}
	return nil, nil
}
func (s *StubDatastore) RevokeAPIToken(ctx context.Context, userID, id string) error {
	if s.RevokeAPITokenFn != nil {
		return s.RevokeAPITokenFn(ctx, userID, id)
	}
	return nil
}
func (s *StubDatastore) RevokeAllAPITokens(ctx context.Context, userID string) (int64, error) {
	if s.RevokeAllAPITokensFn != nil {
		return s.RevokeAllAPITokensFn(ctx, userID)
	}
	return 0, nil
}
func (s *StubDatastore) StoreStagedConnection(ctx context.Context, sc *core.StagedConnection) error {
	if s.StoreStagedConnectionFn != nil {
		return s.StoreStagedConnectionFn(ctx, sc)
	}
	return nil
}
func (s *StubDatastore) GetStagedConnection(ctx context.Context, id string) (*core.StagedConnection, error) {
	if s.GetStagedConnectionFn != nil {
		return s.GetStagedConnectionFn(ctx, id)
	}
	return nil, core.ErrNotFound
}
func (s *StubDatastore) DeleteStagedConnection(ctx context.Context, id string) error {
	if s.DeleteStagedConnectionFn != nil {
		return s.DeleteStagedConnectionFn(ctx, id)
	}
	return nil
}
func (s *StubDatastore) Migrate(context.Context) error { return nil }
func (s *StubDatastore) Close() error                  { return nil }

type StubAuthProvider struct {
	N                string
	HandleCallbackFn func(context.Context, string) (*core.UserIdentity, error)
	ValidateTokenFn  func(context.Context, string) (*core.UserIdentity, error)
}

func (s *StubAuthProvider) Name() string                    { return s.N }
func (s *StubAuthProvider) LoginURL(string) (string, error) { return "", nil }
func (s *StubAuthProvider) HandleCallback(ctx context.Context, code string) (*core.UserIdentity, error) {
	if s.HandleCallbackFn != nil {
		return s.HandleCallbackFn(ctx, code)
	}
	return nil, nil
}
func (s *StubAuthProvider) ValidateToken(ctx context.Context, token string) (*core.UserIdentity, error) {
	if s.ValidateTokenFn != nil {
		return s.ValidateTokenFn(ctx, token)
	}
	return nil, nil
}

type StubRuntime struct {
	N       string
	StartFn func(context.Context) error
	StopFn  func(context.Context) error
}

func (r *StubRuntime) Name() string { return r.N }
func (r *StubRuntime) Start(ctx context.Context) error {
	if r.StartFn != nil {
		return r.StartFn(ctx)
	}
	return nil
}
func (r *StubRuntime) Stop(ctx context.Context) error {
	if r.StopFn != nil {
		return r.StopFn(ctx)
	}
	return nil
}

type StubBinding struct {
	N       string
	StartFn func(context.Context) error
	CloseFn func() error
	R       []core.Route
}

func (b *StubBinding) Name() string         { return b.N }
func (b *StubBinding) Routes() []core.Route { return b.R }
func (b *StubBinding) Start(ctx context.Context) error {
	if b.StartFn != nil {
		return b.StartFn(ctx)
	}
	return nil
}
func (b *StubBinding) Close() error {
	if b.CloseFn != nil {
		return b.CloseFn()
	}
	return nil
}

type StubIntegration struct {
	N              string
	DN             string
	Desc           string
	ConnMode       core.ConnectionMode
	CatalogVal     *catalog.Catalog
	ExchangeCodeFn func(context.Context, string) (*core.TokenResponse, error)
	ExecuteFn      func(context.Context, string, map[string]any, string) (*core.OperationResult, error)
}

func (s *StubIntegration) Name() string        { return s.N }
func (s *StubIntegration) DisplayName() string { return s.DN }
func (s *StubIntegration) Description() string { return s.Desc }

func (s *StubIntegration) ConnectionMode() core.ConnectionMode {
	if s.ConnMode == "" {
		return core.ConnectionModeUser
	}
	return s.ConnMode
}
func (s *StubIntegration) AuthorizationURL(string, []string) string { return "" }
func (s *StubIntegration) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	if s.ExchangeCodeFn != nil {
		return s.ExchangeCodeFn(ctx, code)
	}
	return nil, nil
}
func (s *StubIntegration) RefreshToken(context.Context, string) (*core.TokenResponse, error) {
	return nil, nil
}
func (s *StubIntegration) Catalog() *catalog.Catalog { return s.CatalogVal }
func (s *StubIntegration) Execute(ctx context.Context, op string, params map[string]any, token string) (*core.OperationResult, error) {
	if s.ExecuteFn != nil {
		return s.ExecuteFn(ctx, op, params, token)
	}
	return nil, nil
}
