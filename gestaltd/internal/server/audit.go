package server

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

func (s *Server) resolvePrincipalUserID(ctx context.Context, p *principal.Principal) (*principal.Principal, error) {
	if p == nil {
		return nil, nil
	}
	if p.Kind == principal.KindWorkload {
		return p, nil
	}
	if p.Kind == "" {
		p.Kind = principal.KindUser
	}
	if p.UserID != "" {
		if p.SubjectID == "" {
			clone := *p
			clone.SubjectID = principal.UserSubjectID(p.UserID)
			return &clone, nil
		}
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
	clone.SubjectID = principal.UserSubjectID(dbUser.ID)
	return &clone, nil
}

func (s *Server) auditHTTPEvent(ctx context.Context, p *principal.Principal, provider, operation string, allowed bool, err error) {
	if s.auditSink == nil {
		return
	}

	ctx, entry := invocation.BuildAuditEntry(ctx, p, "http", provider, operation)
	entry.Allowed = allowed
	if err != nil {
		entry.Error = err.Error()
	}
	s.auditSink.Log(ctx, entry)
}

func (s *Server) auditHTTPEventWithUserID(ctx context.Context, userID, authSource, provider, operation string, allowed bool, err error) {
	if s.auditSink == nil {
		return
	}

	ctx, entry := invocation.BuildAuditEntry(ctx, nil, "http", provider, operation)
	entry.UserID = userID
	entry.AuthSource = authSource
	entry.SubjectID = principal.UserSubjectID(userID)
	entry.SubjectKind = string(principal.KindUser)
	entry.Allowed = allowed
	if err != nil {
		entry.Error = err.Error()
	}
	s.auditSink.Log(ctx, entry)
}
