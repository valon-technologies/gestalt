package bootstrap

import (
	"context"
	"fmt"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

func currentAppSHA(entry *config.ProviderEntry) string {
	if entry == nil || entry.ResolvedManifest == nil {
		return ""
	}
	artifact, err := providerpkg.CurrentPlatformArtifact(entry.ResolvedManifest)
	if err != nil || artifact == nil {
		return ""
	}
	return artifact.SHA256
}

func readAppSHAs(ctx context.Context, db indexeddb.IndexedDB) map[string]string {
	shas := make(map[string]string)
	if db == nil {
		return shas
	}
	records, err := db.ObjectStore(coredata.StoreAppSHAs).GetAll(ctx, nil)
	if err != nil {
		return shas
	}
	for _, record := range records {
		name, _ := record["id"].(string)
		sha, _ := record["sha"].(string)
		if name != "" && sha != "" {
			shas[name] = sha
		}
	}
	return shas
}

func writeAppSHA(ctx context.Context, db indexeddb.IndexedDB, appName, sha string) error {
	if db == nil {
		return fmt.Errorf("indexeddb is required to persist app sha")
	}
	return db.ObjectStore(coredata.StoreAppSHAs).Put(ctx, idb.Record{"id": appName, "sha": sha})
}
