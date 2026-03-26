package principal

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/core"
)

type TokenType int

const (
	TokenTypeAPI TokenType = iota
	TokenTypeEgressClient
)

const (
	prefixAPI          = "gst_api_"
	prefixEgressClient = "gst_ec_"
)

func GenerateToken(typ TokenType) (plaintext, hashed string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generating random bytes: %w", err)
	}
	raw := hex.EncodeToString(b)

	var prefix string
	switch typ {
	case TokenTypeAPI:
		prefix = prefixAPI
	case TokenTypeEgressClient:
		prefix = prefixEgressClient
	default:
		return "", "", fmt.Errorf("unknown token type %d", typ)
	}

	plaintext = prefix + raw
	hashed = HashToken(plaintext)
	return plaintext, hashed, nil
}

func ParseTokenType(token string) (TokenType, bool) {
	switch {
	case strings.HasPrefix(token, prefixAPI):
		return TokenTypeAPI, true
	case strings.HasPrefix(token, prefixEgressClient):
		return TokenTypeEgressClient, true
	default:
		return 0, false
	}
}

type ResolverOption func(*Resolver)

func WithEgressClientStore(ecs core.EgressClientStore) ResolverOption {
	return func(r *Resolver) { r.egressClients = ecs }
}

type Resolver struct {
	auth          core.AuthProvider
	datastore     core.Datastore
	egressClients core.EgressClientStore
}

func NewResolver(auth core.AuthProvider, ds core.Datastore, opts ...ResolverOption) *Resolver {
	r := &Resolver{auth: auth, datastore: ds}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func (r *Resolver) ResolveToken(ctx context.Context, token string) (*Principal, error) {
	if typ, ok := ParseTokenType(token); ok {
		switch typ {
		case TokenTypeAPI:
			return r.resolveAPIToken(ctx, token)
		case TokenTypeEgressClient:
			return r.resolveEgressClientToken(ctx, token)
		}
	}

	identity, err := r.auth.ValidateToken(ctx, token)
	if err == nil && identity != nil {
		return &Principal{Identity: identity, Source: SourceSession}, nil
	}

	return nil, ErrInvalidToken
}

func (r *Resolver) resolveAPIToken(ctx context.Context, token string) (*Principal, error) {
	hashed := HashToken(token)
	apiToken, err := r.datastore.ValidateAPIToken(ctx, hashed)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return nil, ErrInvalidToken
		}
		return nil, err
	}
	if apiToken == nil {
		return nil, ErrInvalidToken
	}

	user, err := r.datastore.GetUser(ctx, apiToken.UserID)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return nil, ErrInvalidToken
		}
		return nil, err
	}

	return &Principal{
		Identity: &core.UserIdentity{
			Email:       user.Email,
			DisplayName: user.DisplayName,
		},
		UserID: user.ID,
		Source: SourceAPIToken,
	}, nil
}

func (r *Resolver) resolveEgressClientToken(ctx context.Context, token string) (*Principal, error) {
	if r.egressClients == nil {
		return nil, ErrInvalidToken
	}

	hashed := HashToken(token)
	ecToken, err := r.egressClients.ValidateEgressClientToken(ctx, hashed)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return nil, ErrInvalidToken
		}
		return nil, err
	}
	if ecToken == nil {
		return nil, ErrInvalidToken
	}

	client, err := r.egressClients.GetEgressClient(ctx, ecToken.ClientID)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return nil, ErrInvalidToken
		}
		return nil, err
	}

	return &Principal{
		EgressClientID: client.ID,
		Source:         SourceEgressClient,
	}, nil
}

func (r *Resolver) ResolveEmail(email string) *Principal {
	return &Principal{
		Identity: &core.UserIdentity{Email: email},
		Source:   SourceEnv,
	}
}

func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
