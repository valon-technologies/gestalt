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
	auth      core.AuthenticationProvider
	users     *coredata.UserService
	apiTokens *coredata.APITokenService
}

func NewResolver(auth core.AuthenticationProvider, users *coredata.UserService, apiTokens *coredata.APITokenService) *Resolver {
	return &Resolver{
		auth:      auth,
		users:     users,
		apiTokens: apiTokens,
	}
}

func (r *Resolver) ResolveToken(ctx context.Context, token string) (*Principal, error) {
	if typ, ok := ParseTokenType(token); ok {
		if typ == TokenTypeAPI {
			return r.resolveAPIToken(ctx, token)
		}
		return nil, ErrInvalidToken
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
	if r.apiTokens == nil {
		return nil, ErrInvalidToken
	}
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

	switch ownerKind := r.apiTokenOwnerKind(apiToken); ownerKind {
	case core.APITokenOwnerKindUser:
		return r.resolveUserAPIToken(ctx, apiToken)
	case core.APITokenOwnerKindSubject:
		return resolveSubjectAPIToken(apiToken)
	default:
		return nil, ErrInvalidToken
	}
}

func (r *Resolver) resolveUserAPIToken(ctx context.Context, apiToken *core.APIToken) (*Principal, error) {
	ownerID := strings.TrimSpace(apiToken.OwnerID)
	if ownerID == "" {
		return nil, ErrInvalidToken
	}
	if r.users == nil {
		return nil, ErrInvalidToken
	}

	user, err := r.users.GetUser(ctx, ownerID)
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
		UserID:              user.ID,
		SubjectID:           UserSubjectID(user.ID),
		CredentialSubjectID: apiToken.CredentialSubjectID,
		Kind:                KindUser,
		Source:              SourceAPIToken,
	}
	if perms, actionPerms, ok := permissionsForAPIToken(apiToken); ok {
		p.TokenPermissions = perms
		p.ActionPermissions = actionPerms
		p.Scopes = PermissionPlugins(perms)
	}
	return p, nil
}

func resolveSubjectAPIToken(apiToken *core.APIToken) (*Principal, error) {
	subjectID := strings.TrimSpace(apiToken.OwnerID)
	if subjectID == "" || UserIDFromSubjectID(subjectID) != "" || IsSystemSubjectID(subjectID) {
		return nil, ErrInvalidToken
	}
	kind := KindFromSubjectID(subjectID)
	if kind == "" {
		return nil, ErrInvalidToken
	}
	credentialSubjectID := strings.TrimSpace(apiToken.CredentialSubjectID)
	if credentialSubjectID == "" {
		credentialSubjectID = subjectID
	}
	if credentialSubjectID != subjectID {
		return nil, ErrInvalidToken
	}
	p := &Principal{
		Kind:                kind,
		SubjectID:           subjectID,
		CredentialSubjectID: credentialSubjectID,
		DisplayName:         strings.TrimSpace(apiToken.Name),
		Source:              SourceAPIToken,
	}
	if perms, actionPerms, ok := permissionsForAPIToken(apiToken); ok {
		p.TokenPermissions = perms
		p.ActionPermissions = actionPerms
		p.Scopes = PermissionPlugins(perms)
	}
	return Canonicalize(p), nil
}

func (r *Resolver) ResolveEmail(email string) *Principal {
	return &Principal{
		Identity: &core.UserIdentity{Email: email},
		Kind:     KindUser,
		Source:   SourceEnv,
	}
}

func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func permissionsForAPIToken(apiToken *core.APIToken) (PermissionSet, ActionPermissionSet, bool) {
	if apiToken == nil {
		return nil, nil, false
	}
	if len(apiToken.Permissions) > 0 {
		return CompilePermissions(apiToken.Permissions), CompileActionPermissions(apiToken.Permissions), true
	}
	if perms := PermissionsFromScopeString(apiToken.Scopes); perms != nil {
		return perms, nil, true
	}
	return nil, nil, false
}

func (r *Resolver) apiTokenOwnerKind(apiToken *core.APIToken) string {
	if apiToken == nil {
		return ""
	}
	return strings.TrimSpace(apiToken.OwnerKind)
}
