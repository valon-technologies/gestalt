package firestore

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	gcpfirestore "cloud.google.com/go/firestore"
	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/internal/drivers/datastore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	usersByEmailCollection         = "users_by_email"
	integrationTokenKeysCollection = "integration_token_keys"
	apiTokensByHashCollection      = "api_tokens_by_hash"
)

type Store struct {
	client *gcpfirestore.Client
	enc    *crypto.AESGCMEncryptor
}

var _ core.Datastore = (*Store)(nil)

func New(projectID, database string, encryptionKey []byte) (*Store, error) {
	ctx := context.Background()
	var (
		client *gcpfirestore.Client
		err    error
	)
	if database != "" {
		client, err = gcpfirestore.NewClientWithDatabase(ctx, projectID, database)
	} else {
		client, err = gcpfirestore.NewClient(ctx, projectID)
	}
	if err != nil {
		return nil, fmt.Errorf("firestore: creating client: %w", err)
	}

	enc, err := crypto.NewAESGCM(encryptionKey)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("firestore: creating encryptor: %w", err)
	}

	return &Store{client: client, enc: enc}, nil
}

func (s *Store) Ping(ctx context.Context) error {
	iter := s.client.Collection(datastore.UsersCollection).Limit(1).Documents(ctx)
	defer iter.Stop()
	_, err := iter.Next()
	if err == iterator.Done {
		return nil
	}
	return err
}

// Firestore creates collections implicitly. This datastore avoids composite
// indexes by using deterministic lookup documents for uniqueness and point
// lookups, then filtering in memory where appropriate.
func (s *Store) Migrate(_ context.Context) error {
	return nil
}

func (s *Store) Close() error {
	return s.client.Close()
}

// --- Users ---

type userDoc struct {
	Email       string    `firestore:"email"`
	DisplayName string    `firestore:"display_name"`
	CreatedAt   time.Time `firestore:"created_at"`
	UpdatedAt   time.Time `firestore:"updated_at"`
}

type userLookupDoc struct {
	UserID string `firestore:"user_id"`
}

func (s *Store) GetUser(ctx context.Context, id string) (*core.User, error) {
	snap, err := s.client.Collection(datastore.UsersCollection).Doc(id).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("firestore: getting user: %w", err)
	}
	return snapToUser(snap)
}

func (s *Store) FindOrCreateUser(ctx context.Context, email string) (*core.User, error) {
	user, err := s.findUserByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if user != nil {
		return user, nil
	}

	now := time.Now().UTC().Truncate(time.Second)
	id := uuid.NewString()
	doc := userDoc{
		Email:     email,
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Use a transaction to prevent concurrent creation of duplicate
	// email entries. Firestore has no unique-constraint equivalent, so
	// we maintain a synthetic "users_by_email" lookup document keyed
	// by email. The transaction atomically checks-then-creates both
	// the lookup doc and the real user doc.
	lookupRef := s.client.Collection(usersByEmailCollection).Doc(email)
	userRef := s.client.Collection(datastore.UsersCollection).Doc(id)

	var created bool
	err = s.client.RunTransaction(ctx, func(_ context.Context, tx *gcpfirestore.Transaction) error {
		snap, err := tx.Get(lookupRef)
		if err != nil && status.Code(err) != codes.NotFound {
			return fmt.Errorf("checking email lookup: %w", err)
		}
		if snap != nil && snap.Exists() {
			created = false
			return nil
		}
		if err := tx.Create(lookupRef, userLookupDoc{UserID: id}); err != nil {
			return err
		}
		created = true
		return tx.Create(userRef, doc)
	})
	if err != nil {
		user, err2 := s.findUserByEmail(ctx, email)
		if err2 != nil {
			return nil, fmt.Errorf("firestore: re-querying user after conflict: %w", err2)
		}
		if user != nil {
			return user, nil
		}
		return nil, fmt.Errorf("firestore: creating user: %w", err)
	}
	if !created {
		user, err = s.findUserByEmail(ctx, email)
		if err != nil {
			return nil, fmt.Errorf("firestore: querying existing user: %w", err)
		}
		if user == nil {
			return nil, fmt.Errorf("firestore: email lookup exists but user not found")
		}
		return user, nil
	}

	return &core.User{
		ID:        id,
		Email:     email,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (s *Store) findUserByEmail(ctx context.Context, email string) (*core.User, error) {
	lookupSnap, err := s.client.Collection(usersByEmailCollection).Doc(email).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("firestore: getting user email lookup: %w", err)
	}

	var lookup userLookupDoc
	if err := lookupSnap.DataTo(&lookup); err != nil {
		return nil, fmt.Errorf("firestore: unmarshalling user email lookup: %w", err)
	}

	user, err := s.GetUser(ctx, lookup.UserID)
	if err == core.ErrNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("firestore: getting user from email lookup: %w", err)
	}
	return user, nil
}

func snapToUser(snap *gcpfirestore.DocumentSnapshot) (*core.User, error) {
	var doc userDoc
	if err := snap.DataTo(&doc); err != nil {
		return nil, fmt.Errorf("firestore: unmarshalling user: %w", err)
	}
	return &core.User{
		ID:          snap.Ref.ID,
		Email:       doc.Email,
		DisplayName: doc.DisplayName,
		CreatedAt:   doc.CreatedAt,
		UpdatedAt:   doc.UpdatedAt,
	}, nil
}

func firestoreDocKey(parts ...string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(parts, "\x1f")))
}

// --- Integration Tokens ---

type integrationTokenDoc struct {
	UserID                string     `firestore:"user_id"`
	Integration           string     `firestore:"integration"`
	Connection            string     `firestore:"connection"`
	Instance              string     `firestore:"instance"`
	AccessTokenEncrypted  string     `firestore:"access_token_encrypted"`
	RefreshTokenEncrypted string     `firestore:"refresh_token_encrypted"`
	Scopes                string     `firestore:"scopes"`
	ExpiresAt             *time.Time `firestore:"expires_at"`
	LastRefreshedAt       *time.Time `firestore:"last_refreshed_at"`
	RefreshErrorCount     int        `firestore:"refresh_error_count"`
	MetadataJSON          string     `firestore:"metadata_json"`
	CreatedAt             time.Time  `firestore:"created_at"`
	UpdatedAt             time.Time  `firestore:"updated_at"`
}

type integrationTokenLookupDoc struct {
	TokenID string `firestore:"token_id"`
}

func (s *Store) StoreToken(ctx context.Context, token *core.IntegrationToken) error {
	accessEnc, refreshEnc, err := s.enc.EncryptTokenPair(token.AccessToken, token.RefreshToken)
	if err != nil {
		return fmt.Errorf("firestore: %w", err)
	}

	doc := integrationTokenDoc{
		UserID:                token.UserID,
		Integration:           token.Integration,
		Connection:            token.Connection,
		Instance:              token.Instance,
		AccessTokenEncrypted:  accessEnc,
		RefreshTokenEncrypted: refreshEnc,
		Scopes:                token.Scopes,
		ExpiresAt:             token.ExpiresAt,
		LastRefreshedAt:       token.LastRefreshedAt,
		RefreshErrorCount:     token.RefreshErrorCount,
		MetadataJSON:          token.MetadataJSON,
		CreatedAt:             token.CreatedAt,
		UpdatedAt:             token.UpdatedAt,
	}

	lookupRef := s.client.Collection(integrationTokenKeysCollection).Doc(
		firestoreDocKey(token.UserID, token.Integration, token.Connection, token.Instance),
	)
	tokenRef := s.client.Collection(datastore.IntegrationTokensCollection).Doc(token.ID)

	return s.client.RunTransaction(ctx, func(_ context.Context, tx *gcpfirestore.Transaction) error {
		existingTokenSnap, err := tx.Get(tokenRef)
		if err != nil && status.Code(err) != codes.NotFound {
			return fmt.Errorf("getting existing token by id: %w", err)
		}
		if err == nil && existingTokenSnap.Exists() {
			var existing integrationTokenDoc
			if err := existingTokenSnap.DataTo(&existing); err != nil {
				return fmt.Errorf("unmarshalling existing token by id: %w", err)
			}
			oldLookupRef := s.client.Collection(integrationTokenKeysCollection).Doc(
				firestoreDocKey(existing.UserID, existing.Integration, existing.Connection, existing.Instance),
			)
			if oldLookupRef.ID != lookupRef.ID {
				if err := tx.Delete(oldLookupRef); err != nil {
					return fmt.Errorf("deleting stale token lookup: %w", err)
				}
			}
		}

		lookupSnap, err := tx.Get(lookupRef)
		if err != nil && status.Code(err) != codes.NotFound {
			return fmt.Errorf("getting token lookup: %w", err)
		}
		if err == nil && lookupSnap.Exists() {
			var lookup integrationTokenLookupDoc
			if err := lookupSnap.DataTo(&lookup); err != nil {
				return fmt.Errorf("unmarshalling token lookup: %w", err)
			}
			if lookup.TokenID != "" && lookup.TokenID != token.ID {
				if err := tx.Delete(s.client.Collection(datastore.IntegrationTokensCollection).Doc(lookup.TokenID)); err != nil {
					return fmt.Errorf("deleting stale integration token: %w", err)
				}
			}
		}

		if err := tx.Set(lookupRef, integrationTokenLookupDoc{TokenID: token.ID}); err != nil {
			return fmt.Errorf("storing token lookup: %w", err)
		}
		return tx.Set(tokenRef, doc)
	})
}

func (s *Store) Token(ctx context.Context, userID, integration, connection, instance string) (*core.IntegrationToken, error) {
	lookupSnap, err := s.client.Collection(integrationTokenKeysCollection).Doc(
		firestoreDocKey(userID, integration, connection, instance),
	).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("firestore: getting token lookup: %w", err)
	}

	var lookup integrationTokenLookupDoc
	if err := lookupSnap.DataTo(&lookup); err != nil {
		return nil, fmt.Errorf("firestore: unmarshalling token lookup: %w", err)
	}

	snap, err := s.client.Collection(datastore.IntegrationTokensCollection).Doc(lookup.TokenID).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("firestore: getting token by id: %w", err)
	}
	return s.snapToIntegrationToken(snap)
}

func (s *Store) ListTokens(ctx context.Context, userID string) ([]*core.IntegrationToken, error) {
	iter := s.client.Collection(datastore.IntegrationTokensCollection).
		Where("user_id", "==", userID).
		Documents(ctx)
	defer iter.Stop()

	var tokens []*core.IntegrationToken
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("firestore: listing tokens: %w", err)
		}
		t, err := s.snapToIntegrationToken(snap)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, nil
}

func (s *Store) ListTokensForIntegration(ctx context.Context, userID, integration string) ([]*core.IntegrationToken, error) {
	tokens, err := s.ListTokens(ctx, userID)
	if err != nil {
		return nil, err
	}

	filtered := make([]*core.IntegrationToken, 0, len(tokens))
	for _, token := range tokens {
		if token.Integration == integration {
			filtered = append(filtered, token)
		}
	}
	return filtered, nil
}

func (s *Store) ListTokensForConnection(ctx context.Context, userID, integration, connection string) ([]*core.IntegrationToken, error) {
	tokens, err := s.ListTokensForIntegration(ctx, userID, integration)
	if err != nil {
		return nil, err
	}

	filtered := make([]*core.IntegrationToken, 0, len(tokens))
	for _, token := range tokens {
		if token.Connection == connection {
			filtered = append(filtered, token)
		}
	}
	return filtered, nil
}

func (s *Store) DeleteToken(ctx context.Context, id string) error {
	tokenRef := s.client.Collection(datastore.IntegrationTokensCollection).Doc(id)
	snap, err := tokenRef.Get(ctx)
	if status.Code(err) == codes.NotFound {
		return nil
	}
	if err != nil {
		return fmt.Errorf("firestore: getting token for delete: %w", err)
	}

	var doc integrationTokenDoc
	if err := snap.DataTo(&doc); err != nil {
		return fmt.Errorf("firestore: unmarshalling token for delete: %w", err)
	}

	lookupRef := s.client.Collection(integrationTokenKeysCollection).Doc(
		firestoreDocKey(doc.UserID, doc.Integration, doc.Connection, doc.Instance),
	)

	return s.client.RunTransaction(ctx, func(_ context.Context, tx *gcpfirestore.Transaction) error {
		lookupSnap, err := tx.Get(lookupRef)
		if err != nil && status.Code(err) != codes.NotFound {
			return fmt.Errorf("getting token lookup for delete: %w", err)
		}
		if err == nil && lookupSnap.Exists() {
			var lookup integrationTokenLookupDoc
			if err := lookupSnap.DataTo(&lookup); err != nil {
				return fmt.Errorf("unmarshalling token lookup for delete: %w", err)
			}
			if lookup.TokenID == id {
				if err := tx.Delete(lookupRef); err != nil {
					return fmt.Errorf("deleting token lookup: %w", err)
				}
			}
		}
		return tx.Delete(tokenRef)
	})
}

func (s *Store) snapToIntegrationToken(snap *gcpfirestore.DocumentSnapshot) (*core.IntegrationToken, error) {
	var doc integrationTokenDoc
	if err := snap.DataTo(&doc); err != nil {
		return nil, fmt.Errorf("firestore: unmarshalling integration token: %w", err)
	}

	access, refresh, err := s.enc.DecryptTokenPair(doc.AccessTokenEncrypted, doc.RefreshTokenEncrypted)
	if err != nil {
		return nil, fmt.Errorf("firestore: %w", err)
	}

	return &core.IntegrationToken{
		ID:                snap.Ref.ID,
		UserID:            doc.UserID,
		Integration:       doc.Integration,
		Connection:        doc.Connection,
		Instance:          doc.Instance,
		AccessToken:       access,
		RefreshToken:      refresh,
		Scopes:            doc.Scopes,
		ExpiresAt:         doc.ExpiresAt,
		LastRefreshedAt:   doc.LastRefreshedAt,
		RefreshErrorCount: doc.RefreshErrorCount,
		MetadataJSON:      doc.MetadataJSON,
		CreatedAt:         doc.CreatedAt,
		UpdatedAt:         doc.UpdatedAt,
	}, nil
}

// --- API Tokens ---

type apiTokenDoc struct {
	UserID      string     `firestore:"user_id"`
	Name        string     `firestore:"name"`
	HashedToken string     `firestore:"hashed_token"`
	Scopes      string     `firestore:"scopes"`
	ExpiresAt   *time.Time `firestore:"expires_at"`
	CreatedAt   time.Time  `firestore:"created_at"`
	UpdatedAt   time.Time  `firestore:"updated_at"`
}

type apiTokenHashLookupDoc struct {
	TokenID string `firestore:"token_id"`
}

func (s *Store) StoreAPIToken(ctx context.Context, token *core.APIToken) error {
	doc := apiTokenDoc{
		UserID:      token.UserID,
		Name:        token.Name,
		HashedToken: token.HashedToken,
		Scopes:      token.Scopes,
		ExpiresAt:   token.ExpiresAt,
		CreatedAt:   token.CreatedAt,
		UpdatedAt:   token.UpdatedAt,
	}

	hashRef := s.client.Collection(apiTokensByHashCollection).Doc(firestoreDocKey(token.HashedToken))
	tokenRef := s.client.Collection(datastore.APITokensCollection).Doc(token.ID)

	return s.client.RunTransaction(ctx, func(_ context.Context, tx *gcpfirestore.Transaction) error {
		existingTokenSnap, err := tx.Get(tokenRef)
		if err != nil && status.Code(err) != codes.NotFound {
			return fmt.Errorf("getting existing api token by id: %w", err)
		}
		if err == nil && existingTokenSnap.Exists() {
			var existing apiTokenDoc
			if err := existingTokenSnap.DataTo(&existing); err != nil {
				return fmt.Errorf("unmarshalling existing api token by id: %w", err)
			}
			oldHashRef := s.client.Collection(apiTokensByHashCollection).Doc(firestoreDocKey(existing.HashedToken))
			if oldHashRef.ID != hashRef.ID {
				if err := tx.Delete(oldHashRef); err != nil {
					return fmt.Errorf("deleting stale api token hash lookup: %w", err)
				}
			}
		}

		hashSnap, err := tx.Get(hashRef)
		if err != nil && status.Code(err) != codes.NotFound {
			return fmt.Errorf("getting api token hash lookup: %w", err)
		}
		if err == nil && hashSnap.Exists() {
			var lookup apiTokenHashLookupDoc
			if err := hashSnap.DataTo(&lookup); err != nil {
				return fmt.Errorf("unmarshalling api token hash lookup: %w", err)
			}
			if lookup.TokenID != "" && lookup.TokenID != token.ID {
				return fmt.Errorf("firestore: hashed token already exists")
			}
		}

		if err := tx.Set(hashRef, apiTokenHashLookupDoc{TokenID: token.ID}); err != nil {
			return fmt.Errorf("storing api token hash lookup: %w", err)
		}
		return tx.Set(tokenRef, doc)
	})
}

func (s *Store) ValidateAPIToken(ctx context.Context, hashedToken string) (*core.APIToken, error) {
	lookupSnap, err := s.client.Collection(apiTokensByHashCollection).Doc(firestoreDocKey(hashedToken)).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("firestore: getting api token hash lookup: %w", err)
	}

	var lookup apiTokenHashLookupDoc
	if err := lookupSnap.DataTo(&lookup); err != nil {
		return nil, fmt.Errorf("firestore: unmarshalling api token hash lookup: %w", err)
	}

	snap, err := s.client.Collection(datastore.APITokensCollection).Doc(lookup.TokenID).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("firestore: getting api token by id: %w", err)
	}

	token, err := snapToAPIToken(snap)
	if err != nil {
		return nil, err
	}
	if token.ExpiresAt != nil && time.Now().After(*token.ExpiresAt) {
		return nil, nil
	}
	return token, nil
}

func (s *Store) ListAPITokens(ctx context.Context, userID string) ([]*core.APIToken, error) {
	iter := s.client.Collection(datastore.APITokensCollection).
		Where("user_id", "==", userID).
		Documents(ctx)
	defer iter.Stop()

	var tokens []*core.APIToken
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("firestore: listing api tokens: %w", err)
		}
		t, err := snapToAPIToken(snap)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, nil
}

func (s *Store) RevokeAPIToken(ctx context.Context, userID, id string) error {
	tokenRef := s.client.Collection(datastore.APITokensCollection).Doc(id)
	snap, err := tokenRef.Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return core.ErrNotFound
		}
		return fmt.Errorf("firestore: revoking api token: %w", err)
	}
	var doc apiTokenDoc
	if err := snap.DataTo(&doc); err != nil {
		return fmt.Errorf("firestore: unmarshalling api token: %w", err)
	}
	if doc.UserID != userID {
		return core.ErrNotFound
	}

	hashRef := s.client.Collection(apiTokensByHashCollection).Doc(firestoreDocKey(doc.HashedToken))
	return s.client.RunTransaction(ctx, func(_ context.Context, tx *gcpfirestore.Transaction) error {
		if err := tx.Delete(tokenRef); err != nil {
			return fmt.Errorf("deleting api token: %w", err)
		}

		hashSnap, err := tx.Get(hashRef)
		if err != nil && status.Code(err) != codes.NotFound {
			return fmt.Errorf("getting api token hash lookup for revoke: %w", err)
		}
		if err == nil && hashSnap.Exists() {
			var lookup apiTokenHashLookupDoc
			if err := hashSnap.DataTo(&lookup); err != nil {
				return fmt.Errorf("unmarshalling api token hash lookup for revoke: %w", err)
			}
			if lookup.TokenID == id {
				if err := tx.Delete(hashRef); err != nil {
					return fmt.Errorf("deleting api token hash lookup: %w", err)
				}
			}
		}
		return nil
	})
}

func (s *Store) RevokeAllAPITokens(ctx context.Context, userID string) (int64, error) {
	iter := s.client.Collection(datastore.APITokensCollection).
		Where("user_id", "==", userID).
		Documents(ctx)
	defer iter.Stop()

	bw := s.client.BulkWriter(ctx)
	var jobs []*gcpfirestore.BulkWriterJob
	var deleted int64
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("firestore: iterating api tokens for revoke-all: %w", err)
		}
		var doc apiTokenDoc
		if err := snap.DataTo(&doc); err != nil {
			return 0, fmt.Errorf("firestore: unmarshalling api token for revoke-all: %w", err)
		}
		job, err := bw.Delete(snap.Ref)
		if err != nil {
			return 0, fmt.Errorf("firestore: queuing delete for revoke-all: %w", err)
		}
		jobs = append(jobs, job)
		job, err = bw.Delete(s.client.Collection(apiTokensByHashCollection).Doc(firestoreDocKey(doc.HashedToken)))
		if err != nil {
			return 0, fmt.Errorf("firestore: queuing hash lookup delete for revoke-all: %w", err)
		}
		jobs = append(jobs, job)
		deleted++
	}
	bw.End()

	for _, job := range jobs {
		if _, err := job.Results(); err != nil {
			return 0, fmt.Errorf("firestore: deleting api token in revoke-all: %w", err)
		}
	}
	return deleted, nil
}

func snapToAPIToken(snap *gcpfirestore.DocumentSnapshot) (*core.APIToken, error) {
	var doc apiTokenDoc
	if err := snap.DataTo(&doc); err != nil {
		return nil, fmt.Errorf("firestore: unmarshalling api token: %w", err)
	}
	return &core.APIToken{
		ID:          snap.Ref.ID,
		UserID:      doc.UserID,
		Name:        doc.Name,
		HashedToken: doc.HashedToken,
		Scopes:      doc.Scopes,
		ExpiresAt:   doc.ExpiresAt,
		CreatedAt:   doc.CreatedAt,
		UpdatedAt:   doc.UpdatedAt,
	}, nil
}
