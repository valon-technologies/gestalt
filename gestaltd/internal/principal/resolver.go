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
	TokenTypeIdentity
)

const TokenTypeWorkload TokenType = TokenTypeIdentity

const (
	prefixAPI      = "gst_api_"
	prefixWorkload = "gst_wld_"
)

type ResolvedIdentityToken struct {
	ID          string
	DisplayName string
}

type IdentityTokenResolver interface {
	ResolveIdentityToken(token string) (*ResolvedIdentityToken, bool)
}

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
	case TokenTypeIdentity:
		prefix = prefixWorkload
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
	case strings.HasPrefix(token, prefixWorkload):
		return TokenTypeIdentity, true
	default:
		return 0, false
	}
}

type Resolver struct {
	auth              core.AuthProvider
	users             *coredata.UserService
	apiTokens         *coredata.APITokenService
	managedIdentities *coredata.ManagedIdentityService
	identityGrants    *coredata.ManagedIdentityGrantService
	workloads         IdentityTokenResolver
}

func NewResolver(auth core.AuthProvider, users *coredata.UserService, apiTokens *coredata.APITokenService, managedIdentities *coredata.ManagedIdentityService, identityGrants *coredata.ManagedIdentityGrantService, workloads IdentityTokenResolver) *Resolver {
	return &Resolver{
		auth:              auth,
		users:             users,
		apiTokens:         apiTokens,
		managedIdentities: managedIdentities,
		identityGrants:    identityGrants,
		workloads:         workloads,
	}
}

func (r *Resolver) ResolveToken(ctx context.Context, token string) (*Principal, error) {
	if typ, ok := ParseTokenType(token); ok {
		switch typ {
		case TokenTypeAPI:
			return r.resolveAPIToken(ctx, token)
		case TokenTypeIdentity:
			return r.resolveIdentityToken(token)
		}
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

	switch ownerKind := r.apiTokenOwnerKind(apiToken); ownerKind {
	case core.APITokenOwnerKindManagedIdentity:
		return r.resolveManagedIdentityAPIToken(ctx, apiToken)
	case "", core.APITokenOwnerKindUser:
	default:
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
		UserID:    user.ID,
		SubjectID: UserSubjectID(user.ID),
		Kind:      KindUser,
		Source:    SourceAPIToken,
	}
	if perms := permissionsForAPIToken(apiToken); perms != nil {
		p.TokenPermissions = perms
		p.Scopes = PermissionPlugins(perms)
	}
	return p, nil
}

func (r *Resolver) resolveManagedIdentityAPIToken(ctx context.Context, apiToken *core.APIToken) (*Principal, error) {
	if r.managedIdentities == nil || r.identityGrants == nil {
		return nil, ErrInvalidToken
	}
	identityID := strings.TrimSpace(apiToken.OwnerID)
	if identityID == "" {
		return nil, ErrInvalidToken
	}

	identity, err := r.managedIdentities.GetIdentity(ctx, identityID)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return nil, ErrInvalidToken
		}
		return nil, err
	}
	grants, err := r.identityGrants.ListGrantsByIdentity(ctx, identityID)
	if err != nil {
		return nil, err
	}

	tokenPerms := permissionsForAPIToken(apiToken)
	if tokenPerms == nil {
		tokenPerms = PermissionSet{}
	}
	grantPerms := CompileManagedIdentityGrants(grants)
	effectivePerms := IntersectPermissions(tokenPerms, grantPerms)
	if effectivePerms == nil {
		effectivePerms = PermissionSet{}
	}

	return &Principal{
		SubjectID:        IdentitySubjectID(identity.ID),
		DisplayName:      identity.DisplayName,
		Kind:             KindIdentity,
		Source:           SourceAPIToken,
		Scopes:           PermissionPlugins(effectivePerms),
		TokenPermissions: effectivePerms,
	}, nil
}

func (r *Resolver) resolveIdentityToken(token string) (*Principal, error) {
	if r.workloads == nil {
		return nil, ErrInvalidToken
	}
	identity, ok := r.workloads.ResolveIdentityToken(token)
	if !ok || identity == nil || identity.ID == "" {
		return nil, ErrInvalidToken
	}
	return &Principal{
		Kind:        KindIdentity,
		SubjectID:   IdentitySubjectID(identity.ID),
		DisplayName: identity.DisplayName,
		Source:      SourceIdentityToken,
	}, nil
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

func permissionsForAPIToken(apiToken *core.APIToken) PermissionSet {
	if apiToken == nil {
		return nil
	}
	if perms := CompilePermissions(apiToken.Permissions); perms != nil {
		return perms
	}
	return PermissionsFromScopeString(apiToken.Scopes)
}

func (r *Resolver) apiTokenOwnerKind(apiToken *core.APIToken) string {
	if apiToken == nil {
		return ""
	}
	if ownerKind := strings.TrimSpace(apiToken.OwnerKind); ownerKind != "" {
		return ownerKind
	}
	if apiToken.UserID != "" {
		return core.APITokenOwnerKindUser
	}
	return ""
}
