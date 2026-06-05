package operator

import "testing"

func TestResolveArchiveSourceLocationPreservesMetadataQueryForRelativeRefs(t *testing.T) {
	t.Parallel()

	got, err := resolveArchiveSourceLocation(
		"https://storage.googleapis.com/snapshots/github.com/acme/providers/ref/app/demo/provider-release.yaml?sourceRef=ref",
		"validation-catalog.yaml",
		nil,
	)
	if err != nil {
		t.Fatalf("resolveArchiveSourceLocation: %v", err)
	}
	want := "https://storage.googleapis.com/snapshots/github.com/acme/providers/ref/app/demo/validation-catalog.yaml?sourceRef=ref"
	if got != want {
		t.Fatalf("resolveArchiveSourceLocation = %q, want %q", got, want)
	}
}

func TestResolveArchiveSourceLocationDoesNotOverrideExplicitQuery(t *testing.T) {
	t.Parallel()

	got, err := resolveArchiveSourceLocation(
		"https://storage.googleapis.com/snapshots/github.com/acme/providers/ref/app/demo/provider-release.yaml?sourceRef=ref",
		"validation-catalog.yaml?generation=1",
		nil,
	)
	if err != nil {
		t.Fatalf("resolveArchiveSourceLocation: %v", err)
	}
	want := "https://storage.googleapis.com/snapshots/github.com/acme/providers/ref/app/demo/validation-catalog.yaml?generation=1"
	if got != want {
		t.Fatalf("resolveArchiveSourceLocation = %q, want %q", got, want)
	}
}
