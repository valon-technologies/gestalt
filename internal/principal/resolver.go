package principal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/valon-technologies/gestalt/core"
)

type Resolver struct {
	auth      core.AuthProvider
	datastore core.Datastore
}

func NewResolver(auth core.AuthProvider, ds core.Datastore) *Resolver {
	return &Resolver{auth: auth, datastore: ds}
}

func (r *Resolver) ResolveToken(ctx context.Context, token string) (*Principal, error) {
	identity, err := r.auth.ValidateToken(ctx, token)
	if err == nil && identity != nil {
		return &Principal{Identity: identity, Source: SourceSession}, nil
	}

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
