package scim

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	"github.com/valon-technologies/gestalt/server/core"
	coredb "github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	retryAfterSeconds = 1
	maxDueIntentBatch = 200
)

type projection struct {
	Relation     string `json:"relation"`
	ResourceType string `json:"resourceType"`
	ResourceID   string `json:"resourceId"`
	Owned        bool   `json:"owned,omitempty"`
}

func (p projection) key() string { return p.ResourceType + "\x00" + p.ResourceID + "\x00" + p.Relation }

type client struct {
	id            string
	domains       map[string]struct{}
	relationships []projection
}

type credential struct {
	token  string
	client *client
	label  string
}

type storedRow struct {
	id                  string
	clientID            string
	coreUserID          string
	authoritativeDomain string
	resource            persistedUser
	active              bool
	deleted             bool
	version             int64
	createdAt           time.Time
	updatedAt           time.Time
	deletedAt           time.Time
	applied             []projection
	lastFingerprint     string
}

type intentRow struct {
	id                   string
	userID               string
	clientID             string
	coreUserID           string
	authoritativeDomain  string
	baseVersion          int64
	nextVersion          int64
	proposed             persistedUser
	proposedDeleted      bool
	from                 []projection
	to                   []projection
	attemptCount         int64
	createdAt            time.Time
	operationFingerprint string
}

type Service struct {
	db            coredb.IndexedDB
	authorization core.AuthorizationProvider
	baseURL       string
	clients       map[string]*client
	credentials   []credential
	domainOwners  map[string]string
	managed       map[string]struct{}
	retryInterval time.Duration
	driftInterval time.Duration
	now           func() time.Time
	newTicker     func(time.Duration) (<-chan time.Time, func())
	locksMu       sync.Mutex
	locks         map[string]*keyedMutex
}

type keyedMutex struct {
	mu   sync.Mutex
	refs int
}

type ServiceOptions struct {
	Now       func() time.Time
	NewTicker func(time.Duration) (<-chan time.Time, func())
}

type reconciliationMetrics struct {
	runs      metric.Int64Counter
	failures  metric.Int64Counter
	resources metric.Int64Counter
}

var scimReconciliationMetrics metricutil.MeterCache[reconciliationMetrics]

func NewService(db coredb.IndexedDB, authorization core.AuthorizationProvider, baseURL string, cfg config.ServerSCIMConfig) (*Service, error) {
	return NewServiceWithOptions(db, authorization, baseURL, cfg, ServiceOptions{})
}

func NewServiceWithOptions(db coredb.IndexedDB, authorization core.AuthorizationProvider, baseURL string, cfg config.ServerSCIMConfig, opts ServiceOptions) (*Service, error) {
	retryInterval, err := cfg.RetryIntervalDuration()
	if err != nil {
		return nil, fmt.Errorf("SCIM retry interval: %w", err)
	}
	driftInterval, err := cfg.DriftIntervalDuration()
	if err != nil {
		return nil, fmt.Errorf("SCIM drift interval: %w", err)
	}
	s := &Service{
		db: db, authorization: authorization, baseURL: strings.TrimRight(baseURL, "/"),
		clients: make(map[string]*client), domainOwners: make(map[string]string), managed: make(map[string]struct{}),
		retryInterval: retryInterval, driftInterval: driftInterval, now: opts.Now, newTicker: opts.NewTicker, locks: make(map[string]*keyedMutex),
	}
	seenTokens := make(map[string]string)
	for id, clientConfig := range cfg.Clients {
		c := &client{id: id, domains: make(map[string]struct{})}
		for _, rawDomain := range clientConfig.AuthoritativeUserDomains {
			domain := normalize(rawDomain)
			c.domains[domain] = struct{}{}
			s.domainOwners[domain] = id
		}
		for _, relationship := range clientConfig.ActiveUserRelationships {
			p := projection{Relation: strings.TrimSpace(relationship.Relation), ResourceType: strings.TrimSpace(relationship.Resource.Type), ResourceID: strings.TrimSpace(relationship.Resource.ID)}
			c.relationships = append(c.relationships, p)
			s.managed[p.key()] = struct{}{}
		}
		sort.Slice(c.relationships, func(i, j int) bool { return c.relationships[i].key() < c.relationships[j].key() })
		for _, configured := range clientConfig.Credentials {
			token := strings.TrimSpace(configured.BearerToken)
			if previous, ok := seenTokens[token]; ok {
				return nil, fmt.Errorf("SCIM bearer token for %s/%s duplicates %s", id, configured.ID, previous)
			}
			seenTokens[token] = id + "/" + configured.ID
			s.credentials = append(s.credentials, credential{token: token, client: c, label: configured.ID})
		}
		s.clients[id] = c
	}
	return s, nil
}

func (s *Service) Enabled() bool { return s != nil && len(s.clients) > 0 }

func (s *Service) ClientForToken(token string) (string, bool) {
	if s == nil || token == "" {
		return "", false
	}
	for _, candidate := range s.credentials {
		if len(token) == len(candidate.token) && subtle.ConstantTimeCompare([]byte(token), []byte(candidate.token)) == 1 {
			return candidate.client.id, true
		}
	}
	return "", false
}

func (s *Service) Start(ctx context.Context) {
	if !s.Enabled() {
		return
	}
	go func() {
		s.runReconciliation(ctx, "retry", s.reconcileDueIntents)
		s.runReconciliation(ctx, "drift", s.reconcileDrift)
		retryTicks, stopRetry := s.ticker(s.retryInterval)
		defer stopRetry()
		driftTicks, stopDrift := s.ticker(s.driftInterval)
		defer stopDrift()
		for {
			select {
			case <-ctx.Done():
				return
			case <-retryTicks:
				s.runReconciliation(ctx, "retry", s.reconcileDueIntents)
			case <-driftTicks:
				s.runReconciliation(ctx, "drift", s.reconcileDrift)
			}
		}
	}()
}

func (s *Service) currentTime() time.Time {
	if s.now != nil {
		return s.now().UTC().Truncate(time.Millisecond)
	}
	return time.Now().UTC().Truncate(time.Millisecond)
}

func (s *Service) ticker(interval time.Duration) (<-chan time.Time, func()) {
	if s.newTicker != nil {
		return s.newTicker(interval)
	}
	ticker := time.NewTicker(interval)
	return ticker.C, ticker.Stop
}

func (s *Service) runReconciliation(ctx context.Context, kind string, reconcile func(context.Context) (int, error)) {
	processed, err := reconcile(ctx)
	recordReconciliationMetrics(ctx, kind, processed, err)
	if err != nil {
		slog.WarnContext(ctx, "SCIM reconciliation finished with errors", "kind", kind, "processed", processed, "error", err)
	}
}

func recordReconciliationMetrics(ctx context.Context, kind string, processed int, err error) {
	metrics := scimReconciliationMetrics.Load(ctx, "gestaltd", func(meter metric.Meter) reconciliationMetrics {
		return reconciliationMetrics{
			runs:      metricutil.NewInt64Counter(meter, "gestaltd.scim.reconciliation.run_count", "Counts SCIM reconciliation runs."),
			failures:  metricutil.NewInt64Counter(meter, "gestaltd.scim.reconciliation.error_count", "Counts failed SCIM reconciliation runs."),
			resources: metricutil.NewInt64Counter(meter, "gestaltd.scim.reconciliation.resource_count", "Counts resources processed by SCIM reconciliation."),
		}
	})
	attrs := metric.WithAttributes(attribute.String("gestalt.scim.reconciliation.kind", kind))
	metrics.runs.Add(ctx, 1, attrs)
	if processed > 0 {
		metrics.resources.Add(ctx, int64(processed), attrs)
	}
	if err != nil {
		metrics.failures.Add(ctx, 1, attrs)
	}
}

func (s *Service) lock(key string) func() {
	s.locksMu.Lock()
	entry := s.locks[key]
	if entry == nil {
		entry = &keyedMutex{}
		s.locks[key] = entry
	}
	entry.refs++
	s.locksMu.Unlock()
	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		s.locksMu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(s.locks, key)
		}
		s.locksMu.Unlock()
	}
}

func createLockKey(clientID, userName string) string {
	return "create\x00" + clientID + "\x00" + normalize(userName)
}

func userLockKey(userID string) string {
	return "user\x00" + userID
}

func (s *Service) Create(ctx context.Context, clientID string, input userInput) (*User, error) {
	resource, email, err := createResource(input)
	if err != nil {
		return nil, err
	}
	fingerprint := operationFingerprint("create", "", resource)
	unlock := s.lock(createLockKey(clientID, resource.UserName))
	defer unlock()

	userID, err := s.createIntent(ctx, clientID, resource, email, fingerprint)
	if err != nil {
		var scimErr *Error
		if !errors.As(err, &scimErr) || scimErr.Status != 409 && scimErr.Status != 503 {
			return nil, err
		}
		pending, found, pendingErr := s.findPendingCreate(ctx, clientID, resource)
		if pendingErr != nil {
			return nil, pendingErr
		}
		if found {
			userID = pending.userID
		} else {
			committed, replayed, replayErr := s.findCommittedCreate(ctx, clientID, resource.UserName, fingerprint)
			if replayErr != nil {
				return nil, replayErr
			}
			if replayed {
				return committed, nil
			}
			return nil, err
		}
	}
	if err := s.convergeUser(ctx, userID); err != nil && !isNoIntent(err) {
		return nil, err
	}
	return s.Get(ctx, clientID, userID)
}

func (s *Service) createIntent(ctx context.Context, clientID string, resource persistedUser, email, fingerprint string) (string, error) {
	now := s.currentTime()
	userID := uuid.NewString()
	tx, err := s.db.Transaction(ctx, []string{coredata.StoreUsers, coredata.StoreSCIMUsers, coredata.StoreSCIMProjectionIntents}, idb.TransactionReadwrite, idb.TransactionOptions{DurabilityHint: idb.TransactionDurabilityStrict})
	if err != nil {
		return "", unavailable("could not begin SCIM mutation")
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(context.WithoutCancel(ctx))
		}
	}()
	if err := ensureUnique(ctx, tx, clientID, userID, resource, email); err != nil {
		return "", err
	}
	coreUserID, err := linkCoreUser(ctx, tx, "", email, displayName(resource), now)
	if err != nil {
		return "", err
	}
	desired := s.desiredProjections(clientID, resource.Active, false)
	intent := newIntentRecord(uuid.NewString(), userID, clientID, coreUserID, s.authoritativeDomain(clientID, email), 0, 1, resource, false, nil, desired, fingerprint, now)
	if err := tx.ObjectStore(coredata.StoreSCIMProjectionIntents).Add(ctx, intent); err != nil {
		return "", mapIntentWriteError("create SCIM User", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", mapIntentWriteError("create SCIM User", err)
	}
	committed = true
	return userID, nil
}

func (s *Service) findCommittedCreate(ctx context.Context, clientID, userName, fingerprint string) (*User, bool, error) {
	rec, err := s.db.ObjectStore(coredata.StoreSCIMUsers).Index("by_user_name_key").Get(ctx, namespaceKey(clientID, normalize(userName)))
	if errors.Is(err, idb.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, unavailable("could not inspect committed SCIM create")
	}
	row, err := decodeStoredRow(rec)
	if err != nil {
		return nil, false, unavailable("committed SCIM User is invalid")
	}
	if row.clientID != clientID || row.deleted || row.lastFingerprint != fingerprint {
		return nil, false, nil
	}
	user := s.publicUser(row)
	return &user, true, nil
}

func (s *Service) findPendingCreate(ctx context.Context, clientID string, resource persistedUser) (intentRow, bool, error) {
	rec, err := s.db.ObjectStore(coredata.StoreSCIMProjectionIntents).Index("by_user_name_key").Get(ctx, namespaceKey(clientID, normalize(resource.UserName)))
	if errors.Is(err, idb.ErrNotFound) {
		return intentRow{}, false, nil
	}
	if err != nil {
		return intentRow{}, false, unavailable("could not inspect pending SCIM create")
	}
	intent, err := decodeIntentRow(rec)
	if err != nil {
		return intentRow{}, false, unavailable("pending SCIM create is invalid")
	}
	if intent.clientID != clientID || intent.baseVersion != 0 || intent.nextVersion != 1 || intent.proposedDeleted || !equalPersistedUsers(intent.proposed, resource) {
		return intentRow{}, false, nil
	}
	return intent, true, nil
}

func equalPersistedUsers(left, right persistedUser) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func operationFingerprint(operation, ifMatch string, payload any) string {
	encoded, _ := json.Marshal(struct {
		Operation string `json:"operation"`
		IfMatch   string `json:"ifMatch,omitempty"`
		Payload   any    `json:"payload,omitempty"`
	}{Operation: operation, IfMatch: ifMatch, Payload: payload})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func patchOperationFingerprint(ifMatch string, request patchRequest) string {
	type canonicalOperation struct {
		Op    string `json:"op"`
		Path  string `json:"path,omitempty"`
		Value any    `json:"value,omitempty"`
	}
	operations := make([]canonicalOperation, 0, len(request.Operations))
	for _, operation := range request.Operations {
		canonical := canonicalOperation{Op: strings.ToLower(strings.TrimSpace(operation.Op)), Path: normalizePatchPath(operation.Path)}
		if len(operation.Value) > 0 {
			if err := json.Unmarshal(operation.Value, &canonical.Value); err != nil {
				canonical.Value = string(operation.Value)
			}
		}
		operations = append(operations, canonical)
	}
	return operationFingerprint("patch", ifMatch, operations)
}

func (s *Service) Get(ctx context.Context, clientID, id string) (*User, error) {
	rec, err := s.db.ObjectStore(coredata.StoreSCIMUsers).Get(ctx, id)
	if errors.Is(err, idb.ErrNotFound) {
		return nil, notFound()
	}
	if err != nil {
		return nil, unavailable("could not read SCIM User")
	}
	row, err := decodeStoredRow(rec)
	if err != nil {
		return nil, unavailable("stored SCIM User is invalid")
	}
	if row.clientID != clientID || row.deleted {
		return nil, notFound()
	}
	user := s.publicUser(row)
	return &user, nil
}

func (s *Service) list(ctx context.Context, clientID, rawFilter string, startIndex, count int) (listResponse, error) {
	clauses, err := parseFilter(rawFilter)
	if err != nil {
		return listResponse{}, invalid(err.Error())
	}
	records, err := s.db.ObjectStore(coredata.StoreSCIMUsers).Index("by_client").GetAll(ctx, clientID)
	if err != nil {
		return listResponse{}, unavailable("could not list SCIM Users")
	}
	rows := make([]storedRow, 0, len(records))
	for _, rec := range records {
		row, decodeErr := decodeStoredRow(rec)
		if decodeErr != nil {
			return listResponse{}, unavailable("stored SCIM User is invalid")
		}
		if !row.deleted && matchesFilter(row.resource, clauses) {
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].id < rows[j].id })
	total := len(rows)
	begin := startIndex - 1
	if begin > total {
		begin = total
	}
	end := begin + count
	if end > total {
		end = total
	}
	resources := make([]User, 0, end-begin)
	for i := begin; i < end; i++ {
		resources = append(resources, s.publicUser(rows[i]))
	}
	return listResponse{Schemas: []string{ListSchemaURN}, TotalResults: total, StartIndex: startIndex, ItemsPerPage: len(resources), Resources: resources}, nil
}

func (s *Service) Replace(ctx context.Context, clientID, id, ifMatch string, input userInput) (*User, error) {
	resource, email, err := createResource(input)
	if err != nil {
		return nil, err
	}
	return s.mutate(ctx, clientID, id, ifMatch, resource, email, false, operationFingerprint("replace", ifMatch, resource))
}

func (s *Service) Patch(ctx context.Context, clientID, id, ifMatch string, request patchRequest) (*User, error) {
	fingerprint := patchOperationFingerprint(ifMatch, request)
	unlock := s.lock(userLockKey(id))
	defer unlock()
	if err := s.convergeUserLocked(ctx, id); err != nil && !isNoIntent(err) {
		return nil, err
	}
	current, err := s.loadStored(ctx, clientID, id)
	if err != nil {
		return nil, err
	}
	if current.lastFingerprint == fingerprint {
		user := s.publicUser(current)
		return &user, nil
	}
	resource, err := applyPatch(current.resource, request)
	if err != nil {
		return nil, err
	}
	email, err := loginEmail(resource)
	if err != nil {
		return nil, err
	}
	return s.commitMutationLocked(ctx, clientID, id, ifMatch, current, resource, email, false, fingerprint)
}

func (s *Service) Delete(ctx context.Context, clientID, id, ifMatch string) error {
	fingerprint := operationFingerprint("delete", ifMatch, nil)
	unlock := s.lock(userLockKey(id))
	defer unlock()
	if err := s.convergeUserLocked(ctx, id); err != nil && !isNoIntent(err) {
		return err
	}
	current, err := s.loadStoredIncludingDeleted(ctx, clientID, id)
	if err != nil {
		return err
	}
	if current.deleted {
		if current.lastFingerprint == fingerprint {
			return nil
		}
		return notFound()
	}
	_, err = s.commitMutationLocked(ctx, clientID, id, ifMatch, current, current.resource, mustLoginEmail(current.resource), true, fingerprint)
	return err
}

func (s *Service) mutate(ctx context.Context, clientID, id, ifMatch string, proposed persistedUser, email string, deleted bool, fingerprint string) (*User, error) {
	unlock := s.lock(userLockKey(id))
	defer unlock()
	if err := s.convergeUserLocked(ctx, id); err != nil && !isNoIntent(err) {
		return nil, err
	}
	current, err := s.loadStored(ctx, clientID, id)
	if err != nil {
		return nil, err
	}
	if current.lastFingerprint == fingerprint {
		user := s.publicUser(current)
		return &user, nil
	}
	return s.commitMutationLocked(ctx, clientID, id, ifMatch, current, proposed, email, deleted, fingerprint)
}

func (s *Service) commitMutationLocked(ctx context.Context, clientID, id, ifMatch string, current storedRow, proposed persistedUser, email string, deleted bool, fingerprint string) (*User, error) {
	if ifMatch != "" && ifMatch != "*" && ifMatch != etag(current.version) {
		return nil, &Error{Status: 412, Detail: "If-Match does not match the current SCIM User version"}
	}
	now := s.currentTime()
	tx, err := s.db.Transaction(ctx, []string{coredata.StoreUsers, coredata.StoreSCIMUsers, coredata.StoreSCIMProjectionIntents}, idb.TransactionReadwrite, idb.TransactionOptions{DurabilityHint: idb.TransactionDurabilityStrict})
	if err != nil {
		return nil, unavailable("could not begin SCIM mutation")
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(context.WithoutCancel(ctx))
		}
	}()
	latestRec, err := tx.ObjectStore(coredata.StoreSCIMUsers).Get(ctx, id)
	if errors.Is(err, idb.ErrNotFound) {
		return nil, notFound()
	}
	if err != nil {
		return nil, unavailable("could not verify current SCIM User")
	}
	latest, decodeErr := decodeStoredRow(latestRec)
	if decodeErr != nil || latest.clientID != clientID || latest.deleted || latest.version != current.version {
		return nil, &Error{Status: 412, Detail: "SCIM User changed during mutation"}
	}
	if !deleted {
		if err := ensureUnique(ctx, tx, clientID, id, proposed, email); err != nil {
			return nil, err
		}
		if _, err := linkCoreUser(ctx, tx, current.coreUserID, email, displayName(proposed), now); err != nil {
			return nil, err
		}
	}
	desired := s.desiredProjections(clientID, proposed.Active, deleted)
	authoritativeDomain := current.authoritativeDomain
	if authoritativeDomain == "" {
		authoritativeDomain = s.authoritativeDomain(clientID, mustLoginEmail(current.resource))
	}
	if authoritativeDomain == "" && !deleted {
		authoritativeDomain = s.authoritativeDomain(clientID, email)
	}
	intent := newIntentRecord(uuid.NewString(), id, clientID, current.coreUserID, authoritativeDomain, current.version, current.version+1, proposed, deleted, current.applied, desired, fingerprint, now)
	if err := tx.ObjectStore(coredata.StoreSCIMProjectionIntents).Add(ctx, intent); err != nil {
		return nil, mapIntentWriteError("update SCIM User", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, mapIntentWriteError("update SCIM User", err)
	}
	committed = true
	if err := s.convergeUserLocked(ctx, id); err != nil && !isNoIntent(err) {
		return nil, err
	}
	if deleted {
		return nil, nil
	}
	return s.Get(ctx, clientID, id)
}

func (s *Service) loadStored(ctx context.Context, clientID, id string) (storedRow, error) {
	row, err := s.loadStoredIncludingDeleted(ctx, clientID, id)
	if err != nil {
		return storedRow{}, err
	}
	if row.deleted {
		return storedRow{}, notFound()
	}
	return row, nil
}

func (s *Service) loadStoredIncludingDeleted(ctx context.Context, clientID, id string) (storedRow, error) {
	rec, err := s.db.ObjectStore(coredata.StoreSCIMUsers).Get(ctx, id)
	if errors.Is(err, idb.ErrNotFound) {
		return storedRow{}, notFound()
	}
	if err != nil {
		return storedRow{}, unavailable("could not read SCIM User")
	}
	row, decodeErr := decodeStoredRow(rec)
	if decodeErr != nil {
		return storedRow{}, unavailable("stored SCIM User is invalid")
	}
	if row.clientID != clientID {
		return storedRow{}, notFound()
	}
	return row, nil
}

func createResource(input userInput) (persistedUser, string, error) {
	resource := persistedUser{}
	if input.ExternalID != nil {
		resource.ExternalID = *input.ExternalID
	}
	if input.UserName != nil {
		resource.UserName = *input.UserName
	}
	if strings.TrimSpace(resource.UserName) == "" {
		return persistedUser{}, "", invalid("userName is required")
	}
	resource.Active = input.Active != nil && *input.Active
	if input.DisplayName != nil {
		resource.DisplayName = *input.DisplayName
	}
	if input.Name != nil {
		resource.Name = *input.Name
	}
	if input.Emails != nil {
		resource.Emails = append([]Email(nil), (*input.Emails)...)
	}
	email, err := loginEmail(resource)
	return resource, email, err
}

func loginEmail(resource persistedUser) (string, error) {
	var primaryWork, primary []string
	var work []string
	for _, email := range resource.Emails {
		value := normalizedEmail(email.Value)
		if value == "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(email.Type), "work") {
			work = append(work, value)
			if email.Primary {
				primaryWork = append(primaryWork, value)
			}
		}
		if email.Primary {
			primary = append(primary, value)
		}
	}
	candidates := [][]string{primaryWork, primary}
	for _, values := range candidates {
		if len(values) > 0 {
			return values[0], nil
		}
	}
	if len(work) == 1 {
		return work[0], nil
	}
	if value := normalizedEmail(resource.UserName); value != "" {
		return value, nil
	}
	return "", invalid("User must have a primary work email, primary email, sole work email, or email-form userName")
}

func normalizedEmail(raw string) string {
	raw = strings.TrimSpace(raw)
	parsed, err := mail.ParseAddress(raw)
	if err != nil || !strings.EqualFold(parsed.Address, raw) || strings.Count(parsed.Address, "@") != 1 {
		return ""
	}
	return normalize(parsed.Address)
}

func getTransactionRecord(ctx context.Context, store idb.TransactionObjectStore, id string) (idb.Record, error) {
	records, err := store.GetAll(ctx, id, 1)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, idb.ErrNotFound
	}
	return records[0], nil
}

func displayName(resource persistedUser) string {
	if value := strings.TrimSpace(resource.DisplayName); value != "" {
		return value
	}
	if value := strings.TrimSpace(resource.Name.Formatted); value != "" {
		return value
	}
	return strings.TrimSpace(resource.Name.GivenName + " " + resource.Name.FamilyName)
}

func ensureUnique(ctx context.Context, tx idb.Transaction, clientID, userID string, resource persistedUser, email string) error {
	checks := []struct {
		index string
		key   string
		name  string
	}{
		{"by_user_name_key", namespaceKey(clientID, normalize(resource.UserName)), "userName"},
		{"by_email_key", namespaceKey(clientID, email), "email"},
	}
	if value := normalize(resource.ExternalID); value != "" {
		checks = append(checks, struct{ index, key, name string }{"by_external_id_key", namespaceKey(clientID, value), "externalId"})
	}
	for _, storeName := range []string{coredata.StoreSCIMUsers, coredata.StoreSCIMProjectionIntents} {
		store := tx.ObjectStore(storeName)
		for _, check := range checks {
			// Use GetAll for compatibility with remote providers that treat a
			// transactional Get miss as terminal. An empty GetAll is a successful
			// query, so subsequent uniqueness checks can use the same transaction.
			records, err := store.Index(check.index).GetAll(ctx, check.key, 1)
			if err != nil {
				return unavailable("could not validate SCIM uniqueness")
			}
			if len(records) == 0 {
				continue
			}
			rec := records[0]
			if recordString(rec, "user_id") != userID && recordString(rec, "id") != userID {
				return conflict(check.name + " is already assigned in this SCIM client namespace")
			}
		}
	}
	return nil
}

func linkCoreUser(ctx context.Context, tx idb.Transaction, coreUserID, email, name string, now time.Time) (string, error) {
	user, err := coredata.LinkUserInTransaction(ctx, tx, coreUserID, email, name, now)
	if errors.Is(err, coredata.ErrUserEmailConflict) {
		return "", conflict("email is already linked to another Gestalt user")
	}
	if err != nil {
		return "", unavailable("could not link Gestalt user")
	}
	return user.ID, nil
}

func namespaceKey(clientID, value string) string { return clientID + "\x00" + value }

func mapIntentWriteError(operation string, err error) error {
	if errors.Is(err, idb.ErrAlreadyExists) {
		return unavailable(operation + " is contending with another SCIM worker")
	}
	return unavailable(operation + " could not be persisted")
}

func newIntentRecord(intentID, userID, clientID, coreUserID, authoritativeDomain string, baseVersion, nextVersion int64, proposed persistedUser, deleted bool, from, to []projection, fingerprint string, now time.Time) idb.Record {
	rec := idb.Record{
		"id": intentID, "user_id": userID, "client_id": clientID, "core_user_id": coreUserID,
		"base_version": baseVersion, "next_version": nextVersion, "proposed": jsonMap(proposed), "proposed_deleted": deleted,
		"from_relationships": jsonSlice(from), "to_relationships": jsonSlice(to), "attempt_count": int64(0), "next_attempt_at": now,
		"created_at": now, "updated_at": now,
	}
	if authoritativeDomain != "" {
		rec["authoritative_domain"] = authoritativeDomain
	}
	if fingerprint != "" {
		rec["operation_fingerprint"] = fingerprint
	}
	if !deleted {
		rec["user_name_key"] = namespaceKey(clientID, normalize(proposed.UserName))
		rec["email_key"] = namespaceKey(clientID, mustLoginEmail(proposed))
		if externalID := normalize(proposed.ExternalID); externalID != "" {
			rec["external_id_key"] = namespaceKey(clientID, externalID)
		}
	}
	return rec
}

func mustLoginEmail(resource persistedUser) string {
	email, _ := loginEmail(resource)
	return email
}

func (s *Service) desiredProjections(clientID string, active, deleted bool) []projection {
	if deleted || !active {
		return nil
	}
	client := s.clients[clientID]
	if client == nil {
		return nil
	}
	return append([]projection(nil), client.relationships...)
}

func (s *Service) publicUser(row storedRow) User {
	return User{
		Schemas: []string{UserSchemaURN}, ID: row.id, ExternalID: row.resource.ExternalID, UserName: row.resource.UserName,
		Active: row.resource.Active, DisplayName: row.resource.DisplayName, Name: row.resource.Name, Emails: append([]Email(nil), row.resource.Emails...),
		Meta: Meta{ResourceType: "User", Created: row.createdAt, LastModified: row.updatedAt, Location: s.baseURL + "/scim/v2/Users/" + row.id, Version: etag(row.version)},
	}
}

func decodeStoredRow(rec idb.Record) (storedRow, error) {
	var resource persistedUser
	if err := decodeJSONValue(rec["resource"], &resource); err != nil {
		return storedRow{}, err
	}
	var applied []projection
	if err := decodeJSONValue(rec["applied_relationships"], &applied); err != nil {
		return storedRow{}, err
	}
	return storedRow{
		id: recordString(rec, "id"), clientID: recordString(rec, "client_id"), coreUserID: recordString(rec, "core_user_id"), authoritativeDomain: recordString(rec, "authoritative_domain"), resource: resource,
		active: recordBool(rec, "active"), deleted: recordBool(rec, "deleted"), version: recordInt(rec, "version"), createdAt: recordTime(rec, "created_at"),
		updatedAt: recordTime(rec, "updated_at"), deletedAt: recordTime(rec, "deleted_at"), applied: applied, lastFingerprint: recordString(rec, "last_operation_fingerprint"),
	}, nil
}

func decodeIntentRow(rec idb.Record) (intentRow, error) {
	var proposed persistedUser
	var from, to []projection
	if err := decodeJSONValue(rec["proposed"], &proposed); err != nil {
		return intentRow{}, err
	}
	if err := decodeJSONValue(rec["from_relationships"], &from); err != nil {
		return intentRow{}, err
	}
	if err := decodeJSONValue(rec["to_relationships"], &to); err != nil {
		return intentRow{}, err
	}
	return intentRow{id: recordString(rec, "id"), userID: recordString(rec, "user_id"), clientID: recordString(rec, "client_id"), coreUserID: recordString(rec, "core_user_id"), authoritativeDomain: recordString(rec, "authoritative_domain"), baseVersion: recordInt(rec, "base_version"), nextVersion: recordInt(rec, "next_version"), proposed: proposed, proposedDeleted: recordBool(rec, "proposed_deleted"), from: from, to: to, attemptCount: recordInt(rec, "attempt_count"), createdAt: recordTime(rec, "created_at"), operationFingerprint: recordString(rec, "operation_fingerprint")}, nil
}

func jsonMap(value any) map[string]any {
	var result map[string]any
	data, _ := json.Marshal(value)
	_ = json.Unmarshal(data, &result)
	return result
}

func jsonSlice(value any) []any {
	var result []any
	data, _ := json.Marshal(value)
	_ = json.Unmarshal(data, &result)
	return result
}

func decodeJSONValue(value any, target any) error {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func recordString(rec idb.Record, key string) string {
	value, _ := rec[key].(string)
	return value
}

func recordBool(rec idb.Record, key string) bool {
	value, _ := rec[key].(bool)
	return value
}

func recordInt(rec idb.Record, key string) int64 {
	switch value := rec[key].(type) {
	case int:
		return int64(value)
	case int32:
		return int64(value)
	case int64:
		return value
	case float64:
		return int64(value)
	default:
		return 0
	}
}

func recordTime(rec idb.Record, key string) time.Time {
	switch value := rec[key].(type) {
	case time.Time:
		return value
	case string:
		parsed, _ := time.Parse(time.RFC3339Nano, value)
		return parsed
	default:
		return time.Time{}
	}
}

func projectionSet(values []projection) map[string]projection {
	out := make(map[string]projection, len(values))
	for _, value := range values {
		out[value.key()] = value
	}
	return out
}

func (s *Service) applyProjections(ctx context.Context, intent intentRow) ([]projection, error) {
	if s.authorization == nil && (len(intent.from) > 0 || len(intent.to) > 0) {
		return nil, fmt.Errorf("authorization provider is unavailable")
	}
	from, to := projectionSet(intent.from), projectionSet(intent.to)
	for key, value := range from {
		if _, keep := to[key]; keep || !value.Owned {
			continue
		}
		existing, err := s.findProjection(ctx, intent.coreUserID, value)
		if err != nil {
			return nil, err
		}
		if existing == nil || !relationshipOwnedBy(existing, intent.clientID, intent.userID) {
			continue
		}
		_, err = s.authorization.DeleteRelationship(ctx, &proto.DeleteRelationshipRequest{RelationshipTuple: projectionTuple(intent.coreUserID, value)})
		if err != nil && !errors.Is(err, core.ErrNotFound) {
			return nil, err
		}
	}
	desired := make([]projection, 0, len(to))
	for _, value := range to {
		desired = append(desired, value)
	}
	sort.Slice(desired, func(i, j int) bool { return desired[i].key() < desired[j].key() })
	applied := make([]projection, 0, len(desired))
	for _, value := range desired {
		value.Owned = false
		existing, err := s.findProjection(ctx, intent.coreUserID, value)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			value.Owned = relationshipOwnedBy(existing, intent.clientID, intent.userID)
			applied = append(applied, value)
			continue
		}
		properties, _ := structpb.NewStruct(map[string]any{"managedBy": "scim", "scimClientId": intent.clientID, "scimUserId": intent.userID})
		_, err = s.authorization.AddRelationship(ctx, &proto.AddRelationshipRequest{Relationship: &proto.Relationship{Tuple: projectionTuple(intent.coreUserID, value), Properties: properties, SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME}})
		if err != nil {
			return nil, err
		}
		value.Owned = true
		applied = append(applied, value)
	}
	return applied, nil
}

func (s *Service) findProjection(ctx context.Context, coreUserID string, value projection) (*proto.Relationship, error) {
	tuple := projectionTuple(coreUserID, value)
	response, err := s.authorization.ListRelationships(ctx, &proto.ListRelationshipsRequest{
		Filter:   &proto.RelationshipFilter{Target: tuple.Target, Relation: tuple.Relation, Resource: tuple.Resource},
		PageSize: 2,
	})
	if err != nil {
		return nil, err
	}
	for _, relationship := range response.GetRelationships() {
		if gproto.Equal(relationship.GetTuple(), tuple) {
			return relationship, nil
		}
	}
	return nil, nil
}

func relationshipOwnedBy(relationship *proto.Relationship, clientID, userID string) bool {
	if relationship == nil || relationship.GetSourceLayer() != proto.SourceLayer_SOURCE_LAYER_RUNTIME || relationship.GetProperties() == nil {
		return false
	}
	properties := relationship.GetProperties().AsMap()
	return properties["managedBy"] == "scim" && properties["scimClientId"] == clientID && properties["scimUserId"] == userID
}

func projectionTuple(coreUserID string, value projection) *proto.RelationshipTuple {
	return &proto.RelationshipTuple{
		Target:   &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{Type: "subject", Id: "user:" + coreUserID}}},
		Relation: value.Relation,
		Resource: &proto.Resource{Type: value.ResourceType, Id: value.ResourceID},
	}
}

var errNoIntent = errors.New("SCIM projection intent not found")

func isNoIntent(err error) bool { return errors.Is(err, errNoIntent) }

func (s *Service) convergeUser(ctx context.Context, userID string) error {
	unlock := s.lock(userLockKey(userID))
	defer unlock()
	return s.convergeUserLocked(ctx, userID)
}

func (s *Service) convergeUserLocked(ctx context.Context, userID string) error {
	rec, err := s.db.ObjectStore(coredata.StoreSCIMProjectionIntents).Index("by_user").Get(ctx, userID)
	if errors.Is(err, idb.ErrNotFound) {
		return errNoIntent
	}
	if err != nil {
		return unavailable("could not load pending SCIM mutation")
	}
	intent, err := decodeIntentRow(rec)
	if err != nil {
		return unavailable("pending SCIM mutation is invalid")
	}
	applied, err := s.applyProjections(ctx, intent)
	if err != nil {
		if recordErr := s.recordProjectionFailure(ctx, rec, err); recordErr != nil {
			slog.WarnContext(ctx, "SCIM projection failure could not be recorded", "user_id", intent.userID, "client_id", intent.clientID, "error", recordErr)
		}
		return unavailable("authorization projection has not converged")
	}
	intent.to = applied
	now := s.currentTime()
	tx, err := s.db.Transaction(ctx, []string{coredata.StoreSCIMUsers, coredata.StoreSCIMProjectionIntents}, idb.TransactionReadwrite, idb.TransactionOptions{DurabilityHint: idb.TransactionDurabilityStrict})
	if err != nil {
		return unavailable("could not commit SCIM mutation")
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(context.WithoutCancel(ctx))
		}
	}()
	latest, err := getTransactionRecord(ctx, tx.ObjectStore(coredata.StoreSCIMProjectionIntents), intent.id)
	if errors.Is(err, idb.ErrNotFound) {
		alreadyCommitted, committedErr := intentAlreadyCommitted(ctx, tx, intent)
		if committedErr != nil {
			return unavailable("could not verify superseded SCIM mutation")
		}
		if alreadyCommitted {
			return nil
		}
		return unavailable("SCIM mutation was superseded")
	}
	if err != nil || recordString(latest, "user_id") != intent.userID || recordInt(latest, "next_version") != intent.nextVersion {
		return unavailable("SCIM mutation was superseded")
	}
	createdAt := intent.createdAt
	lastFingerprint := intent.operationFingerprint
	if existing, getErr := getTransactionRecord(ctx, tx.ObjectStore(coredata.StoreSCIMUsers), intent.userID); getErr == nil {
		createdAt = recordTime(existing, "created_at")
		if lastFingerprint == "" {
			lastFingerprint = recordString(existing, "last_operation_fingerprint")
		}
	} else if !errors.Is(getErr, idb.ErrNotFound) {
		return unavailable("could not inspect current SCIM User")
	}
	row := idb.Record{
		"id": intent.userID, "client_id": intent.clientID, "core_user_id": intent.coreUserID,
		"active": intent.proposed.Active && !intent.proposedDeleted, "deleted": intent.proposedDeleted, "version": intent.nextVersion,
		"resource": jsonMap(intent.proposed), "applied_relationships": jsonSlice(intent.to), "created_at": createdAt, "updated_at": now,
	}
	if intent.authoritativeDomain != "" {
		row["authoritative_domain"] = intent.authoritativeDomain
	}
	if lastFingerprint != "" {
		row["last_operation_fingerprint"] = lastFingerprint
	}
	if intent.proposedDeleted {
		row["deleted_at"] = now
	} else {
		row["user_name_key"] = namespaceKey(intent.clientID, normalize(intent.proposed.UserName))
		row["email_key"] = namespaceKey(intent.clientID, mustLoginEmail(intent.proposed))
		if value := normalize(intent.proposed.ExternalID); value != "" {
			row["external_id_key"] = namespaceKey(intent.clientID, value)
		}
	}
	if err := tx.ObjectStore(coredata.StoreSCIMUsers).Put(ctx, row); err != nil {
		return mapIntentWriteError("commit SCIM User", err)
	}
	if err := tx.ObjectStore(coredata.StoreSCIMProjectionIntents).Delete(ctx, intent.id); err != nil {
		return unavailable("could not clear committed SCIM mutation")
	}
	if err := tx.Commit(ctx); err != nil {
		return unavailable("could not commit SCIM mutation")
	}
	committed = true
	return nil
}

func intentAlreadyCommitted(ctx context.Context, tx idb.Transaction, intent intentRow) (bool, error) {
	rec, err := getTransactionRecord(ctx, tx.ObjectStore(coredata.StoreSCIMUsers), intent.userID)
	if errors.Is(err, idb.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	row, err := decodeStoredRow(rec)
	if err != nil {
		return false, err
	}
	if row.clientID != intent.clientID || row.coreUserID != intent.coreUserID || row.version < intent.nextVersion {
		return false, nil
	}
	return intent.operationFingerprint == "" || row.lastFingerprint == intent.operationFingerprint, nil
}

func (s *Service) recordProjectionFailure(ctx context.Context, rec idb.Record, projectionErr error) error {
	tx, err := s.db.Transaction(ctx, []string{coredata.StoreSCIMProjectionIntents}, idb.TransactionReadwrite, idb.TransactionOptions{DurabilityHint: idb.TransactionDurabilityStrict})
	if err != nil {
		return fmt.Errorf("begin failure update: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(context.WithoutCancel(ctx))
		}
	}()
	latest, err := getTransactionRecord(ctx, tx.ObjectStore(coredata.StoreSCIMProjectionIntents), recordString(rec, "id"))
	if errors.Is(err, idb.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load pending intent: %w", err)
	}
	if recordString(latest, "user_id") != recordString(rec, "user_id") || recordInt(latest, "next_version") != recordInt(rec, "next_version") {
		return nil
	}
	attempt := recordInt(latest, "attempt_count") + 1
	delay := time.Second << min(attempt-1, 6)
	if delay > time.Minute {
		delay = time.Minute
	}
	now := s.currentTime()
	latest["attempt_count"] = attempt
	latest["next_attempt_at"] = now.Add(delay)
	latest["updated_at"] = now
	latest["last_error"] = projectionErr.Error()
	if err := tx.ObjectStore(coredata.StoreSCIMProjectionIntents).Put(ctx, latest); err != nil {
		return fmt.Errorf("store failure update: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit failure update: %w", err)
	}
	committed = true
	return nil
}

func (s *Service) reconcileDueIntents(ctx context.Context) (int, error) {
	intents, err := s.db.ObjectStore(coredata.StoreSCIMProjectionIntents).Index("by_next_attempt").GetAll(ctx, idb.UpperBound(s.currentTime(), false), maxDueIntentBatch)
	if err != nil {
		return 0, fmt.Errorf("list due projection intents: %w", err)
	}
	var errs []error
	for _, rec := range intents {
		userID := recordString(rec, "user_id")
		if userID == "" {
			errs = append(errs, fmt.Errorf("projection intent %q has no user", recordString(rec, "id")))
			continue
		}
		if err := s.convergeUser(ctx, userID); err != nil && !isNoIntent(err) {
			errs = append(errs, fmt.Errorf("converge user %q: %w", userID, err))
		}
	}
	return len(intents), errors.Join(errs...)
}

func (s *Service) reconcileDrift(ctx context.Context) (int, error) {
	records, err := s.db.ObjectStore(coredata.StoreSCIMUsers).GetAll(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("list SCIM users for drift reconciliation: %w", err)
	}
	var errs []error
	processed := 0
	for _, rec := range records {
		row, decodeErr := decodeStoredRow(rec)
		if decodeErr != nil {
			errs = append(errs, fmt.Errorf("decode SCIM user %q: %w", recordString(rec, "id"), decodeErr))
			continue
		}
		if row.deleted {
			continue
		}
		desired := s.desiredProjections(row.clientID, row.resource.Active, false)
		if len(desired) == 0 && len(row.applied) == 0 {
			continue
		}
		processed++
		unlock := s.lock(userLockKey(row.id))
		_, pendingErr := s.db.ObjectStore(coredata.StoreSCIMProjectionIntents).Index("by_user").Get(ctx, row.id)
		if errors.Is(pendingErr, idb.ErrNotFound) {
			converged, convergenceErr := s.projectionsConverged(ctx, row, desired)
			if convergenceErr != nil {
				errs = append(errs, fmt.Errorf("inspect projections for user %q: %w", row.id, convergenceErr))
				unlock()
				continue
			}
			if converged {
				unlock()
				continue
			}
			now := s.currentTime()
			authoritativeDomain := row.authoritativeDomain
			if authoritativeDomain == "" {
				authoritativeDomain = s.authoritativeDomain(row.clientID, mustLoginEmail(row.resource))
			}
			intent := newIntentRecord(uuid.NewString(), row.id, row.clientID, row.coreUserID, authoritativeDomain, row.version, row.version, row.resource, false, row.applied, desired, "", now)
			if addErr := s.db.ObjectStore(coredata.StoreSCIMProjectionIntents).Add(ctx, intent); addErr != nil && !errors.Is(addErr, idb.ErrAlreadyExists) {
				errs = append(errs, fmt.Errorf("record projection drift for user %q: %w", row.id, addErr))
				unlock()
				continue
			}
			if convergenceErr := s.convergeUserLocked(ctx, row.id); convergenceErr != nil && !isNoIntent(convergenceErr) {
				errs = append(errs, fmt.Errorf("converge projection drift for user %q: %w", row.id, convergenceErr))
			}
		} else if pendingErr != nil {
			errs = append(errs, fmt.Errorf("inspect pending projection for user %q: %w", row.id, pendingErr))
		}
		unlock()
	}
	return processed, errors.Join(errs...)
}

func (s *Service) projectionsConverged(ctx context.Context, row storedRow, desired []projection) (bool, error) {
	if !equalProjections(row.applied, desired) {
		return false, nil
	}
	applied := projectionSet(row.applied)
	for _, value := range desired {
		existing, err := s.findProjection(ctx, row.coreUserID, value)
		if err != nil {
			return false, err
		}
		if existing == nil || applied[value.key()].Owned != relationshipOwnedBy(existing, row.clientID, row.id) {
			return false, nil
		}
	}
	return true, nil
}

func equalProjections(left, right []projection) bool {
	if len(left) != len(right) {
		return false
	}
	a, b := projectionSet(left), projectionSet(right)
	if len(a) != len(b) {
		return false
	}
	for key := range a {
		if _, ok := b[key]; !ok {
			return false
		}
	}
	return true
}

func (s *Service) IsEligible(ctx context.Context, coreUserID, email string) (bool, error) {
	intents, err := s.db.ObjectStore(coredata.StoreSCIMProjectionIntents).Index("by_core_user").GetAll(ctx, coreUserID)
	if err != nil {
		return false, err
	}
	users, err := s.db.ObjectStore(coredata.StoreSCIMUsers).Index("by_core_user").GetAll(ctx, coreUserID)
	if err != nil {
		return false, err
	}
	clientID := ""
	if parts := strings.Split(normalize(email), "@"); len(parts) == 2 {
		clientID = s.domainOwners[parts[1]]
	}
	for _, rec := range users {
		candidateClient := recordString(rec, "client_id")
		if s.clientOwnsDomain(candidateClient, recordString(rec, "authoritative_domain")) {
			clientID = candidateClient
			break
		}
	}
	if clientID == "" {
		for _, rec := range intents {
			candidateClient := recordString(rec, "client_id")
			if s.clientOwnsDomain(candidateClient, recordString(rec, "authoritative_domain")) {
				clientID = candidateClient
				break
			}
		}
	}
	if clientID == "" {
		return true, nil
	}
	activeUsers := make(map[string]storedRow)
	for _, rec := range users {
		if recordString(rec, "client_id") != clientID || !recordBool(rec, "active") || recordBool(rec, "deleted") {
			continue
		}
		row, decodeErr := decodeStoredRow(rec)
		if decodeErr != nil {
			return false, fmt.Errorf("decode active SCIM user: %w", decodeErr)
		}
		activeUsers[row.id] = row
	}
	if len(activeUsers) == 0 {
		return false, nil
	}
	for _, rec := range intents {
		if recordString(rec, "client_id") != clientID {
			continue
		}
		intent, decodeErr := decodeIntentRow(rec)
		if decodeErr != nil {
			return false, fmt.Errorf("decode pending SCIM mutation: %w", decodeErr)
		}
		committed, ok := activeUsers[intent.userID]
		if !ok || !intentPreservesEligibility(intent, committed) {
			return false, nil
		}
	}
	return true, nil
}

func intentPreservesEligibility(intent intentRow, committed storedRow) bool {
	if intent.clientID != committed.clientID || intent.coreUserID != committed.coreUserID || intent.proposedDeleted || !intent.proposed.Active {
		return false
	}
	currentEmail, currentErr := loginEmail(committed.resource)
	proposedEmail, proposedErr := loginEmail(intent.proposed)
	if currentErr != nil || proposedErr != nil || normalize(currentEmail) != normalize(proposedEmail) {
		return false
	}
	desired := projectionSet(intent.to)
	for key := range projectionSet(intent.from) {
		if _, retained := desired[key]; !retained {
			return false
		}
	}
	return true
}

func (s *Service) authoritativeDomain(clientID, email string) string {
	parts := strings.Split(normalize(email), "@")
	if len(parts) != 2 || !s.clientOwnsDomain(clientID, parts[1]) {
		return ""
	}
	return parts[1]
}

func (s *Service) clientOwnsDomain(clientID, domain string) bool {
	client := s.clients[clientID]
	if client == nil || domain == "" {
		return false
	}
	_, ok := client.domains[normalize(domain)]
	return ok
}

func (s *Service) ManagedRelationship(resourceType, resourceID, relation string) bool {
	_, ok := s.managed[(projection{ResourceType: resourceType, ResourceID: resourceID, Relation: relation}).key()]
	return ok
}
