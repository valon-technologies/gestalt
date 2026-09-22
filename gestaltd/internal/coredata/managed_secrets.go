package coredata

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

var (
	// ErrSecretNotFound distinguishes a missing managed secret from datastore
	// failures. It is deliberately separate from core.ErrNotFound because the
	// secret store may be configured while a particular logical name is absent.
	ErrSecretNotFound = errors.New("managed secret not found")

	secretNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
)

// ManagedSecret is the non-secret metadata and current pointer for one logical
// secret. Ciphertext and plaintext are never exposed through this type.
type ManagedSecret struct {
	Name          string
	OwnerApp      string
	Scope         string
	Description   string
	CurrentVer    int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
	CreatedBy     string
	UpdatedBy     string
	RetiredAt     *time.Time
	RetiredReason string
}

// ManagedSecretVersion records one immutable ciphertext version. The ciphertext
// is intentionally private to this package; callers receive only metadata.
type ManagedSecretVersion struct {
	Name             string
	Version          int64
	State            string
	CreatedAt        time.Time
	CreatedBy        string
	Reason           string
	Description      string
	KMSKey           string
	KMSKeyVersion    string
	CiphertextSHA256 string
}

// ManagedSecretAuditLog records a mutation. It never contains a value or
// ciphertext.
type ManagedSecretAuditLog struct {
	ID         string
	Name       string
	Action     string
	Version    int64
	OwnerApp   string
	Actor      string
	Reason     string
	Source     string
	RequestID  string
	KMSKey     string
	CipherHash string
	CreatedAt  time.Time
	Result     string
}

// ManagedSecretService is the authoritative metadata and current-pointer store
// for database-backed application secrets. It deliberately does not know how to
// encrypt values; an admin service encrypts and passes ciphertext in.
type ManagedSecretService struct {
	db indexeddb.IndexedDB
}

func NewManagedSecretService(ds indexeddb.IndexedDB) *ManagedSecretService {
	return &ManagedSecretService{db: ds}
}

// ValidateSecretName enforces the logical-name contract used by the platform
// and by the runtime provider's KMS additional-authenticated-data binding.
func ValidateSecretName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("secret name is required")
	}
	if len(name) > 255 {
		return fmt.Errorf("secret name must be at most 255 bytes")
	}
	if !secretNamePattern.MatchString(name) {
		return fmt.Errorf("secret name must match %s", secretNamePattern.String())
	}
	return nil
}

func NormalizeSecretScope(scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return "app"
	}
	if scope != "app" && scope != "shared" {
		return ""
	}
	return scope
}

func NewSecretAuditID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate audit id: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func HashCiphertext(ciphertext []byte) string {
	sum := sha256.Sum256(ciphertext)
	return hex.EncodeToString(sum[:])
}

func GenerateSecretValue(byteCount int) (string, []byte, error) {
	if byteCount <= 0 {
		byteCount = 48
	}
	if byteCount > 64*1024 {
		return "", nil, fmt.Errorf("generated secret must not exceed 64 KiB")
	}
	raw := make([]byte, byteCount)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate secret value: %w", err)
	}
	value := base64.RawURLEncoding.EncodeToString(raw)
	return value, []byte(value), nil
}

func (s *ManagedSecretService) EnsureStore(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("managed secrets service is not configured")
	}
	if _, err := s.db.CreateObjectStore(ctx, StoreManagedSecrets, ManagedSecretsSchema); err != nil {
		return fmt.Errorf("ensure managed_secrets store: %w", err)
	}
	if _, err := s.db.CreateObjectStore(ctx, StoreManagedSecretVersions, ManagedSecretVersionsSchema); err != nil {
		return fmt.Errorf("ensure managed_secret_versions store: %w", err)
	}
	if _, err := s.db.CreateObjectStore(ctx, StoreManagedSecretAuditLogs, ManagedSecretAuditLogsSchema); err != nil {
		return fmt.Errorf("ensure managed_secret_audit_logs store: %w", err)
	}
	return nil
}

func (s *ManagedSecretService) Get(ctx context.Context, name string) (*ManagedSecret, error) {
	if s == nil {
		return nil, fmt.Errorf("managed secrets service is not configured")
	}
	if err := ValidateSecretName(name); err != nil {
		return nil, err
	}
	rec, err := s.db.ObjectStore(StoreManagedSecrets).Get(ctx, name)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil, ErrSecretNotFound
		}
		return nil, fmt.Errorf("get managed secret: %w", err)
	}
	return recordToManagedSecret(rec), nil
}

func (s *ManagedSecretService) List(ctx context.Context) ([]*ManagedSecret, error) {
	if s == nil {
		return nil, fmt.Errorf("managed secrets service is not configured")
	}
	recs, err := s.db.ObjectStore(StoreManagedSecrets).GetAll(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("list managed secrets: %w", err)
	}
	out := make([]*ManagedSecret, 0, len(recs))
	for _, rec := range recs {
		out = append(out, recordToManagedSecret(rec))
	}
	return out, nil
}

func (s *ManagedSecretService) ListVersions(ctx context.Context, name string) ([]*ManagedSecretVersion, error) {
	if s == nil {
		return nil, fmt.Errorf("managed secrets service is not configured")
	}
	if err := ValidateSecretName(name); err != nil {
		return nil, err
	}
	recs, err := s.db.ObjectStore(StoreManagedSecretVersions).
		Index("by_name_version").
		GetAll(ctx, idb.LowerBound([]any{name}, false))
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("list managed secret versions: %w", err)
	}
	out := make([]*ManagedSecretVersion, 0, len(recs))
	for _, rec := range recs {
		if recordToManagedSecretVersion(rec).Name != name {
			continue
		}
		out = append(out, recordToManagedSecretVersion(rec))
	}
	return out, nil
}

func (s *ManagedSecretService) ListAudit(ctx context.Context, name string, limit int) ([]*ManagedSecretAuditLog, error) {
	if s == nil {
		return nil, fmt.Errorf("managed secrets service is not configured")
	}
	if err := ValidateSecretName(name); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	recs, err := s.db.ObjectStore(StoreManagedSecretAuditLogs).
		Index("by_name_created").
		GetAll(ctx, idb.LowerBound([]any{name}, false))
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("list managed secret audit logs: %w", err)
	}
	out := make([]*ManagedSecretAuditLog, 0, len(recs))
	for _, rec := range recs {
		if row := recordToManagedSecretAuditLog(rec); row != nil && row.Name == name {
			out = append(out, row)
		}
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

type PutSecretInput struct {
	Name          string
	OwnerApp      string
	Scope         string
	Description   string
	Reason        string
	Actor         string
	Source        string
	RequestID     string
	KMSKey        string
	KMSKeyVersion string
	Ciphertext    []byte
	Now           time.Time
}

// Put creates a secret or creates a new immutable version. Create and rotate
// share this path: create is the transition from no pointer to version 1, and
// rotate is the transition to version N+1. The previous version remains
// available as history.
func (s *ManagedSecretService) Put(ctx context.Context, input PutSecretInput) (*ManagedSecret, *ManagedSecretVersion, error) {
	if s == nil || s.db == nil {
		return nil, nil, fmt.Errorf("managed secrets service is not configured")
	}
	if err := ValidateSecretName(input.Name); err != nil {
		return nil, nil, err
	}
	if len(input.Ciphertext) == 0 {
		return nil, nil, fmt.Errorf("ciphertext is required")
	}
	if len(input.Ciphertext) > 2*1024*1024 {
		return nil, nil, fmt.Errorf("ciphertext exceeds the 2 MiB management limit")
	}
	scope := NormalizeSecretScope(input.Scope)
	if scope == "" {
		return nil, nil, fmt.Errorf("scope must be app or shared")
	}
	if scope == "app" && strings.TrimSpace(input.OwnerApp) == "" {
		return nil, nil, fmt.Errorf("ownerApp is required for app-scoped secrets")
	}
	if strings.TrimSpace(input.Actor) == "" {
		return nil, nil, fmt.Errorf("actor is required")
	}
	if strings.TrimSpace(input.Reason) == "" {
		return nil, nil, fmt.Errorf("reason is required")
	}
	now := input.Now
	if now.IsZero() {
		now = time.Now().UTC().Truncate(time.Millisecond)
	}

	tx, err := s.db.Transaction(ctx, []string{
		StoreManagedSecrets,
		StoreManagedSecretVersions,
		StoreManagedSecretAuditLogs,
	}, idb.TransactionReadwrite, idb.TransactionOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("managed secret write: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(context.WithoutCancel(ctx))
		}
	}()

	secrets := tx.ObjectStore(StoreManagedSecrets)
	versions := tx.ObjectStore(StoreManagedSecretVersions)
	audit := tx.ObjectStore(StoreManagedSecretAuditLogs)

	currentRec, err := secrets.Get(ctx, input.Name)
	if err != nil && !errors.Is(err, idb.ErrNotFound) {
		return nil, nil, fmt.Errorf("managed secret write: load current: %w", err)
	}
	current := recordToManagedSecret(currentRec)
	nextVersion := int64(1)
	if current != nil {
		if current.RetiredAt != nil {
			return nil, nil, fmt.Errorf("cannot write retired secret %q", input.Name)
		}
		if current.OwnerApp != strings.TrimSpace(input.OwnerApp) {
			return nil, nil, fmt.Errorf("ownerApp cannot change after creation")
		}
		nextVersion = current.CurrentVer + 1
	}

	cipherHash := HashCiphertext(input.Ciphertext)
	version := &ManagedSecretVersion{
		Name:             input.Name,
		Version:          nextVersion,
		State:            "active",
		CreatedAt:        now,
		CreatedBy:        strings.TrimSpace(input.Actor),
		Reason:           strings.TrimSpace(input.Reason),
		Description:      strings.TrimSpace(input.Description),
		KMSKey:           strings.TrimSpace(input.KMSKey),
		KMSKeyVersion:    strings.TrimSpace(input.KMSKeyVersion),
		CiphertextSHA256: cipherHash,
	}
	if err := versions.Put(ctx, managedSecretVersionRecord(version, input.Ciphertext)); err != nil {
		return nil, nil, fmt.Errorf("managed secret write: version: %w", err)
	}

	secret := &ManagedSecret{
		Name:        input.Name,
		OwnerApp:    strings.TrimSpace(input.OwnerApp),
		Scope:       scope,
		Description: strings.TrimSpace(input.Description),
		CurrentVer:  nextVersion,
		CreatedAt:   now,
		UpdatedAt:   now,
		CreatedBy:   strings.TrimSpace(input.Actor),
		UpdatedBy:   strings.TrimSpace(input.Actor),
	}
	if current != nil {
		secret.CreatedAt = current.CreatedAt
		secret.CreatedBy = current.CreatedBy
	}
	if err := secrets.Put(ctx, managedSecretRecord(secret)); err != nil {
		return nil, nil, fmt.Errorf("managed secret write: current pointer: %w", err)
	}

	auditID, err := NewSecretAuditID()
	if err != nil {
		return nil, nil, err
	}
	action := "create"
	if nextVersion > 1 {
		action = "rotate"
	}
	logRow := &ManagedSecretAuditLog{
		ID:         auditID,
		Name:       input.Name,
		Action:     action,
		Version:    nextVersion,
		OwnerApp:   secret.OwnerApp,
		Actor:      strings.TrimSpace(input.Actor),
		Reason:     strings.TrimSpace(input.Reason),
		Source:     strings.TrimSpace(input.Source),
		RequestID:  strings.TrimSpace(input.RequestID),
		KMSKey:     version.KMSKey,
		CipherHash: cipherHash,
		CreatedAt:  now,
		Result:     "success",
	}
	if err := audit.Put(ctx, managedSecretAuditLogRecord(logRow)); err != nil {
		return nil, nil, fmt.Errorf("managed secret write: audit: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("managed secret write: commit: %w", err)
	}
	committed = true
	return secret, version, nil
}

func (s *ManagedSecretService) Retire(ctx context.Context, name, actor, reason string, now time.Time) (*ManagedSecret, error) {
	if s == nil {
		return nil, fmt.Errorf("managed secrets service is not configured")
	}
	if err := ValidateSecretName(name); err != nil {
		return nil, err
	}
	if strings.TrimSpace(actor) == "" || strings.TrimSpace(reason) == "" {
		return nil, fmt.Errorf("actor and reason are required")
	}
	if now.IsZero() {
		now = time.Now().UTC().Truncate(time.Millisecond)
	}
	current, err := s.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if current.RetiredAt != nil {
		return current, nil
	}
	retired := *current
	retired.RetiredAt = &now
	retired.RetiredReason = strings.TrimSpace(reason)
	if err := s.db.ObjectStore(StoreManagedSecrets).Put(ctx, managedSecretRecord(&retired)); err != nil {
		return nil, fmt.Errorf("retire managed secret: %w", err)
	}
	return &retired, nil
}

func managedSecretRecord(secret *ManagedSecret) idb.Record {
	return idb.Record{
		"id":             secret.Name,
		"name":           secret.Name,
		"owner_app":      secret.OwnerApp,
		"scope":          secret.Scope,
		"description":    secret.Description,
		"current_ver":    secret.CurrentVer,
		"created_at":     secret.CreatedAt,
		"updated_at":     secret.UpdatedAt,
		"created_by":     secret.CreatedBy,
		"updated_by":     secret.UpdatedBy,
		"retired_at":     secret.RetiredAt,
		"retired_reason": secret.RetiredReason,
	}
}

func recordToManagedSecret(rec idb.Record) *ManagedSecret {
	if rec == nil {
		return nil
	}
	var retired *time.Time
	if value, ok := rec["retired_at"].(time.Time); ok && !value.IsZero() {
		copied := value
		retired = &copied
	}
	return &ManagedSecret{
		Name:          recString(rec, "name"),
		OwnerApp:      recString(rec, "owner_app"),
		Scope:         recString(rec, "scope"),
		Description:   recString(rec, "description"),
		CurrentVer:    int64(recordInt(rec, "current_ver")),
		CreatedAt:     recTime(rec, "created_at"),
		UpdatedAt:     recTime(rec, "updated_at"),
		CreatedBy:     recString(rec, "created_by"),
		UpdatedBy:     recString(rec, "updated_by"),
		RetiredAt:     retired,
		RetiredReason: recString(rec, "retired_reason"),
	}
}

func managedSecretVersionRecord(version *ManagedSecretVersion, ciphertext []byte) idb.Record {
	return idb.Record{
		"id":                fmt.Sprintf("%s:%09d", version.Name, version.Version),
		"name":              version.Name,
		"version":           version.Version,
		"state":             version.State,
		"ciphertext":        ciphertext,
		"created_at":        version.CreatedAt,
		"created_by":        version.CreatedBy,
		"reason":            version.Reason,
		"description":       version.Description,
		"kms_key":           version.KMSKey,
		"kms_key_version":   version.KMSKeyVersion,
		"ciphertext_sha256": version.CiphertextSHA256,
	}
}

func recordToManagedSecretVersion(rec idb.Record) *ManagedSecretVersion {
	if rec == nil {
		return nil
	}
	return &ManagedSecretVersion{
		Name:             recString(rec, "name"),
		Version:          int64(recordInt(rec, "version")),
		State:            recString(rec, "state"),
		CreatedAt:        recTime(rec, "created_at"),
		CreatedBy:        recString(rec, "created_by"),
		Reason:           recString(rec, "reason"),
		Description:      recString(rec, "description"),
		KMSKey:           recString(rec, "kms_key"),
		KMSKeyVersion:    recString(rec, "kms_key_version"),
		CiphertextSHA256: recString(rec, "ciphertext_sha256"),
	}
}

func managedSecretAuditLogRecord(logRow *ManagedSecretAuditLog) idb.Record {
	return idb.Record{
		"id":          logRow.ID,
		"name":        logRow.Name,
		"action":      logRow.Action,
		"version":     logRow.Version,
		"owner_app":   logRow.OwnerApp,
		"actor":       logRow.Actor,
		"reason":      logRow.Reason,
		"source":      logRow.Source,
		"request_id":  logRow.RequestID,
		"kms_key":     logRow.KMSKey,
		"cipher_hash": logRow.CipherHash,
		"created_at":  logRow.CreatedAt,
		"result":      logRow.Result,
	}
}

func recordToManagedSecretAuditLog(rec idb.Record) *ManagedSecretAuditLog {
	if rec == nil {
		return nil
	}
	return &ManagedSecretAuditLog{
		ID:         recString(rec, "id"),
		Name:       recString(rec, "name"),
		Action:     recString(rec, "action"),
		Version:    int64(recordInt(rec, "version")),
		OwnerApp:   recString(rec, "owner_app"),
		Actor:      recString(rec, "actor"),
		Reason:     recString(rec, "reason"),
		Source:     recString(rec, "source"),
		RequestID:  recString(rec, "request_id"),
		KMSKey:     recString(rec, "kms_key"),
		CipherHash: recString(rec, "cipher_hash"),
		CreatedAt:  recTime(rec, "created_at"),
		Result:     recString(rec, "result"),
	}
}
