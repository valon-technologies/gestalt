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

func TestResolveCredentialSubjectID_CanonicalUUIDFastPathSkipsUserStore(t *testing.T) {
	t.Parallel()

	const uuid = "28542db5-ae88-404f-a231-a0034cb9212c"
	cases := []struct {
		name string
		p    *Principal
	}{
		{"with UserID", &Principal{UserID: uuid, SubjectID: UserSubjectID(uuid), Kind: KindUser, Source: SourceBearer}},
		{"SubjectID only", &Principal{SubjectID: UserSubjectID(uuid), Kind: KindUser, Source: SourceBearer}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			subjectID, err := ResolveCredentialSubjectID(context.Background(), nil, tc.p)
			if err != nil {
				t.Fatalf("ResolveCredentialSubjectID: %v", err)
			}
			if subjectID != UserSubjectID(uuid) {
				t.Fatalf("subjectID = %q, want %q", subjectID, UserSubjectID(uuid))
			}
		})
	}
}

func TestResolveCredentialSubjectID_EmailSubjectWithoutIdentityStillCanonicalizes(t *testing.T) {
	t.Parallel()

	subjectID, err := ResolveCredentialSubjectID(
		context.Background(),
		stubCredentialUserResolver{users: map[string]string{"hugh@valon.com": "db-hugh"}},
		&Principal{SubjectID: "user:hugh@valon.com", Kind: KindUser, Source: SourceBearer},
	)
	if err != nil {
		t.Fatalf("ResolveCredentialSubjectID: %v", err)
	}
	if subjectID != "user:db-hugh" {
		t.Fatalf("subjectID = %q, want %q", subjectID, "user:db-hugh")
	}
}
