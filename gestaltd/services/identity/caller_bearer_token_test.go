package identity

import (
	"context"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

func TestWithCallerBearerTokenPreservesCallerSubject(t *testing.T) {
	t.Parallel()

	ctx := gestalt.WithIdentityCallContext(context.Background(), gestalt.IdentityCallContext{
		CallerSubjectID: "user:11111111-1111-1111-1111-111111111111",
		Introspection:   &gestalt.IntrospectResponse{Active: true, Subject: "user:login@example.test"},
	})
	got := WithCallerBearerToken(ctx, "session-token")
	call := gestalt.IdentityCallContextFromContext(got)
	if call.CallerBearerToken != "session-token" {
		t.Fatalf("CallerBearerToken = %q, want session-token", call.CallerBearerToken)
	}
	if call.CallerSubjectID != "user:11111111-1111-1111-1111-111111111111" {
		t.Fatalf("CallerSubjectID = %q, want canonical user id", call.CallerSubjectID)
	}
	if call.Introspection == nil || !call.Introspection.Active {
		t.Fatal("Introspection was dropped")
	}
}
