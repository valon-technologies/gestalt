package coretesting

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
)

type StubExternalCredentialProvider struct {
	mu                           sync.Mutex
	credentials                  map[string]core.ExternalCredential
	nextSequence                 int
	PutErr                       error
	GetErr                       error
	ListErr                      error
	DeleteErr                    error
	ValidateCredentialConfigFunc func(context.Context, *core.ValidateExternalCredentialConfigRequest) error
	ResolveCredentialFunc        func(context.Context, *core.ResolveExternalCredentialRequest) (*core.ResolveExternalCredentialResponse, error)
	ExchangeCredentialFunc       func(context.Context, *core.ExchangeExternalCredentialRequest) (*core.ExchangeExternalCredentialResponse, error)
}

func NewStubExternalCredentialProvider() *StubExternalCredentialProvider {
	return &StubExternalCredentialProvider{credentials: make(map[string]core.ExternalCredential)}
}

func (p *StubExternalCredentialProvider) CreateCredential(_ context.Context, credential *core.ExternalCredential) error {
	if p != nil && p.PutErr != nil {
		return p.PutErr
	}
	return p.storeCredential(credential, true)
}

func (p *StubExternalCredentialProvider) UpsertCredential(_ context.Context, credential *core.ExternalCredential) error {
	if p != nil && p.PutErr != nil {
		return p.PutErr
	}
	return p.storeCredential(credential, false)
}

func (p *StubExternalCredentialProvider) GetCredential(_ context.Context, subject, audience, qualifier string) (*core.ExternalCredential, error) {
	if p != nil && p.GetErr != nil {
		return nil, p.GetErr
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, credential := range p.credentials {
		if credential.Subject == subject && credential.Audience == audience && credential.Qualifier == qualifier {
			return cloneExternalCredential(credential), nil
		}
	}
	return nil, core.ErrNotFound
}

func (p *StubExternalCredentialProvider) ListCredentials(_ context.Context, subject, audience string) ([]*core.ExternalCredential, error) {
	return p.listCredentials(subject, audience)
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

func (p *StubExternalCredentialProvider) ValidateCredentialConfig(ctx context.Context, req *core.ValidateExternalCredentialConfigRequest) error {
	if p != nil && p.ValidateCredentialConfigFunc != nil {
		return p.ValidateCredentialConfigFunc(ctx, req)
	}
	return nil
}

func (p *StubExternalCredentialProvider) ResolveCredential(ctx context.Context, req *core.ResolveExternalCredentialRequest) (*core.ResolveExternalCredentialResponse, error) {
	if p != nil && p.ResolveCredentialFunc != nil {
		return p.ResolveCredentialFunc(ctx, req)
	}
	if req == nil {
		return nil, core.ErrNotFound
	}
	var credential *core.ExternalCredential
	var err error
	if req.Instance != "" {
		credential, err = p.GetCredential(ctx, req.CredentialSubjectID, req.ConnectionID, req.Instance)
	} else {
		var credentials []*core.ExternalCredential
		credentials, err = p.ListCredentials(ctx, req.CredentialSubjectID, req.ConnectionID)
		if err == nil {
			switch len(credentials) {
			case 0:
				err = core.ErrNotFound
			case 1:
				credential = credentials[0]
			default:
				err = core.ErrAmbiguousCredential
			}
		}
	}
	if err != nil {
		return nil, err
	}
	resp := &core.ResolveExternalCredentialResponse{
		MetadataJSON: credential.MetadataJSON,
		Credential:   cloneExternalCredential(*credential),
	}
	switch {
	case credential.Grant != nil:
		resp.Token = credential.Grant.AccessToken
		resp.ExpiresAt = credential.Grant.ExpiresAt
	case credential.Opaque != nil:
		if data, err := json.Marshal(credential.Opaque.Fields); err == nil {
			resp.Token = string(data)
		}
		resp.Params = credential.Opaque.Fields
	}
	return resp, nil
}

func (p *StubExternalCredentialProvider) ExchangeCredential(ctx context.Context, req *core.ExchangeExternalCredentialRequest) (*core.ExchangeExternalCredentialResponse, error) {
	if p != nil && p.ExchangeCredentialFunc != nil {
		return p.ExchangeCredentialFunc(ctx, req)
	}
	if req == nil {
		return nil, core.ErrNotFound
	}
	return &core.ExchangeExternalCredentialResponse{
		TokenResponse: &core.ExternalCredentialTokenResponse{
			AccessToken:   req.CredentialJSON,
			RefreshSource: req.CredentialJSON,
		},
	}, nil
}

func (p *StubExternalCredentialProvider) storeCredential(credential *core.ExternalCredential, insertOnly bool) error {
	if credential == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	cloned := *credential
	for _, existing := range p.credentials {
		if existing.Subject == cloned.Subject && existing.Audience == cloned.Audience && existing.Qualifier == cloned.Qualifier {
			if insertOnly {
				return core.ErrAlreadyExists
			}
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
	cloned.UpdatedAt = now
	p.credentials[cloned.ID] = cloned
	*credential = cloned
	return nil
}

func (p *StubExternalCredentialProvider) listCredentials(subject, audience string) ([]*core.ExternalCredential, error) {
	if p != nil && p.ListErr != nil {
		return nil, p.ListErr
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*core.ExternalCredential, 0, len(p.credentials))
	for _, credential := range p.credentials {
		if credential.Subject != subject {
			continue
		}
		if audience != "" && credential.Audience != audience {
			continue
		}
		out = append(out, cloneExternalCredential(credential))
	}
	return out, nil
}

func cloneExternalCredential(src core.ExternalCredential) *core.ExternalCredential {
	cloned := src
	if src.Grant != nil {
		grant := *src.Grant
		if grant.ExpiresAt != nil {
			value := *grant.ExpiresAt
			grant.ExpiresAt = &value
		}
		if grant.LastRefreshedAt != nil {
			value := *grant.LastRefreshedAt
			grant.LastRefreshedAt = &value
		}
		cloned.Grant = &grant
	}
	if src.Client != nil {
		client := *src.Client
		if client.ClientSecretExpiresAt != nil {
			value := *client.ClientSecretExpiresAt
			client.ClientSecretExpiresAt = &value
		}
		cloned.Client = &client
	}
	if src.Opaque != nil {
		fields := make(map[string]string, len(src.Opaque.Fields))
		for k, v := range src.Opaque.Fields {
			fields[k] = v
		}
		cloned.Opaque = &core.ExternalCredentialOpaque{Fields: fields}
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

type StubIntegration struct {
	N              string
	DN             string
	Desc           string
	ConnMode       core.ConnectionMode
	CatalogVal     *catalog.Catalog
	ExchangeCodeFn func(context.Context, string) (*core.OAuthTokenResponse, error)
	ExecuteFn      func(context.Context, string, map[string]any, string) (*core.OperationResult, error)
}

func (s *StubIntegration) Name() string        { return s.N }
func (s *StubIntegration) DisplayName() string { return s.DN }
func (s *StubIntegration) Description() string { return s.Desc }

func (s *StubIntegration) ConnectionMode() core.ConnectionMode {
	if s.ConnMode == "" {
		return core.ConnectionModeSubject
	}
	return core.NormalizeConnectionMode(s.ConnMode)
}
func (s *StubIntegration) AuthTypes() []string { return nil }
func (s *StubIntegration) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return nil
}
func (s *StubIntegration) CredentialFields() []core.CredentialFieldDef { return nil }
func (s *StubIntegration) DiscoveryConfig() *core.DiscoveryConfig      { return nil }
func (s *StubIntegration) ConnectionForOperation(string) string        { return "" }
func (s *StubIntegration) AuthorizationURL(string, []string) string    { return "" }
func (s *StubIntegration) ExchangeCode(ctx context.Context, code string) (*core.OAuthTokenResponse, error) {
	if s.ExchangeCodeFn != nil {
		return s.ExchangeCodeFn(ctx, code)
	}
	return nil, nil
}
func (s *StubIntegration) RefreshToken(context.Context, string) (*core.OAuthTokenResponse, error) {
	return nil, nil
}
func (s *StubIntegration) Catalog() *catalog.Catalog { return s.CatalogVal }
func (s *StubIntegration) Execute(ctx context.Context, op string, params map[string]any, token string) (*core.OperationResult, error) {
	if s.ExecuteFn != nil {
		return s.ExecuteFn(ctx, op, params, token)
	}
	return nil, nil
}
