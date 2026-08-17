package appregistry

import (
	"encoding/json"
	"fmt"
	"os"
)

type catalogCASLabels struct {
	start          string
	doneUnchanged  string
	doneUpdated    string
	retryStatus    func(attempt, max int) string
	retryConflict  string
	logUnchanged   func(storageURL string) string
	logUpdated     func(storageURL string) string
}

// DefaultCatalogUpdateAttempts is the retry budget for generation-matched catalog writes.
const DefaultCatalogUpdateAttempts = 5

func compareAndSwapCatalog[T any](
	store RegistryObjectStore,
	storageURL, sourceRef string,
	attempts int,
	progress PublishProgress,
	decode func([]byte) (T, error),
	mutate func(T) (T, bool, error),
	labels catalogCASLabels,
) (CatalogWriteOutcome, error) {
	if store == nil {
		return CatalogWriteOutcomeNotAttempted, fmt.Errorf("registry object store is required")
	}
	if attempts <= 0 {
		attempts = DefaultCatalogUpdateAttempts
	}
	if labels.start != "" && progress.Start != nil {
		progress.Start("%s", labels.start)
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 && labels.retryStatus != nil && progress.Status != nil {
			progress.Status(labels.retryStatus(attempt, attempts))
		}
		generation, existing, err := store.ReadObject(storageURL)
		if err != nil {
			return CatalogWriteOutcomeNotAttempted, err
		}
		decoded, err := decode(existing)
		if err != nil {
			return CatalogWriteOutcomeNotAttempted, err
		}
		updated, changed, err := mutate(decoded)
		if err != nil {
			return CatalogWriteOutcomeNotAttempted, err
		}
		if !changed {
			if labels.doneUnchanged != "" && progress.Done != nil {
				progress.Done("%s", labels.doneUnchanged)
			}
			if labels.logUnchanged != nil && progress.Log != nil {
				progress.Log(labels.logUnchanged(storageURL))
			}
			return CatalogWriteOutcomeUnchanged, nil
		}
		data, err := json.MarshalIndent(updated, "", "  ")
		if err != nil {
			return CatalogWriteOutcomeNotAttempted, err
		}
		tmpPath, err := WriteTempJSON("gestalt-app-catalog-*", append(data, '\n'))
		if err != nil {
			return CatalogWriteOutcomeNotAttempted, err
		}
		err = store.WriteCatalogObject(WriteCatalogObjectInput{
			LocalPath:  tmpPath,
			StorageURL: storageURL,
			SourceRef:  sourceRef,
			Generation: generation,
		})
		_ = os.Remove(tmpPath)
		if err != nil {
			if isCatalogPreconditionFailed(err) && attempt < attempts {
				if labels.retryConflict != "" && progress.Status != nil {
					progress.Status("%s", labels.retryConflict)
				}
				continue
			}
			return CatalogWriteOutcomeNotAttempted, err
		}
		if labels.doneUpdated != "" && progress.Done != nil {
			progress.Done("%s", labels.doneUpdated)
		}
		if labels.logUpdated != nil && progress.Log != nil {
			progress.Log(labels.logUpdated(storageURL))
		}
		return CatalogWriteOutcomeUpdated, nil
	}
	return CatalogWriteOutcomeNotAttempted, fmt.Errorf("update %s: exceeded retry limit after concurrent catalog updates", storageURL)
}
