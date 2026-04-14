package fileapi

import (
	"testing"
)

func TestNormalizeType(t *testing.T) {
	if got := NormalizeType("TEXT/PLAIN"); got != "text/plain" {
		t.Fatalf("NormalizeType = %q, want text/plain", got)
	}
	if got := NormalizeType("text/plain\x19"); got != "" {
		t.Fatalf("NormalizeType invalid = %q, want empty", got)
	}
}

func TestSliceBounds(t *testing.T) {
	start, end := int64(2), int64(5)
	gotStart, gotEnd := SliceBounds(10, &start, &end)
	if gotStart != 2 || gotEnd != 5 {
		t.Fatalf("SliceBounds = (%d, %d), want (2, 5)", gotStart, gotEnd)
	}

	negStart, negEnd := int64(-4), int64(-1)
	gotStart, gotEnd = SliceBounds(10, &negStart, &negEnd)
	if gotStart != 6 || gotEnd != 9 {
		t.Fatalf("SliceBounds negative = (%d, %d), want (6, 9)", gotStart, gotEnd)
	}
}

func TestSliceBytes(t *testing.T) {
	start, end := int64(1), int64(4)
	if got := string(SliceBytes([]byte("hello"), &start, &end)); got != "ell" {
		t.Fatalf("SliceBytes = %q, want ell", got)
	}
}

func TestResolveLastModified(t *testing.T) {
	if got := ResolveLastModified(1234); got != 1234 {
		t.Fatalf("ResolveLastModified = %d, want 1234", got)
	}
	if got := ResolveLastModified(0); got <= 0 {
		t.Fatalf("ResolveLastModified default = %d, want > 0", got)
	}
}

func TestPackageDataURL(t *testing.T) {
	got := PackageDataURL("text/plain", []byte("hello"))
	want := "data:text/plain;base64,aGVsbG8="
	if got != want {
		t.Fatalf("PackageDataURL = %q, want %q", got, want)
	}
}
