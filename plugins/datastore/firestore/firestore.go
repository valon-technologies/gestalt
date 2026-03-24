package firestore

import (
	"context"
	"fmt"
	"time"

	gcpfirestore "cloud.google.com/go/firestore"
	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/crypto"
	"github.com/valon-technologies/gestalt/plugins/datastore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const usersByEmailCollection = "users_by_email"

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

// Firestore creates collections implicitly; composite indexes must be
// created externally via gcloud CLI or Firebase console.
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
		if err := tx.Create(lookupRef, map[string]string{"user_id": id}); err != nil {
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
	iter := s.client.Collection(datastore.UsersCollection).Where("email", "==", email).Limit(1).Documents(ctx)
	defer iter.Stop()

	snap, err := iter.Next()
	if err == iterator.Done {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("firestore: querying user by email: %w", err)
	}
	return snapToUser(snap)
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

// --- Integration Tokens ---

type integrationTokenDoc struct {
	UserID                string     `firestore:"user_id"`
	Integration           string     `firestore:"integration"`
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

func (s *Store) StoreToken(ctx context.Context, token *core.IntegrationToken) error {
	accessEnc, refreshEnc, err := s.enc.EncryptTokenPair(token.AccessToken, token.RefreshToken)
	if err != nil {
		return fmt.Errorf("firestore: %w", err)
	}

	doc := integrationTokenDoc{
		UserID:                token.UserID,
		Integration:           token.Integration,
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

	// Atomically delete any existing token for the same
	// (user_id, integration, instance) triple, then write the new one.
	// This mirrors the INSERT ... ON CONFLICT behavior of the SQL stores.
	return s.client.RunTransaction(ctx, func(_ context.Context, tx *gcpfirestore.Transaction) error {
		query := s.client.Collection(datastore.IntegrationTokensCollection).
			Where("user_id", "==", token.UserID).
			Where("integration", "==", token.Integration).
			Where("instance", "==", token.Instance).
			Limit(1)
		iter := tx.Documents(query)
		defer iter.Stop()

		snap, err := iter.Next()
		if err != nil && err != iterator.Done {
			return fmt.Errorf("querying existing token: %w", err)
		}
		if err == nil && snap.Ref.ID != token.ID {
			if err := tx.Delete(snap.Ref); err != nil {
				return fmt.Errorf("deleting stale integration token: %w", err)
			}
		}
		return tx.Set(s.client.Collection(datastore.IntegrationTokensCollection).Doc(token.ID), doc)
	})
}

func (s *Store) Token(ctx context.Context, userID, integration, instance string) (*core.IntegrationToken, error) {
	iter := s.client.Collection(datastore.IntegrationTokensCollection).
		Where("user_id", "==", userID).
		Where("integration", "==", integration).
		Where("instance", "==", instance).
		Limit(1).
		Documents(ctx)
	defer iter.Stop()

	snap, err := iter.Next()
	if err == iterator.Done {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("firestore: querying token: %w", err)
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

func (s *Store) DeleteToken(ctx context.Context, id string) error {
	_, err := s.client.Collection(datastore.IntegrationTokensCollection).Doc(id).Delete(ctx)
	if err != nil {
		return fmt.Errorf("firestore: deleting token: %w", err)
	}
	return nil
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
	_, err := s.client.Collection(datastore.APITokensCollection).Doc(token.ID).Set(ctx, doc)
	if err != nil {
		return fmt.Errorf("firestore: storing api token: %w", err)
	}
	return nil
}

func (s *Store) ValidateAPIToken(ctx context.Context, hashedToken string) (*core.APIToken, error) {
	iter := s.client.Collection(datastore.APITokensCollection).
		Where("hashed_token", "==", hashedToken).
		Limit(1).
		Documents(ctx)
	defer iter.Stop()

	snap, err := iter.Next()
	if err == iterator.Done {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("firestore: validating api token: %w", err)
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
	snap, err := s.client.Collection(datastore.APITokensCollection).Doc(id).Get(ctx)
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
	_, err = s.client.Collection(datastore.APITokensCollection).Doc(id).Delete(ctx)
	if err != nil {
		return fmt.Errorf("firestore: revoking api token: %w", err)
	}
	return nil
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
