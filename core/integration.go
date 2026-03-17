package core

import "context"

type Integration interface {
	Name() string
	DisplayName() string
	Description() string
	AuthorizationURL(state string, scopes []string) string
	ExchangeCode(ctx context.Context, code string) (*TokenResponse, error)
	RefreshToken(ctx context.Context, refreshToken string) (*TokenResponse, error)
	ListOperations() []Operation
	Execute(ctx context.Context, operation string, params map[string]any, token string) (*OperationResult, error)
}
