package daemon

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/operator"
)

func TestReloadServeWatchCandidateKeepsActiveWhenPrepareFails(t *testing.T) {
	t.Parallel()

	active := &serveWatchCandidate{}
	missingConfigPath := filepath.Join(t.TempDir(), "missing.yaml")

	gotActive, gotWatcher := reloadServeWatchCandidate(context.Background(), []string{missingConfigPath}, operator.StatePaths{}, nil, active, nil)
	if gotActive != active {
		t.Fatal("reload replaced active candidate after prepare failure")
	}
	if gotWatcher != nil {
		t.Fatalf("reload created watcher after prepare failure: %v", gotWatcher)
	}
}
