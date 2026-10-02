package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
)

const appAccessUpgradeAliceEmail = "alice@example.com"

var errAppAccessHistory = errors.New("history unavailable")

type fakeOperationHistory struct {
	ops   []string
	known bool
	err   error
}

func (h *fakeOperationHistory) OperationsAt(context.Context, string, time.Time) ([]string, bool, error) {
	return h.ops, h.known, h.err
}

func (f *appAccessCatalogFixture) adminView(t *testing.T) appAdminAccessResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/apps/"+appAccessTestApp+"/admin/access?email="+appAccessUpgradeAliceEmail, nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("app", appAccessTestApp)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
	recorder := httptest.NewRecorder()
	f.server.getAppAdminAccess(recorder, req)
	return decodeAppAdminAccess(t, recorder)
}

func (f *appAccessCatalogFixture) storedProfile(t *testing.T) *core.AppAccessProfile {
	t.Helper()
	stored, err := f.services.AppAccessProfiles.GetAppAccessProfile(context.Background(), f.alice.SubjectID, appAccessTestApp)
	if err != nil {
		t.Fatalf("GetAppAccessProfile: %v", err)
	}
	return stored
}

func TestAppAccessUpgradesLegacyProfileAtReadTimeWithoutPersisting(t *testing.T) {
	t.Parallel()

	f := newAppAccessCatalogFixture(t)
	f.server.operationHistory = &fakeOperationHistory{known: true, ops: []string{appAccessPostMessageOp, appAccessConversationsOp}}
	f.writeLegacyProfile(t, []string{appAccessConversationsOp})
	f.addOperation(appAccessNewOp)

	want := []string{appAccessConversationsOp, appAccessNewOp}
	slices.Sort(want)
	if got := f.get(t).EnabledOperations; !slices.Equal(got, want) {
		t.Fatalf("GET enabled = %v, want %v: the later operation is on, the earlier absent one stays off", got, want)
	}
	if !f.storedProfile(t).Legacy {
		t.Fatalf("stored profile was rewritten by a read, want it left legacy")
	}
	admin := f.adminView(t)
	if !slices.Equal(admin.EnabledOperations, want) || !slices.Equal(admin.DeniedOperations, []string{appAccessPostMessageOp}) {
		t.Fatalf("admin view = %#v, want enabled %v and denied [%s]", admin, want, appAccessPostMessageOp)
	}
}

func TestAppAccessAdminViewConvertsLegacyProfileWithoutWriting(t *testing.T) {
	t.Parallel()

	f := newAppAccessCatalogFixture(t)
	f.server.operationHistory = &fakeOperationHistory{known: true, ops: []string{appAccessPostMessageOp, appAccessConversationsOp}}
	f.writeLegacyProfile(t, []string{appAccessConversationsOp})
	f.addOperation(appAccessNewOp)

	admin := f.adminView(t)
	if !slices.Contains(admin.EnabledOperations, appAccessNewOp) || !slices.Equal(admin.DeniedOperations, []string{appAccessPostMessageOp}) {
		t.Fatalf("admin view = %#v, want %s enabled and only %s denied", admin, appAccessNewOp, appAccessPostMessageOp)
	}
	if !f.storedProfile(t).Legacy {
		t.Fatalf("admin read rewrote the stored profile, want it left legacy")
	}
}

func TestAppAccessKeepsLegacyListWhenHistoryCannotAnswer(t *testing.T) {
	t.Parallel()

	for name, history := range map[string]core.OperationHistory{
		"unknown app": &fakeOperationHistory{known: false},
		"error":       &fakeOperationHistory{known: true, err: errAppAccessHistory},
		"no history":  nil,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newAppAccessCatalogFixture(t)
			f.server.operationHistory = history
			f.writeLegacyProfile(t, []string{appAccessConversationsOp})
			f.addOperation(appAccessNewOp)

			if got := f.get(t).EnabledOperations; !slices.Equal(got, []string{appAccessConversationsOp}) {
				t.Fatalf("GET enabled = %v, want exactly the legacy list", got)
			}
			if !f.storedProfile(t).Legacy {
				t.Fatalf("stored profile was rewritten, want it left legacy")
			}
			if got := f.adminView(t).EnabledOperations; !slices.Equal(got, []string{appAccessConversationsOp}) {
				t.Fatalf("admin enabled = %v, want exactly the legacy list", got)
			}
		})
	}
}

func TestAppAccessPutOnLegacyProfileKeepsUnlistedDecisions(t *testing.T) {
	t.Parallel()

	f := newAppAccessCatalogFixture(t)
	f.server.operationHistory = &fakeOperationHistory{known: true, ops: []string{appAccessPostMessageOp, appAccessConversationsOp}}
	f.writeLegacyProfile(t, []string{appAccessConversationsOp})

	f.put(t, []string{appAccessConversationsOp, appAccessPostMessageOp})

	stored := f.storedProfile(t)
	if stored.Legacy || !slices.Equal(stored.DisabledOperations, []string{core.GraphQLCapabilityID}) {
		t.Fatalf("stored = %#v, want the user's save to win, keeping only the graphql opt-out", stored)
	}
}

func TestAppAccessUserSaveAfterReadTimeConversionWins(t *testing.T) {
	t.Parallel()

	f := newAppAccessCatalogFixture(t)
	f.server.operationHistory = &fakeOperationHistory{known: true, ops: []string{appAccessPostMessageOp, appAccessConversationsOp}}
	f.writeLegacyProfile(t, []string{appAccessConversationsOp})

	if got := f.get(t).EnabledOperations; !slices.Equal(got, []string{appAccessConversationsOp}) {
		t.Fatalf("GET enabled = %v, want the converted view", got)
	}
	f.put(t, []string{appAccessPostMessageOp})

	stored := f.storedProfile(t)
	if stored.Legacy || !slices.Equal(stored.DisabledOperations, []string{appAccessConversationsOp, core.GraphQLCapabilityID}) {
		t.Fatalf("stored = %#v, want the user's save in the new format", stored)
	}
}
