package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
)

type StoredUser = proto.StoredUser
type StoredIntegrationToken = proto.StoredIntegrationToken
type StoredAPIToken = proto.StoredAPIToken
type OAuthRegistration = proto.OAuthRegistration

type DatastoreProvider interface {
	PluginProvider
	HealthChecker
	Migrate(ctx context.Context) error
	UserStore
	IntegrationTokenStore
	APITokenStore
}

type OAuthRegistrationStore interface {
	GetOAuthRegistration(ctx context.Context, authServerURL, redirectURI string) (*OAuthRegistration, error)
	PutOAuthRegistration(ctx context.Context, registration *OAuthRegistration) error
	DeleteOAuthRegistration(ctx context.Context, authServerURL, redirectURI string) error
}

type UserStore interface {
	GetUser(ctx context.Context, id string) (*StoredUser, error)
	FindOrCreateUser(ctx context.Context, email string) (*StoredUser, error)
}

type IntegrationTokenStore interface {
	PutIntegrationToken(ctx context.Context, token *StoredIntegrationToken) error
	GetIntegrationToken(ctx context.Context, userID, integration, connection, instance string) (*StoredIntegrationToken, error)
	ListIntegrationTokens(ctx context.Context, userID, integration, connection string) ([]*StoredIntegrationToken, error)
	DeleteIntegrationToken(ctx context.Context, id string) error
}

type APITokenStore interface {
	PutAPIToken(ctx context.Context, token *StoredAPIToken) error
	GetAPITokenByHash(ctx context.Context, hashedToken string) (*StoredAPIToken, error)
	ListAPITokens(ctx context.Context, userID string) ([]*StoredAPIToken, error)
	RevokeAPIToken(ctx context.Context, userID, id string) error
	RevokeAllAPITokens(ctx context.Context, userID string) (int64, error)
}
