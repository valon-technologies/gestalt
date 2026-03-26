package slack

import (
	"sort"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
	"github.com/valon-technologies/gestalt/internal/composite"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/provider"
	"github.com/valon-technologies/gestalt/plugins/providers/inventory"
	"github.com/valon-technologies/gestalt/plugins/providers/providertest"
)

func TestOverlayProvider_Operations(t *testing.T) {
	t.Parallel()

	overlay := NewOverlayProvider()
	ops := overlay.ListOperations()

	if len(ops) != 3 {
		t.Fatalf("ListOperations() = %d, want 3", len(ops))
	}

	got := make([]string, len(ops))
	for i, op := range ops {
		got[i] = op.Name
	}
	sort.Strings(got)

	want := overlayOperationNames()
	sort.Strings(want)

	for i := range got {
		if got[i] != want[i] {
			t.Errorf("operation[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestOverlayProvider_Catalog(t *testing.T) {
	t.Parallel()

	overlay := NewOverlayProvider()
	cat := overlay.Catalog()
	if cat == nil {
		t.Fatal("Catalog() returned nil")
	}

	gotIDs := make([]string, len(cat.Operations))
	for i, op := range cat.Operations {
		gotIDs[i] = op.ID
	}
	sort.Strings(gotIDs)

	wantIDs := overlayOperationNames()
	sort.Strings(wantIDs)

	if len(gotIDs) != len(wantIDs) {
		t.Fatalf("catalog ops = %d, want %d", len(gotIDs), len(wantIDs))
	}
	for i := range gotIDs {
		if gotIDs[i] != wantIDs[i] {
			t.Errorf("catalog op[%d] = %q, want %q", i, gotIDs[i], wantIDs[i])
		}
	}
}

func TestCompositeOverlay_Slack(t *testing.T) {
	t.Parallel()

	inv, err := inventory.Load()
	if err != nil {
		t.Fatalf("inventory.Load: %v", err)
	}
	spec := inv.Providers["slack"]

	def := providertest.ParseDefinition(t, definitionYAML)
	auth := newSlackAuthHandler("dummy-id", "dummy-secret", "https://dummy.example.com/callback", def.Auth.Scopes)
	base := providertest.BuildProvider(t, def, config.IntegrationDef{
		ClientID:     "dummy-id",
		ClientSecret: "dummy-secret",
	}, provider.WithAuthHandler(auth))

	overlay := NewOverlayProvider()
	comp := composite.NewOverlay("slack", base, overlay)

	ops := comp.ListOperations()
	gotNames := make([]string, len(ops))
	for i, op := range ops {
		gotNames[i] = op.Name
	}
	sort.Strings(gotNames)

	wantNames := make([]string, len(spec.Operations))
	copy(wantNames, spec.Operations)
	sort.Strings(wantNames)

	if len(gotNames) != len(wantNames) {
		t.Fatalf("composite ops = %d, want %d\ngot:  %v\nwant: %v", len(gotNames), len(wantNames), gotNames, wantNames)
	}
	for i := range gotNames {
		if gotNames[i] != wantNames[i] {
			t.Errorf("composite op[%d] = %q, want %q", i, gotNames[i], wantNames[i])
		}
	}

	cp, ok := comp.(core.CatalogProvider)
	if !ok {
		t.Fatal("composite does not implement CatalogProvider")
	}
	cat := cp.Catalog()
	if cat == nil {
		t.Fatal("composite Catalog() returned nil")
	}

	gotCatIDs := make([]string, len(cat.Operations))
	for i, op := range cat.Operations {
		gotCatIDs[i] = op.ID
	}
	sort.Strings(gotCatIDs)

	if len(gotCatIDs) != len(wantNames) {
		t.Fatalf("composite catalog ops = %d, want %d\ngot:  %v\nwant: %v", len(gotCatIDs), len(wantNames), gotCatIDs, wantNames)
	}
	for i := range gotCatIDs {
		if gotCatIDs[i] != wantNames[i] {
			t.Errorf("composite catalog op[%d] = %q, want %q", i, gotCatIDs[i], wantNames[i])
		}
	}

	overlaySet := map[string]struct{}{}
	for _, name := range overlayOperationNames() {
		overlaySet[name] = struct{}{}
	}
	for _, op := range cat.Operations {
		_, isOverlay := overlaySet[op.ID]
		if isOverlay && op.Transport != catalog.TransportPlugin {
			t.Errorf("overlay op %q transport = %q, want %q", op.ID, op.Transport, catalog.TransportPlugin)
		}
		if !isOverlay && op.Transport == catalog.TransportPlugin {
			t.Errorf("base op %q should not have transport %q", op.ID, catalog.TransportPlugin)
		}
	}

	providertest.CheckOAuth(t, comp)
}
