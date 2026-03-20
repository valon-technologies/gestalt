package broker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/internal/registry"
)

var _ core.Broker = (*Broker)(nil)

type CredentialResolver interface {
	Token(ctx context.Context, userID, integration, instance string) (*core.IntegrationToken, error)
	StoreToken(ctx context.Context, token *core.IntegrationToken) error
}

type Broker struct {
	providers *registry.PluginMap[core.Provider]
	creds     CredentialResolver
}

const (
	defaultInstance       = "default"
	tokenRefreshThreshold = 5 * time.Minute
)

func New(providers *registry.PluginMap[core.Provider], creds CredentialResolver) *Broker {
	return &Broker{providers: providers, creds: creds}
}

func (b *Broker) Invoke(ctx context.Context, req core.InvocationRequest) (*core.OperationResult, error) {
	prov, err := b.providers.Get(req.Provider)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return nil, &ProviderNotFoundError{Name: req.Provider}
		}
		return nil, fmt.Errorf("looking up provider: %w", err)
	}

	if !hasOperation(prov, req.Operation) {
		return nil, &OperationNotFoundError{Provider: req.Provider, Operation: req.Operation}
	}

	token, err := b.resolveToken(ctx, prov, req)
	if err != nil {
		return nil, err
	}

	return prov.Execute(ctx, req.Operation, req.Params, token)
}

func (b *Broker) ListCapabilities() []core.Capability {
	var caps []core.Capability
	for _, name := range b.providers.List() {
		prov, err := b.providers.Get(name)
		if err != nil {
			continue
		}
		for _, op := range prov.ListOperations() {
			caps = append(caps, core.Capability{
				Provider:    name,
				Operation:   op.Name,
				Description: op.Description,
				Parameters:  op.Parameters,
			})
		}
	}
	return caps
}

func (b *Broker) resolveToken(ctx context.Context, prov core.Provider, req core.InvocationRequest) (string, error) {
	mode := prov.ConnectionMode()
	switch mode {
	case core.ConnectionModeNone:
		return "", nil

	case core.ConnectionModeUser, "":
		tok, err := b.creds.Token(ctx, req.UserID, req.Provider, defaultInstance)
		if err != nil {
			if errors.Is(err, core.ErrNotFound) {
				return "", &NoCredentialError{Provider: req.Provider}
			}
			return "", fmt.Errorf("retrieving credential: %w", err)
		}
		if tok == nil {
			return "", &NoCredentialError{Provider: req.Provider}
		}
		return b.refreshTokenIfNeeded(ctx, prov, tok)

	case core.ConnectionModeIdentity, core.ConnectionModeEither:
		return "", fmt.Errorf("connection mode %q not yet implemented", mode)

	default:
		return "", fmt.Errorf("unknown connection mode %q", mode)
	}
}

func (b *Broker) refreshTokenIfNeeded(ctx context.Context, prov core.Provider, token *core.IntegrationToken) (string, error) {
	if token.RefreshToken == "" || token.ExpiresAt == nil {
		return token.AccessToken, nil
	}

	if time.Until(*token.ExpiresAt) > tokenRefreshThreshold {
		return token.AccessToken, nil
	}

	oauthProv, ok := prov.(core.OAuthProvider)
	if !ok {
		return token.AccessToken, nil
	}

	resp, err := oauthProv.RefreshToken(ctx, token.RefreshToken)
	if err != nil {
		fresh, fetchErr := b.creds.Token(ctx, token.UserID, token.Integration, token.Instance)
		if fetchErr == nil && fresh != nil && fresh.AccessToken != token.AccessToken {
			return fresh.AccessToken, nil
		}

		token.RefreshErrorCount++
		token.UpdatedAt = time.Now()
		_ = b.creds.StoreToken(ctx, token)

		if time.Now().Before(*token.ExpiresAt) {
			return token.AccessToken, nil
		}
		return "", fmt.Errorf("token expired and refresh failed: %w", err)
	}

	now := time.Now()
	token.AccessToken = resp.AccessToken
	if resp.RefreshToken != "" {
		token.RefreshToken = resp.RefreshToken
	}
	if resp.ExpiresIn > 0 {
		t := now.Add(time.Duration(resp.ExpiresIn) * time.Second)
		token.ExpiresAt = &t
	} else {
		token.ExpiresAt = nil
	}
	token.LastRefreshedAt = now
	token.RefreshErrorCount = 0
	token.UpdatedAt = now

	if err := b.creds.StoreToken(ctx, token); err != nil {
		return "", fmt.Errorf("persisting refreshed token: %w", err)
	}
	return token.AccessToken, nil
}

func hasOperation(prov core.Provider, name string) bool {
	for _, op := range prov.ListOperations() {
		if op.Name == name {
			return true
		}
	}
	return false
}
