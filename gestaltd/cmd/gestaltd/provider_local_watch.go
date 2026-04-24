package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
)

const (
	providerDevReadyTimeout    = 60 * time.Second
	providerDevShutdownTimeout = 15 * time.Second
	providerDevRestartDebounce = 300 * time.Millisecond
)

func runProviderDevWatch(session *providerLocalSession) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	watcher, err := newProviderDevWatcher(session)
	if err != nil {
		return err
	}
	defer func() { _ = watcher.Close() }()

	slog.Info("provider dev watch enabled",
		"watch_roots", watcher.watchRoots(),
		"config_files", watcher.configFilesList(),
	)

	proc, err := startProviderDevServeProcess(session)
	if err != nil {
		return err
	}
	if err := waitForProviderDevReady(ctx, session.PublicURL, proc); err != nil {
		_ = proc.Stop(providerDevShutdownTimeout)
		return err
	}
	logProviderLocalSummary("provider dev ready", session)

	var (
		restartTimer *time.Timer
		restartCh    <-chan time.Time
		restartPath  string
	)

	for {
		var procDone <-chan struct{}
		if proc != nil {
			procDone = proc.Done()
		}

		select {
		case <-ctx.Done():
			if restartTimer != nil {
				stopAndDrainTimer(restartTimer)
			}
			if proc != nil {
				_ = proc.Stop(providerDevShutdownTimeout)
			}
			return nil
		case err, ok := <-watcher.Errors():
			if !ok {
				if proc != nil {
					_ = proc.Stop(providerDevShutdownTimeout)
				}
				return errors.New("provider dev watcher error channel closed unexpectedly")
			}
			if proc != nil {
				_ = proc.Stop(providerDevShutdownTimeout)
			}
			return fmt.Errorf("provider dev watch error: %w", err)
		case event, ok := <-watcher.Events():
			if !ok {
				if proc != nil {
					_ = proc.Stop(providerDevShutdownTimeout)
				}
				return errors.New("provider dev watcher stopped unexpectedly")
			}
			relevant, changedPath, err := watcher.HandleEvent(event)
			if err != nil {
				slog.Warn("provider dev watch event error", "path", event.Name, "err", err)
				continue
			}
			if !relevant {
				continue
			}
			restartPath = changedPath
			if restartTimer == nil {
				restartTimer = time.NewTimer(providerDevRestartDebounce)
			} else {
				stopAndDrainTimer(restartTimer)
				restartTimer.Reset(providerDevRestartDebounce)
			}
			restartCh = restartTimer.C
		case <-restartCh:
			restartCh = nil
			changedPath := restartPath
			restartPath = ""
			slog.Info("provider dev change detected; restarting", "path", changedPath)

			if err := refreshProviderLocalSession(session); err != nil {
				slog.Error("provider dev refresh failed", "path", changedPath, "err", err)
				continue
			}
			if err := watcher.SyncSession(session); err != nil {
				slog.Error("provider dev watch refresh failed", "path", changedPath, "err", err)
				continue
			}

			if proc != nil {
				_ = proc.Stop(providerDevShutdownTimeout)
				proc = nil
			}

			nextProc, err := startProviderDevServeProcess(session)
			if err != nil {
				slog.Error("provider dev restart failed", "path", changedPath, "err", err)
				continue
			}
			if err := waitForProviderDevReady(ctx, session.PublicURL, nextProc); err != nil {
				_ = nextProc.Stop(providerDevShutdownTimeout)
				slog.Error("provider dev restart failed", "path", changedPath, "err", err)
				continue
			}

			proc = nextProc
			logProviderLocalSummary("provider dev restarted", session)
			slog.Info("provider dev restart source", "path", changedPath)
		case <-procDone:
			if proc == nil {
				continue
			}
			slog.Warn("provider dev server exited; waiting for the next change to restart", "err", proc.WaitErr())
			proc = nil
		}
	}
}

type providerDevServeProcess struct {
	cmd     *exec.Cmd
	done    chan struct{}
	mu      sync.RWMutex
	waitErr error
}

func startProviderDevServeProcess(session *providerLocalSession) (*providerDevServeProcess, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve current executable: %w", err)
	}

	args := []string{"serve", "--artifacts-dir", session.State.ArtifactsDir, "--lockfile", session.State.LockfilePath}
	for _, configPath := range session.ConfigPaths {
		args = append(args, "--config", configPath)
	}

	cmd := exec.Command(executable, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	proc := &providerDevServeProcess{
		cmd:  cmd,
		done: make(chan struct{}),
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start provider dev server: %w", err)
	}
	go func() {
		err := cmd.Wait()
		proc.mu.Lock()
		proc.waitErr = err
		proc.mu.Unlock()
		close(proc.done)
	}()
	return proc, nil
}

func (p *providerDevServeProcess) Done() <-chan struct{} {
	if p == nil {
		return nil
	}
	return p.done
}

func (p *providerDevServeProcess) WaitErr() error {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.waitErr
}

func (p *providerDevServeProcess) Stop(timeout time.Duration) error {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	select {
	case <-p.done:
		return nil
	default:
	}

	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("signal provider dev server: %w", err)
	}

	select {
	case <-p.done:
		return nil
	case <-time.After(timeout):
		if err := p.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("kill provider dev server: %w", err)
		}
		<-p.done
		return nil
	}
}

func waitForProviderDevReady(ctx context.Context, baseURL string, proc *providerDevServeProcess) error {
	client := &http.Client{Timeout: 2 * time.Second}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(providerDevReadyTimeout)
	defer timeout.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout.C:
			return fmt.Errorf("provider dev server did not become ready within %s", providerDevReadyTimeout)
		case <-proc.Done():
			if err := proc.WaitErr(); err != nil {
				return fmt.Errorf("provider dev server exited before becoming ready: %w", err)
			}
			return errors.New("provider dev server exited before becoming ready")
		case <-ticker.C:
			resp, err := client.Get(strings.TrimRight(baseURL, "/") + "/ready")
			if err != nil {
				continue
			}
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
	}
}

type providerDevWatcher struct {
	watcher           *fsnotify.Watcher
	packageDir        string
	staticCatalogPath string
	watchDirs         []string
	configFiles       map[string]struct{}
	watchedDirs       map[string]struct{}
}

func newProviderDevWatcher(session *providerLocalSession) (*providerDevWatcher, error) {
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create file watcher: %w", err)
	}

	watcher := &providerDevWatcher{
		watcher:     fsWatcher,
		configFiles: map[string]struct{}{},
		watchedDirs: map[string]struct{}{},
	}
	if err := watcher.SyncSession(session); err != nil {
		_ = fsWatcher.Close()
		return nil, err
	}
	return watcher, nil
}

func (w *providerDevWatcher) Close() error {
	if w == nil || w.watcher == nil {
		return nil
	}
	return w.watcher.Close()
}

func (w *providerDevWatcher) Events() <-chan fsnotify.Event {
	return w.watcher.Events
}

func (w *providerDevWatcher) Errors() <-chan error {
	return w.watcher.Errors
}

func (w *providerDevWatcher) SyncSession(session *providerLocalSession) error {
	if w == nil || session == nil {
		return nil
	}

	packageDir, err := canonicalPath(session.PackageDir)
	if err != nil {
		return err
	}
	w.packageDir = packageDir
	staticCatalogPath, err := canonicalPath(providerpkg.StaticCatalogPath(packageDir))
	if err != nil {
		return err
	}
	w.staticCatalogPath = staticCatalogPath

	if err := w.syncWatchDirs(providerDevWatchRoots(session)); err != nil {
		return err
	}

	configFiles := make(map[string]struct{}, len(session.UserConfigPaths))
	for _, path := range session.UserConfigPaths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		canonicalFile, err := canonicalPath(path)
		if err != nil {
			return err
		}
		configFiles[canonicalFile] = struct{}{}
		if err := w.addDir(filepath.Dir(canonicalFile)); err != nil {
			return err
		}
	}
	w.configFiles = configFiles
	return nil
}

func (w *providerDevWatcher) HandleEvent(event fsnotify.Event) (bool, string, error) {
	if event.Op&^fsnotify.Chmod == 0 {
		return false, "", nil
	}

	eventPath, err := canonicalPath(event.Name)
	if err != nil {
		return false, "", err
	}
	if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		w.dropWatchedDir(eventPath)
	}
	if eventPath == w.staticCatalogPath {
		return false, "", nil
	}
	if err := w.addCreatedDirs(eventPath); err != nil {
		return false, "", err
	}

	if pathWithinAnyRoot(eventPath, w.watchDirs) {
		return true, eventPath, nil
	}
	if _, ok := w.configFiles[eventPath]; ok {
		return true, eventPath, nil
	}
	return false, "", nil
}

func (w *providerDevWatcher) watchRoots() []string {
	if len(w.watchDirs) == 0 {
		return nil
	}
	return append([]string(nil), w.watchDirs...)
}

func (w *providerDevWatcher) configFilesList() []string {
	if len(w.configFiles) == 0 {
		return nil
	}
	paths := make([]string, 0, len(w.configFiles))
	for path := range w.configFiles {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	return paths
}

func (w *providerDevWatcher) syncWatchDirs(roots []string) error {
	canonicalRoots := make([]string, 0, len(roots))
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		canonicalRoot, err := canonicalPath(root)
		if err != nil {
			return err
		}
		if slices.Contains(canonicalRoots, canonicalRoot) {
			continue
		}
		if err := w.addRecursiveDir(canonicalRoot); err != nil {
			return err
		}
		canonicalRoots = append(canonicalRoots, canonicalRoot)
	}
	slices.Sort(canonicalRoots)
	w.watchDirs = canonicalRoots
	return nil
}

func (w *providerDevWatcher) addCreatedDirs(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}
	if pathWithinRoot(path, w.packageDir) {
		return w.addRecursiveDir(path)
	}
	return w.addDir(path)
}

func (w *providerDevWatcher) dropWatchedDir(path string) {
	if len(w.watchedDirs) == 0 {
		return
	}
	for watchedDir := range w.watchedDirs {
		if watchedDir == path || pathWithinRoot(watchedDir, path) {
			delete(w.watchedDirs, watchedDir)
		}
	}
}

func (w *providerDevWatcher) addRecursiveDir(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		return w.addDir(path)
	})
}

func (w *providerDevWatcher) addDir(dir string) error {
	canonicalDir, err := canonicalPath(dir)
	if err != nil {
		return err
	}
	if _, ok := w.watchedDirs[canonicalDir]; ok {
		return nil
	}
	if err := w.watcher.Add(canonicalDir); err != nil {
		return fmt.Errorf("watch %s: %w", canonicalDir, err)
	}
	w.watchedDirs[canonicalDir] = struct{}{}
	return nil
}

func providerDevWatchRoots(session *providerLocalSession) []string {
	if session == nil {
		return nil
	}

	roots := []string{session.PackageDir}
	if session.Manifest == nil || session.Manifest.Spec == nil || session.Manifest.Spec.UI == nil {
		return roots
	}

	ownedUIPath := strings.TrimSpace(session.Manifest.Spec.UI.Path)
	if ownedUIPath == "" {
		return roots
	}

	resolvedPath := filepath.Clean(filepath.Join(session.PackageDir, filepath.FromSlash(ownedUIPath)))
	if info, err := os.Stat(resolvedPath); err == nil && !info.IsDir() {
		resolvedPath = filepath.Dir(resolvedPath)
	}
	roots = append(roots, resolvedPath)
	return roots
}

func pathWithinRoot(path, root string) bool {
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func pathWithinAnyRoot(path string, roots []string) bool {
	for _, root := range roots {
		if pathWithinRoot(path, root) {
			return true
		}
	}
	return false
}

func stopAndDrainTimer(timer *time.Timer) {
	if timer == nil {
		return
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}
