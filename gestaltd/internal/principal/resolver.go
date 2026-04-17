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
	auth           core.AuthProvider
	users          *coredata.UserService
	identities     *coredata.IdentityService
	identityAccess *coredata.IdentityPluginAccessService
	apiTokens      *coredata.APITokenService
	workloads      WorkloadTokenResolver
}

func NewResolver(auth core.AuthProvider, users *coredata.UserService, identities *coredata.IdentityService, identityAccess *coredata.IdentityPluginAccessService, apiTokens *coredata.APITokenService, workloads WorkloadTokenResolver) *Resolver {
	return &Resolver{
		auth:           auth,
		users:          users,
		identities:     identities,
		identityAccess: identityAccess,
		apiTokens:      apiTokens,
		workloads:      workloads,
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
		return r.resolveSessionPrincipal(ctx, identity)
	}

	return nil, ErrInvalidToken
}

func (r *Resolver) resolveSessionPrincipal(ctx context.Context, identity *core.UserIdentity) (*Principal, error) {
	p := &Principal{Identity: identity, Source: SourceSession, Kind: KindUser}
	if identity == nil || strings.TrimSpace(identity.Email) == "" || r.users == nil {
		return p, nil
	}
	user := r.lookupSessionUser(ctx, identity.Email)
	if user == nil || user.ID == "" {
		return p, nil
	}
	if identityID, err := r.users.CanonicalIdentityIDForUser(ctx, user.ID); err == nil {
		p.IdentityID = identityID
	}
	p.UserID = user.ID
	p.SubjectID = UserSubjectID(user.ID)
	if p.Identity != nil && p.Identity.DisplayName == "" {
		p.Identity.DisplayName = user.DisplayName
	}
	return p, nil
}

func (r *Resolver) lookupSessionUser(ctx context.Context, email string) *core.User {
	if r == nil || r.users == nil {
		return nil
	}
	user, err := r.users.FindOrCreateUser(ctx, email)
	if err != nil {
		return nil
	}
	return user
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
		return r.resolveIdentityAPIToken(ctx, apiToken)
	case "", core.APITokenOwnerKindUser:
	default:
		return nil, ErrInvalidToken
	}

	identityID, err := r.identityIDForAPIToken(ctx, apiToken)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return nil, ErrInvalidToken
		}
		return nil, err
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
		IdentityID: identityID,
		UserID:     user.ID,
		SubjectID:  UserSubjectID(user.ID),
		Kind:       KindUser,
		Source:     SourceAPIToken,
	}
	if perms := permissionsForAPIToken(apiToken); perms != nil {
		p.TokenPermissions = perms
		p.Scopes = PermissionPlugins(perms)
	}
	return p, nil
}

func (r *Resolver) resolveIdentityAPIToken(ctx context.Context, apiToken *core.APIToken) (*Principal, error) {
	if r.identities == nil {
		return nil, ErrInvalidToken
	}
	identityID, err := r.identityIDForAPIToken(ctx, apiToken)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return nil, ErrInvalidToken
		}
		return nil, err
	}
	if identityID == "" {
		return nil, ErrInvalidToken
	}

	identity, err := r.identities.GetIdentity(ctx, identityID)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return nil, ErrInvalidToken
		}
		return nil, err
	}

	identityPerms := PermissionSet{}
	if r.identityAccess != nil {
		access, err := r.identityAccess.ListByIdentity(ctx, identityID)
		if err != nil {
			return nil, err
		}
		identityPerms = CompileIdentityPluginAccess(access)
	}
	if identityPerms == nil {
		identityPerms = PermissionSet{}
	}

	effectivePerms := identityPerms
	if tokenPerms := permissionsForAPIToken(apiToken); tokenPerms != nil {
		effectivePerms = IntersectPermissions(identityPerms, tokenPerms)
		if effectivePerms == nil {
			effectivePerms = PermissionSet{}
		}
	}

	return &Principal{
		IdentityID:       identity.ID,
		SubjectID:        ManagedIdentitySubjectID(identity.ID),
		DisplayName:      identity.DisplayName,
		Kind:             KindServiceAccount,
		Source:           SourceAPIToken,
		Scopes:           PermissionPlugins(effectivePerms),
		TokenPermissions: effectivePerms,
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
	if strings.TrimSpace(apiToken.UserID) != "" {
		return core.APITokenOwnerKindUser
	}
	if strings.TrimSpace(apiToken.IdentityID) != "" || strings.TrimSpace(apiToken.OwnerID) != "" {
		return core.APITokenOwnerKindManagedIdentity
	}
	return ""
}

func (r *Resolver) identityIDForAPIToken(ctx context.Context, apiToken *core.APIToken) (string, error) {
	if apiToken == nil {
		return "", ErrInvalidToken
	}
	if identityID := strings.TrimSpace(apiToken.IdentityID); identityID != "" {
		return identityID, nil
	}
	if userID := strings.TrimSpace(apiToken.UserID); userID != "" {
		if r.users == nil {
			return userID, nil
		}
		return r.users.CanonicalIdentityIDForUser(ctx, userID)
	}
	if ownerID := strings.TrimSpace(apiToken.OwnerID); ownerID != "" {
		return ownerID, nil
	}
	return "", ErrInvalidToken
}
