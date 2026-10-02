package core

import (
	"slices"
	"testing"

	"github.com/valon-technologies/gestalt/server/core/catalog"
)

func accessOp(id string, visible *bool) catalog.CatalogOperation {
	return catalog.CatalogOperation{ID: id, Visible: visible}
}

func boolPtr(v bool) *bool { return &v }

func TestAppAccessProfileAllows(t *testing.T) {
	t.Parallel()

	hidden := accessOp("hidden", boolPtr(false))
	visible := accessOp("visible", nil)
	other := accessOp("other", nil)
	configured := AppAccessDefaults{Configured: true, Listed: []string{"visible"}}

	tests := []struct {
		name     string
		profile  *AppAccessProfile
		defaults AppAccessDefaults
		op       catalog.CatalogOperation
		want     bool
	}{
		{"nil profile allows default-on", nil, AppAccessDefaults{}, visible, true},
		{"nil profile allows hidden", nil, AppAccessDefaults{}, hidden, true},
		{"empty profile follows visible default", &AppAccessProfile{}, AppAccessDefaults{}, visible, true},
		{"empty profile denies hidden", &AppAccessProfile{}, AppAccessDefaults{}, hidden, false},
		{"disabled default-on is denied", &AppAccessProfile{DisabledOperations: []string{"visible"}}, AppAccessDefaults{}, visible, false},
		{"unlisted default-on stays allowed", &AppAccessProfile{DisabledOperations: []string{"visible"}}, AppAccessDefaults{}, other, true},
		{"extra enables non-default", &AppAccessProfile{ExtraOperations: []string{"hidden"}}, AppAccessDefaults{}, hidden, true},
		{"configured defaults allow listed", &AppAccessProfile{}, configured, visible, true},
		{"configured defaults deny unlisted", &AppAccessProfile{}, configured, other, false},
		{"configured extra enables unlisted", &AppAccessProfile{ExtraOperations: []string{"other"}}, configured, other, true},
		{"configured disabled denies listed", &AppAccessProfile{DisabledOperations: []string{"visible"}}, configured, visible, false},
		{"configured empty list denies all", &AppAccessProfile{}, AppAccessDefaults{Configured: true}, visible, false},
		{"legacy allows its list", &AppAccessProfile{Legacy: true, LegacyEnabledOperations: []string{"hidden"}}, AppAccessDefaults{}, hidden, true},
		{"legacy denies operations added later", &AppAccessProfile{Legacy: true, LegacyEnabledOperations: []string{"hidden"}}, AppAccessDefaults{}, visible, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.profile.Allows(tt.op, tt.defaults); got != tt.want {
				t.Fatalf("Allows(%q) = %v, want %v", tt.op.ID, got, tt.want)
			}
		})
	}
}

func TestAppAccessEnabledOperationsAndOverridesRoundTrip(t *testing.T) {
	t.Parallel()

	cat := &catalog.Catalog{Operations: []catalog.CatalogOperation{
		accessOp("b", nil),
		accessOp("a", nil),
		accessOp("c", boolPtr(false)),
		accessOp("d", boolPtr(false)),
	}}

	var nilProfile *AppAccessProfile
	if got := nilProfile.EnabledOperations(cat, AppAccessDefaults{}); !slices.Equal(got, []string{"a", "b", "c", "d"}) {
		t.Fatalf("nil profile enabled = %v", got)
	}
	if got := nilProfile.EnabledOperations(nil, AppAccessDefaults{}); got != nil {
		t.Fatalf("nil catalog enabled = %v", got)
	}
	if got := (AppAccessDefaults{}).Operations(cat); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("default operations = %v", got)
	}
	configured := AppAccessDefaults{Configured: true, Listed: []string{"d"}}
	if got := configured.Operations(cat); !slices.Equal(got, []string{"d"}) {
		t.Fatalf("configured operations = %v", got)
	}

	for _, defaults := range []AppAccessDefaults{{}, configured} {
		want := []string{"a", "c"}
		disabled, extra := AppAccessOverrides(append(want, "unknown"), cat, defaults)
		profile := &AppAccessProfile{DisabledOperations: disabled, ExtraOperations: extra}
		if got := profile.EnabledOperations(cat, defaults); !slices.Equal(got, want) {
			t.Fatalf("defaults %+v: round trip enabled = %v (disabled=%v extra=%v), want %v", defaults, got, disabled, extra, want)
		}
		for _, id := range append(disabled, extra...) {
			if id == "unknown" {
				t.Fatalf("unknown id leaked into overrides: disabled=%v extra=%v", disabled, extra)
			}
		}
	}

	disabled, extra := AppAccessOverrides([]string{"a", "b"}, cat, AppAccessDefaults{})
	if len(disabled) != 0 || len(extra) != 0 {
		t.Fatalf("selecting exactly the defaults produced overrides: %v %v", disabled, extra)
	}
	if disabled, extra := AppAccessOverrides([]string{"a"}, nil, AppAccessDefaults{}); disabled != nil || extra != nil {
		t.Fatalf("nil catalog overrides = %v %v", disabled, extra)
	}

	legacy := &AppAccessProfile{Legacy: true, LegacyEnabledOperations: []string{"d", "gone"}}
	if got := legacy.EnabledOperations(cat, AppAccessDefaults{}); !slices.Equal(got, []string{"d"}) {
		t.Fatalf("legacy enabled = %v", got)
	}
}
