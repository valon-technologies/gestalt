package providerdev

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	pluginservice "github.com/valon-technologies/gestalt/server/services/plugins"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
)

const (
	indexedDBAttachmentStore = "provider_dev_attachments"
	indexedDBCallStore       = "provider_dev_calls"
	indexedDBAuthStore       = "provider_dev_attach_authorizations"

	indexedDBCallStatePending  = "pending"
	indexedDBCallStateLeased   = "leased"
	indexedDBCallStateComplete = "complete"
	indexedDBCallStateFailed   = "failed"
	indexedDBCallStateClosed   = "closed"
)

var (
	indexedDBAttachmentSchema = indexeddb.ObjectStoreSchema{
		Indexes: []indexeddb.IndexSchema{
			{Name: "by_owner", KeyPath: []string{"owner"}},
			{Name: "by_owner_created_at", KeyPath: []string{"owner", "created_at"}},
		},
		Columns: []indexeddb.ColumnDef{
			{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
			{Name: "owner", Type: indexeddb.TypeString, NotNull: true},
			{Name: "dispatcher_secret_hash", Type: indexeddb.TypeString, NotNull: true},
			{Name: "targets_json", Type: indexeddb.TypeString, NotNull: true},
			{Name: "created_at", Type: indexeddb.TypeInt},
			{Name: "last_seen_at", Type: indexeddb.TypeInt},
			{Name: "closed", Type: indexeddb.TypeBool},
			{Name: "closed_at", Type: indexeddb.TypeInt},
		},
	}
	indexedDBCallSchema = indexeddb.ObjectStoreSchema{
		Indexes: []indexeddb.IndexSchema{
			{Name: "by_attachment_state_created_at", KeyPath: []string{"attachment_id", "state", "created_at"}},
			{Name: "by_attachment_state", KeyPath: []string{"attachment_id", "state"}},
			{Name: "by_attachment", KeyPath: []string{"attachment_id"}},
		},
		Columns: []indexeddb.ColumnDef{
			{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
			{Name: "attachment_id", Type: indexeddb.TypeString, NotNull: true},
			{Name: "owner", Type: indexeddb.TypeString, NotNull: true},
			{Name: "provider", Type: indexeddb.TypeString, NotNull: true},
			{Name: "method", Type: indexeddb.TypeString, NotNull: true},
			{Name: "request_base64", Type: indexeddb.TypeString, NotNull: true},
			{Name: "response_base64", Type: indexeddb.TypeString},
			{Name: "error_code", Type: indexeddb.TypeInt},
			{Name: "error_message", Type: indexeddb.TypeString},
			{Name: "state", Type: indexeddb.TypeString, NotNull: true},
			{Name: "created_at", Type: indexeddb.TypeInt},
			{Name: "leased_at", Type: indexeddb.TypeInt},
			{Name: "completed_at", Type: indexeddb.TypeInt},
			{Name: "expires_at", Type: indexeddb.TypeInt},
		},
	}
	indexedDBAuthSchema = indexeddb.ObjectStoreSchema{
		Indexes: []indexeddb.IndexSchema{
			{Name: "by_expires_at", KeyPath: []string{"expires_at"}},
		},
		Columns: []indexeddb.ColumnDef{
			{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
			{Name: "client_secret_hash", Type: indexeddb.TypeString, NotNull: true},
			{Name: "verification_hash", Type: indexeddb.TypeString, NotNull: true},
			{Name: "request_hash", Type: indexeddb.TypeString, NotNull: true},
			{Name: "providers_json", Type: indexeddb.TypeString, NotNull: true},
			{Name: "approved_by_json", Type: indexeddb.TypeString},
			{Name: "used", Type: indexeddb.TypeBool},
			{Name: "created_at", Type: indexeddb.TypeInt},
			{Name: "expires_at", Type: indexeddb.TypeInt},
		},
	}
)

type indexedDBSessionStore struct {
	db indexeddb.IndexedDB
}

type indexedDBSessionRecord struct {
	id                   string
	owner                string
	dispatcherSecretHash string
	targets              []storedTarget
	createdAt            time.Time
	lastSeenAt           time.Time
	closed               bool
	closedAt             time.Time
}

type storedTarget struct {
	Name      string                           `json:"name"`
	Source    string                           `json:"source,omitempty"`
	Spec      pluginservice.StaticProviderSpec `json:"spec"`
	Config    map[string]any                   `json:"config,omitempty"`
	ConfigSet bool                             `json:"configSet,omitempty"`
	UI        bool                             `json:"ui,omitempty"`
	UIPath    string                           `json:"uiPath,omitempty"`
}

type indexedDBCallRecord struct {
	id           string
	attachmentID string
	owner        string
	provider     string
	method       string
	request      []byte
	response     []byte
	errorCode    codes.Code
	errorMessage string
	state        string
	createdAt    time.Time
	leasedAt     time.Time
	completedAt  time.Time
	expiresAt    time.Time
}

type indexedDBAttachAuthorizationRecord struct {
	id               string
	clientSecretHash string
	verificationHash string
	requestHash      string
	providers        []string
	expiresAt        time.Time
	approvedBy       *principal.Principal
	used             bool
	createdAt        time.Time
}

type sharedSession struct {
	id    string
	owner string
	store *indexedDBSessionStore
}

func WithIndexedDBAttachmentState(ctx context.Context, db indexeddb.IndexedDB) ManagerOption {
	return func(m *Manager) error {
		store, err := newIndexedDBSessionStore(ctx, db)
		if err != nil {
			return err
		}
		m.shared = store
		return nil
	}
}

func newIndexedDBSessionStore(ctx context.Context, db indexeddb.IndexedDB) (*indexedDBSessionStore, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if db == nil {
		return nil, fmt.Errorf("provider dev indexeddb attachment state requires an IndexedDB provider")
	}
	if err := db.CreateObjectStore(ctx, indexedDBAttachmentStore, indexedDBAttachmentSchema); err != nil {
		return nil, fmt.Errorf("create provider dev attachment store: %w", err)
	}
	if err := db.CreateObjectStore(ctx, indexedDBCallStore, indexedDBCallSchema); err != nil {
		return nil, fmt.Errorf("create provider dev call store: %w", err)
	}
	if err := db.CreateObjectStore(ctx, indexedDBAuthStore, indexedDBAuthSchema); err != nil {
		return nil, fmt.Errorf("create provider dev attach authorization store: %w", err)
	}
	return &indexedDBSessionStore{db: db}, nil
}

func (s *indexedDBSessionStore) createSession(ctx context.Context, owner, id, dispatcherSecretHash string, targets []Target, now time.Time) error {
	if s == nil {
		return status.Error(codes.FailedPrecondition, "provider dev indexeddb attachment state is not configured")
	}
	if now.IsZero() {
		now = time.Now()
	}
	_ = s.cleanupExpired(ctx, now)
	stored := make([]storedTarget, 0, len(targets))
	for i := range targets {
		target := targets[i]
		var storedConfig map[string]any
		if target.ConfigSet {
			storedConfig = cloneAnyMap(target.Config)
		}
		stored = append(stored, storedTarget{
			Name:      target.Name,
			Source:    target.Source,
			Spec:      target.Spec,
			Config:    storedConfig,
			ConfigSet: target.ConfigSet,
			UI:        target.UI,
			UIPath:    target.UIPath,
		})
	}
	targetsJSON, err := json.Marshal(stored)
	if err != nil {
		return status.Errorf(codes.Internal, "encode provider dev attachment targets: %v", err)
	}
	rec := indexeddb.Record{
		"id":                     id,
		"owner":                  owner,
		"dispatcher_secret_hash": dispatcherSecretHash,
		"targets_json":           string(targetsJSON),
		"created_at":             unixNano(now),
		"last_seen_at":           unixNano(now),
		"closed":                 false,
	}
	if err := s.db.ObjectStore(indexedDBAttachmentStore).Add(ctx, rec); err != nil {
		return status.Errorf(codes.Internal, "record provider dev attachment: %v", err)
	}
	return nil
}

func (s *indexedDBSessionStore) createAttachAuthorization(ctx context.Context, auth AttachAuthorization, now time.Time) (*AttachAuthorizationInfo, error) {
	if s == nil {
		return nil, status.Error(codes.FailedPrecondition, "provider dev indexeddb attachment state is not configured")
	}
	if now.IsZero() {
		now = time.Now()
	}
	tx, err := s.db.Transaction(ctx, []string{indexedDBAuthStore}, indexeddb.TransactionReadwrite, indexeddb.TransactionOptions{})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "open provider dev attach authorization transaction: %v", err)
	}
	committed := false
	defer abortIfUncommitted(ctx, tx, &committed)
	store := tx.ObjectStore(indexedDBAuthStore)
	records, err := store.GetAll(ctx, nil)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list provider dev attach authorizations: %v", err)
	}
	active := 0
	for _, raw := range records {
		existing, err := attachAuthorizationFromRecord(raw)
		if err != nil {
			return nil, err
		}
		if existing.used || (!existing.expiresAt.IsZero() && now.After(existing.expiresAt)) {
			_ = store.Delete(ctx, existing.id)
			continue
		}
		active++
	}
	if active >= DefaultMaxAttachAuthorizations {
		return nil, status.Errorf(codes.ResourceExhausted, "too many pending provider dev attach authorizations; try again later")
	}
	if auth.expiresAt.IsZero() {
		auth.expiresAt = now.Add(5 * time.Minute)
	}
	rec, err := attachAuthorizationToRecord(indexedDBAttachAuthorizationRecord{
		id:               auth.id,
		clientSecretHash: auth.clientSecretHash,
		verificationHash: auth.verificationHash,
		requestHash:      auth.requestHash,
		providers:        slices.Clone(auth.providers),
		expiresAt:        auth.expiresAt,
		createdAt:        now,
	})
	if err != nil {
		return nil, err
	}
	if err := store.Add(ctx, rec); err != nil {
		return nil, status.Errorf(codes.Internal, "record provider dev attach authorization: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, status.Errorf(codes.Internal, "commit provider dev attach authorization: %v", err)
	}
	committed = true
	info := (&AttachAuthorization{
		id:        auth.id,
		providers: slices.Clone(auth.providers),
		expiresAt: auth.expiresAt,
	}).info()
	return &info, nil
}

func (s *indexedDBSessionStore) getAttachAuthorization(ctx context.Context, id string, now time.Time) (*AttachAuthorizationInfo, error) {
	auth, err := s.loadActiveAttachAuthorization(ctx, id, now)
	if err != nil {
		return nil, err
	}
	info := auth.info()
	return &info, nil
}

func (s *indexedDBSessionStore) approveAttachAuthorization(ctx context.Context, id string, p *principal.Principal, verificationCode string, now time.Time) error {
	tx, err := s.db.Transaction(ctx, []string{indexedDBAuthStore}, indexeddb.TransactionReadwrite, indexeddb.TransactionOptions{})
	if err != nil {
		return status.Errorf(codes.Internal, "open provider dev attach authorization approval transaction: %v", err)
	}
	committed := false
	defer abortIfUncommitted(ctx, tx, &committed)
	store := tx.ObjectStore(indexedDBAuthStore)
	auth, err := s.attachAuthorizationFromStore(ctx, store, id, now)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare([]byte(hashAttachAuthorizationSecret(verificationCode)), []byte(auth.verificationHash)) != 1 {
		return status.Error(codes.PermissionDenied, "provider dev attach verification code is invalid")
	}
	if auth.approvedBy != nil {
		if principalSubjectID(auth.approvedBy) == principalSubjectID(p) {
			return nil
		}
		return status.Error(codes.FailedPrecondition, "provider dev attach authorization is already approved")
	}
	auth.approvedBy = clonePrincipal(p)
	rec, err := attachAuthorizationToRecord(*auth)
	if err != nil {
		return err
	}
	if err := store.Put(ctx, rec); err != nil {
		return status.Errorf(codes.Internal, "approve provider dev attach authorization: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return status.Errorf(codes.Internal, "commit provider dev attach authorization approval: %v", err)
	}
	committed = true
	return nil
}

func (s *indexedDBSessionStore) pollAttachAuthorization(ctx context.Context, id, clientSecret string, now time.Time) (*PollAttachAuthorizationResponse, error) {
	auth, err := s.loadActiveAttachAuthorization(ctx, id, now)
	if err != nil {
		return nil, err
	}
	if err := verifyAttachAuthorizationClientSecret(auth.clientSecretHash, clientSecret); err != nil {
		return nil, err
	}
	return &PollAttachAuthorizationResponse{Approved: auth.approvedBy != nil}, nil
}

func (s *indexedDBSessionStore) consumeAttachAuthorization(ctx context.Context, id, clientSecret, requestHash string, now time.Time) (*principal.Principal, error) {
	tx, err := s.db.Transaction(ctx, []string{indexedDBAuthStore}, indexeddb.TransactionReadwrite, indexeddb.TransactionOptions{})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "open provider dev attach authorization consume transaction: %v", err)
	}
	committed := false
	defer abortIfUncommitted(ctx, tx, &committed)
	store := tx.ObjectStore(indexedDBAuthStore)
	auth, err := s.attachAuthorizationFromStore(ctx, store, id, now)
	if err != nil {
		return nil, err
	}
	if err := verifyAttachAuthorizationClientSecret(auth.clientSecretHash, clientSecret); err != nil {
		return nil, err
	}
	if auth.approvedBy == nil {
		return nil, status.Error(codes.FailedPrecondition, "provider dev attach authorization is not approved")
	}
	if subtle.ConstantTimeCompare([]byte(requestHash), []byte(auth.requestHash)) != 1 {
		return nil, status.Error(codes.PermissionDenied, "provider dev attach authorization request does not match")
	}
	auth.used = true
	rec, err := attachAuthorizationToRecord(*auth)
	if err != nil {
		return nil, err
	}
	if err := store.Put(ctx, rec); err != nil {
		return nil, status.Errorf(codes.Internal, "consume provider dev attach authorization: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, status.Errorf(codes.Internal, "commit provider dev attach authorization consume: %v", err)
	}
	committed = true
	return clonePrincipal(auth.approvedBy), nil
}

func (s *indexedDBSessionStore) poll(ctx context.Context, id, dispatcherSecret string) (*PollResponse, bool, error) {
	nextHeartbeat := time.Time{}
	for {
		if pollContextDone(ctx) {
			return nil, false, nil
		}
		now := time.Now()
		heartbeat := nextHeartbeat.IsZero() || !now.Before(nextHeartbeat)
		resp, ok, err := s.claimCall(ctx, id, dispatcherSecret, now, heartbeat)
		if err != nil || ok {
			return resp, ok, err
		}
		if heartbeat {
			nextHeartbeat = now.Add(DefaultSessionIdleTimeout / 4)
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled) {
				return nil, false, nil
			}
			return nil, false, ctx.Err()
		}
	}
}

func (s *indexedDBSessionStore) claimCall(ctx context.Context, id, dispatcherSecret string, now time.Time, heartbeat bool) (*PollResponse, bool, error) {
	tx, err := s.db.Transaction(ctx, []string{indexedDBAttachmentStore, indexedDBCallStore}, indexeddb.TransactionReadwrite, indexeddb.TransactionOptions{})
	if err != nil {
		if pollContextOperationDone(ctx, err) {
			return nil, false, nil
		}
		return nil, false, status.Errorf(codes.Internal, "open provider dev poll transaction: %v", err)
	}
	committed := false
	defer abortIfUncommitted(ctx, tx, &committed)

	attachments := tx.ObjectStore(indexedDBAttachmentStore)
	calls := tx.ObjectStore(indexedDBCallStore)
	session, err := pollAttachmentFromStore(ctx, attachments, id, now)
	if err != nil {
		if pollContextOperationDone(ctx, err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if err := verifyStoredDispatcherSecret(session, dispatcherSecret); err != nil {
		return nil, false, err
	}
	touched := false
	touchSession := func(suppressContextDone bool) (bool, error) {
		if touched {
			return false, nil
		}
		rec, err := attachmentToRecord(session)
		if err != nil {
			return false, err
		}
		rec["last_seen_at"] = unixNano(now)
		if err := attachments.Put(ctx, rec); err != nil {
			if suppressContextDone && pollContextOperationDone(ctx, err) {
				return true, nil
			}
			return false, status.Errorf(codes.Internal, "touch provider dev attachment: %v", err)
		}
		touched = true
		return false, nil
	}
	if heartbeat {
		if done, err := touchSession(true); done || err != nil {
			return nil, false, err
		}
	}

	callRecords, err := calls.Index("by_attachment_state").GetAll(ctx, nil, id, indexedDBCallStatePending)
	if err != nil {
		if pollContextOperationDone(ctx, err) {
			return nil, false, nil
		}
		return nil, false, status.Errorf(codes.Internal, "list provider dev calls: %v", err)
	}
	var claim *indexedDBCallRecord
	for _, raw := range callRecords {
		call, err := callFromRecord(raw)
		if err != nil {
			return nil, false, err
		}
		if call.attachmentID != id || call.state != indexedDBCallStatePending {
			continue
		}
		if !call.expiresAt.IsZero() && now.After(call.expiresAt) {
			call.state = indexedDBCallStateClosed
			call.errorCode = codes.DeadlineExceeded
			call.errorMessage = "provider dev call expired before dispatch"
			if err := calls.Put(ctx, callToRecord(call)); err != nil {
				if pollContextOperationDone(ctx, err) {
					return nil, false, nil
				}
				return nil, false, status.Errorf(codes.Internal, "expire provider dev call: %v", err)
			}
			continue
		}
		if claim == nil || call.createdAt.Before(claim.createdAt) {
			copy := call
			claim = &copy
		}
	}
	if claim == nil {
		if err := tx.Commit(ctx); err != nil {
			if pollContextOperationDone(ctx, err) {
				return nil, false, nil
			}
			return nil, false, status.Errorf(codes.Internal, "commit provider dev poll heartbeat: %v", err)
		}
		committed = true
		return nil, false, nil
	}
	claim.state = indexedDBCallStateLeased
	claim.leasedAt = now
	if _, err := touchSession(false); err != nil {
		return nil, false, err
	}
	if err := calls.Put(ctx, callToRecord(*claim)); err != nil {
		return nil, false, status.Errorf(codes.Internal, "claim provider dev call: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, status.Errorf(codes.Internal, "commit provider dev poll: %v", err)
	}
	committed = true
	return &PollResponse{
		CallID:        claim.id,
		Provider:      claim.provider,
		Method:        claim.method,
		RequestBase64: base64.StdEncoding.EncodeToString(claim.request),
	}, true, nil
}

func pollContextDone(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	err := ctx.Err()
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func pollContextOperationDone(ctx context.Context, err error) bool {
	if err == nil || !pollContextDone(ctx) {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	st, ok := status.FromError(err)
	if !ok {
		return false
	}
	return st.Code() == codes.Canceled || st.Code() == codes.DeadlineExceeded
}

func pollAttachmentFromStore(ctx context.Context, attachments indexeddb.TransactionObjectStore, id string, now time.Time) (*indexedDBSessionRecord, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "session id is required")
	}
	raw, err := attachments.Get(ctx, id)
	if err != nil {
		if errors.Is(err, indexeddb.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "provider dev session %q not found", id)
		}
		return nil, err
	}
	session, err := sessionFromRecord(raw)
	if err != nil {
		return nil, err
	}
	if !attachmentActive(session, now) {
		return nil, status.Errorf(codes.NotFound, "provider dev session %q not found", id)
	}
	return &session, nil
}

func (s *indexedDBSessionStore) completeCall(ctx context.Context, attachmentID, callID, dispatcherSecret string, req CompleteCallRequest) error {
	attachmentID = strings.TrimSpace(attachmentID)
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return status.Error(codes.InvalidArgument, "call id is required")
	}
	tx, err := s.db.Transaction(ctx, []string{indexedDBAttachmentStore, indexedDBCallStore}, indexeddb.TransactionReadwrite, indexeddb.TransactionOptions{})
	if err != nil {
		return status.Errorf(codes.Internal, "open provider dev completion transaction: %v", err)
	}
	committed := false
	defer abortIfUncommitted(ctx, tx, &committed)

	attachments := tx.ObjectStore(indexedDBAttachmentStore)
	calls := tx.ObjectStore(indexedDBCallStore)
	session, err := s.attachmentFromStore(ctx, attachments, attachmentID, time.Now())
	if err != nil {
		return err
	}
	if err := verifyStoredDispatcherSecret(session, dispatcherSecret); err != nil {
		return err
	}
	raw, err := calls.Get(ctx, callID)
	if err != nil {
		if errors.Is(err, indexeddb.ErrNotFound) {
			return status.Errorf(codes.NotFound, "provider dev call %q not found", callID)
		}
		return status.Errorf(codes.Internal, "load provider dev call: %v", err)
	}
	call, err := callFromRecord(raw)
	if err != nil {
		return err
	}
	if call.attachmentID != attachmentID {
		return status.Errorf(codes.NotFound, "provider dev call %q not found", callID)
	}
	if call.state != indexedDBCallStateLeased && call.state != indexedDBCallStatePending {
		return status.Errorf(codes.FailedPrecondition, "provider dev call %q is already completed", callID)
	}

	switch {
	case req.Error != nil:
		call.state = indexedDBCallStateFailed
		call.errorCode = codes.Code(req.Error.Code)
		call.errorMessage = req.Error.Message
		if call.errorCode == codes.OK {
			call.errorCode = codes.InvalidArgument
			call.errorMessage = "provider dev error code must be non-OK"
		}
	case req.ResponseBase64 != "":
		payload, err := base64.StdEncoding.DecodeString(req.ResponseBase64)
		if err != nil {
			call.state = indexedDBCallStateFailed
			call.errorCode = codes.InvalidArgument
			call.errorMessage = fmt.Sprintf("decode provider dev response: %v", err)
		} else {
			call.state = indexedDBCallStateComplete
			call.response = payload
		}
	default:
		call.state = indexedDBCallStateComplete
	}
	call.completedAt = time.Now()
	if err := calls.Put(ctx, callToRecord(call)); err != nil {
		return status.Errorf(codes.Internal, "record provider dev call completion: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return status.Errorf(codes.Internal, "commit provider dev call completion: %v", err)
	}
	committed = true
	return nil
}

func (s *indexedDBSessionStore) verifyDispatcherSecret(ctx context.Context, id, dispatcherSecret string) error {
	session, err := s.getActiveAttachment(ctx, id, time.Now())
	if err != nil {
		return err
	}
	return verifyStoredDispatcherSecret(session, dispatcherSecret)
}

func (s *indexedDBSessionStore) closeSessionWithDispatcherSecret(ctx context.Context, id, dispatcherSecret string) error {
	session, err := s.getActiveAttachment(ctx, id, time.Now())
	if err != nil {
		return err
	}
	if err := verifyStoredDispatcherSecret(session, dispatcherSecret); err != nil {
		return err
	}
	return s.closeAttachment(ctx, id, session.owner)
}

func (s *indexedDBSessionStore) closeSession(ctx context.Context, owner, id string) error {
	session, err := s.getActiveAttachment(ctx, id, time.Now())
	if err != nil {
		return err
	}
	if session.owner != owner {
		return status.Error(codes.PermissionDenied, "provider dev session belongs to another principal")
	}
	return s.closeAttachment(ctx, id, owner)
}

func (s *indexedDBSessionStore) closeAttachment(ctx context.Context, id, owner string) error {
	tx, err := s.db.Transaction(ctx, []string{indexedDBAttachmentStore, indexedDBCallStore}, indexeddb.TransactionReadwrite, indexeddb.TransactionOptions{})
	if err != nil {
		return status.Errorf(codes.Internal, "open provider dev close transaction: %v", err)
	}
	committed := false
	defer abortIfUncommitted(ctx, tx, &committed)

	attachments := tx.ObjectStore(indexedDBAttachmentStore)
	calls := tx.ObjectStore(indexedDBCallStore)
	session, err := s.attachmentFromStore(ctx, attachments, id, time.Now())
	if err != nil {
		return err
	}
	if session.owner != owner {
		return status.Error(codes.PermissionDenied, "provider dev session belongs to another principal")
	}
	session.closed = true
	session.closedAt = time.Now()
	rec, err := attachmentToRecord(session)
	if err != nil {
		return err
	}
	if err := attachments.Put(ctx, rec); err != nil {
		return status.Errorf(codes.Internal, "close provider dev attachment: %v", err)
	}
	callRecords, err := calls.Index("by_attachment").GetAll(ctx, nil, id)
	if err != nil {
		return status.Errorf(codes.Internal, "list provider dev calls: %v", err)
	}
	for _, raw := range callRecords {
		call, err := callFromRecord(raw)
		if err != nil {
			return err
		}
		if call.attachmentID != id || call.state == indexedDBCallStateComplete || call.state == indexedDBCallStateFailed || call.state == indexedDBCallStateClosed {
			continue
		}
		call.state = indexedDBCallStateClosed
		call.errorCode = codes.Unavailable
		call.errorMessage = "provider dev session closed"
		call.completedAt = time.Now()
		if err := calls.Put(ctx, callToRecord(call)); err != nil {
			return status.Errorf(codes.Internal, "close provider dev call: %v", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return status.Errorf(codes.Internal, "commit provider dev close: %v", err)
	}
	committed = true
	return nil
}

func (s *indexedDBSessionStore) listSessions(ctx context.Context, owner string) ([]AttachmentInfo, error) {
	_ = s.cleanupExpired(ctx, time.Now())
	records, err := s.db.ObjectStore(indexedDBAttachmentStore).Index("by_owner").GetAll(ctx, nil, owner)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list provider dev attachments: %v", err)
	}
	now := time.Now()
	out := make([]AttachmentInfo, 0, len(records))
	for _, raw := range records {
		session, err := sessionFromRecord(raw)
		if err != nil {
			return nil, err
		}
		if session.owner != owner || !attachmentActive(session, now) {
			continue
		}
		out = append(out, session.attachmentInfo())
	}
	slices.SortFunc(out, func(a, b AttachmentInfo) int {
		switch {
		case a.CreatedAt.Before(b.CreatedAt):
			return -1
		case a.CreatedAt.After(b.CreatedAt):
			return 1
		case a.AttachID < b.AttachID:
			return -1
		case a.AttachID > b.AttachID:
			return 1
		default:
			return 0
		}
	})
	return out, nil
}

func (s *indexedDBSessionStore) getSession(ctx context.Context, owner, id string) (*AttachmentInfo, error) {
	session, err := s.getActiveAttachment(ctx, id, time.Now())
	if err != nil {
		return nil, err
	}
	if session.owner != owner {
		return nil, status.Error(codes.PermissionDenied, "provider dev session belongs to another principal")
	}
	info := session.attachmentInfo()
	return &info, nil
}

func (s *indexedDBSessionStore) latestTarget(ctx context.Context, owner, providerName string) (*indexedDBSessionRecord, Target, bool, error) {
	_ = s.cleanupExpired(ctx, time.Now())
	records, err := s.db.ObjectStore(indexedDBAttachmentStore).Index("by_owner").GetAll(ctx, nil, owner)
	if err != nil {
		return nil, Target{}, false, status.Errorf(codes.Internal, "list provider dev attachments: %v", err)
	}
	now := time.Now()
	var latest *indexedDBSessionRecord
	var latestTarget Target
	for _, raw := range records {
		session, err := sessionFromRecord(raw)
		if err != nil {
			return nil, Target{}, false, err
		}
		if session.owner != owner || !attachmentActive(session, now) {
			continue
		}
		for i := range session.targets {
			target := &session.targets[i]
			if target.Name != providerName {
				continue
			}
			if latest == nil || session.createdAt.After(latest.createdAt) || (session.createdAt.Equal(latest.createdAt) && session.id > latest.id) {
				copySession := session
				latest = &copySession
				latestTarget = target.target()
			}
		}
	}
	if latest == nil {
		return nil, Target{}, false, nil
	}
	return latest, latestTarget, true, nil
}

func (s *indexedDBSessionStore) getActiveAttachment(ctx context.Context, id string, now time.Time) (*indexedDBSessionRecord, error) {
	raw, err := s.db.ObjectStore(indexedDBAttachmentStore).Get(ctx, strings.TrimSpace(id))
	if err != nil {
		if errors.Is(err, indexeddb.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "provider dev session %q not found", id)
		}
		return nil, status.Errorf(codes.Internal, "load provider dev attachment: %v", err)
	}
	session, err := sessionFromRecord(raw)
	if err != nil {
		return nil, err
	}
	if !attachmentActive(session, now) {
		return nil, status.Errorf(codes.NotFound, "provider dev session %q not found", id)
	}
	return &session, nil
}

func (s *indexedDBSessionStore) sessionActive(ctx context.Context, id string, now time.Time) (bool, error) {
	if s == nil {
		return false, nil
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return false, nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	raw, err := s.db.ObjectStore(indexedDBAttachmentStore).Get(ctx, id)
	if err != nil {
		if errors.Is(err, indexeddb.ErrNotFound) {
			return false, nil
		}
		return false, status.Errorf(codes.Internal, "load provider dev attachment: %v", err)
	}
	session, err := sessionFromRecord(raw)
	if err != nil {
		return false, err
	}
	return attachmentActive(session, now), nil
}

func (s *indexedDBSessionStore) attachmentFromStore(ctx context.Context, attachments indexeddb.TransactionObjectStore, id string, now time.Time) (*indexedDBSessionRecord, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "session id is required")
	}
	raw, err := attachments.Get(ctx, id)
	if err != nil {
		if errors.Is(err, indexeddb.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "provider dev session %q not found", id)
		}
		return nil, status.Errorf(codes.Internal, "load provider dev attachment: %v", err)
	}
	session, err := sessionFromRecord(raw)
	if err != nil {
		return nil, err
	}
	if !attachmentActive(session, now) {
		return nil, status.Errorf(codes.NotFound, "provider dev session %q not found", id)
	}
	return &session, nil
}

func attachmentActive(session indexedDBSessionRecord, now time.Time) bool {
	if session.closed {
		return false
	}
	return session.lastSeenAt.IsZero() || now.Sub(session.lastSeenAt) <= DefaultSessionIdleTimeout
}

func (s *indexedDBSessionStore) enqueueCall(ctx context.Context, attachmentID, owner, providerName, method string, payload []byte) (string, error) {
	callID, err := randomID()
	if err != nil {
		return "", status.Errorf(codes.Internal, "create provider dev call: %v", err)
	}
	now := time.Now()
	tx, err := s.db.Transaction(ctx, []string{indexedDBAttachmentStore, indexedDBCallStore}, indexeddb.TransactionReadwrite, indexeddb.TransactionOptions{})
	if err != nil {
		return "", status.Errorf(codes.Internal, "open provider dev call transaction: %v", err)
	}
	committed := false
	defer abortIfUncommitted(ctx, tx, &committed)

	attachments := tx.ObjectStore(indexedDBAttachmentStore)
	calls := tx.ObjectStore(indexedDBCallStore)
	session, err := s.attachmentFromStore(ctx, attachments, attachmentID, now)
	if err != nil {
		return "", err
	}
	if session.owner != owner {
		return "", status.Error(codes.PermissionDenied, "provider dev session belongs to another principal")
	}
	call := indexedDBCallRecord{
		id:           callID,
		attachmentID: attachmentID,
		owner:        owner,
		provider:     providerName,
		method:       method,
		request:      slices.Clone(payload),
		state:        indexedDBCallStatePending,
		createdAt:    now,
		expiresAt:    now.Add(DefaultCallIdleTimeout),
	}
	if err := calls.Add(ctx, callToRecord(call)); err != nil {
		return "", status.Errorf(codes.Internal, "record provider dev call: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", status.Errorf(codes.Internal, "commit provider dev call: %v", err)
	}
	committed = true
	return callID, nil
}

func (s *indexedDBSessionStore) waitCall(ctx context.Context, attachmentID, callID string) ([]byte, error) {
	for {
		raw, err := s.db.ObjectStore(indexedDBCallStore).Get(ctx, callID)
		if err != nil {
			if errors.Is(err, indexeddb.ErrNotFound) {
				return nil, status.Errorf(codes.NotFound, "provider dev call %q not found", callID)
			}
			return nil, status.Errorf(codes.Internal, "load provider dev call: %v", err)
		}
		call, err := callFromRecord(raw)
		if err != nil {
			return nil, err
		}
		if call.attachmentID != attachmentID {
			return nil, status.Errorf(codes.NotFound, "provider dev call %q not found", callID)
		}
		switch call.state {
		case indexedDBCallStateComplete:
			return call.response, nil
		case indexedDBCallStateFailed, indexedDBCallStateClosed:
			if call.errorCode == codes.OK {
				call.errorCode = codes.Unavailable
			}
			if call.errorMessage == "" {
				call.errorMessage = "provider dev call failed"
			}
			return nil, status.Error(call.errorCode, call.errorMessage)
		}
		if !call.expiresAt.IsZero() && time.Now().After(call.expiresAt) {
			_ = s.cancelCall(context.Background(), attachmentID, callID, codes.DeadlineExceeded, "provider dev call timed out")
			return nil, status.Error(codes.DeadlineExceeded, "provider dev call timed out")
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			_ = s.cancelCall(context.Background(), attachmentID, callID, status.Code(ctx.Err()), ctx.Err().Error())
			return nil, ctx.Err()
		}
	}
}

func (s *indexedDBSessionStore) cancelCall(ctx context.Context, attachmentID, callID string, code codes.Code, message string) error {
	tx, err := s.db.Transaction(ctx, []string{indexedDBCallStore}, indexeddb.TransactionReadwrite, indexeddb.TransactionOptions{})
	if err != nil {
		return err
	}
	committed := false
	defer abortIfUncommitted(ctx, tx, &committed)
	calls := tx.ObjectStore(indexedDBCallStore)
	raw, err := calls.Get(ctx, callID)
	if err != nil {
		return err
	}
	call, err := callFromRecord(raw)
	if err != nil {
		return err
	}
	if call.attachmentID != attachmentID || !callOpen(call.state) {
		return nil
	}
	call.state = indexedDBCallStateClosed
	call.errorCode = code
	call.errorMessage = message
	call.completedAt = time.Now()
	if err := calls.Put(ctx, callToRecord(call)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *indexedDBSessionStore) cleanupExpired(ctx context.Context, now time.Time) error {
	if s == nil {
		return nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	tx, err := s.db.Transaction(ctx, []string{indexedDBAttachmentStore, indexedDBCallStore, indexedDBAuthStore}, indexeddb.TransactionReadwrite, indexeddb.TransactionOptions{})
	if err != nil {
		return err
	}
	committed := false
	defer abortIfUncommitted(ctx, tx, &committed)

	attachments := tx.ObjectStore(indexedDBAttachmentStore)
	calls := tx.ObjectStore(indexedDBCallStore)
	auths := tx.ObjectStore(indexedDBAuthStore)

	callRecords, err := calls.GetAll(ctx, nil)
	if err != nil {
		return err
	}
	for _, raw := range callRecords {
		call, err := callFromRecord(raw)
		if err != nil {
			return err
		}
		switch {
		case callOpen(call.state) && !call.expiresAt.IsZero() && now.After(call.expiresAt):
			call.state = indexedDBCallStateClosed
			call.errorCode = codes.DeadlineExceeded
			call.errorMessage = "provider dev call timed out"
			call.completedAt = now
			if err := calls.Put(ctx, callToRecord(call)); err != nil {
				return err
			}
		case !call.completedAt.IsZero() && now.Sub(call.completedAt) > DefaultSessionIdleTimeout:
			if err := calls.Delete(ctx, call.id); err != nil {
				return err
			}
		}
	}

	attachmentRecords, err := attachments.GetAll(ctx, nil)
	if err != nil {
		return err
	}
	for _, raw := range attachmentRecords {
		session, err := sessionFromRecord(raw)
		if err != nil {
			return err
		}
		idleExpired := !session.lastSeenAt.IsZero() && now.Sub(session.lastSeenAt) > DefaultSessionIdleTimeout
		closedExpired := session.closed && (session.closedAt.IsZero() || now.Sub(session.closedAt) > DefaultSessionIdleTimeout)
		if !closedExpired && !idleExpired {
			continue
		}
		if err := attachments.Delete(ctx, session.id); err != nil {
			return err
		}
		records, err := calls.Index("by_attachment").GetAll(ctx, nil, session.id)
		if err != nil {
			return err
		}
		for _, rawCall := range records {
			call, err := callFromRecord(rawCall)
			if err != nil {
				return err
			}
			if callOpen(call.state) {
				call.state = indexedDBCallStateClosed
				call.errorCode = codes.Unavailable
				call.errorMessage = "provider dev session expired"
				call.completedAt = now
				if err := calls.Put(ctx, callToRecord(call)); err != nil {
					return err
				}
				continue
			}
			if !call.completedAt.IsZero() && now.Sub(call.completedAt) > DefaultSessionIdleTimeout {
				if err := calls.Delete(ctx, call.id); err != nil {
					return err
				}
			}
		}
	}

	authRecords, err := auths.GetAll(ctx, nil)
	if err != nil {
		return err
	}
	for _, raw := range authRecords {
		auth, err := attachAuthorizationFromRecord(raw)
		if err != nil {
			return err
		}
		if auth.used || (!auth.expiresAt.IsZero() && now.After(auth.expiresAt)) {
			if err := auths.Delete(ctx, auth.id); err != nil {
				return err
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *sharedSession) invoke(ctx context.Context, providerName, method string, req gproto.Message, resp gproto.Message) error {
	if s == nil || s.store == nil {
		return status.Error(codes.Unavailable, "provider dev session is unavailable")
	}
	payload, err := gproto.Marshal(req)
	if err != nil {
		return status.Errorf(codes.Internal, "marshal provider dev request: %v", err)
	}
	result, err := s.invokeRaw(ctx, providerName, method, payload)
	if err != nil {
		return err
	}
	if resp == nil {
		return nil
	}
	if err := gproto.Unmarshal(result, resp); err != nil {
		return status.Errorf(codes.Internal, "unmarshal provider dev response: %v", err)
	}
	return nil
}

func (s *sharedSession) serveUIAsset(ctx context.Context, providerName string, req UIAssetRequest) (*UIAssetResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "marshal provider dev ui request: %v", err)
	}
	result, err := s.invokeRaw(ctx, providerName, "ServeUIAsset", payload)
	if err != nil {
		return nil, err
	}
	var resp UIAssetResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, status.Errorf(codes.Internal, "unmarshal provider dev ui response: %v", err)
	}
	return &resp, nil
}

func (s *sharedSession) invokeRaw(ctx context.Context, providerName, method string, payload []byte) ([]byte, error) {
	callID, err := s.store.enqueueCall(ctx, s.id, s.owner, providerName, method, payload)
	if err != nil {
		return nil, err
	}
	return s.store.waitCall(ctx, s.id, callID)
}

func (r indexedDBSessionRecord) attachmentInfo() AttachmentInfo {
	providers := make([]AttachmentProviderInfo, 0, len(r.targets))
	for i := range r.targets {
		target := &r.targets[i]
		providers = append(providers, AttachmentProviderInfo{
			Name:   target.Name,
			Source: target.Source,
			UI:     target.UI,
			UIPath: target.UIPath,
		})
	}
	slices.SortFunc(providers, func(a, b AttachmentProviderInfo) int {
		return strings.Compare(a.Name, b.Name)
	})
	return AttachmentInfo{
		AttachID:           r.id,
		CreatedAt:          r.createdAt,
		LastSeenAt:         r.lastSeenAt,
		IdleTimeoutSeconds: int(DefaultSessionIdleTimeout / time.Second),
		Providers:          providers,
	}
}

func (t storedTarget) target() Target {
	return Target{
		Name:      t.Name,
		Source:    t.Source,
		Spec:      t.Spec,
		Config:    cloneAnyMap(t.Config),
		ConfigSet: t.ConfigSet,
		UI:        t.UI,
		UIPath:    t.UIPath,
	}
}

func verifyStoredDispatcherSecret(session *indexedDBSessionRecord, secret string) error {
	if session == nil {
		return status.Error(codes.NotFound, "provider dev session not found")
	}
	secret = strings.TrimSpace(secret)
	if secret == "" || session.dispatcherSecretHash == "" {
		return status.Error(codes.Unauthenticated, "provider dev dispatcher secret is required")
	}
	if subtle.ConstantTimeCompare([]byte(hashDispatcherSecret(secret)), []byte(session.dispatcherSecretHash)) == 1 {
		return nil
	}
	return status.Error(codes.PermissionDenied, "provider dev dispatcher secret is invalid")
}

func verifyAttachAuthorizationClientSecret(hash, secret string) error {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return status.Error(codes.Unauthenticated, "provider dev attach authorization secret is required")
	}
	if subtle.ConstantTimeCompare([]byte(hashAttachAuthorizationSecret(secret)), []byte(hash)) != 1 {
		return status.Error(codes.PermissionDenied, "provider dev attach authorization secret is invalid")
	}
	return nil
}

func (s *indexedDBSessionStore) loadActiveAttachAuthorization(ctx context.Context, id string, now time.Time) (*indexedDBAttachAuthorizationRecord, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "provider dev attach authorization id is required")
	}
	raw, err := s.db.ObjectStore(indexedDBAuthStore).Get(ctx, id)
	if err != nil {
		if errors.Is(err, indexeddb.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "provider dev attach authorization %q not found", id)
		}
		return nil, status.Errorf(codes.Internal, "load provider dev attach authorization: %v", err)
	}
	auth, err := attachAuthorizationFromRecord(raw)
	if err != nil {
		return nil, err
	}
	if auth.used {
		return nil, status.Errorf(codes.NotFound, "provider dev attach authorization %q not found", id)
	}
	if !auth.expiresAt.IsZero() && now.After(auth.expiresAt) {
		_ = s.db.ObjectStore(indexedDBAuthStore).Delete(ctx, id)
		return nil, status.Error(codes.DeadlineExceeded, "provider dev attach authorization expired")
	}
	return &auth, nil
}

func (s *indexedDBSessionStore) attachAuthorizationFromStore(ctx context.Context, store indexeddb.TransactionObjectStore, id string, now time.Time) (*indexedDBAttachAuthorizationRecord, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "provider dev attach authorization id is required")
	}
	raw, err := store.Get(ctx, id)
	if err != nil {
		if errors.Is(err, indexeddb.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "provider dev attach authorization %q not found", id)
		}
		return nil, status.Errorf(codes.Internal, "load provider dev attach authorization: %v", err)
	}
	auth, err := attachAuthorizationFromRecord(raw)
	if err != nil {
		return nil, err
	}
	if auth.used {
		return nil, status.Errorf(codes.NotFound, "provider dev attach authorization %q not found", id)
	}
	if !auth.expiresAt.IsZero() && now.After(auth.expiresAt) {
		_ = store.Delete(ctx, id)
		return nil, status.Error(codes.DeadlineExceeded, "provider dev attach authorization expired")
	}
	return &auth, nil
}

func (a indexedDBAttachAuthorizationRecord) info() AttachAuthorizationInfo {
	return AttachAuthorizationInfo{
		AuthorizationID: a.id,
		Providers:       slices.Clone(a.providers),
		ExpiresAt:       a.expiresAt,
	}
}

func sessionFromRecord(rec indexeddb.Record) (indexedDBSessionRecord, error) {
	targetsJSON := recordString(rec, "targets_json")
	var targets []storedTarget
	if targetsJSON != "" {
		if err := json.Unmarshal([]byte(targetsJSON), &targets); err != nil {
			return indexedDBSessionRecord{}, status.Errorf(codes.Internal, "decode provider dev attachment targets: %v", err)
		}
	}
	return indexedDBSessionRecord{
		id:                   recordString(rec, "id"),
		owner:                recordString(rec, "owner"),
		dispatcherSecretHash: recordString(rec, "dispatcher_secret_hash"),
		targets:              targets,
		createdAt:            timeFromUnixNano(recordInt64(rec, "created_at")),
		lastSeenAt:           timeFromUnixNano(recordInt64(rec, "last_seen_at")),
		closed:               recordBool(rec, "closed"),
		closedAt:             timeFromUnixNano(recordInt64(rec, "closed_at")),
	}, nil
}

func attachmentToRecord(session *indexedDBSessionRecord) (indexeddb.Record, error) {
	targetsJSON, err := json.Marshal(session.targets)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode provider dev attachment targets: %v", err)
	}
	return indexeddb.Record{
		"id":                     session.id,
		"owner":                  session.owner,
		"dispatcher_secret_hash": session.dispatcherSecretHash,
		"targets_json":           string(targetsJSON),
		"created_at":             unixNano(session.createdAt),
		"last_seen_at":           unixNano(session.lastSeenAt),
		"closed":                 session.closed,
		"closed_at":              unixNano(session.closedAt),
	}, nil
}

func attachAuthorizationFromRecord(rec indexeddb.Record) (indexedDBAttachAuthorizationRecord, error) {
	var providers []string
	if value := recordString(rec, "providers_json"); value != "" {
		if err := json.Unmarshal([]byte(value), &providers); err != nil {
			return indexedDBAttachAuthorizationRecord{}, status.Errorf(codes.Internal, "decode provider dev attach authorization providers: %v", err)
		}
	}
	var approvedBy *principal.Principal
	if value := recordString(rec, "approved_by_json"); value != "" {
		var p principal.Principal
		if err := json.Unmarshal([]byte(value), &p); err != nil {
			return indexedDBAttachAuthorizationRecord{}, status.Errorf(codes.Internal, "decode provider dev attach authorization principal: %v", err)
		}
		approvedBy = &p
	}
	return indexedDBAttachAuthorizationRecord{
		id:               recordString(rec, "id"),
		clientSecretHash: recordString(rec, "client_secret_hash"),
		verificationHash: recordString(rec, "verification_hash"),
		requestHash:      recordString(rec, "request_hash"),
		providers:        providers,
		expiresAt:        timeFromUnixNano(recordInt64(rec, "expires_at")),
		approvedBy:       approvedBy,
		used:             recordBool(rec, "used"),
		createdAt:        timeFromUnixNano(recordInt64(rec, "created_at")),
	}, nil
}

func attachAuthorizationToRecord(auth indexedDBAttachAuthorizationRecord) (indexeddb.Record, error) {
	providersJSON, err := json.Marshal(auth.providers)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode provider dev attach authorization providers: %v", err)
	}
	approvedByJSON := ""
	if auth.approvedBy != nil {
		payload, err := json.Marshal(auth.approvedBy)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "encode provider dev attach authorization principal: %v", err)
		}
		approvedByJSON = string(payload)
	}
	return indexeddb.Record{
		"id":                 auth.id,
		"client_secret_hash": auth.clientSecretHash,
		"verification_hash":  auth.verificationHash,
		"request_hash":       auth.requestHash,
		"providers_json":     string(providersJSON),
		"approved_by_json":   approvedByJSON,
		"used":               auth.used,
		"created_at":         unixNano(auth.createdAt),
		"expires_at":         unixNano(auth.expiresAt),
	}, nil
}

func callFromRecord(rec indexeddb.Record) (indexedDBCallRecord, error) {
	request, err := base64.StdEncoding.DecodeString(recordString(rec, "request_base64"))
	if err != nil {
		return indexedDBCallRecord{}, status.Errorf(codes.Internal, "decode provider dev call request: %v", err)
	}
	var response []byte
	if value := recordString(rec, "response_base64"); value != "" {
		response, err = base64.StdEncoding.DecodeString(value)
		if err != nil {
			return indexedDBCallRecord{}, status.Errorf(codes.Internal, "decode provider dev call response: %v", err)
		}
	}
	return indexedDBCallRecord{
		id:           recordString(rec, "id"),
		attachmentID: recordString(rec, "attachment_id"),
		owner:        recordString(rec, "owner"),
		provider:     recordString(rec, "provider"),
		method:       recordString(rec, "method"),
		request:      request,
		response:     response,
		errorCode:    codes.Code(recordInt64(rec, "error_code")),
		errorMessage: recordString(rec, "error_message"),
		state:        recordString(rec, "state"),
		createdAt:    timeFromUnixNano(recordInt64(rec, "created_at")),
		leasedAt:     timeFromUnixNano(recordInt64(rec, "leased_at")),
		completedAt:  timeFromUnixNano(recordInt64(rec, "completed_at")),
		expiresAt:    timeFromUnixNano(recordInt64(rec, "expires_at")),
	}, nil
}

func callToRecord(call indexedDBCallRecord) indexeddb.Record {
	rec := indexeddb.Record{
		"id":              call.id,
		"attachment_id":   call.attachmentID,
		"owner":           call.owner,
		"provider":        call.provider,
		"method":          call.method,
		"request_base64":  base64.StdEncoding.EncodeToString(call.request),
		"response_base64": base64.StdEncoding.EncodeToString(call.response),
		"error_code":      int64(call.errorCode),
		"error_message":   call.errorMessage,
		"state":           call.state,
		"created_at":      unixNano(call.createdAt),
		"leased_at":       unixNano(call.leasedAt),
		"completed_at":    unixNano(call.completedAt),
		"expires_at":      unixNano(call.expiresAt),
	}
	return rec
}

func callOpen(state string) bool {
	return state == indexedDBCallStatePending || state == indexedDBCallStateLeased
}

func abortIfUncommitted(ctx context.Context, tx indexeddb.Transaction, committed *bool) {
	if tx == nil || committed == nil || *committed {
		return
	}
	abortCtx := ctx
	var cancel context.CancelFunc
	if pollContextDone(ctx) {
		abortCtx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
	}
	_ = tx.Abort(abortCtx)
}

func recordString(rec indexeddb.Record, key string) string {
	switch v := rec[key].(type) {
	case nil:
		return ""
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func recordBool(rec indexeddb.Record, key string) bool {
	switch v := rec[key].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	default:
		return false
	}
}

func recordInt64(rec indexeddb.Record, key string) int64 {
	switch v := rec[key].(type) {
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	default:
		return 0
	}
}

func unixNano(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UTC().UnixNano()
}

func timeFromUnixNano(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.Unix(0, v).UTC()
}
