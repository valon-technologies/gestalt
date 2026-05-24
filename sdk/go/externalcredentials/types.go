package externalcredentials

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

type ExternalCredentialTokenExchangeDriver struct {
	Type            string
	TargetPrincipal string
	Scopes          []string
	LifetimeSeconds int32
	Endpoint        string
	Params          map[string]string
}

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

type ValidateExternalCredentialConfigRequest struct {
	Provider         string
	Connection       string
	ConnectionID     string
	Mode             string
	Auth             *ExternalCredentialAuthConfig
	ConnectionParams map[string]string
}

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

type ResolveExternalCredentialResponse struct {
	Token        string
	ExpiresAt    *time.Time
	MetadataJSON string
	Params       map[string]string
	Credential   *ExternalCredential
}

type ExternalCredentialTokenResponse struct {
	AccessToken   string
	RefreshToken  string
	ExpiresIn     int32
	TokenType     string
	ExtraJSON     string
	RefreshSource string
}

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

type ExchangeExternalCredentialResponse struct {
	TokenResponse *ExternalCredentialTokenResponse
}

func (c *ExternalCredential) GetId() string {
	if c == nil {
		return ""
	}
	return c.ID
}

func (c *ExternalCredential) GetSubjectId() string {
	if c == nil {
		return ""
	}
	return c.SubjectID
}

func (c *ExternalCredential) GetInstance() string {
	if c == nil {
		return ""
	}
	return c.Instance
}

func (c *ExternalCredential) GetAccessToken() string {
	if c == nil {
		return ""
	}
	return c.AccessToken
}

func (c *ExternalCredential) GetRefreshToken() string {
	if c == nil {
		return ""
	}
	return c.RefreshToken
}

func (c *ExternalCredential) GetScopes() string {
	if c == nil {
		return ""
	}
	return c.Scopes
}

func (c *ExternalCredential) GetExpiresAt() *time.Time {
	if c == nil {
		return nil
	}
	return c.ExpiresAt
}

func (c *ExternalCredential) GetLastRefreshedAt() *time.Time {
	if c == nil {
		return nil
	}
	return c.LastRefreshedAt
}

func (c *ExternalCredential) GetRefreshErrorCount() int32 {
	if c == nil {
		return 0
	}
	return c.RefreshErrorCount
}

func (c *ExternalCredential) GetMetadataJson() string {
	if c == nil {
		return ""
	}
	return c.MetadataJSON
}

func (c *ExternalCredential) GetCreatedAt() *time.Time {
	if c == nil {
		return nil
	}
	return c.CreatedAt
}

func (c *ExternalCredential) GetUpdatedAt() *time.Time {
	if c == nil {
		return nil
	}
	return c.UpdatedAt
}

func (c *ExternalCredential) GetConnectionId() string {
	if c == nil {
		return ""
	}
	return c.ConnectionID
}

func (l *ExternalCredentialLookup) GetSubjectId() string {
	if l == nil {
		return ""
	}
	return l.SubjectID
}

func (l *ExternalCredentialLookup) GetInstance() string {
	if l == nil {
		return ""
	}
	return l.Instance
}

func (l *ExternalCredentialLookup) GetConnectionId() string {
	if l == nil {
		return ""
	}
	return l.ConnectionID
}

func (r *UpsertExternalCredentialRequest) GetCredential() *ExternalCredential {
	if r == nil {
		return nil
	}
	return r.Credential
}

func (r *UpsertExternalCredentialRequest) GetPreserveTimestamps() bool {
	if r == nil {
		return false
	}
	return r.PreserveTimestamps
}

func (r *GetExternalCredentialRequest) GetLookup() *ExternalCredentialLookup {
	if r == nil {
		return nil
	}
	return r.Lookup
}

func (r *ListExternalCredentialsRequest) GetSubjectId() string {
	if r == nil {
		return ""
	}
	return r.SubjectID
}

func (r *ListExternalCredentialsRequest) GetInstance() string {
	if r == nil {
		return ""
	}
	return r.Instance
}

func (r *ListExternalCredentialsRequest) GetConnectionId() string {
	if r == nil {
		return ""
	}
	return r.ConnectionID
}

func (r *ListExternalCredentialsResponse) GetCredentials() []*ExternalCredential {
	if r == nil {
		return nil
	}
	return r.Credentials
}

func (r *DeleteExternalCredentialRequest) GetId() string {
	if r == nil {
		return ""
	}
	return r.ID
}

func (d *ExternalCredentialTokenExchangeDriver) GetType() string {
	if d == nil {
		return ""
	}
	return d.Type
}

func (d *ExternalCredentialTokenExchangeDriver) GetTargetPrincipal() string {
	if d == nil {
		return ""
	}
	return d.TargetPrincipal
}

func (d *ExternalCredentialTokenExchangeDriver) GetScopes() []string {
	if d == nil {
		return nil
	}
	return d.Scopes
}

func (d *ExternalCredentialTokenExchangeDriver) GetLifetimeSeconds() int32 {
	if d == nil {
		return 0
	}
	return d.LifetimeSeconds
}

func (d *ExternalCredentialTokenExchangeDriver) GetEndpoint() string {
	if d == nil {
		return ""
	}
	return d.Endpoint
}

func (d *ExternalCredentialTokenExchangeDriver) GetParams() map[string]string {
	if d == nil {
		return nil
	}
	return d.Params
}

func (a *ExternalCredentialAuthConfig) GetType() string {
	if a == nil {
		return ""
	}
	return a.Type
}

func (a *ExternalCredentialAuthConfig) GetToken() string {
	if a == nil {
		return ""
	}
	return a.Token
}

func (a *ExternalCredentialAuthConfig) GetTokenPrefix() string {
	if a == nil {
		return ""
	}
	return a.TokenPrefix
}

func (a *ExternalCredentialAuthConfig) GetGrantType() string {
	if a == nil {
		return ""
	}
	return a.GrantType
}

func (a *ExternalCredentialAuthConfig) GetTokenUrl() string {
	if a == nil {
		return ""
	}
	return a.TokenURL
}

func (a *ExternalCredentialAuthConfig) GetClientId() string {
	if a == nil {
		return ""
	}
	return a.ClientID
}

func (a *ExternalCredentialAuthConfig) GetClientSecret() string {
	if a == nil {
		return ""
	}
	return a.ClientSecret
}

func (a *ExternalCredentialAuthConfig) GetClientAuth() string {
	if a == nil {
		return ""
	}
	return a.ClientAuth
}

func (a *ExternalCredentialAuthConfig) GetTokenExchange() string {
	if a == nil {
		return ""
	}
	return a.TokenExchange
}

func (a *ExternalCredentialAuthConfig) GetScopes() []string {
	if a == nil {
		return nil
	}
	return a.Scopes
}

func (a *ExternalCredentialAuthConfig) GetScopeParam() string {
	if a == nil {
		return ""
	}
	return a.ScopeParam
}

func (a *ExternalCredentialAuthConfig) GetScopeSeparator() string {
	if a == nil {
		return ""
	}
	return a.ScopeSeparator
}

func (a *ExternalCredentialAuthConfig) GetTokenParams() map[string]string {
	if a == nil {
		return nil
	}
	return a.TokenParams
}

func (a *ExternalCredentialAuthConfig) GetRefreshParams() map[string]string {
	if a == nil {
		return nil
	}
	return a.RefreshParams
}

func (a *ExternalCredentialAuthConfig) GetAcceptHeader() string {
	if a == nil {
		return ""
	}
	return a.AcceptHeader
}

func (a *ExternalCredentialAuthConfig) GetAccessTokenPath() string {
	if a == nil {
		return ""
	}
	return a.AccessTokenPath
}

func (a *ExternalCredentialAuthConfig) GetTokenExchangeDrivers() []*ExternalCredentialTokenExchangeDriver {
	if a == nil {
		return nil
	}
	return a.TokenExchangeDrivers
}

func (a *ExternalCredentialAuthConfig) GetRefreshToken() string {
	if a == nil {
		return ""
	}
	return a.RefreshToken
}

func (r *ValidateExternalCredentialConfigRequest) GetProvider() string {
	if r == nil {
		return ""
	}
	return r.Provider
}

func (r *ValidateExternalCredentialConfigRequest) GetConnection() string {
	if r == nil {
		return ""
	}
	return r.Connection
}

func (r *ValidateExternalCredentialConfigRequest) GetConnectionId() string {
	if r == nil {
		return ""
	}
	return r.ConnectionID
}

func (r *ValidateExternalCredentialConfigRequest) GetMode() string {
	if r == nil {
		return ""
	}
	return r.Mode
}

func (r *ValidateExternalCredentialConfigRequest) GetAuth() *ExternalCredentialAuthConfig {
	if r == nil {
		return nil
	}
	return r.Auth
}

func (r *ValidateExternalCredentialConfigRequest) GetConnectionParams() map[string]string {
	if r == nil {
		return nil
	}
	return r.ConnectionParams
}

func (r *ResolveExternalCredentialRequest) GetProvider() string {
	if r == nil {
		return ""
	}
	return r.Provider
}

func (r *ResolveExternalCredentialRequest) GetConnection() string {
	if r == nil {
		return ""
	}
	return r.Connection
}

func (r *ResolveExternalCredentialRequest) GetConnectionId() string {
	if r == nil {
		return ""
	}
	return r.ConnectionID
}

func (r *ResolveExternalCredentialRequest) GetMode() string {
	if r == nil {
		return ""
	}
	return r.Mode
}

func (r *ResolveExternalCredentialRequest) GetCredentialSubjectId() string {
	if r == nil {
		return ""
	}
	return r.CredentialSubjectID
}

func (r *ResolveExternalCredentialRequest) GetActorSubjectId() string {
	if r == nil {
		return ""
	}
	return r.ActorSubjectID
}

func (r *ResolveExternalCredentialRequest) GetInstance() string {
	if r == nil {
		return ""
	}
	return r.Instance
}

func (r *ResolveExternalCredentialRequest) GetAuth() *ExternalCredentialAuthConfig {
	if r == nil {
		return nil
	}
	return r.Auth
}

func (r *ResolveExternalCredentialRequest) GetConnectionParams() map[string]string {
	if r == nil {
		return nil
	}
	return r.ConnectionParams
}

func (r *ResolveExternalCredentialResponse) GetToken() string {
	if r == nil {
		return ""
	}
	return r.Token
}

func (r *ResolveExternalCredentialResponse) GetExpiresAt() *time.Time {
	if r == nil {
		return nil
	}
	return r.ExpiresAt
}

func (r *ResolveExternalCredentialResponse) GetMetadataJson() string {
	if r == nil {
		return ""
	}
	return r.MetadataJSON
}

func (r *ResolveExternalCredentialResponse) GetParams() map[string]string {
	if r == nil {
		return nil
	}
	return r.Params
}

func (r *ResolveExternalCredentialResponse) GetCredential() *ExternalCredential {
	if r == nil {
		return nil
	}
	return r.Credential
}

func (r *ExternalCredentialTokenResponse) GetAccessToken() string {
	if r == nil {
		return ""
	}
	return r.AccessToken
}

func (r *ExternalCredentialTokenResponse) GetRefreshToken() string {
	if r == nil {
		return ""
	}
	return r.RefreshToken
}

func (r *ExternalCredentialTokenResponse) GetExpiresIn() int32 {
	if r == nil {
		return 0
	}
	return r.ExpiresIn
}

func (r *ExternalCredentialTokenResponse) GetTokenType() string {
	if r == nil {
		return ""
	}
	return r.TokenType
}

func (r *ExternalCredentialTokenResponse) GetExtraJson() string {
	if r == nil {
		return ""
	}
	return r.ExtraJSON
}

func (r *ExternalCredentialTokenResponse) GetRefreshSource() string {
	if r == nil {
		return ""
	}
	return r.RefreshSource
}

func (r *ExchangeExternalCredentialRequest) GetProvider() string {
	if r == nil {
		return ""
	}
	return r.Provider
}

func (r *ExchangeExternalCredentialRequest) GetConnection() string {
	if r == nil {
		return ""
	}
	return r.Connection
}

func (r *ExchangeExternalCredentialRequest) GetConnectionId() string {
	if r == nil {
		return ""
	}
	return r.ConnectionID
}

func (r *ExchangeExternalCredentialRequest) GetCredentialSubjectId() string {
	if r == nil {
		return ""
	}
	return r.CredentialSubjectID
}

func (r *ExchangeExternalCredentialRequest) GetActorSubjectId() string {
	if r == nil {
		return ""
	}
	return r.ActorSubjectID
}

func (r *ExchangeExternalCredentialRequest) GetInstance() string {
	if r == nil {
		return ""
	}
	return r.Instance
}

func (r *ExchangeExternalCredentialRequest) GetAuth() *ExternalCredentialAuthConfig {
	if r == nil {
		return nil
	}
	return r.Auth
}

func (r *ExchangeExternalCredentialRequest) GetCredentialJson() string {
	if r == nil {
		return ""
	}
	return r.CredentialJSON
}

func (r *ExchangeExternalCredentialRequest) GetConnectionParams() map[string]string {
	if r == nil {
		return nil
	}
	return r.ConnectionParams
}

func (r *ExchangeExternalCredentialResponse) GetTokenResponse() *ExternalCredentialTokenResponse {
	if r == nil {
		return nil
	}
	return r.TokenResponse
}

// ExternalCredentials serves CRUD operations for host-managed external credentials.
type ExternalCredentials interface {
	UpsertCredential(ctx context.Context, req *UpsertExternalCredentialRequest) (*ExternalCredential, error)
	GetCredential(ctx context.Context, req *GetExternalCredentialRequest) (*ExternalCredential, error)
	ListCredentials(ctx context.Context, req *ListExternalCredentialsRequest) (*ListExternalCredentialsResponse, error)
	DeleteCredential(ctx context.Context, req *DeleteExternalCredentialRequest) error
	ValidateCredentialConfig(ctx context.Context, req *ValidateExternalCredentialConfigRequest) error
	ResolveCredential(ctx context.Context, req *ResolveExternalCredentialRequest) (*ResolveExternalCredentialResponse, error)
	ExchangeCredential(ctx context.Context, req *ExchangeExternalCredentialRequest) (*ExchangeExternalCredentialResponse, error)
}
