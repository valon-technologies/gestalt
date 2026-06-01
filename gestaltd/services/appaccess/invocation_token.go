package appaccess

import (
	"context"
	"crypto/rand"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowgrants"
)

const (
	invocationTokenIssuer          = "gestaltd"
	invocationTokenAudience        = "gestalt-provider-host"
	defaultRootInvocationTokenTTL  = 15 * time.Minute
	defaultChildInvocationTokenTTL = 10 * time.Minute
	maxChildInvocationTokenTTL     = 15 * time.Minute
)

type InvocationTokenManager struct {
	secret          []byte
	now             func() time.Time
	rootTTL         time.Duration
	defaultChildTTL time.Duration
	maxChildTTL     time.Duration
}

type invocationTokenClaims struct {
	jwt.RegisteredClaims
	DelegationExpiresAt *jwt.NumericDate                 `json:"delegation_expires_at,omitempty"`
	CallerApp           string                           `json:"caller_app,omitempty"`
	CallerProviderKind  string                           `json:"caller_provider_kind,omitempty"`
	SubjectKind         string                           `json:"subject_kind,omitempty"`
	Email               string                           `json:"email,omitempty"`
	DisplayName         string                           `json:"display_name,omitempty"`
	AuthSource          string                           `json:"auth_source,omitempty"`
	CredentialSubjectID string                           `json:"credential_subject_id,omitempty"`
	TokenPermissions    map[string][]string              `json:"token_permissions,omitempty"`
	Grants              map[string]invocationGrantClaims `json:"grants,omitempty"`
	WorkflowGrants      *workflowGrantClaims             `json:"workflow_grants,omitempty"`
	Workflow            map[string]any                   `json:"workflow,omitempty"`
	RequestMeta         requestMetaClaims                `json:"request_meta,omitempty"`
	Credential          credentialClaims                 `json:"credential,omitempty"`
	Invocation          invocationClaims                 `json:"invocation,omitempty"`
	Connection          string                           `json:"connection,omitempty"`
}

type requestMetaClaims struct {
	ClientIP   string `json:"client_ip,omitempty"`
	RemoteAddr string `json:"remote_addr,omitempty"`
	UserAgent  string `json:"user_agent,omitempty"`
}

type credentialClaims struct {
	Mode       string `json:"mode,omitempty"`
	SubjectID  string `json:"subject_id,omitempty"`
	Connection string `json:"connection,omitempty"`
	Instance   string `json:"instance,omitempty"`
}

type invocationClaims struct {
	RequestID                string   `json:"request_id,omitempty"`
	Depth                    int      `json:"depth,omitempty"`
	CallChain                []string `json:"call_chain,omitempty"`
	Surface                  string   `json:"surface,omitempty"`
	InternalConnectionAccess bool     `json:"internal_connection_access,omitempty"`
}

type workflowGrantClaims struct {
	Operations []string `json:"operations,omitempty"`
}

type invocationTokenContext struct {
	token                  string
	callerApp              string
	callerProviderKind     invocation.ProviderKind
	principal              *principal.Principal
	requestMeta            invocation.RequestMeta
	credential             invocation.CredentialContext
	credentialModeOverride core.ConnectionMode
	operationProfile       OperationProfile
	invocation             *invocation.InvocationMeta
	surface                invocation.InvocationSurface
	internalConnection     bool
	connection             string
	grants                 InvocationGrants
	workflowGrants         workflowgrants.Grants
	workflow               map[string]any
}

func NewInvocationTokenManager(secret []byte) (*InvocationTokenManager, error) {
	if len(secret) == 0 {
		secret = make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return nil, fmt.Errorf("generate invocation token secret: %w", err)
		}
	}
	return &InvocationTokenManager{
		secret:          append([]byte(nil), secret...),
		now:             time.Now,
		rootTTL:         defaultRootInvocationTokenTTL,
		defaultChildTTL: defaultChildInvocationTokenTTL,
		maxChildTTL:     maxChildInvocationTokenTTL,
	}, nil
}

func (m *InvocationTokenManager) MintRootToken(ctx context.Context, appName string, grants InvocationGrants) (string, error) {
	return m.MintRootTokenWithWorkflowGrants(ctx, appName, grants, nil)
}

func (m *InvocationTokenManager) MintRootTokenWithWorkflowGrants(ctx context.Context, appName string, grants InvocationGrants, workflowGrants workflowgrants.Grants) (string, error) {
	if m == nil {
		return "", fmt.Errorf("invocation tokens are not available")
	}
	now := m.now()
	expiresAt := now.Add(m.rootTTL)
	return m.signClaims(claimsFromContext(ctx, appName, grants, workflowGrants, now, expiresAt, m.delegationExpiry(expiresAt, now)))
}

func (m *InvocationTokenManager) ExchangeToken(parentToken string, grants InvocationGrants, ttl time.Duration) (string, error) {
	if m == nil {
		return "", fmt.Errorf("invocation tokens are not available")
	}
	claims, err := m.parseClaims(parentToken)
	if err != nil {
		return "", err
	}
	parentGrants := decodeInvocationGrantClaims(claims.Grants)
	preparedGrants, ok := prepareChildInvocationGrants(grants, parentGrants)
	if !ok {
		return "", fmt.Errorf("requested invocation grants exceed the parent token")
	}
	grants = preparedGrants
	child := *claims
	child.ID = uuid.NewString()
	now := m.now()
	delegationExpiresAt, err := m.delegationExpiresAt(claims, now)
	if err != nil {
		return "", err
	}
	child.IssuedAt = jwt.NewNumericDate(now)
	child.NotBefore = jwt.NewNumericDate(now)
	child.ExpiresAt = jwt.NewNumericDate(minTime(now.Add(m.childTTL(ttl)), delegationExpiresAt))
	child.DelegationExpiresAt = jwt.NewNumericDate(delegationExpiresAt)
	child.Grants = encodeInvocationGrantClaims(grants)
	return m.signClaims(&child)
}

func (m *InvocationTokenManager) resolveToken(token, appName string) (invocationTokenContext, error) {
	if m == nil {
		return invocationTokenContext{}, fmt.Errorf("invocation tokens are not available")
	}
	claims, err := m.parseClaims(token)
	if err != nil {
		return invocationTokenContext{}, err
	}
	if strings.TrimSpace(appName) != "" && strings.TrimSpace(claims.CallerApp) != strings.TrimSpace(appName) {
		return invocationTokenContext{}, fmt.Errorf("app invocation token is not valid for %q", appName)
	}
	return invocationTokenContext{
		token:     strings.TrimSpace(token),
		callerApp: strings.TrimSpace(claims.CallerApp),
		callerProviderKind: invocation.ProviderKind(
			strings.TrimSpace(claims.CallerProviderKind),
		),
		principal: principalFromInvocationClaims(claims),
		requestMeta: invocation.RequestMeta{
			ClientIP:   claims.RequestMeta.ClientIP,
			RemoteAddr: claims.RequestMeta.RemoteAddr,
			UserAgent:  claims.RequestMeta.UserAgent,
		},
		credential: invocation.CredentialContext{
			Mode:       core.NormalizeOptionalConnectionMode(core.ConnectionMode(claims.Credential.Mode)),
			SubjectID:  claims.Credential.SubjectID,
			Connection: claims.Credential.Connection,
			Instance:   claims.Credential.Instance,
		},
		invocation: &invocation.InvocationMeta{
			RequestID: claims.Invocation.RequestID,
			Depth:     claims.Invocation.Depth,
			CallChain: append([]string(nil), claims.Invocation.CallChain...),
		},
		surface:            invocation.InvocationSurface(strings.TrimSpace(claims.Invocation.Surface)),
		internalConnection: claims.Invocation.InternalConnectionAccess,
		connection:         strings.TrimSpace(claims.Connection),
		grants:             decodeInvocationGrantClaims(claims.Grants),
		workflowGrants:     decodeWorkflowGrantClaims(claims.WorkflowGrants),
		workflow:           invocation.CloneWorkflowContext(claims.Workflow),
	}, nil
}

// TokenContext is the invocation context recovered from an app invocation token.
// It is intentionally opaque outside this package; manager host services use it
// to restore caller identity and request metadata without re-parsing tokens.
type TokenContext struct {
	inner invocationTokenContext
}

func (m *InvocationTokenManager) ResolveToken(token, appName string) (TokenContext, error) {
	tokenCtx, err := m.resolveToken(token, appName)
	if err != nil {
		return TokenContext{}, err
	}
	return TokenContext{inner: tokenCtx}, nil
}

func (c TokenContext) CallerApp() string {
	return strings.TrimSpace(c.inner.callerApp)
}

func (c TokenContext) Principal() *principal.Principal {
	return principal.Canonicalized(c.inner.principal)
}

func (c TokenContext) AllowsWorkflowManagerOperation(operation string) bool {
	return c.inner.workflowGrants.Allows(operation)
}

func (m *InvocationTokenManager) parseClaims(token string) (*invocationTokenClaims, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("invocation token is required")
	}
	parsed, err := jwt.ParseWithClaims(token, &invocationTokenClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.secret, nil
	}, jwt.WithAudience(invocationTokenAudience), jwt.WithIssuer(invocationTokenIssuer), jwt.WithTimeFunc(m.now))
	if err != nil {
		return nil, fmt.Errorf("invocation token is invalid or expired")
	}
	claims, ok := parsed.Claims.(*invocationTokenClaims)
	if !ok || !parsed.Valid {
		return nil, fmt.Errorf("invocation token is invalid or expired")
	}
	return claims, nil
}

func (m *InvocationTokenManager) signClaims(claims *invocationTokenClaims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(m.secret)
}

func (m *InvocationTokenManager) childTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return m.defaultChildTTL
	}
	if ttl > m.maxChildTTL {
		return m.maxChildTTL
	}
	return ttl
}

func (m *InvocationTokenManager) delegationExpiry(rootExpiresAt, now time.Time) time.Time {
	expiresAt := now.Add(m.maxChildTTL)
	if expiresAt.Before(rootExpiresAt) {
		return rootExpiresAt
	}
	return expiresAt
}

func (m *InvocationTokenManager) delegationExpiresAt(claims *invocationTokenClaims, now time.Time) (time.Time, error) {
	if claims != nil && claims.DelegationExpiresAt != nil {
		expiresAt := claims.DelegationExpiresAt.Time
		if !expiresAt.After(now) {
			return time.Time{}, fmt.Errorf("invocation token is invalid or expired")
		}
		return expiresAt, nil
	}
	if claims == nil || claims.ExpiresAt == nil {
		return time.Time{}, fmt.Errorf("invocation token is invalid or expired")
	}
	expiresAt := claims.ExpiresAt.Time
	if !expiresAt.After(now) {
		return time.Time{}, fmt.Errorf("invocation token is invalid or expired")
	}
	return expiresAt, nil
}

func claimsFromContext(ctx context.Context, appName string, grants InvocationGrants, workflowGrants workflowgrants.Grants, now, expiresAt, delegationExpiresAt time.Time) *invocationTokenClaims {
	p := principal.FromContext(ctx)
	meta := invocation.MetaFromContext(ctx)
	if meta == nil {
		meta = &invocation.InvocationMeta{RequestID: uuid.NewString()}
	}
	cred := invocation.CredentialContextFromContext(ctx)
	reqMeta := invocation.RequestMetaFromContext(ctx)
	appName = strings.TrimSpace(appName)
	caller := invocation.CallerProviderFromContext(ctx)
	callerKind := invocation.ProviderKind("")
	if caller.Name == appName && caller.Kind != "" {
		callerKind = caller.Kind
	}
	return &invocationTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.NewString(),
			Issuer:    invocationTokenIssuer,
			Subject:   subjectIDForInvocationClaims(p),
			Audience:  jwt.ClaimStrings{invocationTokenAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
		DelegationExpiresAt: jwt.NewNumericDate(delegationExpiresAt),
		CallerApp:           appName,
		CallerProviderKind:  string(callerKind),
		SubjectKind:         string(subjectKindForInvocationClaims(p)),
		Email:               emailForInvocationClaims(p),
		DisplayName:         displayNameForInvocationClaims(p),
		AuthSource:          authSourceForInvocationClaims(p),
		CredentialSubjectID: credentialSubjectIDForInvocationClaims(p),
		TokenPermissions:    encodePermissionSet(tokenPermissionsForInvocationClaims(p)),
		Grants:              encodeInvocationGrantClaims(grants),
		WorkflowGrants:      encodeWorkflowGrantClaims(workflowGrants),
		Workflow:            invocation.CloneWorkflowContext(invocation.WorkflowContextFromContext(ctx)),
		RequestMeta: requestMetaClaims{
			ClientIP:   reqMeta.ClientIP,
			RemoteAddr: reqMeta.RemoteAddr,
			UserAgent:  reqMeta.UserAgent,
		},
		Credential: credentialClaims{
			Mode:       string(cred.Mode),
			SubjectID:  cred.SubjectID,
			Connection: cred.Connection,
			Instance:   cred.Instance,
		},
		Invocation: invocationClaims{
			RequestID:                meta.RequestID,
			Depth:                    meta.Depth,
			CallChain:                append([]string(nil), meta.CallChain...),
			Surface:                  string(invocation.InvocationSurfaceFromContext(ctx)),
			InternalConnectionAccess: invocation.InternalConnectionAccessFromContext(ctx),
		},
		Connection: invocation.ConnectionFromContext(ctx),
	}
}

func encodeWorkflowGrantClaims(src workflowgrants.Grants) *workflowGrantClaims {
	if src == nil {
		return nil
	}
	return &workflowGrantClaims{Operations: workflowgrants.EncodeClaims(src)}
}

func decodeWorkflowGrantClaims(src *workflowGrantClaims) workflowgrants.Grants {
	if src == nil {
		return nil
	}
	grants := workflowgrants.DecodeClaims(src.Operations)
	if grants == nil {
		return workflowgrants.Grants{}
	}
	return grants
}

func restoreInvocationTokenContext(ctx context.Context, tokenCtx invocationTokenContext, connectionOverride string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if tokenCtx.principal != nil {
		ctx = principal.WithPrincipal(ctx, principal.Canonicalized(tokenCtx.principal))
	}
	if callerApp := strings.TrimSpace(tokenCtx.callerApp); callerApp != "" && tokenCtx.callerProviderKind != "" {
		ctx = invocation.WithCallerProvider(ctx, tokenCtx.callerProviderKind, callerApp)
	}
	if tokenCtx.invocation != nil {
		ctx = invocation.ContextWithMeta(ctx, &invocation.InvocationMeta{
			RequestID: tokenCtx.invocation.RequestID,
			Depth:     tokenCtx.invocation.Depth,
			CallChain: append([]string(nil), tokenCtx.invocation.CallChain...),
		})
	}
	if tokenCtx.requestMeta != (invocation.RequestMeta{}) {
		ctx = invocation.WithRequestMeta(ctx, tokenCtx.requestMeta)
	}
	if tokenCtx.credential != (invocation.CredentialContext{}) {
		ctx = invocation.WithCredentialContext(ctx, tokenCtx.credential)
	}
	if tokenCtx.credentialModeOverride != "" {
		ctx = invocation.WithCredentialModeOverride(ctx, tokenCtx.credentialModeOverride)
	}
	if tokenCtx.surface != "" {
		ctx = invocation.WithInvocationSurface(ctx, tokenCtx.surface)
	}
	if tokenCtx.internalConnection {
		ctx = invocation.WithInternalConnectionAccess(ctx)
	}
	if tokenCtx.workflow != nil {
		ctx = invocation.WithWorkflowContext(ctx, invocation.CloneWorkflowContext(tokenCtx.workflow))
	}
	if token := strings.TrimSpace(tokenCtx.token); token != "" {
		ctx = WithInvocationToken(ctx, token)
	}

	connection := strings.TrimSpace(connectionOverride)
	if connection == "" {
		connection = strings.TrimSpace(tokenCtx.connection)
	}
	if connection == "" {
		connection = strings.TrimSpace(tokenCtx.credential.Connection)
	}
	if connection != "" {
		ctx = invocation.WithConnection(ctx, connection)
	}
	return ctx
}

func RestoreTokenContext(ctx context.Context, tokenCtx TokenContext, connectionOverride string) context.Context {
	return restoreInvocationTokenContext(ctx, tokenCtx.inner, connectionOverride)
}

func subjectIDForInvocationClaims(p *principal.Principal) string {
	p = principal.Canonicalized(p)
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.SubjectID)
}

func subjectKindForInvocationClaims(p *principal.Principal) principal.Kind {
	p = principal.Canonicalized(p)
	if p == nil {
		return ""
	}
	if p.Kind != "" {
		return p.Kind
	}
	if p.Identity != nil {
		return principal.KindUser
	}
	return ""
}

func displayNameForInvocationClaims(p *principal.Principal) string {
	if p == nil {
		return ""
	}
	if p.Identity != nil && strings.TrimSpace(p.Identity.DisplayName) != "" {
		return strings.TrimSpace(p.Identity.DisplayName)
	}
	return strings.TrimSpace(p.DisplayName)
}

func emailForInvocationClaims(p *principal.Principal) string {
	if p == nil || p.Identity == nil {
		return ""
	}
	return strings.TrimSpace(p.Identity.Email)
}

func authSourceForInvocationClaims(p *principal.Principal) string {
	if p == nil {
		return ""
	}
	return p.AuthSource()
}

func credentialSubjectIDForInvocationClaims(p *principal.Principal) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.CredentialSubjectID)
}

func tokenPermissionsForInvocationClaims(p *principal.Principal) principal.PermissionSet {
	if p == nil {
		return nil
	}
	if p.TokenPermissions == nil {
		if len(p.Scopes) == 0 {
			return nil
		}
		return principal.PermissionsFromScopeString(strings.Join(p.Scopes, " "))
	}
	out := make(principal.PermissionSet, len(p.TokenPermissions))
	for plugin, ops := range p.TokenPermissions {
		if ops == nil {
			out[plugin] = nil
			continue
		}
		out[plugin] = maps.Clone(ops)
	}
	return out
}

func principalFromInvocationClaims(claims *invocationTokenClaims) *principal.Principal {
	if claims == nil {
		return nil
	}
	tokenPerms := decodePermissionSet(claims.TokenPermissions)
	p := &principal.Principal{
		SubjectID:           strings.TrimSpace(claims.Subject),
		CredentialSubjectID: strings.TrimSpace(claims.CredentialSubjectID),
		DisplayName:         strings.TrimSpace(claims.DisplayName),
		Kind:                principal.Kind(strings.TrimSpace(claims.SubjectKind)),
		TokenPermissions:    tokenPerms,
	}
	principal.SetAuthSource(p, claims.AuthSource)
	if strings.TrimSpace(claims.Email) != "" || strings.TrimSpace(claims.DisplayName) != "" {
		p.Identity = &core.UserIdentity{
			Email:       strings.TrimSpace(claims.Email),
			DisplayName: strings.TrimSpace(claims.DisplayName),
		}
	}
	if p.TokenPermissions != nil {
		p.Scopes = principal.PermissionApps(p.TokenPermissions)
	}
	principal.Canonicalize(p)
	if p.SubjectID == "" && p.UserID == "" && p.Kind == "" && p.DisplayName == "" && p.CredentialSubjectID == "" && p.TokenPermissions == nil && p.Source == principal.SourceUnknown && p.AuthSourceOverride == "" {
		return nil
	}
	return p
}

func encodePermissionSet(src principal.PermissionSet) map[string][]string {
	if src == nil {
		return nil
	}
	out := make(map[string][]string, len(src))
	for plugin, ops := range src {
		if ops == nil {
			out[plugin] = nil
			continue
		}
		out[plugin] = slices.Sorted(maps.Keys(ops))
	}
	return out
}

func decodePermissionSet(src map[string][]string) principal.PermissionSet {
	if src == nil {
		return nil
	}
	out := make(principal.PermissionSet, len(src))
	for plugin, ops := range src {
		if ops == nil {
			out[plugin] = nil
			continue
		}
		decoded := make(map[string]struct{}, len(ops))
		for _, op := range ops {
			op = strings.TrimSpace(op)
			if op == "" {
				continue
			}
			decoded[op] = struct{}{}
		}
		out[plugin] = decoded
	}
	return out
}

func minTime(a, b time.Time) time.Time {
	if a.After(b) {
		return b
	}
	return a
}
