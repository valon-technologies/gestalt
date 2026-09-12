package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
)

const promoteSharedStateOnActivateEnv = "GESTALTD_PROMOTE_SHARED_STATE_ON_ACTIVATE"

// resolvePromoteSharedStateOnActivate reports whether POST /activate may promote
// shared gestaltd source-version and rollout coordination state. The config
// field takes precedence over the environment variable. When unset, promotion on
// activate defaults to true so existing deploy flows keep working until a
// candidate opts into isolated preparation.
func resolvePromoteSharedStateOnActivate(cfg *config.Config) bool {
	if cfg != nil && cfg.Server.PromoteSharedStateOnActivate != nil {
		return *cfg.Server.PromoteSharedStateOnActivate
	}
	raw := strings.TrimSpace(os.Getenv(promoteSharedStateOnActivateEnv))
	if raw == "" {
		return true
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return true
	}
	return parsed
}

type pendingAppSHAWriter struct {
	mu      sync.Mutex
	db      indexeddb.IndexedDB
	enabled bool
	pending map[string]string
}

func newPendingAppSHAWriter(db indexeddb.IndexedDB, enabled bool) *pendingAppSHAWriter {
	return &pendingAppSHAWriter{
		db:      db,
		enabled: enabled,
		pending: make(map[string]string),
	}
}

func (w *pendingAppSHAWriter) Record(appName, sha string) {
	if w == nil || !w.enabled {
		return
	}
	w.mu.Lock()
	w.pending[appName] = sha
	w.mu.Unlock()
}

func (w *pendingAppSHAWriter) Flush(ctx context.Context) error {
	if w == nil || !w.enabled {
		return nil
	}
	w.mu.Lock()
	pending := make(map[string]string, len(w.pending))
	for name, sha := range w.pending {
		pending[name] = sha
	}
	w.pending = make(map[string]string)
	w.mu.Unlock()

	var errs []error
	for name, sha := range pending {
		if err := writeAppSHA(ctx, w.db, name, sha); err != nil {
			errs = append(errs, fmt.Errorf("persist app sha for %q: %w", name, err))
		}
	}
	return errors.Join(errs...)
}

type promotableWorkflowProvider interface {
	PromoteWorkers(context.Context) error
}

func promoteDelegatedWorkflowWorkers(ctx context.Context, provider coreworkflow.Provider) error {
	if provider == nil {
		return fmt.Errorf("workflow provider is not configured")
	}
	promotable, ok := provider.(promotableWorkflowProvider)
	if !ok {
		return fmt.Errorf("workflow provider does not support explicit worker promotion")
	}
	return promotable.PromoteWorkers(ctx)
}

func promoteWorkflowProviders(ctx context.Context, providers []coreworkflow.Provider) error {
	var errs []error
	promoted := 0
	configured := 0
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		configured++
		if promotable, ok := provider.(promotableWorkflowProvider); ok {
			if err := promotable.PromoteWorkers(ctx); err != nil {
				errs = append(errs, err)
				continue
			}
			promoted++
			continue
		}
		slog.WarnContext(
			ctx,
			"workflow provider does not support explicit worker promotion",
			"provider_type",
			fmt.Sprintf("%T", provider),
		)
	}
	if configured > 0 && promoted == 0 {
		errs = append(
			errs,
			fmt.Errorf("no workflow providers handled explicit worker promotion (provider_count=%d)", configured),
		)
	}
	return errors.Join(errs...)
}
