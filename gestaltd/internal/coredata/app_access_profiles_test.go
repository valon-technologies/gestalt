package coredata_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

const (
	testAppAccessSubject = "user:person@example.com"
	testAppAccessApp     = "slack"
	appAccessKeySep      = "\x1f"
)

func appAccessCatalog(ids ...string) *catalog.Catalog {
	cat := &catalog.Catalog{}
	for _, id := range ids {
		cat.Operations = append(cat.Operations, catalog.CatalogOperation{ID: id})
	}
	return cat
}

func TestAppAccessProfileService(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, _ := newTestServicesWithDB(t)
	profiles := svc.AppAccessProfiles

	if _, err := profiles.GetAppAccessProfile(ctx, testAppAccessSubject, testAppAccessApp); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("missing profile error = %v, want %v", err, core.ErrNotFound)
	}

	created, err := profiles.EnsureAppAccessDefaults(ctx, testAppAccessSubject, testAppAccessApp)
	if err != nil {
		t.Fatalf("EnsureAppAccessDefaults: %v", err)
	}
	if !created.DefaultsInitialized || created.Legacy || len(created.DisabledOperations) != 0 || len(created.ExtraOperations) != 0 {
		t.Fatalf("created profile = %#v, want empty overrides", created)
	}

	updated, err := profiles.SetAppAccessOverrides(ctx, testAppAccessSubject, testAppAccessApp,
		[]string{"users.list", "chat.delete", "users.list", " "}, []string{"files.upload"})
	if err != nil {
		t.Fatalf("SetAppAccessOverrides: %v", err)
	}
	if !slices.Equal(updated.DisabledOperations, []string{"chat.delete", "users.list"}) ||
		!slices.Equal(updated.ExtraOperations, []string{"files.upload"}) {
		t.Fatalf("updated profile = %#v", updated)
	}

	unchanged, err := profiles.EnsureAppAccessDefaults(ctx, testAppAccessSubject, testAppAccessApp)
	if err != nil {
		t.Fatalf("EnsureAppAccessDefaults existing: %v", err)
	}
	if !slices.Equal(unchanged.DisabledOperations, updated.DisabledOperations) ||
		!slices.Equal(unchanged.ExtraOperations, updated.ExtraOperations) {
		t.Fatalf("reconnect changed profile from %#v to %#v", updated, unchanged)
	}

	loaded, err := profiles.GetAppAccessProfile(ctx, testAppAccessSubject, testAppAccessApp)
	if err != nil {
		t.Fatalf("GetAppAccessProfile: %v", err)
	}
	if loaded.Legacy || !slices.Equal(loaded.DisabledOperations, updated.DisabledOperations) ||
		!slices.Equal(loaded.ExtraOperations, updated.ExtraOperations) {
		t.Fatalf("loaded profile = %#v, want %#v", loaded, updated)
	}

	cleared, err := profiles.SetAppAccessOverrides(ctx, testAppAccessSubject, testAppAccessApp, nil, nil)
	if err != nil {
		t.Fatalf("SetAppAccessOverrides empty: %v", err)
	}
	if len(cleared.DisabledOperations) != 0 || len(cleared.ExtraOperations) != 0 {
		t.Fatalf("cleared profile = %#v", cleared)
	}
}

func TestAppAccessProfileLegacyRecord(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, db := newTestServicesWithDB(t)
	profiles := svc.AppAccessProfiles

	if err := profiles.EnsureStore(ctx); err != nil {
		t.Fatalf("EnsureStore: %v", err)
	}
	if err := db.ObjectStore(coredata.StoreAppAccessProfiles).Put(ctx, idb.Record{
		"id":                   testAppAccessSubject + appAccessKeySep + testAppAccessApp,
		"subject_id":           testAppAccessSubject,
		"app":                  testAppAccessApp,
		"enabled_operations":   `["users.list","chat.post"]`,
		"defaults_initialized": true,
	}); err != nil {
		t.Fatalf("seed legacy record: %v", err)
	}

	legacy, err := profiles.GetAppAccessProfile(ctx, testAppAccessSubject, testAppAccessApp)
	if err != nil {
		t.Fatalf("GetAppAccessProfile: %v", err)
	}
	if !legacy.Legacy || !slices.Equal(legacy.LegacyEnabledOperations, []string{"chat.post", "users.list"}) {
		t.Fatalf("legacy profile = %#v", legacy)
	}

	ensured, err := profiles.EnsureAppAccessDefaults(ctx, testAppAccessSubject, testAppAccessApp)
	if err != nil {
		t.Fatalf("EnsureAppAccessDefaults: %v", err)
	}
	if !ensured.Legacy {
		t.Fatalf("Ensure converted a legacy profile: %#v", ensured)
	}

	// A legacy profile stays authoritative: operations added since are off.
	cat := appAccessCatalog("users.list", "chat.post", "new.op")
	if got := legacy.EnabledOperations(cat, core.AppAccessDefaults{}); !slices.Equal(got, []string{"chat.post", "users.list"}) {
		t.Fatalf("legacy enabled = %v", got)
	}

	saved, err := profiles.SetAppAccessOverrides(ctx, testAppAccessSubject, testAppAccessApp, []string{"chat.post"}, nil)
	if err != nil {
		t.Fatalf("SetAppAccessOverrides: %v", err)
	}
	if saved.Legacy {
		t.Fatalf("saved profile still legacy: %#v", saved)
	}
	reloaded, err := profiles.GetAppAccessProfile(ctx, testAppAccessSubject, testAppAccessApp)
	if err != nil {
		t.Fatalf("GetAppAccessProfile after save: %v", err)
	}
	if reloaded.Legacy || len(reloaded.LegacyEnabledOperations) != 0 || !slices.Equal(reloaded.DisabledOperations, []string{"chat.post"}) {
		t.Fatalf("reloaded profile = %#v", reloaded)
	}
	if got := reloaded.EnabledOperations(cat, core.AppAccessDefaults{}); !slices.Equal(got, []string{"new.op", "users.list"}) {
		t.Fatalf("converted enabled = %v", got)
	}
}

func TestAppAccessProfileFollowsNewDefaultOperations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, _ := newTestServicesWithDB(t)
	profiles := svc.AppAccessProfiles

	if _, err := profiles.SetAppAccessOverrides(ctx, testAppAccessSubject, testAppAccessApp, []string{"a"}, nil); err != nil {
		t.Fatalf("SetAppAccessOverrides: %v", err)
	}
	profile, err := profiles.GetAppAccessProfile(ctx, testAppAccessSubject, testAppAccessApp)
	if err != nil {
		t.Fatalf("GetAppAccessProfile: %v", err)
	}

	// The app later ships operation b, default-on.
	defaults := core.AppAccessDefaults{}
	if profile.Allows(catalog.CatalogOperation{ID: "a"}, defaults) {
		t.Fatal("disabled operation a is allowed")
	}
	if !profile.Allows(catalog.CatalogOperation{ID: "b"}, defaults) {
		t.Fatal("newly added default-on operation b is denied")
	}
}

func seedAppAccessPayload(t *testing.T, db *coretesting.StubIndexedDB, payload string) {
	t.Helper()
	ctx := context.Background()
	if err := db.ObjectStore(coredata.StoreAppAccessProfiles).Put(ctx, idb.Record{
		"id":                   testAppAccessSubject + appAccessKeySep + testAppAccessApp,
		"subject_id":           testAppAccessSubject,
		"app":                  testAppAccessApp,
		"enabled_operations":   payload,
		"defaults_initialized": true,
	}); err != nil {
		t.Fatalf("seed payload %q: %v", payload, err)
	}
}

func TestAppAccessProfileUnreadablePayloadFailsClosed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for name, payload := range map[string]string{
		"unknown future version": `{"v":2,"disabled":["x"]}`,
		"missing version":        `{"disabled":["x"]}`,
		"malformed":              `{`,
		"malformed legacy array": `["x"`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			svc, db := newTestServicesWithDB(t)
			if err := svc.AppAccessProfiles.EnsureStore(ctx); err != nil {
				t.Fatalf("EnsureStore: %v", err)
			}
			seedAppAccessPayload(t, db, payload)

			profile, err := svc.AppAccessProfiles.GetAppAccessProfile(ctx, testAppAccessSubject, testAppAccessApp)
			if err == nil {
				t.Fatalf("GetAppAccessProfile(%s) = %#v, want error", payload, profile)
			}
			if errors.Is(err, core.ErrNotFound) {
				t.Fatalf("unreadable payload reported as not found: %v", err)
			}
		})
	}
}

func TestAppAccessProfileBareArrayIsLegacy(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, db := newTestServicesWithDB(t)
	if err := svc.AppAccessProfiles.EnsureStore(ctx); err != nil {
		t.Fatalf("EnsureStore: %v", err)
	}
	seedAppAccessPayload(t, db, `["b.op","a.op"]`)

	profile, err := svc.AppAccessProfiles.GetAppAccessProfile(ctx, testAppAccessSubject, testAppAccessApp)
	if err != nil {
		t.Fatalf("GetAppAccessProfile: %v", err)
	}
	if !profile.Legacy || !slices.Equal(profile.LegacyEnabledOperations, []string{"a.op", "b.op"}) {
		t.Fatalf("profile = %#v, want legacy with a.op,b.op", profile)
	}
	if len(profile.DisabledOperations) != 0 || len(profile.ExtraOperations) != 0 {
		t.Fatalf("legacy profile carries decisions: %#v", profile)
	}
}

func TestAppAccessProfileDecisionsRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, _ := newTestServicesWithDB(t)
	if _, err := svc.AppAccessProfiles.SetAppAccessOverrides(ctx, testAppAccessSubject, testAppAccessApp,
		[]string{"chat.delete"}, []string{"files.upload"}); err != nil {
		t.Fatalf("SetAppAccessOverrides: %v", err)
	}

	loaded, err := svc.AppAccessProfiles.GetAppAccessProfile(ctx, testAppAccessSubject, testAppAccessApp)
	if err != nil {
		t.Fatalf("GetAppAccessProfile: %v", err)
	}
	if loaded.Legacy ||
		!slices.Equal(loaded.DisabledOperations, []string{"chat.delete"}) ||
		!slices.Equal(loaded.ExtraOperations, []string{"files.upload"}) {
		t.Fatalf("loaded = %#v, want decisions preserved", loaded)
	}
}
