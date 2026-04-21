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
	TokenTypeWorkload
)

const (
	prefixAPI      = "gst_api_"
	prefixWorkload = "gst_wld_"
)

type ResolvedWorkload struct {
	ID          string
	DisplayName string
}

type WorkloadTokenResolver interface {
	ResolveWorkloadToken(token string) (*ResolvedWorkload, bool)
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
	case TokenTypeWorkload:
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
		return TokenTypeWorkload, true
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
	workloads         WorkloadTokenResolver
}

func NewResolver(auth core.AuthProvider, users *coredata.UserService, apiTokens *coredata.APITokenService, managedIdentities *coredata.ManagedIdentityService, identityGrants *coredata.ManagedIdentityGrantService, workloads WorkloadTokenResolver) *Resolver {
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
		case TokenTypeWorkload:
			return r.resolveWorkloadToken(token)
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
		UserID:              user.ID,
		SubjectID:           UserSubjectID(user.ID),
		CredentialSubjectID: apiToken.CredentialSubjectID,
		Kind:                KindUser,
		Source:              SourceAPIToken,
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
		SubjectID:           ManagedIdentitySubjectID(identity.ID),
		CredentialSubjectID: apiToken.CredentialSubjectID,
		DisplayName:         identity.DisplayName,
		Kind:                KindWorkload,
		Source:              SourceAPIToken,
		Scopes:              PermissionPlugins(effectivePerms),
		TokenPermissions:    effectivePerms,
	}, nil
}

func (r *Resolver) resolveWorkloadToken(token string) (*Principal, error) {
	if r.workloads == nil {
		return nil, ErrInvalidToken
	}
	workload, ok := r.workloads.ResolveWorkloadToken(token)
	if !ok || workload == nil || workload.ID == "" {
		return nil, ErrInvalidToken
	}
	return &Principal{
		Kind:        KindWorkload,
		SubjectID:   WorkloadSubjectID(workload.ID),
		DisplayName: workload.DisplayName,
		Source:      SourceWorkloadToken,
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
