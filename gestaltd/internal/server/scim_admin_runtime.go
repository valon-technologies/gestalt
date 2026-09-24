package server

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/scim"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
)

var (
	ErrSCIMRuntimeUnavailable = errors.New("SCIM runtime configuration is unavailable")
	// ErrSCIMPropagationUnsupported explicitly fences runtime SCIM writes until
	// cross-replica propagation is implemented. This is safer than reporting a
	// write as applied while other replicas keep stale credentials.
	ErrSCIMPropagationUnsupported = errors.New("runtime SCIM writes require a single server replica")
)

const scimRuntimePollInterval = 2 * time.Second

// SCIMRuntime owns runtime SCIM configuration, validation, encryption, and
// propagation into the single SCIM runtime snapshot.
type SCIMRuntime struct {
	services                *coredata.Services
	db                      indexeddb.IndexedDB
	authz                   core.AuthorizationProvider
	runtime                 *scim.Runtime
	baseURL                 string
	fallback                config.ServerSCIMConfig
	platformManagedGroupIDs map[string]struct{}
	source                  string
	writesEnabled           bool
	encrypt                 func(plaintext string) (string, error)
	decrypt                 func(encoded string) (string, error)
	gateway                 *providergateway.ProviderGatewayTransport
	mu                      sync.Mutex
}

func NewSCIMRuntime(
	services *coredata.Services,
	db indexeddb.IndexedDB,
	authz core.AuthorizationProvider,
	runtime *scim.Runtime,
	baseURL string,
	fallback config.ServerSCIMConfig,
	platformManagedGroupIDs map[string]struct{},
	stateSecret []byte,
	source string,
	writesEnabled bool,
	gateway *providergateway.ProviderGatewayTransport,
) *SCIMRuntime {
	if runtime == nil {
		runtime = scim.NewRuntime(nil, config.ServerSCIMConfig{})
	}
	r := &SCIMRuntime{
		services:                services,
		db:                      db,
		authz:                   authz,
		runtime:                 runtime,
		baseURL:                 baseURL,
		fallback:                fallback,
		platformManagedGroupIDs: platformManagedGroupIDs,
		source:                  source,
		writesEnabled:           writesEnabled,
		gateway:                 gateway,
	}
	if len(stateSecret) > 0 {
		r.encrypt = func(plaintext string) (string, error) {
			encryptor, err := crypto.NewAESGCM(stateSecret)
			if err != nil {
				return "", err
			}
			return encryptor.Encrypt(plaintext)
		}
		r.decrypt = func(encoded string) (string, error) {
			encryptor, err := crypto.NewAESGCM(stateSecret)
			if err != nil {
				return "", err
			}
			return encryptor.Decrypt(encoded)
		}
	}
	r.applyCurrentSnapshot(context.Background())
	go r.pollSharedStore()
	return r
}

func (r *SCIMRuntime) available() error {
	if r == nil || r.services == nil || r.services.SCIMConfig == nil || r.db == nil || r.encrypt == nil || r.decrypt == nil {
		return ErrSCIMRuntimeUnavailable
	}
	return nil
}

// Current returns retained runtime clients, or YAML clients when runtime
// storage is empty. Credential tokens are never included.
func (r *SCIMRuntime) Current(ctx context.Context) ([]*coredata.SCIMClientRecord, string, error) {
	if err := r.available(); err != nil {
		return nil, "", err
	}
	runtimeClients, err := r.services.SCIMConfig.List(ctx)
	if err != nil {
		return nil, "", err
	}
	for _, client := range runtimeClients {
		secrets, err := r.services.SCIMConfig.Secrets(ctx, client.ID)
		if err != nil {
			return nil, "", err
		}
		client.CredentialIDs = make([]string, 0, len(secrets))
		for i := range secrets {
			client.CredentialIDs = append(client.CredentialIDs, secrets[i].CredentialID)
		}
	}
	if len(runtimeClients) > 0 {
		return runtimeClients, "runtime", nil
	}
	fallbackClients := make([]*coredata.SCIMClientRecord, 0, len(r.fallback.Clients))
	for clientID, client := range r.fallback.Clients {
		data := scim.DataFromClientConfig(client)
		data.CredentialIDs = make([]string, len(client.Credentials))
		for i, credential := range client.Credentials {
			data.CredentialIDs[i] = credential.ID
		}
		fallbackClients = append(fallbackClients, &coredata.SCIMClientRecord{
			SCIMClientData: data,
			ID:             clientID,
			Enabled:        true,
		})
	}
	return fallbackClients, "config", nil
}

// Put validates, encrypts, persists, and publishes one complete snapshot.
func (r *SCIMRuntime) Put(ctx context.Context, input *coredata.SCIMClientRecord, plaintextTokens map[string]string, actor string, requireRevisionSet bool, requireRevision int64) (*coredata.SCIMClientRecord, error) {
	if err := r.available(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.ensurePropagationSupported(ctx); err != nil {
		return nil, err
	}
	client := *input
	if strings.TrimSpace(client.ID) == "" || strings.TrimSpace(client.ID) != client.ID {
		return nil, fmt.Errorf("SCIM client id must be non-empty and trimmed")
	}
	if !requireRevisionSet {
		if _, err := r.services.SCIMConfig.Get(ctx, client.ID); err == nil {
			return nil, coredata.ErrSCIMClientExists
		} else if !errors.Is(err, coredata.ErrSCIMConfigNotFound) {
			return nil, err
		}
	}
	if !client.Enabled && !client.Retained {
		client.Retained = true
	}

	secrets, err := r.prepareSecrets(ctx, client, plaintextTokens)
	if err != nil {
		return nil, err
	}
	if client.Enabled {
		if err := r.validateLocked(ctx, client, secrets); err != nil {
			return nil, err
		}
	}
	saved, err := r.services.SCIMConfig.Put(ctx, coredata.PutSCIMClientInput{
		Client: &client, Secrets: secrets, Actor: actor,
		RequireRevisionSet: requireRevisionSet, RequireRevision: requireRevision,
	})
	if err != nil {
		return nil, err
	}
	if err := r.publishLocked(ctx); err != nil {
		return nil, fmt.Errorf("SCIM write persisted but activation failed: %w", err)
	}
	return saved, nil
}

func (r *SCIMRuntime) prepareSecrets(ctx context.Context, client coredata.SCIMClientRecord, plaintextTokens map[string]string) ([]coredata.SCIMClientSecret, error) {
	existing, err := r.services.SCIMConfig.Secrets(ctx, client.ID)
	if err != nil {
		return nil, err
	}
	existingByCredential := make(map[string]coredata.SCIMClientSecret, len(existing))
	for i := range existing {
		existingByCredential[existing[i].CredentialID] = existing[i]
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	secrets := make([]coredata.SCIMClientSecret, 0, len(client.CredentialIDs))
	for _, credentialID := range client.CredentialIDs {
		credentialID = strings.TrimSpace(credentialID)
		if credentialID == "" {
			return nil, fmt.Errorf("credential id is required")
		}
		plaintext := strings.TrimSpace(plaintextTokens[credentialID])
		if plaintext == "" {
			prior, ok := existingByCredential[credentialID]
			if !ok {
				return nil, fmt.Errorf("credential token is required for %q", credentialID)
			}
			secrets = append(secrets, prior)
			continue
		}
		encoded, err := r.encrypt(plaintext)
		if err != nil {
			return nil, fmt.Errorf("encrypt SCIM credential: %w", err)
		}
		secrets = append(secrets, coredata.SCIMClientSecret{
			ClientID:     client.ID,
			CredentialID: credentialID,
			Ciphertext:   []byte(encoded),
			CreatedAt:    now,
		})
	}
	return secrets, nil
}

// Disable reuses Put so disable-and-retain has the same concurrency semantics.
func (r *SCIMRuntime) Disable(ctx context.Context, clientID, actor string, revision int64) (*coredata.SCIMClientRecord, error) {
	if err := r.available(); err != nil {
		return nil, err
	}
	current, err := r.services.SCIMConfig.Get(ctx, clientID)
	if err != nil {
		return nil, err
	}
	if revision != 0 && current.Revision != revision {
		return nil, coredata.ErrSCIMConfigConflict
	}
	current.Enabled = false
	current.Retained = true
	return r.Put(ctx, current, nil, actor, true, current.Revision)
}

// MigrateConfig creates runtime records for every configured YAML client that
// does not already exist. It reuses the plaintext bearer token already
// resolved in-process, so the secret never crosses an API boundary.
func (r *SCIMRuntime) MigrateConfig(ctx context.Context, actor string) ([]*coredata.SCIMClientRecord, error) {
	if err := r.available(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.ensurePropagationSupported(ctx); err != nil {
		return nil, err
	}
	out := make([]*coredata.SCIMClientRecord, 0, len(r.fallback.Clients))
	for clientID, client := range r.fallback.Clients {
		if _, err := r.services.SCIMConfig.Get(ctx, clientID); err == nil {
			continue
		} else if !errors.Is(err, coredata.ErrSCIMConfigNotFound) {
			return nil, err
		}
		tokens := make(map[string]string, len(client.Credentials))
		credentialIDs := make([]string, 0, len(client.Credentials))
		for _, credential := range client.Credentials {
			credentialID := strings.TrimSpace(credential.ID)
			if credentialID == "" || strings.TrimSpace(credential.BearerToken) == "" {
				return nil, fmt.Errorf("configured SCIM client %q has an incomplete credential", clientID)
			}
			credentialIDs = append(credentialIDs, credentialID)
			tokens[credentialID] = credential.BearerToken
		}
		record := &coredata.SCIMClientRecord{
			SCIMClientData: scim.DataFromClientConfig(client),
			ID:             clientID,
			Enabled:        true,
		}
		saved, err := r.Put(ctx, record, tokens, actor, false, 0)
		if err != nil {
			return nil, err
		}
		out = append(out, saved)
	}
	return out, nil
}

// Delete removes a disabled retained client and its encrypted credentials.
// It does not remove SCIM resources or authorization relationships.
func (r *SCIMRuntime) Delete(ctx context.Context, clientID, actor string, revision int64) error {
	if err := r.available(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, err := r.services.SCIMConfig.Get(ctx, clientID)
	if err != nil {
		return err
	}
	if current.Enabled {
		return fmt.Errorf("disable the SCIM client before deleting it")
	}
	if revision != 0 && current.Revision != revision {
		return coredata.ErrSCIMConfigConflict
	}
	if err := r.services.SCIMConfig.Delete(ctx, clientID, current.Revision); err != nil {
		return err
	}
	return r.publishLocked(ctx)
}

func (r *SCIMRuntime) validateLocked(ctx context.Context, client coredata.SCIMClientRecord, secrets []coredata.SCIMClientSecret) error {
	cfg, _, err := scim.PostWriteConfig(ctx, r.services.SCIMConfig, client.ID, &client)
	if err != nil {
		return err
	}
	return config.ValidateRuntimeSCIMConfig(ctx, cfg, r.authz)
}

func (r *SCIMRuntime) publishLocked(ctx context.Context) error {
	cfg, hasRuntimeClients, err := scim.ResolveRuntimeConfig(ctx, r.services.SCIMConfig, func(secret coredata.SCIMClientSecret) (string, error) {
		return r.decrypt(string(secret.Ciphertext))
	})
	if err != nil {
		return err
	}
	source := "runtime"
	if !hasRuntimeClients {
		cfg = r.fallback
		source = "config"
	}
	service, err := scim.NewService(r.db, r.authz, r.baseURL, cfg)
	if err != nil {
		return err
	}
	r.runtime.Apply(service, cfg)
	r.source = source
	if r.gateway != nil {
		r.gateway.SetScimManagedGroupIDs(r.managedGroupIDs(cfg))
	}
	return nil
}

func (r *SCIMRuntime) applyCurrentSnapshot(ctx context.Context) {
	if r == nil || r.services == nil || r.services.SCIMConfig == nil || r.decrypt == nil {
		return
	}
	_ = r.publishLocked(ctx)
}

// pollSharedStore converges this replica when another replica writes the
// shared SCIM stores. Polling is intentionally simple and bounded.
func (r *SCIMRuntime) pollSharedStore() {
	if r == nil || r.services == nil || r.services.SCIMConfig == nil {
		return
	}
	ticker := time.NewTicker(scimRuntimePollInterval)
	defer ticker.Stop()
	var lastFingerprint [32]byte
	for range ticker.C {
		fingerprintCtx, cancelFingerprint := context.WithTimeout(context.Background(), time.Second)
		fp, err := r.storeFingerprint(fingerprintCtx)
		cancelFingerprint()
		if err != nil || fp == lastFingerprint {
			continue
		}
		r.mu.Lock()
		publishCtx, cancelPublish := context.WithTimeout(context.Background(), 2*time.Second)
		publishErr := r.publishLocked(publishCtx)
		cancelPublish()
		if publishErr == nil {
			lastFingerprint = fp
		}
		r.mu.Unlock()
	}
}

func (r *SCIMRuntime) storeFingerprint(ctx context.Context) ([32]byte, error) {
	clients, err := r.services.SCIMConfig.List(ctx)
	if err != nil {
		return [32]byte{}, err
	}
	hash := sha256.New()
	for _, client := range clients {
		_, _ = fmt.Fprintf(hash, "%s\x00%d\x00t", client.ID, client.Revision)
		secrets, err := r.services.SCIMConfig.Secrets(ctx, client.ID)
		if err != nil {
			return [32]byte{}, err
		}
		for i := range secrets {
			_, _ = fmt.Fprintf(hash, "%s\x00%s\x00%d", client.ID, secrets[i].CredentialID, secrets[i].Revision)
		}
	}
	var out [32]byte
	copy(out[:], hash.Sum(nil))
	return out, nil
}

func (r *SCIMRuntime) ensurePropagationSupported(ctx context.Context) error {
	// Runtime SCIM writes require a single replica until a shared-store
	// notification mechanism exists. Polling converges reads, but writes must
	// not claim immediate global application.
	if r.writesEnabled {
		return nil
	}
	return ErrSCIMPropagationUnsupported
}

func (r *SCIMRuntime) managedGroupIDs(cfg config.ServerSCIMConfig) map[string]struct{} {
	ids := config.ManagedGroupIDs(cfg)
	for id := range r.platformManagedGroupIDs {
		ids[id] = struct{}{}
	}
	return ids
}
