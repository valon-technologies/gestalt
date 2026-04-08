package metricutil

import (
	"context"
	"errors"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
)

type namedDatastore interface {
	Name() string
}

type instrumentedDatastore struct {
	inner    core.Datastore
	provider string
}

func WrapDatastore(ds core.Datastore) core.Datastore {
	if ds == nil {
		return nil
	}
	if wrapped, ok := ds.(*instrumentedDatastore); ok {
		return wrapped
	}
	return &instrumentedDatastore{
		inner:    ds,
		provider: datastoreProviderName(ds),
	}
}

func (d *instrumentedDatastore) Name() string {
	return d.provider
}

func (d *instrumentedDatastore) GetUser(ctx context.Context, id string) (_ *core.User, err error) {
	startedAt := time.Now()
	defer func() { recordDatastoreMetrics(ctx, startedAt, d.provider, "get_user", err) }()
	return d.inner.GetUser(ctx, id)
}

func (d *instrumentedDatastore) FindOrCreateUser(ctx context.Context, email string) (_ *core.User, err error) {
	startedAt := time.Now()
	defer func() { recordDatastoreMetrics(ctx, startedAt, d.provider, "find_or_create_user", err) }()
	return d.inner.FindOrCreateUser(ctx, email)
}

func (d *instrumentedDatastore) StoreToken(ctx context.Context, token *core.IntegrationToken) (err error) {
	startedAt := time.Now()
	defer func() { recordDatastoreMetrics(ctx, startedAt, d.provider, "store_token", err) }()
	return d.inner.StoreToken(ctx, token)
}

func (d *instrumentedDatastore) Token(ctx context.Context, userID, integration, connection, instance string) (_ *core.IntegrationToken, err error) {
	startedAt := time.Now()
	defer func() { recordDatastoreMetrics(ctx, startedAt, d.provider, "token", err) }()
	return d.inner.Token(ctx, userID, integration, connection, instance)
}

func (d *instrumentedDatastore) ListTokens(ctx context.Context, userID string) (_ []*core.IntegrationToken, err error) {
	startedAt := time.Now()
	defer func() { recordDatastoreMetrics(ctx, startedAt, d.provider, "list_tokens", err) }()
	return d.inner.ListTokens(ctx, userID)
}

func (d *instrumentedDatastore) ListTokensForIntegration(ctx context.Context, userID, integration string) (_ []*core.IntegrationToken, err error) {
	startedAt := time.Now()
	defer func() { recordDatastoreMetrics(ctx, startedAt, d.provider, "list_tokens_for_integration", err) }()
	return d.inner.ListTokensForIntegration(ctx, userID, integration)
}

func (d *instrumentedDatastore) ListTokensForConnection(ctx context.Context, userID, integration, connection string) (_ []*core.IntegrationToken, err error) {
	startedAt := time.Now()
	defer func() { recordDatastoreMetrics(ctx, startedAt, d.provider, "list_tokens_for_connection", err) }()
	return d.inner.ListTokensForConnection(ctx, userID, integration, connection)
}

func (d *instrumentedDatastore) DeleteToken(ctx context.Context, id string) (err error) {
	startedAt := time.Now()
	defer func() { recordDatastoreMetrics(ctx, startedAt, d.provider, "delete_token", err) }()
	return d.inner.DeleteToken(ctx, id)
}

func (d *instrumentedDatastore) StoreAPIToken(ctx context.Context, token *core.APIToken) (err error) {
	startedAt := time.Now()
	defer func() { recordDatastoreMetrics(ctx, startedAt, d.provider, "store_api_token", err) }()
	return d.inner.StoreAPIToken(ctx, token)
}

func (d *instrumentedDatastore) ValidateAPIToken(ctx context.Context, hashedToken string) (_ *core.APIToken, err error) {
	startedAt := time.Now()
	defer func() { recordDatastoreMetrics(ctx, startedAt, d.provider, "validate_api_token", err) }()
	return d.inner.ValidateAPIToken(ctx, hashedToken)
}

func (d *instrumentedDatastore) ListAPITokens(ctx context.Context, userID string) (_ []*core.APIToken, err error) {
	startedAt := time.Now()
	defer func() { recordDatastoreMetrics(ctx, startedAt, d.provider, "list_api_tokens", err) }()
	return d.inner.ListAPITokens(ctx, userID)
}

func (d *instrumentedDatastore) RevokeAPIToken(ctx context.Context, userID, id string) (err error) {
	startedAt := time.Now()
	defer func() { recordDatastoreMetrics(ctx, startedAt, d.provider, "revoke_api_token", err) }()
	return d.inner.RevokeAPIToken(ctx, userID, id)
}

func (d *instrumentedDatastore) RevokeAllAPITokens(ctx context.Context, userID string) (_ int64, err error) {
	startedAt := time.Now()
	defer func() { recordDatastoreMetrics(ctx, startedAt, d.provider, "revoke_all_api_tokens", err) }()
	return d.inner.RevokeAllAPITokens(ctx, userID)
}

func (d *instrumentedDatastore) Ping(ctx context.Context) (err error) {
	startedAt := time.Now()
	defer func() { recordDatastoreMetrics(ctx, startedAt, d.provider, "ping", err) }()
	return d.inner.Ping(ctx)
}

func (d *instrumentedDatastore) Migrate(ctx context.Context) (err error) {
	startedAt := time.Now()
	defer func() { recordDatastoreMetrics(ctx, startedAt, d.provider, "migrate", err) }()
	return d.inner.Migrate(ctx)
}

func (d *instrumentedDatastore) Close() error {
	return d.inner.Close()
}

func datastoreProviderName(ds core.Datastore) string {
	if ds == nil {
		return UnknownAttrValue
	}
	if named, ok := ds.(namedDatastore); ok {
		return AttrValue(named.Name())
	}
	return UnknownAttrValue
}

func recordDatastoreMetrics(ctx context.Context, startedAt time.Time, provider string, method string, err error) {
	RecordDatastoreMetrics(ctx, startedAt, provider, method, err != nil && !errors.Is(err, core.ErrNotFound))
}
