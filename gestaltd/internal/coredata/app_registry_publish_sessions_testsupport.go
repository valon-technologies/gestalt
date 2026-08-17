package coredata

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"

	"github.com/valon-technologies/gestalt/server/core"
)

// MutatePublishSessionForTest applies a direct publish session mutation for test setup.
// It runs in a serialized read/write transaction and increments Revision so callers
// holding an older revision cannot succeed on subsequent transitions.
func (s *AppRegistryPublishSessionService) MutatePublishSessionForTest(ctx context.Context, id string, mutate func(*core.AppRegistryPublishSession) error) (*core.AppRegistryPublishSession, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("mutate app registry publish session for test: service is not configured")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("mutate app registry publish session for test: id is required")
	}
	if mutate == nil {
		return nil, fmt.Errorf("mutate app registry publish session for test: mutate function is required")
	}
	if err := s.EnsureStore(ctx); err != nil {
		return nil, err
	}

	tx, err := s.db.Transaction(ctx, []string{StoreAppRegistryPublishSessions}, idb.TransactionReadwrite, idb.TransactionOptions{})
	if err != nil {
		return nil, fmt.Errorf("mutate app registry publish session for test: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(context.WithoutCancel(ctx))
		}
	}()

	store := tx.ObjectStore(StoreAppRegistryPublishSessions)
	rec, err := store.Get(ctx, id)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("mutate app registry publish session for test: load: %w", err)
	}
	session := recordToAppRegistryPublishSession(rec)
	if err := mutate(session); err != nil {
		return nil, err
	}
	session.Revision++
	session.UpdatedAt = time.Now().UTC().Truncate(time.Millisecond)
	if err := store.Put(ctx, appRegistryPublishSessionRecord(session)); err != nil {
		return nil, fmt.Errorf("mutate app registry publish session for test: write: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("mutate app registry publish session for test: commit: %w", err)
	}
	committed = true
	return session, nil
}
