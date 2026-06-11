package mcpoauth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	"github.com/valon-technologies/gestalt/server/core/crypto"
)

const registrationsStoreName = "oauth_registrations"

// Frozen at first deploy: the relationaldb provider rejects schema changes
// without an explicit migration.
var registrationsSchema = idb.ObjectStoreOptions{
	Columns: []idb.ColumnDef{
		{Name: "id", Type: idb.TypeString, PrimaryKey: true},
		{Name: "client_id", Type: idb.TypeString, NotNull: true},
		{Name: "client_secret_encrypted", Type: idb.TypeString},
		{Name: "expires_at", Type: idb.TypeTime},
	},
}

// Store persists one dynamically registered OAuth client per
// (authorization server, redirect URI) pair, shared by every server instance
// so the authorize URL and the token exchange always present the same client
// regardless of which instance serves each request.
type Store struct {
	store idb.ObjectStore
	enc   *crypto.AESGCMEncryptor
}

// NewStore provisions the registrations object store. An empty key gets an
// ephemeral replacement so validate-mode bootstraps, which construct handlers
// but never read or write registrations, work without the server key.
func NewStore(ctx context.Context, db idb.Database, encKey []byte) (*Store, error) {
	if len(encKey) == 0 {
		encKey = make([]byte, 32)
		if _, err := rand.Read(encKey); err != nil {
			return nil, fmt.Errorf("generate ephemeral registration key: %w", err)
		}
	}
	enc, err := crypto.NewAESGCM(encKey)
	if err != nil {
		return nil, err
	}
	if _, err := db.CreateObjectStore(ctx, registrationsStoreName, registrationsSchema); err != nil {
		return nil, fmt.Errorf("create %s store: %w", registrationsStoreName, err)
	}
	return &Store{store: db.ObjectStore(registrationsStoreName), enc: enc}, nil
}

// The auth server URL never contains a newline, so the composite is unambiguous.
func registrationID(authServerURL, redirectURI string) string {
	return authServerURL + "\n" + redirectURI
}

// Get returns nil when no registration exists.
func (s *Store) Get(ctx context.Context, authServerURL, redirectURI string) (*Registration, error) {
	rec, err := s.store.Get(ctx, registrationID(authServerURL, redirectURI))
	if errors.Is(err, idb.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get registration: %w", err)
	}
	secret, err := s.enc.Decrypt(recString(rec, "client_secret_encrypted"))
	if err != nil {
		return nil, fmt.Errorf("decrypt client_secret: %w", err)
	}
	return &Registration{
		ClientID:     recString(rec, "client_id"),
		ClientSecret: secret,
		ExpiresAt:    recTimePtr(rec, "expires_at"),
	}, nil
}

// Add is insert-only and returns idb.ErrAlreadyExists when another instance
// registered first; callers re-read and adopt the winner.
func (s *Store) Add(ctx context.Context, authServerURL, redirectURI string, reg *Registration) error {
	rec, err := s.record(authServerURL, redirectURI, reg)
	if err != nil {
		return err
	}
	if err := s.store.Add(ctx, rec); err != nil {
		return fmt.Errorf("add registration: %w", err)
	}
	return nil
}

// Put replaces an expired registration.
func (s *Store) Put(ctx context.Context, authServerURL, redirectURI string, reg *Registration) error {
	rec, err := s.record(authServerURL, redirectURI, reg)
	if err != nil {
		return err
	}
	if err := s.store.Put(ctx, rec); err != nil {
		return fmt.Errorf("put registration: %w", err)
	}
	return nil
}

func (s *Store) record(authServerURL, redirectURI string, reg *Registration) (idb.Record, error) {
	secretEnc, err := s.enc.Encrypt(reg.ClientSecret)
	if err != nil {
		return nil, fmt.Errorf("encrypt client_secret: %w", err)
	}
	return idb.Record{
		"id":                      registrationID(authServerURL, redirectURI),
		"client_id":               reg.ClientID,
		"client_secret_encrypted": secretEnc,
		"expires_at":              reg.ExpiresAt,
	}, nil
}

func recString(rec idb.Record, key string) string {
	switch s := rec[key].(type) {
	case string:
		return s
	case []byte:
		return string(s)
	default:
		return ""
	}
}

func recTimePtr(rec idb.Record, key string) *time.Time {
	switch t := rec[key].(type) {
	case time.Time:
		if t.IsZero() {
			return nil
		}
		return &t
	case *time.Time:
		return t
	case string:
		parsed, err := time.Parse(time.RFC3339, t)
		if err != nil || parsed.IsZero() {
			return nil
		}
		return &parsed
	default:
		return nil
	}
}
