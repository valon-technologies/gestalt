package principal

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
)

type stubCredentialUserResolver struct {
	users map[string]string
	err   error
}

func (s stubCredentialUserResolver) FindOrCreateUser(_ context.Context, email string) (*core.User, error) {
	if s.err != nil {
		return nil, s.err
	}
	id, ok := s.users[email]
	if !ok {
		return nil, errors.New("user not found")
	}
	return &core.User{ID: id, Email: email}, nil
}

func TestResolveCredentialSubjectID_UserPrincipalUsesPersistedUserID(t *testing.T) {
	t.Parallel()

	subjectID, err := ResolveCredentialSubjectID(
		context.Background(),
		stubCredentialUserResolver{users: map[string]string{"alice@example.com": "db-alice"}},
		&Principal{
			SubjectID: "user:alice@example.com",
			Identity:  &core.UserIdentity{Email: "alice@example.com"},
			Kind:      KindUser,
			Source:    SourceBearer,
		},
	)
	if err != nil {
		t.Fatalf("ResolveCredentialSubjectID: %v", err)
	}
	if subjectID != "user:db-alice" {
		t.Fatalf("subjectID = %q, want %q", subjectID, "user:db-alice")
	}
}

func TestResolveCredentialSubjectID_NonUserPrincipalUsesTokenSubject(t *testing.T) {
	t.Parallel()

	subjectID, err := ResolveCredentialSubjectID(
		context.Background(),
		nil,
		&Principal{
			SubjectID: "service_account:workflow-bot",
			Kind:      Kind("service_account"),
			Source:    SourceBearer,
		},
	)
	if err != nil {
		t.Fatalf("ResolveCredentialSubjectID: %v", err)
	}
	if subjectID != "service_account:workflow-bot" {
		t.Fatalf("subjectID = %q, want service account subject", subjectID)
	}
}
