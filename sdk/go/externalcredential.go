package gestalt

import (
	"context"
	"time"
)

// ExternalCredential is the credential record stored by the host.
type ExternalCredential struct {
	ID                string
	SubjectID         string
	Instance          string
	AccessToken       string
	RefreshToken      string
	Scopes            string
	ExpiresAt         *time.Time
	LastRefreshedAt   *time.Time
	RefreshErrorCount int32
	MetadataJSON      string
	CreatedAt         *time.Time
	UpdatedAt         *time.Time
	ConnectionID      string
}

// ExternalCredentialLookup selects a host-managed external credential.
type ExternalCredentialLookup struct {
	SubjectID    string
	Instance     string
	ConnectionID string
}

// UpsertExternalCredentialRequest is the request for creating or updating a credential.
type UpsertExternalCredentialRequest struct {
	Credential         *ExternalCredential
	PreserveTimestamps bool
}

// GetExternalCredentialRequest is the request for fetching one credential.
type GetExternalCredentialRequest struct {
	Lookup *ExternalCredentialLookup
}

// ListExternalCredentialsRequest is the request for listing credentials.
type ListExternalCredentialsRequest struct {
	SubjectID    string
	Instance     string
	ConnectionID string
}

// ListExternalCredentialsResponse is the response returned when listing credentials.
type ListExternalCredentialsResponse struct {
	Credentials []*ExternalCredential
}

// DeleteExternalCredentialRequest is the request for deleting one credential.
type DeleteExternalCredentialRequest struct {
	ID string
}

// ExternalCredentialTokenExchangeDriver is the native message type for gestalt.provider.v1.ExternalCredentialTokenExchangeDriver.
type ExternalCredentialTokenExchangeDriver struct {
	Type            string
	TargetPrincipal string
	Scopes          []string
	LifetimeSeconds int32
	Endpoint        string
	Params          map[string]string
}

// ExternalCredentialAuthConfig is the native message type for gestalt.provider.v1.ExternalCredentialAuthConfig.
type ExternalCredentialAuthConfig struct {
	Type                 string
	Token                string
	TokenPrefix          string
	GrantType            string
	TokenURL             string
	ClientID             string
	ClientSecret         string
	ClientAuth           string
	TokenExchange        string
	Scopes               []string
	ScopeParam           string
	ScopeSeparator       string
	TokenParams          map[string]string
	RefreshParams        map[string]string
	AcceptHeader         string
	AccessTokenPath      string
	TokenExchangeDrivers []*ExternalCredentialTokenExchangeDriver
	RefreshToken         string
}

// ValidateExternalCredentialConfigRequest is the native message type for gestalt.provider.v1.ValidateExternalCredentialConfigRequest.
type ValidateExternalCredentialConfigRequest struct {
	Provider         string
	Connection       string
	ConnectionID     string
	Mode             string
	Auth             *ExternalCredentialAuthConfig
	ConnectionParams map[string]string
}

// ResolveExternalCredentialRequest is the native message type for gestalt.provider.v1.ResolveExternalCredentialRequest.
type ResolveExternalCredentialRequest struct {
	Provider            string
	Connection          string
	ConnectionID        string
	Mode                string
	CredentialSubjectID string
	ActorSubjectID      string
	Instance            string
	Auth                *ExternalCredentialAuthConfig
	ConnectionParams    map[string]string
}

// ResolveExternalCredentialResponse is the native message type for gestalt.provider.v1.ResolveExternalCredentialResponse.
type ResolveExternalCredentialResponse struct {
	Token        string
	ExpiresAt    *time.Time
	MetadataJSON string
	Params       map[string]string
	Credential   *ExternalCredential
}

// ExternalCredentialTokenResponse is the native message type for gestalt.provider.v1.ExternalCredentialTokenResponse.
type ExternalCredentialTokenResponse struct {
	AccessToken   string
	RefreshToken  string
	ExpiresIn     int32
	TokenType     string
	ExtraJSON     string
	RefreshSource string
}

// ExchangeExternalCredentialRequest is the native message type for gestalt.provider.v1.ExchangeExternalCredentialRequest.
type ExchangeExternalCredentialRequest struct {
	Provider            string
	Connection          string
	ConnectionID        string
	CredentialSubjectID string
	ActorSubjectID      string
	Instance            string
	Auth                *ExternalCredentialAuthConfig
	CredentialJSON      string
	ConnectionParams    map[string]string
}

// ExchangeExternalCredentialResponse is the native message type for gestalt.provider.v1.ExchangeExternalCredentialResponse.
type ExchangeExternalCredentialResponse struct {
	TokenResponse *ExternalCredentialTokenResponse
}

// GetId returns the id field; it is safe to call on a nil receiver.
func (c *ExternalCredential) GetId() string {
	if c == nil {
		return ""
	}
	return c.ID
}

// GetSubjectId returns the subject id field; it is safe to call on a nil receiver.
func (c *ExternalCredential) GetSubjectId() string {
	if c == nil {
		return ""
	}
	return c.SubjectID
}

// GetInstance returns the instance field; it is safe to call on a nil receiver.
func (c *ExternalCredential) GetInstance() string {
	if c == nil {
		return ""
	}
	return c.Instance
}

// GetAccessToken returns the access token field; it is safe to call on a nil receiver.
func (c *ExternalCredential) GetAccessToken() string {
	if c == nil {
		return ""
	}
	return c.AccessToken
}

// GetRefreshToken returns the refresh token field; it is safe to call on a nil receiver.
func (c *ExternalCredential) GetRefreshToken() string {
	if c == nil {
		return ""
	}
	return c.RefreshToken
}

// GetScopes returns the scopes field; it is safe to call on a nil receiver.
func (c *ExternalCredential) GetScopes() string {
	if c == nil {
		return ""
	}
	return c.Scopes
}

// GetExpiresAt returns the expires at field; it is safe to call on a nil receiver.
func (c *ExternalCredential) GetExpiresAt() *time.Time {
	if c == nil {
		return nil
	}
	return c.ExpiresAt
}

// GetLastRefreshedAt returns the last refreshed at field; it is safe to call on a nil receiver.
func (c *ExternalCredential) GetLastRefreshedAt() *time.Time {
	if c == nil {
		return nil
	}
	return c.LastRefreshedAt
}

// GetRefreshErrorCount returns the refresh error count field; it is safe to call on a nil receiver.
func (c *ExternalCredential) GetRefreshErrorCount() int32 {
	if c == nil {
		return 0
	}
	return c.RefreshErrorCount
}

// GetMetadataJson returns the metadata json field; it is safe to call on a nil receiver.
func (c *ExternalCredential) GetMetadataJson() string {
	if c == nil {
		return ""
	}
	return c.MetadataJSON
}

// GetCreatedAt returns the created at field; it is safe to call on a nil receiver.
func (c *ExternalCredential) GetCreatedAt() *time.Time {
	if c == nil {
		return nil
	}
	return c.CreatedAt
}

// GetUpdatedAt returns the updated at field; it is safe to call on a nil receiver.
func (c *ExternalCredential) GetUpdatedAt() *time.Time {
	if c == nil {
		return nil
	}
	return c.UpdatedAt
}

// GetConnectionId returns the connection id field; it is safe to call on a nil receiver.
func (c *ExternalCredential) GetConnectionId() string {
	if c == nil {
		return ""
	}
	return c.ConnectionID
}

// GetSubjectId returns the subject id field; it is safe to call on a nil receiver.
func (l *ExternalCredentialLookup) GetSubjectId() string {
	if l == nil {
		return ""
	}
	return l.SubjectID
}

// GetInstance returns the instance field; it is safe to call on a nil receiver.
func (l *ExternalCredentialLookup) GetInstance() string {
	if l == nil {
		return ""
	}
	return l.Instance
}

// GetConnectionId returns the connection id field; it is safe to call on a nil receiver.
func (l *ExternalCredentialLookup) GetConnectionId() string {
	if l == nil {
		return ""
	}
	return l.ConnectionID
}

// GetCredential returns the credential field; it is safe to call on a nil receiver.
func (r *UpsertExternalCredentialRequest) GetCredential() *ExternalCredential {
	if r == nil {
		return nil
	}
	return r.Credential
}

// GetPreserveTimestamps returns the preserve timestamps field; it is safe to call on a nil receiver.
func (r *UpsertExternalCredentialRequest) GetPreserveTimestamps() bool {
	if r == nil {
		return false
	}
	return r.PreserveTimestamps
}

// GetLookup returns the lookup field; it is safe to call on a nil receiver.
func (r *GetExternalCredentialRequest) GetLookup() *ExternalCredentialLookup {
	if r == nil {
		return nil
	}
	return r.Lookup
}

// GetSubjectId returns the subject id field; it is safe to call on a nil receiver.
func (r *ListExternalCredentialsRequest) GetSubjectId() string {
	if r == nil {
		return ""
	}
	return r.SubjectID
}

// GetInstance returns the instance field; it is safe to call on a nil receiver.
func (r *ListExternalCredentialsRequest) GetInstance() string {
	if r == nil {
		return ""
	}
	return r.Instance
}

// GetConnectionId returns the connection id field; it is safe to call on a nil receiver.
func (r *ListExternalCredentialsRequest) GetConnectionId() string {
	if r == nil {
		return ""
	}
	return r.ConnectionID
}

// GetCredentials returns the credentials field; it is safe to call on a nil receiver.
func (r *ListExternalCredentialsResponse) GetCredentials() []*ExternalCredential {
	if r == nil {
		return nil
	}
	return r.Credentials
}

// GetId returns the id field; it is safe to call on a nil receiver.
func (r *DeleteExternalCredentialRequest) GetId() string {
	if r == nil {
		return ""
	}
	return r.ID
}

// GetType returns the type field; it is safe to call on a nil receiver.
func (d *ExternalCredentialTokenExchangeDriver) GetType() string {
	if d == nil {
		return ""
	}
	return d.Type
}

// GetTargetPrincipal returns the target principal field; it is safe to call on a nil receiver.
func (d *ExternalCredentialTokenExchangeDriver) GetTargetPrincipal() string {
	if d == nil {
		return ""
	}
	return d.TargetPrincipal
}

// GetScopes returns the scopes field; it is safe to call on a nil receiver.
func (d *ExternalCredentialTokenExchangeDriver) GetScopes() []string {
	if d == nil {
		return nil
	}
	return d.Scopes
}

// GetLifetimeSeconds returns the lifetime seconds field; it is safe to call on a nil receiver.
func (d *ExternalCredentialTokenExchangeDriver) GetLifetimeSeconds() int32 {
	if d == nil {
		return 0
	}
	return d.LifetimeSeconds
}

// GetEndpoint returns the endpoint field; it is safe to call on a nil receiver.
func (d *ExternalCredentialTokenExchangeDriver) GetEndpoint() string {
	if d == nil {
		return ""
	}
	return d.Endpoint
}

// GetParams returns the params field; it is safe to call on a nil receiver.
func (d *ExternalCredentialTokenExchangeDriver) GetParams() map[string]string {
	if d == nil {
		return nil
	}
	return d.Params
}

// GetType returns the type field; it is safe to call on a nil receiver.
func (a *ExternalCredentialAuthConfig) GetType() string {
	if a == nil {
		return ""
	}
	return a.Type
}

// GetToken returns the token field; it is safe to call on a nil receiver.
func (a *ExternalCredentialAuthConfig) GetToken() string {
	if a == nil {
		return ""
	}
	return a.Token
}

// GetTokenPrefix returns the token prefix field; it is safe to call on a nil receiver.
func (a *ExternalCredentialAuthConfig) GetTokenPrefix() string {
	if a == nil {
		return ""
	}
	return a.TokenPrefix
}

// GetGrantType returns the grant type field; it is safe to call on a nil receiver.
func (a *ExternalCredentialAuthConfig) GetGrantType() string {
	if a == nil {
		return ""
	}
	return a.GrantType
}

// GetTokenUrl returns the token url field; it is safe to call on a nil receiver.
func (a *ExternalCredentialAuthConfig) GetTokenUrl() string {
	if a == nil {
		return ""
	}
	return a.TokenURL
}

// GetClientId returns the client id field; it is safe to call on a nil receiver.
func (a *ExternalCredentialAuthConfig) GetClientId() string {
	if a == nil {
		return ""
	}
	return a.ClientID
}

// GetClientSecret returns the client secret field; it is safe to call on a nil receiver.
func (a *ExternalCredentialAuthConfig) GetClientSecret() string {
	if a == nil {
		return ""
	}
	return a.ClientSecret
}

// GetClientAuth returns the client auth field; it is safe to call on a nil receiver.
func (a *ExternalCredentialAuthConfig) GetClientAuth() string {
	if a == nil {
		return ""
	}
	return a.ClientAuth
}

// GetTokenExchange returns the token exchange field; it is safe to call on a nil receiver.
func (a *ExternalCredentialAuthConfig) GetTokenExchange() string {
	if a == nil {
		return ""
	}
	return a.TokenExchange
}

// GetScopes returns the scopes field; it is safe to call on a nil receiver.
func (a *ExternalCredentialAuthConfig) GetScopes() []string {
	if a == nil {
		return nil
	}
	return a.Scopes
}

// GetScopeParam returns the scope param field; it is safe to call on a nil receiver.
func (a *ExternalCredentialAuthConfig) GetScopeParam() string {
	if a == nil {
		return ""
	}
	return a.ScopeParam
}

// GetScopeSeparator returns the scope separator field; it is safe to call on a nil receiver.
func (a *ExternalCredentialAuthConfig) GetScopeSeparator() string {
	if a == nil {
		return ""
	}
	return a.ScopeSeparator
}

// GetTokenParams returns the token params field; it is safe to call on a nil receiver.
func (a *ExternalCredentialAuthConfig) GetTokenParams() map[string]string {
	if a == nil {
		return nil
	}
	return a.TokenParams
}

// GetRefreshParams returns the refresh params field; it is safe to call on a nil receiver.
func (a *ExternalCredentialAuthConfig) GetRefreshParams() map[string]string {
	if a == nil {
		return nil
	}
	return a.RefreshParams
}

// GetAcceptHeader returns the accept header field; it is safe to call on a nil receiver.
func (a *ExternalCredentialAuthConfig) GetAcceptHeader() string {
	if a == nil {
		return ""
	}
	return a.AcceptHeader
}

// GetAccessTokenPath returns the access token path field; it is safe to call on a nil receiver.
func (a *ExternalCredentialAuthConfig) GetAccessTokenPath() string {
	if a == nil {
		return ""
	}
	return a.AccessTokenPath
}

// GetTokenExchangeDrivers returns the token exchange drivers field; it is safe to call on a nil receiver.
func (a *ExternalCredentialAuthConfig) GetTokenExchangeDrivers() []*ExternalCredentialTokenExchangeDriver {
	if a == nil {
		return nil
	}
	return a.TokenExchangeDrivers
}

// GetRefreshToken returns the refresh token field; it is safe to call on a nil receiver.
func (a *ExternalCredentialAuthConfig) GetRefreshToken() string {
	if a == nil {
		return ""
	}
	return a.RefreshToken
}

// GetProvider returns the provider field; it is safe to call on a nil receiver.
func (r *ValidateExternalCredentialConfigRequest) GetProvider() string {
	if r == nil {
		return ""
	}
	return r.Provider
}

// GetConnection returns the connection field; it is safe to call on a nil receiver.
func (r *ValidateExternalCredentialConfigRequest) GetConnection() string {
	if r == nil {
		return ""
	}
	return r.Connection
}

// GetConnectionId returns the connection id field; it is safe to call on a nil receiver.
func (r *ValidateExternalCredentialConfigRequest) GetConnectionId() string {
	if r == nil {
		return ""
	}
	return r.ConnectionID
}

// GetMode returns the mode field; it is safe to call on a nil receiver.
func (r *ValidateExternalCredentialConfigRequest) GetMode() string {
	if r == nil {
		return ""
	}
	return r.Mode
}

// GetAuth returns the auth field; it is safe to call on a nil receiver.
func (r *ValidateExternalCredentialConfigRequest) GetAuth() *ExternalCredentialAuthConfig {
	if r == nil {
		return nil
	}
	return r.Auth
}

// GetConnectionParams returns the connection params field; it is safe to call on a nil receiver.
func (r *ValidateExternalCredentialConfigRequest) GetConnectionParams() map[string]string {
	if r == nil {
		return nil
	}
	return r.ConnectionParams
}

// GetProvider returns the provider field; it is safe to call on a nil receiver.
func (r *ResolveExternalCredentialRequest) GetProvider() string {
	if r == nil {
		return ""
	}
	return r.Provider
}

// GetConnection returns the connection field; it is safe to call on a nil receiver.
func (r *ResolveExternalCredentialRequest) GetConnection() string {
	if r == nil {
		return ""
	}
	return r.Connection
}

// GetConnectionId returns the connection id field; it is safe to call on a nil receiver.
func (r *ResolveExternalCredentialRequest) GetConnectionId() string {
	if r == nil {
		return ""
	}
	return r.ConnectionID
}

// GetMode returns the mode field; it is safe to call on a nil receiver.
func (r *ResolveExternalCredentialRequest) GetMode() string {
	if r == nil {
		return ""
	}
	return r.Mode
}

// GetCredentialSubjectId returns the credential subject id field; it is safe to call on a nil receiver.
func (r *ResolveExternalCredentialRequest) GetCredentialSubjectId() string {
	if r == nil {
		return ""
	}
	return r.CredentialSubjectID
}

// GetActorSubjectId returns the actor subject id field; it is safe to call on a nil receiver.
func (r *ResolveExternalCredentialRequest) GetActorSubjectId() string {
	if r == nil {
		return ""
	}
	return r.ActorSubjectID
}

// GetInstance returns the instance field; it is safe to call on a nil receiver.
func (r *ResolveExternalCredentialRequest) GetInstance() string {
	if r == nil {
		return ""
	}
	return r.Instance
}

// GetAuth returns the auth field; it is safe to call on a nil receiver.
func (r *ResolveExternalCredentialRequest) GetAuth() *ExternalCredentialAuthConfig {
	if r == nil {
		return nil
	}
	return r.Auth
}

// GetConnectionParams returns the connection params field; it is safe to call on a nil receiver.
func (r *ResolveExternalCredentialRequest) GetConnectionParams() map[string]string {
	if r == nil {
		return nil
	}
	return r.ConnectionParams
}

// GetToken returns the token field; it is safe to call on a nil receiver.
func (r *ResolveExternalCredentialResponse) GetToken() string {
	if r == nil {
		return ""
	}
	return r.Token
}

// GetExpiresAt returns the expires at field; it is safe to call on a nil receiver.
func (r *ResolveExternalCredentialResponse) GetExpiresAt() *time.Time {
	if r == nil {
		return nil
	}
	return r.ExpiresAt
}

// GetMetadataJson returns the metadata json field; it is safe to call on a nil receiver.
func (r *ResolveExternalCredentialResponse) GetMetadataJson() string {
	if r == nil {
		return ""
	}
	return r.MetadataJSON
}

// GetParams returns the params field; it is safe to call on a nil receiver.
func (r *ResolveExternalCredentialResponse) GetParams() map[string]string {
	if r == nil {
		return nil
	}
	return r.Params
}

// GetCredential returns the credential field; it is safe to call on a nil receiver.
func (r *ResolveExternalCredentialResponse) GetCredential() *ExternalCredential {
	if r == nil {
		return nil
	}
	return r.Credential
}

// GetAccessToken returns the access token field; it is safe to call on a nil receiver.
func (r *ExternalCredentialTokenResponse) GetAccessToken() string {
	if r == nil {
		return ""
	}
	return r.AccessToken
}

// GetRefreshToken returns the refresh token field; it is safe to call on a nil receiver.
func (r *ExternalCredentialTokenResponse) GetRefreshToken() string {
	if r == nil {
		return ""
	}
	return r.RefreshToken
}

// GetExpiresIn returns the expires in field; it is safe to call on a nil receiver.
func (r *ExternalCredentialTokenResponse) GetExpiresIn() int32 {
	if r == nil {
		return 0
	}
	return r.ExpiresIn
}

// GetTokenType returns the token type field; it is safe to call on a nil receiver.
func (r *ExternalCredentialTokenResponse) GetTokenType() string {
	if r == nil {
		return ""
	}
	return r.TokenType
}

// GetExtraJson returns the extra json field; it is safe to call on a nil receiver.
func (r *ExternalCredentialTokenResponse) GetExtraJson() string {
	if r == nil {
		return ""
	}
	return r.ExtraJSON
}

// GetRefreshSource returns the refresh source field; it is safe to call on a nil receiver.
func (r *ExternalCredentialTokenResponse) GetRefreshSource() string {
	if r == nil {
		return ""
	}
	return r.RefreshSource
}

// GetProvider returns the provider field; it is safe to call on a nil receiver.
func (r *ExchangeExternalCredentialRequest) GetProvider() string {
	if r == nil {
		return ""
	}
	return r.Provider
}

// GetConnection returns the connection field; it is safe to call on a nil receiver.
func (r *ExchangeExternalCredentialRequest) GetConnection() string {
	if r == nil {
		return ""
	}
	return r.Connection
}

// GetConnectionId returns the connection id field; it is safe to call on a nil receiver.
func (r *ExchangeExternalCredentialRequest) GetConnectionId() string {
	if r == nil {
		return ""
	}
	return r.ConnectionID
}

// GetCredentialSubjectId returns the credential subject id field; it is safe to call on a nil receiver.
func (r *ExchangeExternalCredentialRequest) GetCredentialSubjectId() string {
	if r == nil {
		return ""
	}
	return r.CredentialSubjectID
}

// GetActorSubjectId returns the actor subject id field; it is safe to call on a nil receiver.
func (r *ExchangeExternalCredentialRequest) GetActorSubjectId() string {
	if r == nil {
		return ""
	}
	return r.ActorSubjectID
}

// GetInstance returns the instance field; it is safe to call on a nil receiver.
func (r *ExchangeExternalCredentialRequest) GetInstance() string {
	if r == nil {
		return ""
	}
	return r.Instance
}

// GetAuth returns the auth field; it is safe to call on a nil receiver.
func (r *ExchangeExternalCredentialRequest) GetAuth() *ExternalCredentialAuthConfig {
	if r == nil {
		return nil
	}
	return r.Auth
}

// GetCredentialJson returns the credential json field; it is safe to call on a nil receiver.
func (r *ExchangeExternalCredentialRequest) GetCredentialJson() string {
	if r == nil {
		return ""
	}
	return r.CredentialJSON
}

// GetConnectionParams returns the connection params field; it is safe to call on a nil receiver.
func (r *ExchangeExternalCredentialRequest) GetConnectionParams() map[string]string {
	if r == nil {
		return nil
	}
	return r.ConnectionParams
}

// GetTokenResponse returns the token response field; it is safe to call on a nil receiver.
func (r *ExchangeExternalCredentialResponse) GetTokenResponse() *ExternalCredentialTokenResponse {
	if r == nil {
		return nil
	}
	return r.TokenResponse
}

// ExternalCredentialProvider serves CRUD operations for host-managed external
// credentials.
type ExternalCredentialProvider interface {
	Provider
	UpsertCredential(ctx context.Context, req *UpsertExternalCredentialRequest) (*ExternalCredential, error)
	GetCredential(ctx context.Context, req *GetExternalCredentialRequest) (*ExternalCredential, error)
	ListCredentials(ctx context.Context, req *ListExternalCredentialsRequest) (*ListExternalCredentialsResponse, error)
	DeleteCredential(ctx context.Context, req *DeleteExternalCredentialRequest) error
	ValidateCredentialConfig(ctx context.Context, req *ValidateExternalCredentialConfigRequest) error
	ResolveCredential(ctx context.Context, req *ResolveExternalCredentialRequest) (*ResolveExternalCredentialResponse, error)
	ExchangeCredential(ctx context.Context, req *ExchangeExternalCredentialRequest) (*ExchangeExternalCredentialResponse, error)
}
