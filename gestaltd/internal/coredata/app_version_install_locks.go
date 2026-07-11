package coredata

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

const DefaultAppVersionInstallLockTTL = 15 * time.Minute

type AppVersionInstallLock struct {
	App        string
	Version    string
	Holder     string
	AcquiredAt time.Time
	ExpiresAt  time.Time
}

type AppVersionInstallLockService struct {
	store idb.ObjectStore
}

func NewAppVersionInstallLockService(ds indexeddb.IndexedDB) *AppVersionInstallLockService {
	return &AppVersionInstallLockService{store: ds.ObjectStore(StoreAppVersionInstallLocks)}
}

// Acquire claims the fleet-wide install lock for one app version. The lock expires
// after ttl so crashed instances do not block installs forever.
func (s *AppVersionInstallLockService) Acquire(ctx context.Context, app, version, holder string, ttl time.Duration) error {
	if s == nil {
		return fmt.Errorf("acquire app version install lock: service is not configured")
	}
	app = strings.TrimSpace(app)
	version = strings.TrimSpace(version)
	holder = strings.TrimSpace(holder)
	if app == "" || version == "" {
		return fmt.Errorf("acquire app version install lock: app and version are required")
	}
	if holder == "" {
		return fmt.Errorf("acquire app version install lock: holder is required")
	}
	if ttl <= 0 {
		ttl = DefaultAppVersionInstallLockTTL
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	expiresAt := now.Add(ttl)
	key := appVersionInstallLockKey(app, version)

	if err := s.tryAddLock(ctx, key, app, version, holder, now, expiresAt); err == nil {
		return nil
	} else if !errors.Is(err, idb.ErrAlreadyExists) {
		return err
	}

	existing, getErr := s.store.Get(ctx, key)
	if getErr != nil {
		if errors.Is(getErr, idb.ErrNotFound) {
			return s.tryAddLock(ctx, key, app, version, holder, now, expiresAt)
		}
		return fmt.Errorf("acquire app version install lock: load existing: %w", getErr)
	}

	current := recordToAppVersionInstallLock(existing)
	if current.Holder == holder {
		if current.ExpiresAt.After(now) {
			return nil
		}
		return s.putLock(ctx, key, app, version, holder, now, expiresAt)
	}
	if current.ExpiresAt.After(now) {
		return ErrAppVersionInstallLockHeld
	}

	if err := s.store.Delete(ctx, key); err != nil && !errors.Is(err, idb.ErrNotFound) {
		return fmt.Errorf("acquire app version install lock: delete stale: %w", err)
	}
	if addErr := s.tryAddLock(ctx, key, app, version, holder, now, expiresAt); addErr == nil {
		return nil
	} else if errors.Is(addErr, idb.ErrAlreadyExists) {
		return ErrAppVersionInstallLockHeld
	} else {
		return addErr
	}
}

// Release drops the install lock when the holder still owns it.
func (s *AppVersionInstallLockService) Release(ctx context.Context, app, version, holder string) error {
	if s == nil {
		return nil
	}
	app = strings.TrimSpace(app)
	version = strings.TrimSpace(version)
	holder = strings.TrimSpace(holder)
	if app == "" || version == "" || holder == "" {
		return nil
	}

	key := appVersionInstallLockKey(app, version)
	existing, err := s.store.Get(ctx, key)
	if errors.Is(err, idb.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("release app version install lock: %w", err)
	}
	if strings.TrimSpace(recordToAppVersionInstallLock(existing).Holder) != holder {
		return nil
	}
	if err := s.store.Delete(ctx, key); err != nil && !errors.Is(err, idb.ErrNotFound) {
		return fmt.Errorf("release app version install lock: %w", err)
	}
	return nil
}

var ErrAppVersionInstallLockHeld = errors.New("app version install lock is held")

func (s *AppVersionInstallLockService) tryAddLock(ctx context.Context, key, app, version, holder string, acquiredAt, expiresAt time.Time) error {
	rec := idb.Record{
		"id":          key,
		"app":         app,
		"version":     version,
		"holder":      holder,
		"acquired_at": acquiredAt,
		"expires_at":  expiresAt,
	}
	if err := s.store.Add(ctx, rec); err != nil {
		if errors.Is(err, idb.ErrAlreadyExists) {
			return idb.ErrAlreadyExists
		}
		return fmt.Errorf("acquire app version install lock: %w", err)
	}
	return nil
}

func (s *AppVersionInstallLockService) putLock(ctx context.Context, key, app, version, holder string, acquiredAt, expiresAt time.Time) error {
	rec := idb.Record{
		"id":          key,
		"app":         app,
		"version":     version,
		"holder":      holder,
		"acquired_at": acquiredAt,
		"expires_at":  expiresAt,
	}
	if err := s.store.Put(ctx, rec); err != nil {
		return fmt.Errorf("refresh app version install lock: %w", err)
	}
	return nil
}

func appVersionInstallLockKey(app, version string) string {
	return strings.TrimSpace(app) + "\x00" + strings.TrimSpace(version)
}

func recordToAppVersionInstallLock(rec idb.Record) AppVersionInstallLock {
	return AppVersionInstallLock{
		App:        recString(rec, "app"),
		Version:    recString(rec, "version"),
		Holder:     recString(rec, "holder"),
		AcquiredAt: recTime(rec, "acquired_at"),
		ExpiresAt:  recTime(rec, "expires_at"),
	}
}
