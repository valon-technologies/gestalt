package invocation

import (
	"context"
	"log/slog"
	"sync"

	"github.com/valon-technologies/gestalt/server/core"
)

// AppAccessResolver is the one place that decides when and how a stored
// legacy app access profile is read as decisions relative to the app's
// defaults. Enforcement, the user's page and the admin view all go through it
// so they cannot disagree. Nothing it returns is persisted.
//
// The operation history is bound late because the app registry reader that
// backs it is built after the broker.
type AppAccessResolver struct {
	logger  *slog.Logger
	mu      sync.RWMutex
	history core.OperationHistory
	// noHistoryLogged holds the apps already reported as kept legacy for lack
	// of any history, so the message appears once per app.
	noHistoryLogged sync.Map
}

func NewAppAccessResolver(logger *slog.Logger) *AppAccessResolver {
	return &AppAccessResolver{logger: logger}
}

// SetOperationHistory binds the history used to convert legacy profiles.
// Until it is set, legacy allow-lists stay authoritative.
func (r *AppAccessResolver) SetOperationHistory(history core.OperationHistory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.history = history
}

// Resolve returns the profile as it should be judged now. Apps whose
// operations come from a session catalog are left alone because the static
// catalog would silently drop decisions about them. Whenever the conversion
// cannot be done exactly, the legacy profile is returned, which only ever
// denies.
func (r *AppAccessResolver) Resolve(ctx context.Context, prov core.Provider, profile *core.AppAccessProfile) *core.AppAccessProfile {
	if profile == nil || !profile.Legacy || prov == nil || core.SupportsSessionCatalog(prov) {
		return profile
	}
	var history core.OperationHistory
	if r != nil {
		r.mu.RLock()
		history = r.history
		r.mu.RUnlock()
	}
	resolved, reason, err := core.ResolveLegacyAppAccessProfile(ctx, history, profile, prov.Catalog(), core.AppAccessDefaultsFor(prov))
	log := r.log()
	switch {
	case err != nil:
		log.WarnContext(ctx, "app access profile conversion failed; keeping legacy allow-list", "app", prov.Name(), "reason", string(reason), "error", err)
	case reason == core.LegacyKeptHistoryIncomplete:
		log.InfoContext(ctx, "app access profile kept legacy: history does not cover the saved list", "app", prov.Name(), "reason", string(reason))
	case reason == core.LegacyKeptNoHistory:
		if r.firstNoHistory(prov.Name()) {
			log.InfoContext(ctx, "app access profile kept legacy: no operation history is available", "app", prov.Name(), "reason", string(reason))
		}
	case reason != core.LegacyConverted:
		log.DebugContext(ctx, "app access profile kept legacy", "app", prov.Name(), "reason", string(reason))
	}
	return resolved
}

func (r *AppAccessResolver) log() *slog.Logger {
	if r != nil && r.logger != nil {
		return r.logger
	}
	return slog.Default()
}

func (r *AppAccessResolver) firstNoHistory(app string) bool {
	if r == nil {
		return true
	}
	_, seen := r.noHistoryLogged.LoadOrStore(app, struct{}{})
	return !seen
}
