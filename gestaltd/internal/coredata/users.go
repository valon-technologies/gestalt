package coredata

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/emailutil"
)

type UserService struct {
	store      indexeddb.ObjectStore
	principals *PrincipalService
	profiles   *UserProfileService
}

func NewUserService(ds indexeddb.IndexedDB, principals *PrincipalService, profiles *UserProfileService) *UserService {
	return &UserService{
		store:      ds.ObjectStore(StoreUsers),
		principals: principals,
		profiles:   profiles,
	}
}

func (s *UserService) BackfillNormalizedEmails(ctx context.Context) error {
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

func (s *UserService) BackfillCanonicalPrincipals(ctx context.Context) error {
	if s.principals == nil || s.profiles == nil {
		return nil
	}
	recs, err := s.store.GetAll(ctx, nil)
	if err != nil {
		return fmt.Errorf("list users for canonical backfill: %w", err)
	}
	winners := preferredCanonicalUserRecords(recs)
	for _, rec := range winners {
		if err := s.syncCanonicalUser(ctx, recordToUser(rec)); err != nil {
			return err
		}
	}
	return nil
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
		if err := s.syncCanonicalUser(ctx, user); err != nil {
			return nil, err
		}
		return user, nil
	case !errors.Is(err, core.ErrNotFound):
		return nil, err
	}

	now := time.Now()
	newRec := indexeddb.Record{
		"id":               uuid.New().String(),
		"email":            email,
		"normalized_email": email,
		"display_name":     "",
		"created_at":       now,
		"updated_at":       now,
	}
	if err := s.store.Add(ctx, newRec); err != nil {
		user, retryErr := s.findUserByNormalizedEmail(ctx, rawEmail, email)
		if retryErr != nil {
			return nil, fmt.Errorf("create user: %w", err)
		}
		if err := s.syncCanonicalUser(ctx, user); err != nil {
			return nil, err
		}
		return user, nil
	}
	user = recordToUser(newRec)
	if err := s.syncCanonicalUser(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
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

func (s *UserService) syncCanonicalUser(ctx context.Context, user *core.User) error {
	if s.principals == nil || s.profiles == nil || user == nil || user.ID == "" {
		return nil
	}
	displayName := strings.TrimSpace(user.DisplayName)
	if displayName == "" {
		displayName = user.Email
	}
	if _, err := s.principals.UpsertPrincipal(ctx, &core.Principal{
		ID:          user.ID,
		Kind:        core.PrincipalKindUser,
		Status:      principalStatusActive,
		DisplayName: displayName,
		CreatedAt:   user.CreatedAt,
		UpdatedAt:   user.UpdatedAt,
	}); err != nil {
		return fmt.Errorf("sync canonical user principal %q: %w", user.ID, err)
	}
	if _, err := s.profiles.UpsertProfile(ctx, &core.UserProfile{
		PrincipalID:     user.ID,
		Email:           user.Email,
		NormalizedEmail: user.Email,
		CreatedAt:       user.CreatedAt,
		UpdatedAt:       user.UpdatedAt,
	}); err != nil {
		return fmt.Errorf("sync canonical user profile %q: %w", user.ID, err)
	}
	return nil
}

func preferredCanonicalUserRecords(recs []indexeddb.Record) []indexeddb.Record {
	if len(recs) == 0 {
		return nil
	}
	grouped := make(map[string][]indexeddb.Record, len(recs))
	groupNormalized := make(map[string]string, len(recs))
	for _, rec := range recs {
		normalizedEmail := emailutil.Normalize(recString(rec, "normalized_email"))
		if normalizedEmail == "" {
			normalizedEmail = emailutil.Normalize(recString(rec, "email"))
		}
		key := normalizedEmail
		if key == "" {
			key = "id:" + recString(rec, "id")
		}
		grouped[key] = append(grouped[key], rec)
		groupNormalized[key] = normalizedEmail
	}
	out := make([]indexeddb.Record, 0, len(grouped))
	keys := make([]string, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if winner := preferredCanonicalUserRecord(grouped[key], groupNormalized[key]); winner != nil {
			out = append(out, winner)
		}
	}
	return out
}

func preferredCanonicalUserRecord(recs []indexeddb.Record, normalizedEmail string) indexeddb.Record {
	var canonicalMatch indexeddb.Record
	var fallbackMatch indexeddb.Record
	for _, rec := range recs {
		email := strings.TrimSpace(recString(rec, "email"))
		switch {
		case normalizedEmail != "" && email == normalizedEmail:
			if preferUserRecord(rec, canonicalMatch) {
				canonicalMatch = rec
			}
		default:
			if preferUserRecord(rec, fallbackMatch) {
				fallbackMatch = rec
			}
		}
	}
	if canonicalMatch != nil {
		return canonicalMatch
	}
	return fallbackMatch
}
