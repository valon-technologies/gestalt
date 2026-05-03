package coretesting

import (
	"context"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
)

type StubExternalCredentialProvider struct {
	mu           sync.Mutex
	credentials  map[string]core.ExternalCredential
	nextSequence int
	PutErr       error
	GetErr       error
	ListErr      error
	DeleteErr    error
}

func NewStubExternalCredentialProvider() *StubExternalCredentialProvider {
	return &StubExternalCredentialProvider{credentials: make(map[string]core.ExternalCredential)}
}

func (p *StubExternalCredentialProvider) PutCredential(_ context.Context, credential *core.ExternalCredential) error {
	if p != nil && p.PutErr != nil {
		return p.PutErr
	}
	return p.storeCredential(credential, false)
}

func (p *StubExternalCredentialProvider) RestoreCredential(_ context.Context, credential *core.ExternalCredential) error {
	if p != nil && p.PutErr != nil {
		return p.PutErr
	}
	return p.storeCredential(credential, true)
}

func (p *StubExternalCredentialProvider) GetCredential(_ context.Context, subjectID, connectionID, instance string) (*core.ExternalCredential, error) {
	if p != nil && p.GetErr != nil {
		return nil, p.GetErr
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, credential := range p.credentials {
		if credential.SubjectID == subjectID && credential.ConnectionID == connectionID && credential.Instance == instance {
			return cloneExternalCredential(credential), nil
		}
	}
	return nil, core.ErrNotFound
}

func (p *StubExternalCredentialProvider) ListCredentials(_ context.Context, subjectID string) ([]*core.ExternalCredential, error) {
	return p.listCredentials(subjectID, "")
}

func (p *StubExternalCredentialProvider) ListCredentialsForConnection(_ context.Context, subjectID, connectionID string) ([]*core.ExternalCredential, error) {
	return p.listCredentials(subjectID, connectionID)
}

func (p *StubExternalCredentialProvider) DeleteCredential(_ context.Context, id string) error {
	if p != nil && p.DeleteErr != nil {
		return p.DeleteErr
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.credentials, id)
	return nil
}

func (p *StubExternalCredentialProvider) storeCredential(credential *core.ExternalCredential, preserve bool) error {
	if credential == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	cloned := *credential
	if cloned.ConnectionID == "" {
		connection := cloned.Connection
		if connection == "" {
			connection = core.PluginConnectionName
		}
		cloned.ConnectionID = cloned.Integration + ":" + connection
	}
	for _, existing := range p.credentials {
		if existing.SubjectID == cloned.SubjectID && existing.ConnectionID == cloned.ConnectionID && existing.Instance == cloned.Instance {
			cloned.ID = existing.ID
			cloned.CreatedAt = existing.CreatedAt
			break
		}
	}
	now := time.Now().UTC()
	if cloned.ID == "" {
		p.nextSequence++
		cloned.ID = "cred-" + time.Unix(0, int64(p.nextSequence)).UTC().Format("150405.000000000")
	}
	if cloned.CreatedAt.IsZero() {
		cloned.CreatedAt = now
	}
	if preserve && !credential.UpdatedAt.IsZero() {
		cloned.UpdatedAt = credential.UpdatedAt
	} else {
		cloned.UpdatedAt = now
	}
	p.credentials[cloned.ID] = cloned
	*credential = cloned
	return nil
}

func (p *StubExternalCredentialProvider) listCredentials(subjectID, connectionID string) ([]*core.ExternalCredential, error) {
	if p != nil && p.ListErr != nil {
		return nil, p.ListErr
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*core.ExternalCredential, 0, len(p.credentials))
	for _, credential := range p.credentials {
		if credential.SubjectID != subjectID {
			continue
		}
		if connectionID != "" && credential.ConnectionID != connectionID {
			continue
		}
		out = append(out, cloneExternalCredential(credential))
	}
	return out, nil
}

func cloneExternalCredential(src core.ExternalCredential) *core.ExternalCredential {
	cloned := src
	if cloned.ExpiresAt != nil {
		value := *cloned.ExpiresAt
		cloned.ExpiresAt = &value
	}
	if cloned.LastRefreshedAt != nil {
		value := *cloned.LastRefreshedAt
		cloned.LastRefreshedAt = &value
	}
	return &cloned
}

type StubSecretManager struct {
	Secrets map[string]string
}

func (s *StubSecretManager) GetSecret(_ context.Context, name string) (string, error) {
	if v, ok := s.Secrets[name]; ok {
		return v, nil
	}
	return "", core.ErrSecretNotFound
}

type StubAuthProvider struct {
	N                string
	HandleCallbackFn func(context.Context, string) (*core.UserIdentity, error)
	ValidateTokenFn  func(context.Context, string) (*core.UserIdentity, error)
}

type StubAuthenticationProvider = StubAuthProvider

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
func (s *StubIntegration) AuthTypes() []string { return nil }
func (s *StubIntegration) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return nil
}
func (s *StubIntegration) CredentialFields() []core.CredentialFieldDef { return nil }
func (s *StubIntegration) DiscoveryConfig() *core.DiscoveryConfig      { return nil }
func (s *StubIntegration) ConnectionForOperation(string) string        { return "" }
func (s *StubIntegration) AuthorizationURL(string, []string) string    { return "" }
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
