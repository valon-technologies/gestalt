package coredata

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/emailutil"
)

type UserService struct {
	store                indexeddb.ObjectStore
	normalizedEmailIndex bool
}

func NewUserService(ds indexeddb.IndexedDB, normalizedEmailIndex bool) *UserService {
	return &UserService{
		store:                ds.ObjectStore(StoreUsers),
		normalizedEmailIndex: normalizedEmailIndex,
	}
}

func (s *UserService) BackfillNormalizedEmails(ctx context.Context) error {
	if !s.normalizedEmailIndex {
		return nil
	}
	recs, err := s.store.GetAll(ctx, nil)
	if err != nil {
		return fmt.Errorf("list users: %w", err)
	}
	for _, rec := range recs {
		normalizedEmail := emailutil.Normalize(recString(rec, "email"))
		if normalizedEmail == "" || recString(rec, "normalized_email") == normalizedEmail {
			continue
		}
		updated := cloneRecord(rec)
		updated["normalized_email"] = normalizedEmail
		if err := s.store.Put(ctx, updated); err != nil {
			return fmt.Errorf("backfill normalized email for user %q: %w", recString(rec, "id"), err)
		}
	}
	return nil
}

func (s *UserService) GetUser(ctx context.Context, id string) (*core.User, error) {
	rec, err := s.store.Get(ctx, id)
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get user: %w", err)
	}
	return recordToUser(rec), nil
}

func (s *UserService) FindOrCreateUser(ctx context.Context, email string) (*core.User, error) {
	rawEmail := strings.TrimSpace(email)
	email = emailutil.Normalize(email)
	if email == "" {
		return nil, fmt.Errorf("find user: email is required")
	}

	user, err := s.findUserByNormalizedEmail(ctx, rawEmail, email)
	switch {
	case err == nil:
		return user, nil
	case !errors.Is(err, core.ErrNotFound):
		return nil, err
	}

	now := time.Now()
	newRec := indexeddb.Record{
		"id":           uuid.New().String(),
		"email":        email,
		"display_name": "",
		"created_at":   now,
		"updated_at":   now,
	}
	if s.normalizedEmailIndex {
		newRec["normalized_email"] = email
	}
	if err := s.store.Add(ctx, newRec); err != nil {
		user, retryErr := s.findUserByNormalizedEmail(ctx, rawEmail, email)
		if retryErr != nil {
			return nil, fmt.Errorf("create user: %w", err)
		}
		return user, nil
	}
	return recordToUser(newRec), nil
}

func (s *UserService) FindUserByEmail(ctx context.Context, email string) (*core.User, error) {
	rawEmail := strings.TrimSpace(email)
	email = emailutil.Normalize(email)
	if email == "" {
		return nil, fmt.Errorf("find user: email is required")
	}
	return s.findUserByNormalizedEmail(ctx, rawEmail, email)
}

func (s *UserService) findUserByNormalizedEmail(ctx context.Context, rawEmail, normalizedEmail string) (*core.User, error) {
	if !s.normalizedEmailIndex {
		return s.findUserByNormalizedEmailLegacy(ctx, rawEmail, normalizedEmail)
	}

	recs, err := s.store.Index("by_normalized_email").GetAll(ctx, nil, normalizedEmail)
	if err != nil {
		return nil, fmt.Errorf("find user: %w", err)
	}
	if len(recs) == 0 {
		return nil, core.ErrNotFound
	}

	var canonicalMatch indexeddb.Record
	var rawMatch indexeddb.Record
	var fallbackMatch indexeddb.Record
	for _, rec := range recs {
		email := strings.TrimSpace(recString(rec, "email"))
		switch {
		case email == normalizedEmail:
			if preferUserRecord(rec, canonicalMatch) {
				canonicalMatch = rec
			}
		case rawEmail != "" && email == rawEmail:
			if preferUserRecord(rec, rawMatch) {
				rawMatch = rec
			}
		default:
			if preferUserRecord(rec, fallbackMatch) {
				fallbackMatch = rec
			}
		}
	}
	if canonicalMatch == nil && len(recs) > 1 {
		return nil, fmt.Errorf("find user: ambiguous case-insensitive duplicate users for %q", normalizedEmail)
	}
	match := canonicalMatch
	if match == nil {
		match = rawMatch
	}
	if match == nil {
		match = fallbackMatch
	}
	if match == nil {
		return nil, core.ErrNotFound
	}
	if len(recs) == 1 && (recString(match, "email") != normalizedEmail || recString(match, "normalized_email") != normalizedEmail) {
		// Best-effort repair for legacy mixed-case rows. Reads should not fail
		// if the canonicalizing write cannot be completed.
		updated := cloneRecord(match)
		updated["email"] = normalizedEmail
		updated["normalized_email"] = normalizedEmail
		updated["updated_at"] = time.Now()
		if err := s.store.Put(ctx, updated); err == nil {
			match = updated
		}
	}
	return recordToUser(match), nil
}

func (s *UserService) findUserByNormalizedEmailLegacy(ctx context.Context, rawEmail, normalizedEmail string) (*core.User, error) {
	rec, err := s.store.Index("by_email").Get(ctx, normalizedEmail)
	if err == nil {
		return recordToUser(rec), nil
	}
	if err != indexeddb.ErrNotFound {
		return nil, fmt.Errorf("find user: %w", err)
	}

	if rawEmail != "" && rawEmail != normalizedEmail {
		rec, err := s.store.Index("by_email").Get(ctx, rawEmail)
		if err == nil {
			return recordToUser(rec), nil
		}
		if err != indexeddb.ErrNotFound {
			return nil, fmt.Errorf("find user: %w", err)
		}
	}

	recs, err := s.store.GetAll(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("find user: %w", err)
	}

	var canonicalMatch indexeddb.Record
	var rawMatch indexeddb.Record
	var fallbackMatch indexeddb.Record
	matchCount := 0
	for _, rec := range recs {
		email := strings.TrimSpace(recString(rec, "email"))
		if emailutil.Normalize(email) != normalizedEmail {
			continue
		}
		matchCount++
		switch {
		case email == normalizedEmail:
			if preferUserRecord(rec, canonicalMatch) {
				canonicalMatch = rec
			}
		case rawEmail != "" && email == rawEmail:
			if preferUserRecord(rec, rawMatch) {
				rawMatch = rec
			}
		case preferUserRecord(rec, fallbackMatch):
			fallbackMatch = rec
		}
	}
	if canonicalMatch == nil && rawMatch == nil && matchCount > 1 {
		return nil, fmt.Errorf("find user: ambiguous case-insensitive duplicate users for %q", normalizedEmail)
	}
	match := canonicalMatch
	if match == nil {
		match = rawMatch
	}
	if match == nil {
		match = fallbackMatch
	}
	if match == nil {
		return nil, core.ErrNotFound
	}
	if matchCount == 1 && recString(match, "email") != normalizedEmail {
		updated := cloneRecord(match)
		updated["email"] = normalizedEmail
		updated["updated_at"] = time.Now()
		if err := s.store.Put(ctx, updated); err == nil {
			match = updated
		}
	}
	return recordToUser(match), nil
}

func preferUserRecord(candidate, current indexeddb.Record) bool {
	if current == nil {
		return true
	}
	candidateCreated := recTime(candidate, "created_at")
	currentCreated := recTime(current, "created_at")
	switch {
	case candidateCreated.IsZero() && !currentCreated.IsZero():
		return false
	case !candidateCreated.IsZero() && currentCreated.IsZero():
		return true
	case !candidateCreated.Equal(currentCreated):
		return candidateCreated.Before(currentCreated)
	}
	candidateID := recString(candidate, "id")
	currentID := recString(current, "id")
	if candidateID != currentID {
		return candidateID < currentID
	}
	return recString(candidate, "email") < recString(current, "email")
}

func recordToUser(rec indexeddb.Record) *core.User {
	return &core.User{
		ID:          recString(rec, "id"),
		Email:       recString(rec, "email"),
		DisplayName: recString(rec, "display_name"),
		CreatedAt:   recTime(rec, "created_at"),
		UpdatedAt:   recTime(rec, "updated_at"),
	}
}
