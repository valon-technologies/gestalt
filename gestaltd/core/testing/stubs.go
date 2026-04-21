package coretesting

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	corecrypto "github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

func NewStubServices(t *testing.T) *coredata.Services {
	t.Helper()
	enc, err := corecrypto.NewAESGCM([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewStubServices encryptor: %v", err)
	}
	svc, err := coredata.New(&StubIndexedDB{}, enc)
	if err != nil {
		t.Fatalf("NewStubServices: %v", err)
	}
	return svc
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
	N                        string
	BeginAuthenticationFn    func(context.Context, *core.BeginAuthenticationRequest) (*core.BeginAuthenticationResponse, error)
	CompleteAuthenticationFn func(context.Context, *core.CompleteAuthenticationRequest) (*core.UserIdentity, error)
	AuthenticateFn           func(context.Context, *core.AuthenticateRequest) (*core.UserIdentity, error)
	HandleCallbackFn         func(context.Context, string) (*core.UserIdentity, error)
	ValidateTokenFn          func(context.Context, string) (*core.UserIdentity, error)
}

type StubAuthenticationProvider = StubAuthProvider

func (s *StubAuthProvider) Name() string { return s.N }

func (s *StubAuthProvider) BeginAuthentication(ctx context.Context, req *core.BeginAuthenticationRequest) (*core.BeginAuthenticationResponse, error) {
	if s.BeginAuthenticationFn != nil {
		return s.BeginAuthenticationFn(ctx, req)
	}
	return &core.BeginAuthenticationResponse{}, nil
}

func (s *StubAuthProvider) CompleteAuthentication(ctx context.Context, req *core.CompleteAuthenticationRequest) (*core.UserIdentity, error) {
	if s.CompleteAuthenticationFn != nil {
		return s.CompleteAuthenticationFn(ctx, req)
	}
	if s.HandleCallbackFn != nil {
		code := ""
		if req != nil {
			code = req.Query["code"]
		}
		return s.HandleCallbackFn(ctx, code)
	}
	return nil, nil
}

func (s *StubAuthProvider) Authenticate(ctx context.Context, req *core.AuthenticateRequest) (*core.UserIdentity, error) {
	if s.AuthenticateFn != nil {
		return s.AuthenticateFn(ctx, req)
	}
	if s.ValidateTokenFn != nil {
		token := ""
		if req != nil && req.Token != nil {
			token = req.Token.Token
		}
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
