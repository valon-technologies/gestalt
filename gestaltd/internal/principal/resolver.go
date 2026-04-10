package principal

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
)

type TokenType int

const (
	TokenTypeAPI TokenType = iota
)

const (
	prefixAPI = "gst_api_"
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
	default:
		return 0, false
	}
}

type Resolver struct {
	auth      core.AuthProvider
	users     *coredata.UserService
	apiTokens *coredata.APITokenService
}

func NewResolver(auth core.AuthProvider, users *coredata.UserService, apiTokens *coredata.APITokenService) *Resolver {
	return &Resolver{auth: auth, users: users, apiTokens: apiTokens}
}

func (r *Resolver) ResolveToken(ctx context.Context, token string) (*Principal, error) {
	if typ, ok := ParseTokenType(token); ok && typ == TokenTypeAPI {
		return r.resolveAPIToken(ctx, token)
	}

	startedAt := time.Now()
	provider := "none"
	if r.auth != nil {
		provider = r.auth.Name()
	}
	if r.auth == nil {
		metricutil.RecordAuthMetrics(ctx, startedAt, provider, "validate_token", true)
		return nil, ErrInvalidToken
	}

	identity, err := r.auth.ValidateToken(ctx, token)
	metricutil.RecordAuthMetrics(ctx, startedAt, provider, "validate_token", err != nil || identity == nil)
	if err == nil && identity != nil {
		return &Principal{Identity: identity, Source: SourceSession}, nil
	}

	return nil, ErrInvalidToken
}

func (r *Resolver) resolveAPIToken(ctx context.Context, token string) (*Principal, error) {
	hashed := HashToken(token)
	apiToken, err := r.apiTokens.ValidateAPIToken(ctx, hashed)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return nil, ErrInvalidToken
		}
		return nil, err
	}
	if apiToken == nil {
		return nil, ErrInvalidToken
	}

	user, err := r.users.GetUser(ctx, apiToken.UserID)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return nil, ErrInvalidToken
		}
		return nil, err
	}

	p := &Principal{
		Identity: &core.UserIdentity{
			Email:       user.Email,
			DisplayName: user.DisplayName,
		},
		UserID: user.ID,
		Source: SourceAPIToken,
	}
	if scopes := strings.Fields(apiToken.Scopes); len(scopes) > 0 {
		p.Scopes = scopes
	}
	return p, nil
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
