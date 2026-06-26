package gestalt

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"google.golang.org/grpc"
)

func TestWatchProviderParentSurvivesIntermediateParent(t *testing.T) {
	t.Parallel()

	parent := exec.Command(os.Args[0], "-test.run=TestWatchProviderParentChild", "-test.v")
	parent.Env = append(os.Environ(), "GO_WANT_PARENT_WATCH=1")
	if err := parent.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	defer func() {
		_ = parent.Process.Kill()
		_ = parent.Wait()
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if parent.ProcessState != nil && parent.ProcessState.Exited() {
			t.Fatalf("child exited early with state %v", parent.ProcessState)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestWatchProviderParentChild(t *testing.T) {
	if os.Getenv("GO_WANT_PARENT_WATCH") != "1" {
		return
	}

	parentPID := os.Getpid()
	srv := grpc.NewServer()
	go watchProviderParent(parentPID, srv)
	time.Sleep(1500 * time.Millisecond)
	srv.Stop()
	os.Exit(0)
}
