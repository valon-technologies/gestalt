package mongodb

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/crypto"
	"github.com/valon-technologies/gestalt/plugins/datastore"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Store struct {
	client *mongo.Client
	db     *mongo.Database
	enc    *crypto.AESGCMEncryptor
}

type userDoc struct {
	ID          string    `bson:"_id"`
	Email       string    `bson:"email"`
	DisplayName string    `bson:"display_name"`
	CreatedAt   time.Time `bson:"created_at"`
	UpdatedAt   time.Time `bson:"updated_at"`
}

type integrationTokenDoc struct {
	ID                    string     `bson:"_id"`
	UserID                string     `bson:"user_id"`
	Integration           string     `bson:"integration"`
	Instance              string     `bson:"instance"`
	AccessTokenEncrypted  string     `bson:"access_token_encrypted"`
	RefreshTokenEncrypted string     `bson:"refresh_token_encrypted"`
	Scopes                string     `bson:"scopes"`
	ExpiresAt             *time.Time `bson:"expires_at"`
	LastRefreshedAt       *time.Time `bson:"last_refreshed_at"`
	RefreshErrorCount     int        `bson:"refresh_error_count"`
	MetadataJSON          string     `bson:"metadata_json"`
	CreatedAt             time.Time  `bson:"created_at"`
	UpdatedAt             time.Time  `bson:"updated_at"`
}

type apiTokenDoc struct {
	ID          string     `bson:"_id"`
	UserID      string     `bson:"user_id"`
	Name        string     `bson:"name"`
	HashedToken string     `bson:"hashed_token"`
	Scopes      string     `bson:"scopes"`
	ExpiresAt   *time.Time `bson:"expires_at"`
	CreatedAt   time.Time  `bson:"created_at"`
	UpdatedAt   time.Time  `bson:"updated_at"`
}

func New(uri, database string, encryptionKey []byte) (*Store, error) {
	enc, err := crypto.NewAESGCM(encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("mongodb: creating encryptor: %w", err)
	}

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("mongodb: connecting: %w", err)
	}

	if err := client.Ping(context.Background(), nil); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("mongodb: ping: %w", err)
	}

	return &Store{
		client: client,
		db:     client.Database(database),
		enc:    enc,
	}, nil
}

func (s *Store) Ping(ctx context.Context) error {
	return s.client.Ping(ctx, nil)
}

func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.db.Collection(datastore.UsersCollection).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "email", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return fmt.Errorf("mongodb: creating users email index: %w", err)
	}

	_, err = s.db.Collection(datastore.IntegrationTokensCollection).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "integration", Value: 1}, {Key: "instance", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return fmt.Errorf("mongodb: creating integration_tokens compound index: %w", err)
	}

	_, err = s.db.Collection(datastore.APITokensCollection).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "hashed_token", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return fmt.Errorf("mongodb: creating api_tokens hashed_token index: %w", err)
	}

	return nil
}

func (s *Store) Close() error {
	return s.client.Disconnect(context.Background())
}

func (s *Store) GetUser(ctx context.Context, id string) (*core.User, error) {
	var doc userDoc
	err := s.db.Collection(datastore.UsersCollection).FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, core.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("mongodb: getting user: %w", err)
	}
	return userFromDoc(&doc), nil
}

func (s *Store) FindOrCreateUser(ctx context.Context, email string) (*core.User, error) {
	var doc userDoc
	err := s.db.Collection(datastore.UsersCollection).FindOne(ctx, bson.M{"email": email}).Decode(&doc)
	if err == nil {
		return userFromDoc(&doc), nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, fmt.Errorf("mongodb: querying user by email: %w", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	doc = userDoc{
		ID:        uuid.NewString(),
		Email:     email,
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err = s.db.Collection(datastore.UsersCollection).InsertOne(ctx, doc)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			err2 := s.db.Collection(datastore.UsersCollection).FindOne(ctx, bson.M{"email": email}).Decode(&doc)
			if err2 != nil {
				return nil, fmt.Errorf("mongodb: re-querying user after duplicate key: %w", err2)
			}
			return userFromDoc(&doc), nil
		}
		return nil, fmt.Errorf("mongodb: inserting user: %w", err)
	}
	return userFromDoc(&doc), nil
}

func (s *Store) StoreToken(ctx context.Context, token *core.IntegrationToken) error {
	accessEnc, refreshEnc, err := s.enc.EncryptTokenPair(token.AccessToken, token.RefreshToken)
	if err != nil {
		return fmt.Errorf("mongodb: %w", err)
	}

	filter := bson.M{"user_id": token.UserID, "integration": token.Integration, "instance": token.Instance}
	update := bson.M{
		"$set": bson.M{
			"access_token_encrypted":  accessEnc,
			"refresh_token_encrypted": refreshEnc,
			"scopes":                  token.Scopes,
			"expires_at":              token.ExpiresAt,
			"last_refreshed_at":       token.LastRefreshedAt,
			"refresh_error_count":     token.RefreshErrorCount,
			"metadata_json":           token.MetadataJSON,
			"updated_at":              token.UpdatedAt,
		},
		"$setOnInsert": bson.M{
			"_id":         token.ID,
			"user_id":     token.UserID,
			"integration": token.Integration,
			"instance":    token.Instance,
			"created_at":  token.CreatedAt,
		},
	}
	opts := options.UpdateOne().SetUpsert(true)
	_, err = s.db.Collection(datastore.IntegrationTokensCollection).UpdateOne(ctx, filter, update, opts)
	if err != nil {
		return fmt.Errorf("mongodb: upserting integration token: %w", err)
	}
	return nil
}

func (s *Store) Token(ctx context.Context, userID, integration, instance string) (*core.IntegrationToken, error) {
	filter := bson.M{"user_id": userID, "integration": integration, "instance": instance}
	var doc integrationTokenDoc
	err := s.db.Collection(datastore.IntegrationTokensCollection).FindOne(ctx, filter).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("mongodb: querying token: %w", err)
	}
	return s.integrationTokenFromDoc(&doc)
}

func (s *Store) ListTokens(ctx context.Context, userID string) ([]*core.IntegrationToken, error) {
	cursor, err := s.db.Collection(datastore.IntegrationTokensCollection).Find(ctx, bson.M{"user_id": userID})
	if err != nil {
		return nil, fmt.Errorf("mongodb: listing tokens: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	var tokens []*core.IntegrationToken
	for cursor.Next(ctx) {
		var doc integrationTokenDoc
		if err := cursor.Decode(&doc); err != nil {
			return nil, fmt.Errorf("mongodb: decoding token: %w", err)
		}
		t, err := s.integrationTokenFromDoc(&doc)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, cursor.Err()
}

func (s *Store) ListTokensForIntegration(ctx context.Context, userID, integration string) ([]*core.IntegrationToken, error) {
	cursor, err := s.db.Collection(datastore.IntegrationTokensCollection).Find(ctx, bson.M{"user_id": userID, "integration": integration})
	if err != nil {
		return nil, fmt.Errorf("mongodb: listing tokens for integration: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	var tokens []*core.IntegrationToken
	for cursor.Next(ctx) {
		var doc integrationTokenDoc
		if err := cursor.Decode(&doc); err != nil {
			return nil, fmt.Errorf("mongodb: decoding token: %w", err)
		}
		t, err := s.integrationTokenFromDoc(&doc)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, cursor.Err()
}

func (s *Store) DeleteToken(ctx context.Context, id string) error {
	_, err := s.db.Collection(datastore.IntegrationTokensCollection).DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return fmt.Errorf("mongodb: deleting token: %w", err)
	}
	return nil
}

func (s *Store) StoreAPIToken(ctx context.Context, token *core.APIToken) error {
	doc := apiTokenDoc{
		ID:          token.ID,
		UserID:      token.UserID,
		Name:        token.Name,
		HashedToken: token.HashedToken,
		Scopes:      token.Scopes,
		ExpiresAt:   token.ExpiresAt,
		CreatedAt:   token.CreatedAt,
		UpdatedAt:   token.UpdatedAt,
	}
	_, err := s.db.Collection(datastore.APITokensCollection).InsertOne(ctx, doc)
	if err != nil {
		return fmt.Errorf("mongodb: inserting api token: %w", err)
	}
	return nil
}

func (s *Store) ValidateAPIToken(ctx context.Context, hashedToken string) (*core.APIToken, error) {
	now := time.Now()
	filter := bson.M{
		"hashed_token": hashedToken,
		"$or": bson.A{
			bson.M{"expires_at": nil},
			bson.M{"expires_at": bson.M{"$gt": now}},
		},
	}
	var doc apiTokenDoc
	err := s.db.Collection(datastore.APITokensCollection).FindOne(ctx, filter).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("mongodb: validating api token: %w", err)
	}
	return apiTokenFromDoc(&doc), nil
}

func (s *Store) ListAPITokens(ctx context.Context, userID string) ([]*core.APIToken, error) {
	cursor, err := s.db.Collection(datastore.APITokensCollection).Find(ctx, bson.M{"user_id": userID})
	if err != nil {
		return nil, fmt.Errorf("mongodb: listing api tokens: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	var tokens []*core.APIToken
	for cursor.Next(ctx) {
		var doc apiTokenDoc
		if err := cursor.Decode(&doc); err != nil {
			return nil, fmt.Errorf("mongodb: decoding api token: %w", err)
		}
		tokens = append(tokens, apiTokenFromDoc(&doc))
	}
	return tokens, cursor.Err()
}

func (s *Store) RevokeAPIToken(ctx context.Context, userID, id string) error {
	result, err := s.db.Collection(datastore.APITokensCollection).DeleteOne(ctx, bson.M{"_id": id, "user_id": userID})
	if err != nil {
		return fmt.Errorf("mongodb: revoking api token: %w", err)
	}
	if result.DeletedCount == 0 {
		return core.ErrNotFound
	}
	return nil
}

func userFromDoc(doc *userDoc) *core.User {
	return &core.User{
		ID:          doc.ID,
		Email:       doc.Email,
		DisplayName: doc.DisplayName,
		CreatedAt:   doc.CreatedAt,
		UpdatedAt:   doc.UpdatedAt,
	}
}

func (s *Store) integrationTokenFromDoc(doc *integrationTokenDoc) (*core.IntegrationToken, error) {
	access, refresh, err := s.enc.DecryptTokenPair(doc.AccessTokenEncrypted, doc.RefreshTokenEncrypted)
	if err != nil {
		return nil, fmt.Errorf("mongodb: %w", err)
	}
	return &core.IntegrationToken{
		ID:                doc.ID,
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

func (s *Store) StoreStagedConnection(_ context.Context, _ *core.StagedConnection) error {
	return fmt.Errorf("staged connections not supported by mongodb datastore")
}

func (s *Store) GetStagedConnection(_ context.Context, _ string) (*core.StagedConnection, error) {
	return nil, fmt.Errorf("staged connections not supported by mongodb datastore")
}

func (s *Store) DeleteStagedConnection(_ context.Context, _ string) error {
	return fmt.Errorf("staged connections not supported by mongodb datastore")
}

func apiTokenFromDoc(doc *apiTokenDoc) *core.APIToken {
	return &core.APIToken{
		ID:          doc.ID,
		UserID:      doc.UserID,
		Name:        doc.Name,
		HashedToken: doc.HashedToken,
		Scopes:      doc.Scopes,
		ExpiresAt:   doc.ExpiresAt,
		CreatedAt:   doc.CreatedAt,
		UpdatedAt:   doc.UpdatedAt,
	}
}
