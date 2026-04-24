package coredata

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
)

type TokenService struct {
	provider core.ExternalCredentialProvider
}

var _ core.ExternalCredentialProvider = (*TokenService)(nil)

func NewTokenService(provider core.ExternalCredentialProvider) *TokenService {
	return &TokenService{provider: provider}
}

func (s *TokenService) SetProvider(provider core.ExternalCredentialProvider) {
	if s == nil {
		return
	}
	s.provider = provider
}

func (s *TokenService) PutCredential(ctx context.Context, credential *core.ExternalCredential) error {
	provider, err := s.requireProvider()
	if err != nil {
		return err
	}
	return provider.PutCredential(ctx, credential)
}

func (s *TokenService) RestoreCredential(ctx context.Context, credential *core.ExternalCredential) error {
	provider, err := s.requireProvider()
	if err != nil {
		return err
	}
	return provider.RestoreCredential(ctx, credential)
}

func (s *TokenService) GetCredential(ctx context.Context, subjectID, integration, connection, instance string) (*core.ExternalCredential, error) {
	provider, err := s.requireProvider()
	if err != nil {
		return nil, err
	}
	return provider.GetCredential(ctx, subjectID, integration, connection, instance)
}

func (s *TokenService) ListCredentials(ctx context.Context, subjectID string) ([]*core.ExternalCredential, error) {
	provider, err := s.requireProvider()
	if err != nil {
		return nil, err
	}
	return provider.ListCredentials(ctx, subjectID)
}

func (s *TokenService) ListCredentialsForProvider(ctx context.Context, subjectID, integration string) ([]*core.ExternalCredential, error) {
	provider, err := s.requireProvider()
	if err != nil {
		return nil, err
	}
	return provider.ListCredentialsForProvider(ctx, subjectID, integration)
}

func (s *TokenService) ListCredentialsForConnection(ctx context.Context, subjectID, integration, connection string) ([]*core.ExternalCredential, error) {
	provider, err := s.requireProvider()
	if err != nil {
		return nil, err
	}
	return provider.ListCredentialsForConnection(ctx, subjectID, integration, connection)
}

func (s *TokenService) DeleteCredential(ctx context.Context, id string) error {
	provider, err := s.requireProvider()
	if err != nil {
		return err
	}
	return provider.DeleteCredential(ctx, id)
}

func (s *TokenService) StoreToken(ctx context.Context, token *core.IntegrationToken) error {
	return s.PutCredential(ctx, token)
}

func (s *TokenService) RestoreToken(ctx context.Context, token *core.IntegrationToken) error {
	return s.RestoreCredential(ctx, token)
}

func (s *TokenService) Token(ctx context.Context, subjectID, integration, connection, instance string) (*core.IntegrationToken, error) {
	return s.GetCredential(ctx, subjectID, integration, connection, instance)
}

func (s *TokenService) ListTokens(ctx context.Context, subjectID string) ([]*core.IntegrationToken, error) {
	return s.ListCredentials(ctx, subjectID)
}

func (s *TokenService) ListTokensForIntegration(ctx context.Context, subjectID, integration string) ([]*core.IntegrationToken, error) {
	return s.ListCredentialsForProvider(ctx, subjectID, integration)
}

func (s *TokenService) ListTokensForConnection(ctx context.Context, subjectID, integration, connection string) ([]*core.IntegrationToken, error) {
	return s.ListCredentialsForConnection(ctx, subjectID, integration, connection)
}

func (s *TokenService) DeleteToken(ctx context.Context, id string) error {
	return s.DeleteCredential(ctx, id)
}

func (s *TokenService) Provider() core.ExternalCredentialProvider {
	if s == nil {
		return nil
	}
	return s.provider
}

func (s *TokenService) requireProvider() (core.ExternalCredentialProvider, error) {
	if s == nil || ExternalCredentialProviderMissing(s.provider) {
		return nil, fmt.Errorf("external credential provider is not available")
	}
	return s.provider, nil
}
