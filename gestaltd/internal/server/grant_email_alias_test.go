package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func TestAttachGrantEmailSubjectAlias(t *testing.T) {
	t.Parallel()

	canonical := "user:11111111-1111-1111-1111-111111111111"
	emailSubject := "user:owner@example.com"
	ctx := attachGrantEmailSubjectAlias(context.Background(), &principal.Principal{
		SubjectID: canonical,
		Kind:      principal.KindUser,
		Identity:  &core.UserIdentity{Email: "owner@example.com"},
	})

	call := gestalt.IdentityCallContextFromContext(ctx)
	if call.CallerSubjectID != canonical {
		t.Fatalf("CallerSubjectID = %q, want %q", call.CallerSubjectID, canonical)
	}
	if call.Introspection == nil || !call.Introspection.Active {
		t.Fatal("Introspection missing or inactive")
	}
	if call.Introspection.Subject != emailSubject {
		t.Fatalf("Introspection.Subject = %q, want %q", call.Introspection.Subject, emailSubject)
	}
}

func TestCallerAuthContextAttachesEmailAlias(t *testing.T) {
	t.Parallel()

	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tokens", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-token"})
	canonical := "user:11111111-1111-1111-1111-111111111111"
	emailSubject := "user:owner@example.com"
	ctx := principal.WithPrincipal(req.Context(), &principal.Principal{
		SubjectID: canonical,
		UserID:    "11111111-1111-1111-1111-111111111111",
		Kind:      principal.KindUser,
		Identity:  &core.UserIdentity{Email: "owner@example.com"},
	})

	got := s.callerAuthContext(ctx, req)
	call := gestalt.IdentityCallContextFromContext(got)
	if call.CallerSubjectID != canonical {
		t.Fatalf("CallerSubjectID = %q, want %q", call.CallerSubjectID, canonical)
	}
	if call.Introspection == nil || call.Introspection.Subject != emailSubject {
		t.Fatalf("Introspection.Subject = %q, want %q", call.Introspection.Subject, emailSubject)
	}
}
