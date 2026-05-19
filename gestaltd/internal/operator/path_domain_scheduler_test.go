package operator

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunPathDomainTasksHonorsWorkerCap(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	var active int32
	var maxActive int32

	var tasks []pathDomainTask
	for _, name := range []string{"a", "b", "c", "d"} {
		name := name
		tasks = append(tasks, pathDomainTask{
			name:    name,
			domains: mustNormalizePathDomains(t, filepath.Join(dir, name)),
			run: func() error {
				now := atomic.AddInt32(&active, 1)
				for {
					max := atomic.LoadInt32(&maxActive)
					if now <= max || atomic.CompareAndSwapInt32(&maxActive, max, now) {
						break
					}
				}
				started <- struct{}{}
				<-release
				atomic.AddInt32(&active, -1)
				return nil
			},
		})
	}

	done := make(chan error, 1)
	go func() { done <- runPathDomainTasks(tasks, 2) }()

	waitForStarts(t, started, 2)
	select {
	case <-started:
		t.Fatal("third task started before worker cap allowed it")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := waitForScheduler(t, done); err != nil {
		t.Fatalf("runPathDomainTasks: %v", err)
	}
	if got := atomic.LoadInt32(&maxActive); got != 2 {
		t.Fatalf("max active tasks = %d, want 2", got)
	}
}

func TestRunPathDomainTasksSerializesAncestorDescendantDomains(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	parent := filepath.Join(dir, "project")
	child := filepath.Join(parent, "ui")
	started := make(chan string, 2)
	releaseA := make(chan struct{})

	tasks := []pathDomainTask{
		{
			name:    "a",
			domains: mustNormalizePathDomains(t, parent),
			run: func() error {
				started <- "a"
				<-releaseA
				return nil
			},
		},
		{
			name:    "b",
			domains: mustNormalizePathDomains(t, child),
			run: func() error {
				started <- "b"
				return nil
			},
		},
	}

	done := make(chan error, 1)
	go func() { done <- runPathDomainTasks(tasks, 2) }()
	if got := waitForStart(t, started); got != "a" {
		t.Fatalf("first started task = %q, want a", got)
	}
	select {
	case got := <-started:
		t.Fatalf("task %q started while ancestor domain was active", got)
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseA)
	if got := waitForStart(t, started); got != "b" {
		t.Fatalf("second started task = %q, want b", got)
	}
	if err := waitForScheduler(t, done); err != nil {
		t.Fatalf("runPathDomainTasks: %v", err)
	}
}

func TestRunPathDomainTasksDoesNotRunBlockedTaskAfterFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	shared := filepath.Join(dir, "shared")
	aStarted := make(chan struct{})
	cStarted := make(chan struct{})
	releaseA := make(chan struct{})
	var bRan atomic.Bool
	errC := errors.New("c failed")

	tasks := []pathDomainTask{
		{
			name:    "a",
			domains: mustNormalizePathDomains(t, shared),
			run: func() error {
				close(aStarted)
				<-releaseA
				return nil
			},
		},
		{
			name:    "b",
			domains: mustNormalizePathDomains(t, shared),
			run: func() error {
				bRan.Store(true)
				return nil
			},
		},
		{
			name:    "c",
			domains: mustNormalizePathDomains(t, filepath.Join(dir, "c")),
			run: func() error {
				close(cStarted)
				return errC
			},
		},
	}

	done := make(chan error, 1)
	go func() { done <- runPathDomainTasks(tasks, 2) }()
	<-aStarted
	<-cStarted
	close(releaseA)
	if err := waitForScheduler(t, done); !errors.Is(err, errC) {
		t.Fatalf("runPathDomainTasks error = %v, want %v", err, errC)
	}
	if bRan.Load() {
		t.Fatal("blocked task ran after scheduler failure")
	}
}

func TestCanonicalPathDomainResolvesExistingSymlinkPrefixForMissingPath(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior differs on Windows")
	}
	dir := t.TempDir()
	realRoot := filepath.Join(dir, "real")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatalf("mkdir real root: %v", err)
	}
	linkRoot := filepath.Join(dir, "link")
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	got, err := canonicalPathDomain(filepath.Join(linkRoot, "dist", "index.html"))
	if err != nil {
		t.Fatalf("canonicalPathDomain: %v", err)
	}
	resolvedRealRoot, err := filepath.EvalSymlinks(realRoot)
	if err != nil {
		t.Fatalf("resolve real root: %v", err)
	}
	want := filepath.Join(resolvedRealRoot, "dist", "index.html")
	if got != want {
		t.Fatalf("canonicalPathDomain = %q, want %q", got, want)
	}
}

func mustNormalizePathDomains(t *testing.T, paths ...string) []string {
	t.Helper()
	domains, err := normalizePathDomains(paths...)
	if err != nil {
		t.Fatalf("normalizePathDomains: %v", err)
	}
	return domains
}

func waitForStarts(t *testing.T, started <-chan struct{}, count int) {
	t.Helper()
	for range count {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for %d task starts", count)
		}
	}
}

func waitForStart(t *testing.T, started <-chan string) string {
	t.Helper()
	select {
	case got := <-started:
		return got
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for task start")
		return ""
	}
}

func waitForScheduler(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler timed out")
		return nil
	}
}
