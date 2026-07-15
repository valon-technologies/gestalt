package operator

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckLockAtPathsEmitsLockProgressInOrder(t *testing.T) {
	t.Parallel()

	var events []LifecycleProgressEvent
	lc := NewLifecycle().WithProgress(func(event LifecycleProgressEvent) {
		events = append(events, event)
	})
	dir := t.TempDir()
	configPath := filepath.Join(dir, "gestaltd.yaml")
	lockfilePath := filepath.Join(dir, LockfileName)
	artifactsDir := filepath.Join(dir, "artifacts")
	configYAML := fmt.Sprintf("apiVersion: gestaltd.config/v8\n%s\nserver:\n  providers:\n    indexeddb: sqlite\n  artifactsDir: %s\n  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n", requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")), filepath.ToSlash(artifactsDir))
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := lc.LockAtPaths([]string{configPath}, lockfilePath, artifactsDir); err != nil {
		t.Fatalf("LockAtPaths: %v", err)
	}
	if err := lc.CheckLockAtPaths([]string{configPath}, lockfilePath, artifactsDir); err != nil {
		t.Fatalf("CheckLockAtPaths: %v", err)
	}
	lockStarted, lockCompleted := -1, -1
	for i, event := range events {
		if event.Phase == LifecyclePhaseLock && event.Status == LifecycleProgressStarted {
			lockStarted = i
		}
		if event.Phase == LifecyclePhaseLock && (event.Status == LifecycleProgressCompleted || event.Status == LifecycleProgressNoop) {
			lockCompleted = i
		}
	}
	if lockStarted < 0 || lockCompleted <= lockStarted {
		t.Fatalf("lock progress order = %#v, want started before completed", events)
	}
}
