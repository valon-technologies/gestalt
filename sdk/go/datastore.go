package gestalt

import (
	"context"
	"time"
)

// DatastoreProvider is the runtime contract for pluggable persistence
// backends.
type DatastoreProvider interface {
	PluginProvider
	HealthChecker
	Migrate(ctx context.Context) error
	UserStore
	IntegrationTokenStore
	APITokenStore
}

// OAuthRegistrationStore is implemented by datastore providers that can persist
// MCP OAuth dynamic client registrations for SQL-backed parity with first-party
// datastores.
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

type StoredUser struct {
	ID          string
	Email       string
	DisplayName string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type StoredIntegrationToken struct {
	ID                 string
	UserID             string
	Integration        string
	Connection         string
	Instance           string
	AccessTokenSealed  []byte
	RefreshTokenSealed []byte
	Scopes             string
	ExpiresAt          *time.Time
	LastRefreshedAt    *time.Time
	RefreshErrorCount  int32
	ConnectionParams   map[string]string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type StoredAPIToken struct {
	ID          string
	UserID      string
	Name        string
	HashedToken string
	Scopes      string
	ExpiresAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type OAuthRegistration struct {
	AuthServerURL         string
	RedirectURI           string
	ClientID              string
	ClientSecretSealed    []byte
	ExpiresAt             *time.Time
	AuthorizationEndpoint string
	TokenEndpoint         string
	ScopesSupported       string
	DiscoveredAt          time.Time
}
