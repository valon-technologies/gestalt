package appregistry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreMaterializationBackup(t *testing.T) {
	t.Parallel()

	t.Run("restores_when_backup_exists", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		materialized := filepath.Join(dir, "g-issues", "0.0.0-snapshot.v1")
		backup := materialized + ".backup"
		if err := os.MkdirAll(materialized, 0o755); err != nil {
			t.Fatalf("MkdirAll materialized: %v", err)
		}
		if err := os.WriteFile(filepath.Join(materialized, "content.txt"), []byte("old"), 0o644); err != nil {
			t.Fatalf("WriteFile old: %v", err)
		}
		if err := os.Rename(materialized, backup); err != nil {
			t.Fatalf("Rename to backup: %v", err)
		}
		if err := os.MkdirAll(materialized, 0o755); err != nil {
			t.Fatalf("MkdirAll staged materialized: %v", err)
		}
		if err := os.WriteFile(filepath.Join(materialized, "content.txt"), []byte("new"), 0o644); err != nil {
			t.Fatalf("WriteFile staged: %v", err)
		}

		restoreMaterializationBackup(materialized, backup)

		if _, err := os.Stat(backup); !os.IsNotExist(err) {
			t.Fatalf("backup should be removed, stat err = %v", err)
		}
		if data, err := os.ReadFile(filepath.Join(materialized, "content.txt")); err != nil {
			t.Fatalf("ReadFile restored materialized: %v", err)
		} else if string(data) != "old" {
			t.Fatalf("restored content = %q, want old", data)
		}
	})

	t.Run("removes_materialized_when_no_backup", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		materialized := filepath.Join(dir, "g-issues", "0.0.0-snapshot.v1")
		backup := materialized + ".backup"
		if err := os.MkdirAll(materialized, 0o755); err != nil {
			t.Fatalf("MkdirAll materialized: %v", err)
		}
		if err := os.WriteFile(filepath.Join(materialized, "orphan.txt"), []byte("orphan"), 0o644); err != nil {
			t.Fatalf("WriteFile orphan: %v", err)
		}

		restoreMaterializationBackup(materialized, backup)

		if _, err := os.Stat(materialized); !os.IsNotExist(err) {
			t.Fatalf("materialized should be removed on first-install rollback, stat err = %v", err)
		}
	})
}
