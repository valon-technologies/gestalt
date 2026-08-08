package principal

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// UserSubjectForm classifies the form of the user portion of a subject ID.
type UserSubjectForm string

const (
	// UserSubjectFormEmpty is an absent or blank subject value.
	UserSubjectFormEmpty UserSubjectForm = "empty"
	// UserSubjectFormCanonical is a persisted Gestalt user UUID.
	UserSubjectFormCanonical UserSubjectForm = "canonical"
	// UserSubjectFormEmail is a verified email that must be resolved to a
	// persisted user UUID before it may be authorized.
	UserSubjectFormEmail UserSubjectForm = "email"
	// UserSubjectFormOpaque is any other value, including provider-opaque
	// subjects such as "auth0|abc" or "google-oauth2|123". Opaque values are
	// never authorized directly.
	UserSubjectFormOpaque UserSubjectForm = "opaque"
)

// ClassifyUserSubjectID classifies a full subject ID such as
// "user:<uuid>", "user:person@example.com", or "user:auth0|abc".
func ClassifyUserSubjectID(subjectID string) UserSubjectForm {
	value := strings.TrimSpace(subjectID)
	if value == "" {
		return UserSubjectFormEmpty
	}
	if userID := UserIDFromSubjectID(value); userID != "" {
		return ClassifyUserSubjectValue(userID)
	}
	if strings.Contains(value, ":") {
		// A non-user subject namespace is not classifiable as a user subject.
		return UserSubjectFormOpaque
	}
	return ClassifyUserSubjectValue(value)
}

// ClassifyUserSubjectValue classifies the value of a user subject, i.e. the
// part after the "user:" prefix.
func ClassifyUserSubjectValue(value string) UserSubjectForm {
	value = strings.TrimSpace(value)
	switch {
	case value == "":
		return UserSubjectFormEmpty
	case isCanonicalUserID(value):
		return UserSubjectFormCanonical
	case isEmailForm(value):
		return UserSubjectFormEmail
	default:
		return UserSubjectFormOpaque
	}
}

func isCanonicalUserID(value string) bool {
	if len(value) != 36 {
		return false
	}
	parsed, err := uuid.Parse(value)
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.String(), value)
}

func isEmailForm(value string) bool {
	if strings.ContainsAny(value, " \t\r\n|:/\\,;<>\"") {
		return false
	}
	at := strings.Index(value, "@")
	if at <= 0 || at != strings.LastIndex(value, "@") {
		return false
	}
	domain := value[at+1:]
	if domain == "" || !strings.Contains(domain, ".") {
		return false
	}
	return !strings.HasPrefix(domain, ".") && !strings.HasSuffix(domain, ".")
}

// ResolveAuthorizationSubjectID returns the subject ID an authorization
// boundary must evaluate for p.
//
// Non-user principals keep their canonical token subject. User principals are
// resolved to the persisted "user:<uuid>" subject: a canonical UUID passes
// through, a verified email is resolved through the user store, and any other
// (provider-opaque) subject is rejected. Every failure mode returns an error so
// callers deny instead of falling back to the raw token subject.
func ResolveAuthorizationSubjectID(ctx context.Context, users CredentialUserResolver, p *Principal) (string, error) {
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

	userID := strings.TrimSpace(p.UserID)
	if ClassifyUserSubjectValue(userID) == UserSubjectFormCanonical {
		return UserSubjectID(userID), nil
	}

	email := authorizationSubjectEmail(p, userID)
	if email == "" {
		return "", fmt.Errorf("%w: %q", ErrOpaqueCredentialSubject, strings.TrimSpace(p.SubjectID))
	}
	if users == nil {
		return "", ErrCredentialSubjectRequired
	}

	dbUser, err := users.FindOrCreateUser(ctx, email)
	if err != nil {
		return "", fmt.Errorf("resolve canonical user: %w", err)
	}
	if dbUser == nil || strings.TrimSpace(dbUser.ID) == "" {
		return "", ErrCredentialSubjectRequired
	}
	return UserSubjectID(strings.TrimSpace(dbUser.ID)), nil
}

func authorizationSubjectEmail(p *Principal, userID string) string {
	if p.Identity != nil {
		if email := strings.TrimSpace(p.Identity.Email); isEmailForm(email) {
			return email
		}
	}
	if isEmailForm(userID) {
		return userID
	}
	if suffix := strings.TrimSpace(UserIDFromSubjectID(strings.TrimSpace(p.SubjectID))); isEmailForm(suffix) {
		return suffix
	}
	return ""
}
