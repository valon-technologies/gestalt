package invocation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/paraminterp"
	"github.com/valon-technologies/gestalt/internal/principal"
	"github.com/valon-technologies/gestalt/internal/registry"
)

const tokenRefreshThreshold = 5 * time.Minute

type Invoker interface {
	Invoke(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error)
}

type CapabilityLister interface {
	ListCapabilities() []core.Capability
}

var (
	_ Invoker          = (*Broker)(nil)
	_ CapabilityLister = (*Broker)(nil)
)

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

func (b *Broker) ListCapabilities() []core.Capability {
	if b == nil || b.providers == nil {
		return nil
	}

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

func (b *Broker) Invoke(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error) {
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

	ctx, accessToken, err := b.resolveToken(ctx, prov, p, providerName, instance)
	if err != nil {
		return nil, err
	}

	result, err := prov.Execute(ctx, operation, params, accessToken)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (b *Broker) ResolveToken(ctx context.Context, p *principal.Principal, providerName, instance string) (string, error) {
	prov, err := b.providers.Get(providerName)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return "", fmt.Errorf("%w: %q", ErrProviderNotFound, providerName)
		}
		return "", fmt.Errorf("%w: looking up provider: %v", ErrInternal, err)
	}
	_, tok, resolveErr := b.resolveToken(ctx, prov, p, providerName, instance)
	return tok, resolveErr
}

func (b *Broker) resolveToken(ctx context.Context, prov core.Provider, p *principal.Principal, providerName, instance string) (context.Context, string, error) {
	mode := prov.ConnectionMode()
	switch mode {
	case core.ConnectionModeNone:
		return ctx, "", nil

	case core.ConnectionModeUser, "":
		if p.UserID == "" {
			if p.Identity == nil || p.Identity.Email == "" {
				return ctx, "", fmt.Errorf("%w: principal has no user ID or email", ErrUserResolution)
			}
			dbUser, err := b.datastore.FindOrCreateUser(ctx, p.Identity.Email)
			if err != nil {
				return ctx, "", fmt.Errorf("%w: %v", ErrUserResolution, err)
			}
			if dbUser == nil || dbUser.ID == "" {
				return ctx, "", fmt.Errorf("%w: no user record returned", ErrUserResolution)
			}
			p.UserID = dbUser.ID
		}
		return b.resolveUserToken(ctx, prov, p.UserID, providerName, instance)

	case core.ConnectionModeIdentity:
		return b.resolveUserToken(ctx, prov, principal.IdentityPrincipal, providerName, instance)

	case core.ConnectionModeEither:
		if p.UserID != "" {
			enrichedCtx, tok, err := b.resolveUserToken(ctx, prov, p.UserID, providerName, instance)
			if err == nil {
				return enrichedCtx, tok, nil
			}
			if errors.Is(err, ErrAmbiguousInstance) {
				return ctx, "", err
			}
			if !errors.Is(err, ErrNoToken) {
				return ctx, "", err
			}
		}
		return b.resolveUserToken(ctx, prov, principal.IdentityPrincipal, providerName, instance)

	default:
		return ctx, "", fmt.Errorf("%w: unknown connection mode %q", ErrInternal, mode)
	}
}

func (b *Broker) resolveUserToken(ctx context.Context, prov core.Provider, userID, providerName, instance string) (context.Context, string, error) {
	var storedToken *core.IntegrationToken
	var err error

	if instance != "" {
		storedToken, err = b.datastore.Token(ctx, userID, providerName, instance)
		if err != nil {
			if errors.Is(err, core.ErrNotFound) {
				return ctx, "", fmt.Errorf("%w: no token stored for integration %q instance %q", ErrNoToken, providerName, instance)
			}
			return ctx, "", fmt.Errorf("%w: retrieving integration token: %v", ErrInternal, err)
		}
	} else {
		tokens, listErr := b.datastore.ListTokensForIntegration(ctx, userID, providerName)
		if listErr != nil {
			return ctx, "", fmt.Errorf("%w: listing tokens: %v", ErrInternal, listErr)
		}
		switch len(tokens) {
		case 0:
			return ctx, "", fmt.Errorf("%w: no token stored for integration %q", ErrNoToken, providerName)
		case 1:
			storedToken = tokens[0]
		default:
			instances := make([]string, len(tokens))
			for i, t := range tokens {
				instances[i] = t.Instance
			}
			return ctx, "", fmt.Errorf("%w: integration %q has %d connections (%v); specify which instance to use",
				ErrAmbiguousInstance, providerName, len(tokens), instances)
		}
	}

	if storedToken == nil {
		return ctx, "", fmt.Errorf("%w: no token stored for integration %q", ErrNoToken, providerName)
	}

	if storedToken.MetadataJSON != "" {
		var connParams map[string]string
		if err := json.Unmarshal([]byte(storedToken.MetadataJSON), &connParams); err != nil {
			log.Printf("WARNING: malformed MetadataJSON for %s: %v", providerName, err)
		} else if len(connParams) > 0 {
			ctx = core.WithConnectionParams(ctx, connParams)
		}
	}

	accessToken, err := b.refreshTokenIfNeeded(ctx, prov, storedToken)
	return ctx, accessToken, err
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

	resp, err := b.refreshOAuth(ctx, oauthProv, prov, token.RefreshToken)
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
	token.LastRefreshedAt = &now
	token.RefreshErrorCount = 0
	token.UpdatedAt = now

	if err := b.datastore.StoreToken(ctx, token); err != nil {
		return "", fmt.Errorf("persisting refreshed token: %w", err)
	}
	return token.AccessToken, nil
}

func (b *Broker) refreshOAuth(ctx context.Context, oauthProv core.OAuthProvider, prov core.Provider, refreshToken string) (*core.TokenResponse, error) {
	type refreshWithURL interface {
		RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error)
	}
	type tokenURLer interface{ TokenURL() string }

	if cp := core.ConnectionParams(ctx); cp != nil {
		if tu, ok := prov.(tokenURLer); ok {
			raw := tu.TokenURL()
			resolved := paraminterp.Interpolate(raw, cp)
			if resolved != raw {
				if rw, ok := prov.(refreshWithURL); ok {
					return rw.RefreshTokenWithURL(ctx, refreshToken, resolved)
				}
			}
		}
	}
	return oauthProv.RefreshToken(ctx, refreshToken)
}
