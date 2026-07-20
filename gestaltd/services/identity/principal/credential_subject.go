package principal

import (
	"context"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
)

// CredentialUserResolver looks up persisted users when canonicalizing human
// credential subjects to match HTTP credential routes.
type CredentialUserResolver interface {
	FindOrCreateUser(ctx context.Context, email string) (*core.User, error)
}

func ResolveCredentialSubjectID(ctx context.Context, users CredentialUserResolver, p *Principal) (string, error) {
	p = Canonicalized(p)
	if p == nil {
		return "", ErrCredentialSubjectRequired
	}
	if IsNonUserPrincipal(p) {
		subjectID := strings.TrimSpace(p.SubjectID)
		if subjectID == "" {
			return "", ErrCredentialSubjectRequired
		}
		return subjectID, nil
	}

	if userID := strings.TrimSpace(p.UserID); userID != "" && !strings.Contains(userID, "@") {
		return UserSubjectID(userID), nil
	}

	email := ""
	if p.Identity != nil {
		email = strings.TrimSpace(p.Identity.Email)
	}
	if email == "" {
		if suffix := UserIDFromSubjectID(p.SubjectID); strings.Contains(suffix, "@") {
			email = suffix
		}
	}
	if email == "" {
		return "", ErrCredentialSubjectRequired
	}
	if users == nil {
		return "", ErrCredentialSubjectRequired
	}

	dbUser, err := users.FindOrCreateUser(ctx, email)
	if err != nil {
		return "", err
	}
	if dbUser == nil || strings.TrimSpace(dbUser.ID) == "" {
		return "", ErrCredentialSubjectRequired
	}
	return UserSubjectID(dbUser.ID), nil
}
