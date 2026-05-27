package daemon

import (
	"context"
	"log/slog"
	"os/signal"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/valon-technologies/gestalt/server/internal/operator"
	"github.com/valon-technologies/gestalt/server/internal/server"
)

const defaultServeWatchDebounce = 500 * time.Millisecond

type serveWatchCandidate struct {
	env  *bootstrapEnv
	plan providerLocalWatchPlan
	done chan error
}

func runServeWatch(configPaths []string, state operator.StatePaths) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	active, err := prepareServeWatchCandidate(ctx, configPaths, state)
	if err != nil {
		return err
	}
	active.start()

	watcher, err := newProviderLocalFSWatcher(active.plan)
	if err != nil {
		_ = active.stop()
		return err
	}
	defer func() { _ = watcher.Close() }()

	slog.Info("watch mode enabled",
		"debounce", defaultServeWatchDebounce.String(),
		"files", len(active.plan.Files),
		"roots", len(active.plan.Roots),
		"watch_dirs", len(active.plan.WatchDirs),
	)

	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	var timerC <-chan time.Time
	scheduleReload := func() {
		if timerC == nil {
			timer.Reset(defaultServeWatchDebounce)
			timerC = timer.C
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(defaultServeWatchDebounce)
	}

	for {
		select {
		case err := <-active.done:
			if ctx.Err() != nil {
				return nil
			}
			return err
		case <-ctx.Done():
			return active.stop()
		case event, ok := <-watcher.Events:
			if !ok {
				continue
			}
			if !active.plan.includesEventPath(event.Name) {
				continue
			}
			if event.Op&fsnotify.Create != 0 {
				addProviderLocalCreatedWatchDirs(watcher, active.plan, event.Name)
			}
			if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename|fsnotify.Chmod) != 0 {
				scheduleReload()
			}
		case err, ok := <-watcher.Errors:
			if ok && err != nil {
				slog.Warn("watch filesystem event error", "error", err)
			}
		case <-timerC:
			timerC = nil
			active, watcher = reloadServeWatchCandidate(ctx, configPaths, state, active, watcher)
		}
	}
}

func reloadServeWatchCandidate(ctx context.Context, configPaths []string, state operator.StatePaths, active *serveWatchCandidate, watcher *fsnotify.Watcher) (*serveWatchCandidate, *fsnotify.Watcher) {
	next, err := prepareServeWatchCandidate(ctx, configPaths, state)
	if err != nil {
		slog.Error("watch reload failed; keeping current server", "error", err)
		return active, watcher
	}
	nextWatcher, err := newProviderLocalFSWatcher(next.plan)
	if err != nil {
		next.close()
		slog.Error("watch reload failed; keeping current server", "error", err)
		return active, watcher
	}
	if err := active.stop(); err != nil {
		slog.Warn("watch reload replacing exited server", "error", err)
	}
	next.start()
	if watcher != nil {
		_ = watcher.Close()
	}
	slog.Info("watch reload complete",
		"files", len(next.plan.Files),
		"roots", len(next.plan.Roots),
		"watch_dirs", len(next.plan.WatchDirs),
	)
	return next, nextWatcher
}

func prepareServeWatchCandidate(parent context.Context, configPaths []string, state operator.StatePaths) (*serveWatchCandidate, error) {
	ctx, cancel := context.WithCancel(parent)
	env, err := setupBootstrapWithConfigPathsContext(ctx, cancel, configPaths, state, false)
	if err != nil {
		return nil, err
	}
	plan, err := newProviderLocalWatchPlan(configPaths, env.Config)
	if err != nil {
		env.Close()
		return nil, err
	}
	return &serveWatchCandidate{
		env:  env,
		plan: plan,
		done: make(chan error, 1),
	}, nil
}

func (c *serveWatchCandidate) start() {
	go func() {
		err := server.Run(c.env.Ctx, c.env.Config, c.env.Result)
		c.close()
		c.done <- err
	}()
}

func (c *serveWatchCandidate) stop() error {
	if c == nil || c.env == nil {
		return nil
	}
	c.env.Stop()
	return <-c.done
}

func (c *serveWatchCandidate) close() {
	if c == nil || c.env == nil {
		return
	}
	c.env.Close()
}
