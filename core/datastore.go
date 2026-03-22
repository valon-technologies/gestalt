package core

import "context"

// Implementations must be safe for concurrent use.
type Datastore interface {
	GetUser(ctx context.Context, id string) (*User, error)
	FindOrCreateUser(ctx context.Context, email string) (*User, error)

	StoreToken(ctx context.Context, token *IntegrationToken) error
	Token(ctx context.Context, userID, integration, instance string) (*IntegrationToken, error)
	ListTokens(ctx context.Context, userID string) ([]*IntegrationToken, error)
	DeleteToken(ctx context.Context, id string) error

	StoreAPIToken(ctx context.Context, token *APIToken) error
	ValidateAPIToken(ctx context.Context, hashedToken string) (*APIToken, error)
	ListAPITokens(ctx context.Context, userID string) ([]*APIToken, error)
	RevokeAPIToken(ctx context.Context, userID, id string) error

	Ping(ctx context.Context) error
	Migrate(ctx context.Context) error
	Close() error
}
