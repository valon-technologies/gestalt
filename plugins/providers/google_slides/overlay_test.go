package google_slides

import (
	"sort"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
	"github.com/valon-technologies/gestalt/internal/composite"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/plugins/providers/inventory"
	"github.com/valon-technologies/gestalt/plugins/providers/providertest"
)

var slidesOverlayOpIDs = []string{
	opCreatePresentation,
	opCreateSlide,
	opDeleteObject,
	opDuplicateObject,
	opUpdateSlidesPosition,
	opCreateShape,
	opInsertText,
	opDeleteText,
	opCreateImage,
	opCreateTable,
	opCreateLine,
	opReplaceAllText,
	opReplaceAllShapesWithImage,
	opUpdateTextStyle,
	opUpdateShapeProperties,
	opUpdatePageProperties,
	opUpdateParagraphStyle,
	opCreateParagraphBullets,
	opUpdatePageElementTransform,
	opUpdatePageElementsZOrder,
	opGroupObjects,
	opUngroupObjects,
	opUpdateTableCellProperties,
	opCreateStyledTextBox,
	opCreateBulletList,
	opCreateTitleSlide,
}

func TestOverlayCatalog(t *testing.T) {
	t.Parallel()

	var prov core.Provider = NewOverlayProvider()

	cp, ok := prov.(core.CatalogProvider)
	if !ok {
		t.Fatal("overlay does not implement CatalogProvider")
	}

	cat := cp.Catalog()
	if cat == nil {
		t.Fatal("Catalog() returned nil")
	}

	gotIDs := make([]string, len(cat.Operations))
	for i, op := range cat.Operations {
		gotIDs[i] = op.ID
	}
	sort.Strings(gotIDs)

	wantIDs := make([]string, len(slidesOverlayOpIDs))
	copy(wantIDs, slidesOverlayOpIDs)
	sort.Strings(wantIDs)

	if len(gotIDs) != len(wantIDs) {
		t.Fatalf("overlay catalog ops = %d, want %d\ngot:  %v\nwant: %v", len(gotIDs), len(wantIDs), gotIDs, wantIDs)
	}
	for i := range gotIDs {
		if gotIDs[i] != wantIDs[i] {
			t.Errorf("overlay catalog op[%d] = %q, want %q", i, gotIDs[i], wantIDs[i])
		}
	}
}

func TestOverlayComposite(t *testing.T) {
	t.Parallel()

	inv, err := inventory.Load()
	if err != nil {
		t.Fatalf("inventory.Load: %v", err)
	}
	spec := inv.Providers["google_slides"]

	def := providertest.ParseDefinition(t, definitionYAML)
	base := providertest.BuildProvider(t, def, config.IntegrationDef{
		ClientID: dummyClientID, ClientSecret: dummyClientSecret,
	})

	overlay := NewOverlayProvider()
	comp := composite.NewOverlay("google_slides", base, overlay)

	ops := comp.ListOperations()
	if len(ops) != len(spec.Operations) {
		t.Fatalf("composite ListOperations() = %d, want %d", len(ops), len(spec.Operations))
	}

	gotNames := make([]string, len(ops))
	for i, op := range ops {
		gotNames[i] = op.Name
	}
	sort.Strings(gotNames)

	wantNames := make([]string, len(spec.Operations))
	copy(wantNames, spec.Operations)
	sort.Strings(wantNames)

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

	catIDs := make([]string, len(cat.Operations))
	for i, op := range cat.Operations {
		catIDs[i] = op.ID
	}
	sort.Strings(catIDs)

	if len(catIDs) != len(wantNames) {
		t.Fatalf("composite catalog ops = %d, want %d", len(catIDs), len(wantNames))
	}
	for i := range catIDs {
		if catIDs[i] != wantNames[i] {
			t.Errorf("composite catalog op[%d] = %q, want %q", i, catIDs[i], wantNames[i])
		}
	}

	overlaySet := map[string]struct{}{}
	for _, id := range slidesOverlayOpIDs {
		overlaySet[id] = struct{}{}
	}

	for _, op := range cat.Operations {
		if _, isOverlay := overlaySet[op.ID]; isOverlay {
			if op.Transport != catalog.TransportPlugin {
				t.Errorf("overlay op %q transport = %q, want %q", op.ID, op.Transport, catalog.TransportPlugin)
			}
		}
	}
}
