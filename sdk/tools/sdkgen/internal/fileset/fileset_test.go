package fileset

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newSet(t *testing.T, files map[string]string) *FileSet {
	t.Helper()
	set := New()
	for path, content := range files {
		if err := set.Add(path, []byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	return set
}

func TestReconcileWritesHeaderedFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	set := newSet(t, map[string]string{"pkg/widget.go": "package pkg\n"})

	report, err := Reconcile(root, set, Slash, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Written) != 1 || len(report.Deleted) != 0 {
		t.Fatalf("report = %+v", report)
	}
	got, err := os.ReadFile(filepath.Join(root, "pkg", "widget.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), "// "+Marker+"\n\n") {
		t.Errorf("missing slash header: %q", got)
	}
	if !IsGenerated(got) {
		t.Error("written file not detected as generated")
	}

	// A second reconcile is a no-op.
	report, err = Reconcile(root, set, Slash, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Written) != 0 || len(report.Deleted) != 0 {
		t.Fatalf("second reconcile not idempotent: %+v", report)
	}
}

func TestReconcileRemovesStaleGeneratedOnly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	first := newSet(t, map[string]string{
		"keep.py":       "keep = 1\n",
		"old/gone.py":   "gone = 1\n",
		"old/stays.txt": "ignored\n",
	})
	if _, err := Reconcile(root, first, Hash, nil); err != nil {
		t.Fatal(err)
	}
	// stays.txt is generated this once; overwrite it as handwritten.
	write(t, root, "old/stays.txt", "handwritten\n")
	write(t, root, "notes.md", "handwritten notes\n")

	second := newSet(t, map[string]string{"keep.py": "keep = 1\n"})
	report, err := Reconcile(root, second, Hash, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Deleted) != 1 || report.Deleted[0] != "old/gone.py" {
		t.Fatalf("deleted = %v, want [old/gone.py]", report.Deleted)
	}
	if _, err := os.Stat(filepath.Join(root, "old", "stays.txt")); err != nil {
		t.Error("handwritten file under generated dir was touched")
	}
	if _, err := os.Stat(filepath.Join(root, "notes.md")); err != nil {
		t.Error("handwritten file was touched")
	}
}

func TestReconcilePrunesEmptyDirs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	first := newSet(t, map[string]string{"deep/nested/file.go": "package nested\n"})
	if _, err := Reconcile(root, first, Slash, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Reconcile(root, New(), Slash, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "deep")); !os.IsNotExist(err) {
		t.Error("empty directories not pruned")
	}
}

func TestCheckReportsDriftWithoutMutating(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	set := newSet(t, map[string]string{
		"ok.go":       "package ok\n",
		"modified.go": "package modified\n",
		"missing.go":  "package missing\n",
	})
	if _, err := Reconcile(root, newSet(t, map[string]string{
		"ok.go":       "package ok\n",
		"modified.go": "package old\n",
		"stale.go":    "package stale\n",
	}), Slash, nil); err != nil {
		t.Fatal(err)
	}
	write(t, root, "handwritten.go", "package handwritten\n")

	drift, err := Check(root, set, Slash, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]DriftKind{
		"modified.go": Modified,
		"missing.go":  Missing,
		"stale.go":    Stale,
	}
	if len(drift) != len(want) {
		t.Fatalf("drift = %+v, want %v", drift, want)
	}
	for _, entry := range drift {
		if want[entry.Path] != entry.Kind {
			t.Errorf("drift %s = %v, want %v", entry.Path, entry.Kind, want[entry.Path])
		}
	}

	// Check must not have mutated anything.
	if got, _ := os.ReadFile(filepath.Join(root, "stale.go")); !IsGenerated(got) {
		t.Error("check mutated the tree")
	}
}

func TestSyncDirsMatchesCompare(t *testing.T) {
	t.Parallel()
	want := t.TempDir()
	got := t.TempDir()
	write(t, want, "v1/new.txt", "new\n")
	write(t, want, "v1/changed.txt", "after\n")
	write(t, got, "v1/changed.txt", "before\n")
	write(t, got, "v1/stale.txt", "stale\n")
	write(t, got, "v1/handwritten.note", "kept\n")

	ignore := func(rel string) bool { return strings.HasSuffix(rel, ".note") }
	if err := SyncDirs(want, got, ignore); err != nil {
		t.Fatal(err)
	}

	drift, err := CompareDirs(want, got, true, ignore)
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 0 {
		t.Errorf("drift after sync = %+v, want none", drift)
	}
	if _, err := os.Stat(filepath.Join(got, "v1", "stale.txt")); !os.IsNotExist(err) {
		t.Error("stale file not removed by sync")
	}
	if content, err := os.ReadFile(filepath.Join(got, "v1", "handwritten.note")); err != nil || string(content) != "kept\n" {
		t.Error("ignored file was touched by sync")
	}
}

func TestCompareDirs(t *testing.T) {
	t.Parallel()
	want := t.TempDir()
	got := t.TempDir()
	write(t, want, "v1/same.txt", "same\n")
	write(t, got, "v1/same.txt", "same\n")
	write(t, want, "v1/changed.txt", "new\n")
	write(t, got, "v1/changed.txt", "old\n")
	write(t, want, "v1/missing.txt", "missing\n")
	write(t, got, "v1/extra.txt", "extra\n")
	write(t, got, "v1/__pycache__/cache.pyc", "cache\n")

	drift, err := CompareDirs(want, got, true, func(rel string) bool {
		return strings.Contains(rel, "__pycache__")
	})
	if err != nil {
		t.Fatal(err)
	}
	wantDrift := map[string]DriftKind{
		"v1/changed.txt": Modified,
		"v1/missing.txt": Missing,
		"v1/extra.txt":   Stale,
	}
	if len(drift) != len(wantDrift) {
		t.Fatalf("drift = %+v, want %v", drift, wantDrift)
	}
	for _, entry := range drift {
		if wantDrift[entry.Path] != entry.Kind {
			t.Errorf("drift %s = %v, want %v", entry.Path, entry.Kind, wantDrift[entry.Path])
		}
	}
}
