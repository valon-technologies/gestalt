package invocation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/internal/principal"
	"github.com/valon-technologies/toolshed/internal/registry"
)

const tokenRefreshThreshold = 5 * time.Minute

type Broker struct {
	providers *registry.PluginMap[core.Provider]
	datastore core.Datastore
}

func NewBroker(providers *registry.PluginMap[core.Provider], ds core.Datastore) *Broker {
	return &Broker{providers: providers, datastore: ds}
}

func (b *Broker) ListProviders() []string {
	return b.providers.List()
}

func (b *Broker) Invoke(ctx context.Context, p *principal.Principal, providerName, operation string, params map[string]any) (*core.OperationResult, error) {
	prov, err := b.providers.Get(providerName)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return nil, fmt.Errorf("%w: %q", ErrProviderNotFound, providerName)
		}
		return nil, fmt.Errorf("%w: looking up provider: %v", ErrInternal, err)
	}

	ops := prov.ListOperations()
	found := false
	for _, op := range ops {
		if op.Name == operation {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("%w: %q on provider %q", ErrOperationNotFound, operation, providerName)
	}

	if p == nil {
		return nil, ErrNotAuthenticated
	}

	accessToken, err := b.resolveToken(ctx, prov, p, providerName)
	if err != nil {
		return nil, err
	}

	result, err := prov.Execute(ctx, operation, params, accessToken)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (b *Broker) resolveToken(ctx context.Context, prov core.Provider, p *principal.Principal, providerName string) (string, error) {
	mode := prov.ConnectionMode()
	switch mode {
	case core.ConnectionModeNone:
		return "", nil
	case core.ConnectionModeIdentity, core.ConnectionModeEither:
		return "", fmt.Errorf("%w: connection mode %q not yet implemented", ErrInternal, mode)
	case core.ConnectionModeUser, "":
	default:
		return "", fmt.Errorf("%w: unknown connection mode %q", ErrInternal, mode)
	}

	if p.UserID == "" {
		dbUser, err := b.datastore.FindOrCreateUser(ctx, p.Identity.Email)
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrUserResolution, err)
		}
		if dbUser == nil || dbUser.ID == "" {
			return "", fmt.Errorf("%w: no user record returned", ErrUserResolution)
		}
		p.UserID = dbUser.ID
	}

	storedToken, err := b.datastore.Token(ctx, p.UserID, providerName, "default")
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return "", fmt.Errorf("%w: no token stored for integration %q; connect via OAuth first", ErrNoToken, providerName)
		}
		return "", fmt.Errorf("%w: retrieving integration token: %v", ErrInternal, err)
	}
	if storedToken == nil {
		return "", fmt.Errorf("%w: no token stored for integration %q; connect via OAuth first", ErrNoToken, providerName)
	}

	return b.refreshTokenIfNeeded(ctx, prov, storedToken)
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
		fresh, fetchErr := b.datastore.Token(ctx, token.UserID, token.Integration, token.Instance)
		if fetchErr == nil && fresh != nil && fresh.AccessToken != token.AccessToken {
			return fresh.AccessToken, nil
		}
		token.RefreshErrorCount++
		token.UpdatedAt = time.Now()
		_ = b.datastore.StoreToken(ctx, token)
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

	if err := b.datastore.StoreToken(ctx, token); err != nil {
		return "", fmt.Errorf("persisting refreshed token: %w", err)
	}
	return token.AccessToken, nil
}
