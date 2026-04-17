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
	store        indexeddb.ObjectStore
	identities   *IdentityService
	authBindings *IdentityAuthBindingService
}

func NewUserService(ds indexeddb.IndexedDB, identities *IdentityService, authBindings *IdentityAuthBindingService) *UserService {
	return &UserService{
		store:        ds.ObjectStore(StoreUsers),
		identities:   identities,
		authBindings: authBindings,
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

func (s *UserService) BackfillCanonicalIdentities(ctx context.Context) error {
	if s.identities == nil || s.authBindings == nil {
		return nil
	}
	recs, err := s.store.GetAll(ctx, nil)
	if err != nil {
		return fmt.Errorf("list users for canonical backfill: %w", err)
	}
	winners := preferredCanonicalUserRecords(recs)
	for _, rec := range winners {
		if err := s.syncCanonicalIdentity(ctx, recordToUser(rec)); err != nil {
			return err
		}
	}
	return nil
}

func (s *UserService) CanonicalIdentityIDForUser(ctx context.Context, userID string) (string, error) {
	rec, err := s.canonicalUserRecordForUserID(ctx, strings.TrimSpace(userID))
	if err != nil {
		return "", err
	}
	return recString(rec, "id"), nil
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
		if err := s.syncCanonicalIdentity(ctx, user); err != nil {
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
		if err := s.syncCanonicalIdentity(ctx, user); err != nil {
			return nil, err
		}
		return user, nil
	}
	user = recordToUser(newRec)
	if err := s.syncCanonicalIdentity(ctx, user); err != nil {
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

func (s *UserService) canonicalUserRecordForUserID(ctx context.Context, userID string) (indexeddb.Record, error) {
	if userID == "" {
		return nil, core.ErrNotFound
	}
	rec, err := s.store.Get(ctx, userID)
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("lookup canonical user record: %w", err)
	}
	normalizedEmail := emailutil.Normalize(recString(rec, "normalized_email"))
	if normalizedEmail == "" {
		normalizedEmail = emailutil.Normalize(recString(rec, "email"))
	}
	if normalizedEmail == "" {
		return rec, nil
	}
	recs, err := s.store.Index("by_normalized_email").GetAll(ctx, nil, normalizedEmail)
	if err != nil {
		return nil, fmt.Errorf("lookup canonical user record: %w", err)
	}
	if len(recs) == 0 {
		return rec, nil
	}
	winner := preferredCanonicalUserRecord(recs, normalizedEmail)
	if winner == nil {
		return rec, nil
	}
	return winner, nil
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

func (s *UserService) syncCanonicalIdentity(ctx context.Context, user *core.User) error {
	if s.identities == nil || s.authBindings == nil || user == nil || user.ID == "" {
		return nil
	}
	canonicalRec, err := s.canonicalUserRecordForUserID(ctx, user.ID)
	if err != nil {
		return err
	}
	canonicalUser := recordToUser(canonicalRec)
	displayName := strings.TrimSpace(canonicalUser.DisplayName)
	if displayName == "" {
		displayName = canonicalUser.Email
	}
	if _, err := s.identities.UpsertIdentity(ctx, &core.Identity{
		ID:          canonicalUser.ID,
		Status:      identityStatusActive,
		DisplayName: displayName,
		MetadataJSON: legacyIdentityMetadataJSON("user", map[string]string{
			"email": emailutil.Normalize(canonicalUser.Email),
		}),
		CreatedAt: canonicalUser.CreatedAt,
		UpdatedAt: canonicalUser.UpdatedAt,
	}); err != nil {
		return fmt.Errorf("sync canonical identity %q: %w", canonicalUser.ID, err)
	}
	lookupKey := emailutil.Normalize(canonicalUser.Email)
	if lookupKey == "" {
		return nil
	}
	if _, err := s.authBindings.UpsertBinding(ctx, &core.IdentityAuthBinding{
		IdentityID:  canonicalUser.ID,
		BindingKind: core.IdentityAuthBindingKindEmail,
		Authority:   legacyIdentityBindingAuthority,
		LookupKey:   lookupKey,
		BindingJSON: legacyIdentityMetadataJSON("email", map[string]string{"email": lookupKey}),
		CreatedAt:   canonicalUser.CreatedAt,
		UpdatedAt:   canonicalUser.UpdatedAt,
	}); err != nil {
		return fmt.Errorf("sync canonical identity auth binding %q: %w", canonicalUser.ID, err)
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
