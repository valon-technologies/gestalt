package core

import (
	"context"
	"reflect"
	"time"
)

// ExternalCredentialProvider manages subject-scoped third-party credentials
// used to invoke integrations on behalf of users or other canonical subjects.
type ExternalCredentialProvider interface {
	PutCredential(ctx context.Context, credential *ExternalCredential) error
	RestoreCredential(ctx context.Context, credential *ExternalCredential) error
	GetCredential(ctx context.Context, subjectID, connectionID, instance string) (*ExternalCredential, error)
	ListCredentials(ctx context.Context, subjectID string) ([]*ExternalCredential, error)
	ListCredentialsForConnection(ctx context.Context, subjectID, connectionID string) ([]*ExternalCredential, error)
	DeleteCredential(ctx context.Context, id string) error
	ValidateCredentialConfig(ctx context.Context, req *ValidateExternalCredentialConfigRequest) error
	ResolveCredential(ctx context.Context, req *ResolveExternalCredentialRequest) (*ResolveExternalCredentialResponse, error)
	ExchangeCredential(ctx context.Context, req *ExchangeExternalCredentialRequest) (*ExchangeExternalCredentialResponse, error)
}

type ExternalCredentialTokenExchangeDriver struct {
	Type            string
	TargetPrincipal string
	Scopes          []string
	LifetimeSeconds int
	Endpoint        string
	Params          map[string]string
}

type ExternalCredentialAuthConfig struct {
	Type                 string
	Token                string
	TokenPrefix          string
	GrantType            string
	RefreshToken         string
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
	TokenExchangeDrivers []ExternalCredentialTokenExchangeDriver
}

type ValidateExternalCredentialConfigRequest struct {
	Provider         string
	Connection       string
	ConnectionID     string
	Mode             ConnectionMode
	Auth             ExternalCredentialAuthConfig
	ConnectionParams map[string]string
}

type ResolveExternalCredentialRequest struct {
	Provider            string
	Connection          string
	ConnectionID        string
	Mode                ConnectionMode
	CredentialSubjectID string
	ActorSubjectID      string
	Instance            string
	Auth                ExternalCredentialAuthConfig
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
	RefreshSource string
	ExpiresIn     int
	TokenType     string
	Extra         map[string]any
}

type ExchangeExternalCredentialRequest struct {
	Provider            string
	Connection          string
	ConnectionID        string
	CredentialSubjectID string
	ActorSubjectID      string
	Instance            string
	Auth                ExternalCredentialAuthConfig
	CredentialJSON      string
	ConnectionParams    map[string]string
}

type ExchangeExternalCredentialResponse struct {
	TokenResponse *ExternalCredentialTokenResponse
}

// ExternalCredentialProviderMissing reports whether provider is nil, including
// typed nil implementations stored in the interface.
func ExternalCredentialProviderMissing(provider ExternalCredentialProvider) bool {
	if provider == nil {
		return true
	}
	value := reflect.ValueOf(provider)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
