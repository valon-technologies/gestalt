package core

import (
	"context"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core/catalog"
)

type scenarioOp struct {
	id      string
	visible bool
}

func scenarioCatalog(ops []scenarioOp) *catalog.Catalog {
	cat := &catalog.Catalog{}
	for _, op := range ops {
		cat.Operations = append(cat.Operations, accessOp(op.id, boolPtr(op.visible)))
	}
	return cat
}

// TestLegacyProfileMeaningFollowsUserHistory states, per scenario, what the
// user did when they saved and what must be true afterwards, without
// reference to how the conversion is computed.
func TestLegacyProfileMeaningFollowsUserHistory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// existedWhenSaved are the operations the app offered at save time.
		existedWhenSaved []string
		saved            []string
		// today is the catalog and defaults when the profile is read.
		today    []scenarioOp
		defaults AppAccessDefaults
		// later, when set, is a catalog the app returns to after the read;
		// the profile resolved against today is then evaluated against it.
		later []scenarioOp
		want  map[string]bool
	}{
		{
			name:             "operation user left off stays off even if now default-on",
			existedWhenSaved: []string{"read", "write"},
			saved:            []string{"read"},
			today:            []scenarioOp{{"read", true}, {"write", true}},
			want:             map[string]bool{"read": true, "write": false},
		},
		{
			name:             "operation added after the save follows its default",
			existedWhenSaved: []string{"read"},
			saved:            []string{"read"},
			today:            []scenarioOp{{"read", true}, {"fresh", true}, {"fresh-hidden", false}},
			want:             map[string]bool{"read": true, "fresh": true, "fresh-hidden": false},
		},
		{
			name:             "hidden operation the user enabled stays on",
			existedWhenSaved: []string{"read", "admin"},
			saved:            []string{"read", "admin"},
			today:            []scenarioOp{{"read", true}, {"admin", false}},
			want:             map[string]bool{"read": true, "admin": true},
		},
		{
			name:             "hidden operation the user never enabled stays off",
			existedWhenSaved: []string{"read", "admin"},
			saved:            []string{"read"},
			today:            []scenarioOp{{"read", true}, {"admin", false}},
			want:             map[string]bool{"read": true, "admin": false},
		},
		{
			name:             "operation the user turned off is still off when it was removed then returns",
			existedWhenSaved: []string{"read", "write"},
			saved:            []string{"read"},
			today:            []scenarioOp{{"read", true}},
			later:            []scenarioOp{{"read", true}, {"write", true}},
			want:             map[string]bool{"read": true, "write": false},
		},
		{
			name:             "hidden operation the user enabled is still on when it was removed then returns",
			existedWhenSaved: []string{"read", "admin"},
			saved:            []string{"read", "admin"},
			today:            []scenarioOp{{"read", true}},
			later:            []scenarioOp{{"read", true}, {"admin", false}},
			want:             map[string]bool{"read": true, "admin": true},
		},
		{
			name:             "configured defaults: listed op the user dropped stays off, unlisted op the user enabled stays on",
			existedWhenSaved: []string{"a", "b", "c"},
			saved:            []string{"a", "c"},
			today:            []scenarioOp{{"a", true}, {"b", true}, {"c", true}, {"d", true}},
			defaults:         AppAccessDefaults{Configured: true, Listed: []string{"a", "b", "d"}},
			want:             map[string]bool{"a": true, "b": false, "c": true, "d": true},
		},
		{
			name:             "configured empty defaults: newly added operations stay off",
			existedWhenSaved: []string{"a"},
			saved:            []string{"a"},
			today:            []scenarioOp{{"a", true}, {"fresh", true}},
			defaults:         AppAccessDefaults{Configured: true},
			want:             map[string]bool{"a": true, "fresh": false},
		},
		{
			name:             "graphql the user never enabled stays off",
			existedWhenSaved: []string{"read"},
			saved:            []string{"read"},
			today:            []scenarioOp{{"read", true}},
			want:             map[string]bool{"read": true, GraphQLCapabilityID: false},
		},
		{
			name:             "graphql the user enabled stays on",
			existedWhenSaved: []string{"read"},
			saved:            []string{"read", GraphQLCapabilityID},
			today:            []scenarioOp{{"read", true}},
			want:             map[string]bool{"read": true, GraphQLCapabilityID: true},
		},
		{
			name:             "whitespace in stored ids does not change meaning",
			existedWhenSaved: []string{" read ", "write"},
			saved:            []string{" read "},
			today:            []scenarioOp{{"read", true}, {"write", true}},
			want:             map[string]bool{"read": true, "write": false},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			legacy := NewLegacyAppAccessProfile("user", "app", tt.saved, time.Unix(100, 0))
			history := fakeOperationHistory{ids: tt.existedWhenSaved, known: true}
			got, reason, err := ResolveLegacyAppAccessProfile(context.Background(), history, legacy, scenarioCatalog(tt.today), tt.defaults)
			if err != nil || reason != LegacyConverted {
				t.Fatalf("resolve: reason=%q err=%v", reason, err)
			}
			if err := got.Validate(); err != nil {
				t.Fatalf("converted profile invalid: %v", err)
			}
			evalOps := tt.today
			if tt.later != nil {
				evalOps = tt.later
			}
			evalCat := scenarioCatalog(evalOps)
			for id, want := range tt.want {
				op := accessOp(id, boolPtr(true))
				for _, c := range evalCat.Operations {
					if c.ID == id {
						op = c
					}
				}
				if gotAllowed := got.Allows(op, tt.defaults); gotAllowed != want {
					t.Errorf("Allows(%q) = %v, want %v", id, gotAllowed, want)
				}
			}
		})
	}
}

func TestAppAccessProfileInvariantAndConstructors(t *testing.T) {
	t.Parallel()

	stray := &AppAccessProfile{Legacy: true, LegacyEnabledOperations: []string{"a"}, DisabledOperations: []string{"b"}}
	if stray.Validate() == nil {
		t.Fatal("legacy profile with stray disabled operations must fail Validate")
	}
	stray = &AppAccessProfile{Legacy: true, ExtraOperations: []string{"b"}}
	if stray.Validate() == nil {
		t.Fatal("legacy profile with stray extra operations must fail Validate")
	}
	if (&AppAccessProfile{DisabledOperations: []string{"a"}, LegacyEnabledOperations: []string{"b"}}).Validate() == nil {
		t.Fatal("relative profile holding a legacy list must fail Validate")
	}

	at := time.Unix(5, 0)
	relative := NewRelativeAppAccessProfile("u", "app", []string{" b", "a", "b"}, []string{"c", "c "}, at)
	if err := relative.Validate(); err != nil || relative.Legacy {
		t.Fatalf("relative: %#v err=%v", relative, err)
	}
	if got := relative.DisabledOperations; len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("disabled = %v, want [a b]", got)
	}
	if got := relative.ExtraOperations; len(got) != 1 || got[0] != "c" {
		t.Fatalf("extra = %v, want [c]", got)
	}
	legacy := NewLegacyAppAccessProfile("u", "app", []string{"z", " a", "z"}, at)
	if err := legacy.Validate(); err != nil || !legacy.Legacy || len(legacy.LegacyEnabledOperations) != 2 || legacy.LegacyEnabledOperations[0] != "a" {
		t.Fatalf("legacy: %#v err=%v", legacy, err)
	}
}

func TestAllowsUnlistedAgreesWithAllows(t *testing.T) {
	t.Parallel()

	var nilProfile *AppAccessProfile
	if !nilProfile.AllowsUnlisted("x") {
		t.Fatal("nil profile allows everything")
	}
	relative := NewRelativeAppAccessProfile("u", "app", []string{"off"}, []string{" on "}, time.Time{})
	if !relative.AllowsUnlisted("on") || relative.AllowsUnlisted("off") || relative.AllowsUnlisted("never-chosen") {
		t.Fatal("relative profile allows an unlisted operation only when extra")
	}
	legacy := NewLegacyAppAccessProfile("u", "app", []string{"on"}, time.Time{})
	if !legacy.AllowsUnlisted(" on ") || legacy.AllowsUnlisted("other") {
		t.Fatal("legacy profile allows an unlisted operation only when enabled")
	}
	hiddenOp := accessOp("on", boolPtr(false))
	if relative.Allows(hiddenOp, AppAccessDefaults{}) != relative.AllowsUnlisted("on") {
		t.Fatal("Allows and AllowsUnlisted drifted for a non-default operation")
	}
}

func TestMergeAppAccessIDsAndDeniedOperations(t *testing.T) {
	t.Parallel()

	got := MergeAppAccessIDs([]string{"b", " a", ""}, []string{"a", "c"})
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("merge = %v", got)
	}
	cat := scenarioCatalog([]scenarioOp{{"a", true}, {"b", true}, {"c", false}})
	profile := NewRelativeAppAccessProfile("u", "app", []string{"a"}, nil, time.Time{})
	if denied := profile.DeniedOperations(cat, AppAccessDefaults{}); len(denied) != 2 || denied[0] != "a" || denied[1] != "c" {
		t.Fatalf("denied = %v", denied)
	}
}

func TestResolveLegacyAppAccessProfileReportsReason(t *testing.T) {
	t.Parallel()

	cat := scenarioCatalog([]scenarioOp{{"kept", true}})
	legacy := NewLegacyAppAccessProfile("u", "app", []string{"kept"}, time.Time{})
	tests := []struct {
		name    string
		history OperationHistory
		profile *AppAccessProfile
		want    LegacyKeptReason
	}{
		{"not legacy", fakeOperationHistory{known: true}, NewRelativeAppAccessProfile("u", "app", nil, nil, time.Time{}), LegacyKeptNotLegacy},
		{"no history source", nil, legacy, LegacyKeptNoHistory},
		{"history unknown", fakeOperationHistory{}, legacy, LegacyKeptNoHistory},
		{"history error", fakeOperationHistory{known: true, err: context.Canceled}, legacy, LegacyKeptHistoryError},
		{"history incomplete", fakeOperationHistory{known: true, ids: []string{"other"}}, legacy, LegacyKeptHistoryIncomplete},
		{"converted", fakeOperationHistory{known: true, ids: []string{"kept"}}, legacy, LegacyConverted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, reason, _ := ResolveLegacyAppAccessProfile(context.Background(), tt.history, tt.profile, cat, AppAccessDefaults{})
			if reason != tt.want {
				t.Fatalf("reason = %q, want %q", reason, tt.want)
			}
		})
	}
}
