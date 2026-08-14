package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func TestCallerAuthContextAttachesCanonicalSubject(t *testing.T) {
	t.Parallel()

	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tokens", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-token"})
	canonical := "user:11111111-1111-1111-1111-111111111111"
	ctx := principal.WithPrincipal(req.Context(), &principal.Principal{
		SubjectID: canonical,
		Kind:      principal.KindUser,
	})

	got := s.callerAuthContext(ctx, req)
	call := gestalt.IdentityCallContextFromContext(got)
	if call.CallerBearerToken != "session-token" {
		t.Fatalf("CallerBearerToken = %q, want session-token", call.CallerBearerToken)
	}
	if call.CallerSubjectID != canonical {
		t.Fatalf("CallerSubjectID = %q, want %q", call.CallerSubjectID, canonical)
	}
	if gestalt.TrustedCallerSubjectFromContext(got) != canonical {
		t.Fatalf("TrustedCallerSubject = %q, want %q", gestalt.TrustedCallerSubjectFromContext(got), canonical)
	}
}

func TestTokenExpiresIn(t *testing.T) {
	t.Parallel()

	t.Run("nil omits hint", func(t *testing.T) {
		t.Parallel()
		got, err := tokenExpiresIn(nil)
		if err != nil || got != 0 {
			t.Fatalf("tokenExpiresIn(nil) = (%d, %v), want (0, nil)", got, err)
		}
	})

	t.Run("zero omits hint", func(t *testing.T) {
		t.Parallel()
		zero := int64(0)
		got, err := tokenExpiresIn(&zero)
		if err != nil || got != 0 {
			t.Fatalf("tokenExpiresIn(0) = (%d, %v), want (0, nil)", got, err)
		}
	})

	t.Run("positive forwards seconds", func(t *testing.T) {
		t.Parallel()
		for _, want := range []int64{30 * 24 * 3600, 90 * 24 * 3600, 365 * 24 * 3600} {
			value := want
			got, err := tokenExpiresIn(&value)
			if err != nil || got != want {
				t.Fatalf("tokenExpiresIn(%d) = (%d, %v), want (%d, nil)", want, got, err, want)
			}
		}
	})

	t.Run("negative rejects", func(t *testing.T) {
		t.Parallel()
		negative := int64(-1)
		if _, err := tokenExpiresIn(&negative); err == nil {
			t.Fatal("tokenExpiresIn(-1) error = nil, want rejection")
		}
	})

	t.Run("over max rejects", func(t *testing.T) {
		t.Parallel()
		over := core.MaxTokenExpiresInSeconds + 1
		if _, err := tokenExpiresIn(&over); err == nil {
			t.Fatal("tokenExpiresIn(over max) error = nil, want rejection")
		}
	})
}
