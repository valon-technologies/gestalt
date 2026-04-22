package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

const (
	auditTargetKindAPIToken              = "api_token"
	auditTargetKindAPITokenCollection    = "api_token_collection"
	auditTargetKindConnection            = "connection"
	auditTargetKindExternalIdentity      = "external_identity"
	auditTargetKindManagedIdentity       = "managed_identity"
	auditTargetKindManagedIdentityMember = "managed_identity_member"
	auditTargetKindManagedIdentityGrant  = "managed_identity_grant"
	auditDecisionProviderAccessDenied    = "provider_access_denied"
	auditDecisionOperationBindingDenied  = "operation_binding_denied"
	auditDecisionCatalogRoleDenied       = "catalog_role_denied"
)

type auditTarget struct {
	ID   string
	Kind string
	Name string
}

type auditAuthorization struct {
	Policy   string
	Role     string
	Decision string
}

func (s *Server) resolvePrincipalUserID(ctx context.Context, p *principal.Principal) (*principal.Principal, error) {
	p = principal.Canonicalized(p)
	if p == nil {
		return nil, nil
	}
	if principal.IsWorkloadPrincipal(p) {
		return p, nil
	}
	if p.UserID != "" {
		return p, nil
	}
	if p.Identity == nil || p.Identity.Email == "" {
		return p, nil
	}

	dbUser, err := s.users.FindOrCreateUser(ctx, p.Identity.Email)
	if err != nil {
		return nil, err
	}
	if dbUser == nil || dbUser.ID == "" {
		return nil, fmt.Errorf("authenticated principal missing user ID")
	}

	clone := *p
	clone.UserID = dbUser.ID
	clone.Kind = principal.KindUser
	return principal.Canonicalize(&clone), nil
}

func auditSourceForRequest(r *http.Request) string {
	if r != nil && strings.HasPrefix(r.URL.Path, "/mcp") {
		return "mcp"
	}
	return "http"
}

func (s *Server) auditEventWithTarget(ctx context.Context, p *principal.Principal, source, provider, operation string, allowed bool, err error, target auditTarget) {
	if s.auditSink == nil {
		return
	}

	ctx, entry := invocation.BuildAuditEntry(ctx, p, source, provider, operation)
	populateAuditEntry(&entry, allowed, err, target, auditAuthorization{})
	s.auditSink.Log(ctx, entry)
}

func (s *Server) auditEventWithAuthSource(ctx context.Context, authSource, source, provider, operation string, allowed bool, err error) {
	if s.auditSink == nil {
		return
	}

	ctx, entry := invocation.BuildAuditEntry(ctx, nil, source, provider, operation)
	entry.AuthSource = authSource
	populateAuditEntry(&entry, allowed, err, auditTarget{}, auditAuthorization{})
	s.auditSink.Log(ctx, entry)
}

func (s *Server) auditEventWithSubjectIDAndTarget(ctx context.Context, subjectID, authSource, source, provider, operation string, allowed bool, err error, target auditTarget) {
	if s.auditSink == nil {
		return
	}

	ctx, entry := invocation.BuildAuditEntry(ctx, &principal.Principal{SubjectID: strings.TrimSpace(subjectID)}, source, provider, operation)
	entry.AuthSource = authSource
	populateAuditEntry(&entry, allowed, err, target, auditAuthorization{})
	s.auditSink.Log(ctx, entry)
}

func populateAuditEntry(entry *core.AuditEntry, allowed bool, err error, target auditTarget, authz auditAuthorization) {
	entry.Allowed = allowed
	if err != nil {
		entry.Error = err.Error()
	}
	if authz.Policy != "" {
		entry.AccessPolicy = authz.Policy
	}
	if authz.Role != "" {
		entry.AccessRole = authz.Role
	}
	if authz.Decision != "" {
		entry.AuthorizationDecision = authz.Decision
	}
	if target.ID != "" {
		entry.TargetID = target.ID
	}
	if target.Kind != "" {
		entry.TargetKind = target.Kind
	}
	if target.Name != "" {
		entry.TargetName = target.Name
	}
}

func apiTokenAuditTarget(id, name string) auditTarget {
	return auditTarget{
		ID:   id,
		Kind: auditTargetKindAPIToken,
		Name: name,
	}
}

func apiTokenCollectionAuditTarget() auditTarget {
	return auditTarget{Kind: auditTargetKindAPITokenCollection}
}

func connectionAuditTarget(provider, connection, instance string) auditTarget {
	connection = auditConnectionName(connection)
	if instance == "" {
		instance = defaultTokenInstance
	}

	idParts := []string{}
	if provider != "" {
		idParts = append(idParts, provider)
	}
	idParts = append(idParts, connection, instance)

	return auditTarget{
		ID:   strings.Join(idParts, "/"),
		Kind: auditTargetKindConnection,
		Name: connection + "/" + instance,
	}
}

func externalIdentityAuditTarget(identityType, identityID string) auditTarget {
	identityType = strings.TrimSpace(identityType)
	identityID = strings.TrimSpace(identityID)
	id := identityID
	if identityType != "" && identityID != "" {
		id = identityType + "/" + identityID
	}
	name := identityID
	if identityType != "" && identityID != "" {
		name = identityType + ":" + identityID
	}
	return auditTarget{
		ID:   id,
		Kind: auditTargetKindExternalIdentity,
		Name: name,
	}
}

func managedIdentityAuditTarget(id, name string) auditTarget {
	return auditTarget{
		ID:   strings.TrimSpace(id),
		Kind: auditTargetKindManagedIdentity,
		Name: strings.TrimSpace(name),
	}
}

func managedIdentityMemberAuditTarget(identityID, email string) auditTarget {
	return auditTarget{
		ID:   strings.TrimSpace(identityID),
		Kind: auditTargetKindManagedIdentityMember,
		Name: strings.TrimSpace(email),
	}
}

func managedIdentityGrantAuditTarget(identityID, plugin string) auditTarget {
	return auditTarget{
		ID:   strings.TrimSpace(identityID),
		Kind: auditTargetKindManagedIdentityGrant,
		Name: strings.TrimSpace(plugin),
	}
}

func auditConnectionName(connection string) string {
	connection = userFacingConnectionName(connection)
	if connection == "" {
		return "default"
	}
	return connection
}

func (s *Server) auditHTTPEvent(ctx context.Context, p *principal.Principal, provider, operation string, allowed bool, err error) {
	s.auditHTTPEventWithTarget(ctx, p, provider, operation, allowed, err, auditTarget{})
}

func (s *Server) auditHTTPEventWithTarget(ctx context.Context, p *principal.Principal, provider, operation string, allowed bool, err error, target auditTarget) {
	s.auditEventWithTarget(ctx, p, "http", provider, operation, allowed, err, target)
}

func (s *Server) auditHTTPAuthorizationEvent(ctx context.Context, p *principal.Principal, provider, operation string, allowed bool, err error, authz auditAuthorization) {
	if s.auditSink == nil {
		return
	}

	ctx, entry := invocation.BuildAuditEntry(ctx, p, "http", provider, operation)
	populateAuditEntry(&entry, allowed, err, auditTarget{}, authz)
	s.auditSink.Log(ctx, entry)
}

func (s *Server) auditRequestEventWithAuthSource(r *http.Request, authSource, provider, operation string, allowed bool, err error) {
	s.auditEventWithAuthSource(r.Context(), authSource, auditSourceForRequest(r), provider, operation, allowed, err)
}

func (s *Server) auditHTTPEventWithSubjectID(ctx context.Context, subjectID, authSource, provider, operation string, allowed bool, err error) {
	s.auditHTTPEventWithSubjectIDAndTarget(ctx, subjectID, authSource, provider, operation, allowed, err, auditTarget{})
}

func (s *Server) auditHTTPEventWithSubjectIDAndTarget(ctx context.Context, subjectID, authSource, provider, operation string, allowed bool, err error, target auditTarget) {
	s.auditEventWithSubjectIDAndTarget(ctx, subjectID, authSource, "http", provider, operation, allowed, err, target)
}
