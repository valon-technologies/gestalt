package toolschema

import (
	"strings"
	"testing"
)

func TestNormalizePropertyName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw      string
		wantName string
		wantWire string
	}{
		{raw: "limit", wantName: "limit"},
		{raw: "$select", wantName: "dollar_select", wantWire: "$select"},
		{raw: "@odata.type", wantName: "at_odata.type", wantWire: "@odata.type"},
		{raw: "page[size]", wantName: "page_size", wantWire: "page[size]"},
		{raw: "'x-Cwd'", wantName: "x-Cwd", wantWire: "'x-Cwd'"},
		{raw: "", wantName: "param", wantWire: ""},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()

			gotName, gotWire := NormalizePropertyName(tc.raw)
			if gotName != tc.wantName {
				t.Fatalf("name = %q, want %q", gotName, tc.wantName)
			}
			if gotWire != tc.wantWire {
				t.Fatalf("wireName = %q, want %q", gotWire, tc.wantWire)
			}
			if !ValidPropertyName(gotName) {
				t.Fatalf("normalized name %q is not valid", gotName)
			}
		})
	}
}

func TestNameAllocatorHandlesCollisions(t *testing.T) {
	t.Parallel()

	allocator := NewNameAllocator()

	first := allocator.Allocate("$select")
	if first.Name != "dollar_select" || first.WireName != "$select" {
		t.Fatalf("first = %#v, want dollar_select with $select wire name", first)
	}

	second := allocator.Allocate("dollar_select")
	if second.Name != "dollar_select_2" || second.WireName != "dollar_select" {
		t.Fatalf("second = %#v, want dollar_select_2 with dollar_select wire name", second)
	}
}

func TestNameAllocatorKeepsLongNamesValidAndUnique(t *testing.T) {
	t.Parallel()

	allocator := NewNameAllocator()
	raw := strings.Repeat("a", 80)

	first := allocator.Allocate(raw)
	second := allocator.Allocate(raw)

	if len(first.Name) > MaxPropertyNameLength {
		t.Fatalf("first name length = %d, want <= %d", len(first.Name), MaxPropertyNameLength)
	}
	if len(second.Name) > MaxPropertyNameLength {
		t.Fatalf("second name length = %d, want <= %d", len(second.Name), MaxPropertyNameLength)
	}
	if first.Name == second.Name {
		t.Fatalf("expected unique names, got %q twice", first.Name)
	}
	if !ValidPropertyName(first.Name) || !ValidPropertyName(second.Name) {
		t.Fatalf("names should be valid: %#v %#v", first, second)
	}
}

func TestValidPropertyName(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"limit", "page_size", "at_odata.type", "x-Cwd"} {
		if !ValidPropertyName(name) {
			t.Fatalf("%q should be valid", name)
		}
	}
	for _, name := range []string{"", "$select", "@odata.type", strings.Repeat("a", 65)} {
		if ValidPropertyName(name) {
			t.Fatalf("%q should be invalid", name)
		}
	}
}
