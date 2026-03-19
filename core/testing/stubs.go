package coretesting

import (
	"context"

	"github.com/valon-technologies/toolshed/core"
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

// Set Fn fields to override individual methods; nil fields return zero values.
type StubDatastore struct {
	PingFn             func(context.Context) error
	GetUserFn          func(context.Context, string) (*core.User, error)
	FindOrCreateUserFn func(context.Context, string) (*core.User, error)
	StoreTokenFn       func(context.Context, *core.IntegrationToken) error
	TokenFn            func(context.Context, string, string, string) (*core.IntegrationToken, error)
	ValidateAPITokenFn func(context.Context, string) (*core.APIToken, error)
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
func (s *StubDatastore) Token(ctx context.Context, userID, integration, instance string) (*core.IntegrationToken, error) {
	if s.TokenFn != nil {
		return s.TokenFn(ctx, userID, integration, instance)
	}
	return nil, nil
}
func (s *StubDatastore) ListTokens(context.Context, string) ([]*core.IntegrationToken, error) {
	return nil, nil
}
func (s *StubDatastore) DeleteToken(context.Context, string) error           { return nil }
func (s *StubDatastore) StoreAPIToken(context.Context, *core.APIToken) error { return nil }
func (s *StubDatastore) ValidateAPIToken(ctx context.Context, hashedToken string) (*core.APIToken, error) {
	if s.ValidateAPITokenFn != nil {
		return s.ValidateAPITokenFn(ctx, hashedToken)
	}
	return nil, nil
}
func (s *StubDatastore) ListAPITokens(context.Context, string) ([]*core.APIToken, error) {
	return nil, nil
}
func (s *StubDatastore) RevokeAPIToken(context.Context, string) error { return nil }
func (s *StubDatastore) Migrate(context.Context) error                { return nil }
func (s *StubDatastore) Close() error                                 { return nil }

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

type StubIntegration struct {
	N              string
	DN             string
	Desc           string
	ConnMode       core.ConnectionMode
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
func (s *StubIntegration) ListOperations() []core.Operation { return nil }
func (s *StubIntegration) Execute(ctx context.Context, op string, params map[string]any, token string) (*core.OperationResult, error) {
	if s.ExecuteFn != nil {
		return s.ExecuteFn(ctx, op, params, token)
	}
	return nil, nil
}
