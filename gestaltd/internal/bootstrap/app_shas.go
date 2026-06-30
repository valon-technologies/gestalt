package bootstrap

import (
	"context"
	"log/slog"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

func updateAppSHAs(ctx context.Context, cfg *config.Config, db indexeddb.IndexedDB) {
	store := db.ObjectStore(coredata.StoreAppSHAs)
	for name, entry := range cfg.Apps {
		if entry == nil || entry.ResolvedManifest == nil {
			continue
		}
		artifact, err := providerpkg.CurrentPlatformArtifact(entry.ResolvedManifest)
		if err != nil || artifact == nil || artifact.SHA256 == "" {
			continue
		}
		if err := store.Put(ctx, idb.Record{
			"app_id": name,
			"sha":    artifact.SHA256,
		}); err != nil {
			slog.WarnContext(ctx, "failed to update app sha in indexeddb", "app", name, "error", err)
		}
	}
}
