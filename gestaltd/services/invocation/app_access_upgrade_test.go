package invocation

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

const (
	upgradeListOp   = "conversations.list"
	upgradePostOp   = "chat.postMessage"
	upgradeLaterOp  = "files.list"
	upgradeSavedAge = time.Hour
)

var errUpgradeHistory = errors.New("history unavailable")

type fakeOperationHistory struct {
	ops   []string
	known bool
	err   error
}

func (h *fakeOperationHistory) OperationsAt(context.Context, string, time.Time) ([]string, bool, error) {
	return h.ops, h.known, h.err
}

type memoryAppAccessStore struct {
	mu      sync.Mutex
	profile *core.AppAccessProfile
}

func (s *memoryAppAccessStore) GetAppAccessProfile(context.Context, string, string) (*core.AppAccessProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.profile == nil {
		return nil, core.ErrNotFound
	}
	profile := *s.profile
	return &profile, nil
}

func (s *memoryAppAccessStore) EnsureAppAccessDefaults(context.Context, string, string) (*core.AppAccessProfile, error) {
	return s.GetAppAccessProfile(context.Background(), "", "")
}

func (s *memoryAppAccessStore) SetAppAccessOverrides(_ context.Context, subjectID, app string, disabled, extra []string) (*core.AppAccessProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.profile = &core.AppAccessProfile{
		SubjectID: subjectID, App: app, DisabledOperations: disabled, ExtraOperations: extra,
		DefaultsInitialized: true, UpdatedAt: time.Now(),
	}
	return s.profile, nil
}

func (s *memoryAppAccessStore) stored() *core.AppAccessProfile {
	s.mu.Lock()
	defer s.mu.Unlock()
	profile := *s.profile
	return &profile
}

func newLegacyUpgradeStore(enabled ...string) *memoryAppAccessStore {
	p := appAccessDefaultsPrincipal()
	return &memoryAppAccessStore{profile: &core.AppAccessProfile{
		SubjectID:               p.SubjectID,
		App:                     appAccessDefaultsApp,
		Legacy:                  true,
		LegacyEnabledOperations: enabled,
		DefaultsInitialized:     true,
		UpdatedAt:               time.Now().Add(-upgradeSavedAge),
	}}
}

func newUpgradeBroker(t *testing.T, store core.AppAccessProfileStore, history core.OperationHistory, provider core.Provider) *Broker {
	t.Helper()
	svc := testutil.NewStubServices(t)
	resolver := NewAppAccessResolver(nil)
	resolver.SetOperationHistory(history)
	return NewBroker(
		testutil.NewProviderRegistry(t, provider),
		svc.Users,
		nil,
		WithAppAccessProfiles(store),
		WithAppAccessResolver(resolver),
	)
}

func upgradeProvider() *coretesting.StubIntegration {
	return newAppAccessDefaultsProvider(
		catalog.CatalogOperation{ID: upgradeListOp, Method: "GET"},
		catalog.CatalogOperation{ID: upgradePostOp, Method: "POST"},
		catalog.CatalogOperation{ID: upgradeLaterOp, Method: "GET"},
	)
}

func TestBrokerConvertsLegacyProfileAtReadTimeWithoutPersisting(t *testing.T) {
	t.Parallel()

	store := newLegacyUpgradeStore(upgradeListOp)
	history := &fakeOperationHistory{ops: []string{upgradeListOp, upgradePostOp}, known: true}
	broker := newUpgradeBroker(t, store, history, upgradeProvider())
	p := appAccessDefaultsPrincipal()

	requireInvokeAllowed(t, broker, p, upgradeListOp)
	requireInvokeDenied(t, broker, p, upgradePostOp)
	requireInvokeAllowed(t, broker, p, upgradeLaterOp)

	if !store.stored().Legacy {
		t.Fatalf("stored profile was rewritten by reads, want it left legacy")
	}
	got := listedAllowed(t, broker, p, upgradeListOp, upgradePostOp, upgradeLaterOp)
	if want := []string{upgradeListOp, upgradeLaterOp}; !slices.Equal(got, want) {
		t.Fatalf("listing allowed = %v, want %v", got, want)
	}
}

func TestBrokerKeepsLegacyAllowListWhenHistoryCannotAnswer(t *testing.T) {
	t.Parallel()

	for name, history := range map[string]core.OperationHistory{
		"unknown app": &fakeOperationHistory{known: false},
		"error":       &fakeOperationHistory{known: true, err: errUpgradeHistory},
		"no history":  nil,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := newLegacyUpgradeStore(upgradeListOp)
			broker := newUpgradeBroker(t, store, history, upgradeProvider())
			p := appAccessDefaultsPrincipal()

			requireInvokeAllowed(t, broker, p, upgradeListOp)
			requireInvokeDenied(t, broker, p, upgradePostOp)
			requireInvokeDenied(t, broker, p, upgradeLaterOp)
			if !store.stored().Legacy {
				t.Fatalf("stored profile was rewritten, want it left legacy")
			}
		})
	}
}

type sessionCatalogUpgradeProvider struct {
	*coretesting.StubIntegration
}

func (sessionCatalogUpgradeProvider) SupportsSessionCatalog() bool { return true }

func TestBrokerLeavesLegacyProfileOfSessionCatalogProviderUntouched(t *testing.T) {
	t.Parallel()

	store := newLegacyUpgradeStore(upgradeListOp)
	history := &fakeOperationHistory{ops: []string{upgradeListOp, upgradePostOp}, known: true}
	provider := sessionCatalogUpgradeProvider{upgradeProvider()}
	broker := newUpgradeBroker(t, store, history, provider)
	p := appAccessDefaultsPrincipal()

	requireInvokeAllowed(t, broker, p, upgradeListOp)
	requireInvokeDenied(t, broker, p, upgradeLaterOp)
	if !store.stored().Legacy {
		t.Fatalf("session-catalog profile was converted, want it left legacy")
	}
}

func TestBrokerUserSaveAfterReadTimeConversionWins(t *testing.T) {
	t.Parallel()

	store := newLegacyUpgradeStore(upgradeListOp)
	p := appAccessDefaultsPrincipal()
	history := &fakeOperationHistory{ops: []string{upgradeListOp, upgradePostOp}, known: true}
	broker := newUpgradeBroker(t, store, history, upgradeProvider())

	requireInvokeDenied(t, broker, p, upgradePostOp)
	if _, err := store.SetAppAccessOverrides(context.Background(), p.SubjectID, appAccessDefaultsApp, []string{upgradeListOp}, nil); err != nil {
		t.Fatalf("save: %v", err)
	}

	requireInvokeDenied(t, broker, p, upgradeListOp)
	requireInvokeAllowed(t, broker, p, upgradePostOp)
	if stored := store.stored(); stored.Legacy || !slices.Equal(stored.DisabledOperations, []string{upgradeListOp}) {
		t.Fatalf("stored = %#v, want the user's save in the new format", stored)
	}
}

func TestBrokerKeepsNewDefaultOperationDeniedWhenHistoryOmitsAnEnabledOperation(t *testing.T) {
	t.Parallel()

	store := newLegacyUpgradeStore(upgradeListOp, upgradePostOp)
	history := &fakeOperationHistory{ops: []string{upgradeListOp}, known: true}
	broker := newUpgradeBroker(t, store, history, upgradeProvider())
	p := appAccessDefaultsPrincipal()

	requireInvokeAllowed(t, broker, p, upgradeListOp)
	requireInvokeAllowed(t, broker, p, upgradePostOp)
	requireInvokeDenied(t, broker, p, upgradeLaterOp)
}

func TestBrokerConvertsLegacyProfileEnablingOperationOutsideCatalog(t *testing.T) {
	t.Parallel()

	store := newLegacyUpgradeStore(upgradeListOp, "removed.operation")
	history := &fakeOperationHistory{ops: []string{upgradeListOp, upgradePostOp}, known: true}
	broker := newUpgradeBroker(t, store, history, upgradeProvider())
	p := appAccessDefaultsPrincipal()

	requireInvokeAllowed(t, broker, p, upgradeListOp)
	requireInvokeDenied(t, broker, p, upgradePostOp)
	requireInvokeAllowed(t, broker, p, upgradeLaterOp)
}

type failingAppAccessStore struct{ *memoryAppAccessStore }

var errUnsupportedProfileVersion = errors.New("unsupported app access profile version")

func (failingAppAccessStore) GetAppAccessProfile(context.Context, string, string) (*core.AppAccessProfile, error) {
	return nil, errUnsupportedProfileVersion
}

func TestBrokerDeniesWhenStoredProfileCannotBeDecoded(t *testing.T) {
	t.Parallel()

	broker := newUpgradeBroker(t, failingAppAccessStore{&memoryAppAccessStore{}}, nil, upgradeProvider())
	p := appAccessDefaultsPrincipal()

	requireInvokeDenied(t, broker, p, upgradeListOp)
	requireInvokeDenied(t, broker, p, upgradeLaterOp)
}
