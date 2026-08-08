package principal

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
)

const testCanonicalUserID = "28542db5-ae88-404f-a231-a0034cb9212c"

func TestClassifyUserSubjectID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		subjectID string
		want      UserSubjectForm
	}{
		{"empty", "  ", UserSubjectFormEmpty},
		{"canonical uuid", "user:" + testCanonicalUserID, UserSubjectFormCanonical},
		{"bare canonical uuid", testCanonicalUserID, UserSubjectFormCanonical},
		{"email", "user:person@valon.com", UserSubjectFormEmail},
		{"auth0", "user:auth0|abc123", UserSubjectFormOpaque},
		{"google oauth2", "user:google-oauth2|1234567890", UserSubjectFormOpaque},
		{"samlp", "user:samlp|okta|person@valon.com", UserSubjectFormOpaque},
		{"non user namespace", "service_account:workflow-bot", UserSubjectFormOpaque},
		{"unrecognized", "user:alice", UserSubjectFormOpaque},
		{"email without domain dot", "user:person@localhost", UserSubjectFormOpaque},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ClassifyUserSubjectID(tc.subjectID); got != tc.want {
				t.Fatalf("ClassifyUserSubjectID(%q) = %q, want %q", tc.subjectID, got, tc.want)
			}
		})
	}
}

func TestResolveAuthorizationSubjectID_EmailResolvesToCanonicalUUID(t *testing.T) {
	t.Parallel()

	users := stubCredentialUserResolver{users: map[string]string{"dave@valon.com": testCanonicalUserID}}
	subjectID, err := ResolveAuthorizationSubjectID(context.Background(), users, &Principal{
		SubjectID: "user:dave@valon.com",
		Identity:  &core.UserIdentity{Email: "dave@valon.com"},
		Kind:      KindUser,
		Source:    SourceBearer,
	})
	if err != nil {
		t.Fatalf("ResolveAuthorizationSubjectID: %v", err)
	}
	if subjectID != UserSubjectID(testCanonicalUserID) {
		t.Fatalf("subjectID = %q, want %q", subjectID, UserSubjectID(testCanonicalUserID))
	}
}

func TestResolveAuthorizationSubjectID_CanonicalUUIDPassesThrough(t *testing.T) {
	t.Parallel()

	subjectID, err := ResolveAuthorizationSubjectID(context.Background(), nil, &Principal{
		SubjectID: UserSubjectID(testCanonicalUserID),
		Kind:      KindUser,
		Source:    SourceBearer,
	})
	if err != nil {
		t.Fatalf("ResolveAuthorizationSubjectID: %v", err)
	}
	if subjectID != UserSubjectID(testCanonicalUserID) {
		t.Fatalf("subjectID = %q, want %q", subjectID, UserSubjectID(testCanonicalUserID))
	}
}

func TestResolveAuthorizationSubjectID_OpaqueSubjectRejected(t *testing.T) {
	t.Parallel()

	users := stubCredentialUserResolver{users: map[string]string{"dave@valon.com": testCanonicalUserID}}
	for _, subject := range []string{"user:auth0|abc123", "user:google-oauth2|1234567890", "user:alice"} {
		t.Run(subject, func(t *testing.T) {
			t.Parallel()
			subjectID, err := ResolveAuthorizationSubjectID(context.Background(), users, &Principal{
				SubjectID: subject,
				Kind:      KindUser,
				Source:    SourceBearer,
			})
			if !errors.Is(err, ErrOpaqueCredentialSubject) {
				t.Fatalf("err = %v, want ErrOpaqueCredentialSubject", err)
			}
			if subjectID != "" {
				t.Fatalf("subjectID = %q, want empty", subjectID)
			}
		})
	}
}

func TestResolveAuthorizationSubjectID_OpaqueSubjectWithVerifiedEmailMapsToCanonicalUUID(t *testing.T) {
	t.Parallel()

	users := stubCredentialUserResolver{users: map[string]string{"kevon@valon.com": testCanonicalUserID}}
	subjectID, err := ResolveAuthorizationSubjectID(context.Background(), users, &Principal{
		SubjectID: "user:auth0|abc123",
		Identity:  &core.UserIdentity{Email: "kevon@valon.com"},
		Kind:      KindUser,
		Source:    SourceBearer,
	})
	if err != nil {
		t.Fatalf("ResolveAuthorizationSubjectID: %v", err)
	}
	if subjectID != UserSubjectID(testCanonicalUserID) {
		t.Fatalf("subjectID = %q, want %q", subjectID, UserSubjectID(testCanonicalUserID))
	}
}

func TestResolveAuthorizationSubjectID_UserStoreFailureFailsClosed(t *testing.T) {
	t.Parallel()

	storeErr := errors.New("user store unavailable")
	subjectID, err := ResolveAuthorizationSubjectID(
		context.Background(),
		stubCredentialUserResolver{err: storeErr},
		&Principal{
			SubjectID: "user:dave@valon.com",
			Identity:  &core.UserIdentity{Email: "dave@valon.com"},
			Kind:      KindUser,
			Source:    SourceBearer,
		},
	)
	if !errors.Is(err, storeErr) {
		t.Fatalf("err = %v, want store error", err)
	}
	if subjectID != "" {
		t.Fatalf("subjectID = %q, want empty (no fallback to raw subject)", subjectID)
	}
}

func TestResolveAuthorizationSubjectID_MissingUserStoreFailsClosed(t *testing.T) {
	t.Parallel()

	subjectID, err := ResolveAuthorizationSubjectID(context.Background(), nil, &Principal{
		SubjectID: "user:dave@valon.com",
		Identity:  &core.UserIdentity{Email: "dave@valon.com"},
		Kind:      KindUser,
		Source:    SourceBearer,
	})
	if !errors.Is(err, ErrCredentialSubjectRequired) {
		t.Fatalf("err = %v, want ErrCredentialSubjectRequired", err)
	}
	if subjectID != "" {
		t.Fatalf("subjectID = %q, want empty", subjectID)
	}
}

func TestResolveAuthorizationSubjectID_NonUserPrincipalKeepsTokenSubject(t *testing.T) {
	t.Parallel()

	subjectID, err := ResolveAuthorizationSubjectID(context.Background(), nil, &Principal{
		SubjectID: "service_account:workflow-bot",
		Kind:      Kind("service_account"),
		Source:    SourceBearer,
	})
	if err != nil {
		t.Fatalf("ResolveAuthorizationSubjectID: %v", err)
	}
	if subjectID != "service_account:workflow-bot" {
		t.Fatalf("subjectID = %q, want service account subject", subjectID)
	}
}

func TestResolveAuthorizationSubjectID_NilPrincipalDenied(t *testing.T) {
	t.Parallel()

	if _, err := ResolveAuthorizationSubjectID(context.Background(), nil, nil); !errors.Is(err, ErrCredentialSubjectRequired) {
		t.Fatalf("err = %v, want ErrCredentialSubjectRequired", err)
	}
}
