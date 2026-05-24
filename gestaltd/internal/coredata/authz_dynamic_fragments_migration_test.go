package coredata

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type strictSchemaIndexedDB struct {
	inner   indexeddb.IndexedDB
	schemas map[string]indexeddb.ObjectStoreSchema
}

func newStrictSchemaIndexedDB(inner indexeddb.IndexedDB) *strictSchemaIndexedDB {
	return &strictSchemaIndexedDB{
		inner:   inner,
		schemas: map[string]indexeddb.ObjectStoreSchema{},
	}
}

func (d *strictSchemaIndexedDB) ObjectStore(name string) indexeddb.ObjectStore {
	return d.inner.ObjectStore(name)
}

func (d *strictSchemaIndexedDB) Transaction(ctx context.Context, stores []string, mode indexeddb.TransactionMode, opts indexeddb.TransactionOptions) (indexeddb.Transaction, error) {
	return d.inner.Transaction(ctx, stores, mode, opts)
}

func (d *strictSchemaIndexedDB) CreateObjectStore(ctx context.Context, name string, schema indexeddb.ObjectStoreSchema) error {
	if existing, ok := d.schemas[name]; ok {
		if reflect.DeepEqual(existing, schema) {
			return nil
		}
		return status.Errorf(codes.FailedPrecondition, "object store %q schema does not match", name)
	}
	d.schemas[name] = schema
	return d.inner.CreateObjectStore(ctx, name, schema)
}

func (d *strictSchemaIndexedDB) DeleteObjectStore(ctx context.Context, name string) error {
	delete(d.schemas, name)
	return d.inner.DeleteObjectStore(ctx, name)
}

func (d *strictSchemaIndexedDB) Ping(ctx context.Context) error {
	return d.inner.Ping(ctx)
}

func (d *strictSchemaIndexedDB) Close() error {
	return d.inner.Close()
}

func (d *strictSchemaIndexedDB) schema(name string) indexeddb.ObjectStoreSchema {
	return d.schemas[name]
}

func TestNewMigratesLegacyAuthorizationDynamicFragmentsPluginSchema(t *testing.T) {
	ctx := context.Background()
	db := newStrictSchemaIndexedDB(&coretesting.StubIndexedDB{})
	if err := db.CreateObjectStore(ctx, StoreAuthorizationDynamicFragments, legacyAuthorizationDynamicFragmentsPluginSchema); err != nil {
		t.Fatalf("CreateObjectStore(legacy): %v", err)
	}

	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	relationshipsJSON, err := json.Marshal([]AuthorizationDynamicFragmentRelationship{{
		Subject:  AuthorizationDynamicFragmentSubject{Type: "subject", ID: "user:alice"},
		Relation: "admin",
		Resource: AuthorizationDynamicFragmentResource{Type: legacyProviderResourceTypePluginDynamic, ID: "github"},
		Target: AuthorizationDynamicFragmentTarget{
			Resource: &AuthorizationDynamicFragmentResource{Type: legacyProviderResourceTypePluginDynamic, ID: "github"},
		},
	}})
	if err != nil {
		t.Fatalf("Marshal relationships: %v", err)
	}
	if err := db.ObjectStore(StoreAuthorizationDynamicFragments).Add(ctx, indexeddb.Record{
		"id":                  "plugin/github",
		"owner_kind":          legacyAuthorizationFragmentOwnerKindPlugin,
		"owner_id":            "github",
		"scope":               legacyAuthorizationFragmentScopePlugin,
		"plugin":              "github",
		"version":             int64(2),
		"status":              AuthorizationFragmentStatusActive,
		"resource_types_json": `{"plugin_dynamic":{"relations":{"admin":{"subjectTypes":["subject"]}}}}`,
		"relationships_json":  string(relationshipsJSON),
		"audit_json":          `{"reason":"legacy"}`,
		"created_at":          now,
		"updated_at":          now,
	}); err != nil {
		t.Fatalf("Add legacy fragment: %v", err)
	}

	svc, err := NewWithContext(ctx, db)
	if err != nil {
		t.Fatalf("NewWithContext: %v", err)
	}
	if got := db.schema(StoreAuthorizationDynamicFragments); !reflect.DeepEqual(got, AuthorizationDynamicFragmentsSchema) {
		t.Fatalf("authz_dynamic_fragments schema was not migrated")
	}

	fragment, err := svc.AuthzFragments.GetFragmentByOwner(ctx, AuthorizationAppFragmentOwner("github"))
	if err != nil {
		t.Fatalf("GetFragmentByOwner(app github): %v", err)
	}
	if fragment.ID != "app/github" || fragment.Owner.Kind != AuthorizationFragmentOwnerKindApp || fragment.Owner.App != "github" || fragment.App != "github" || fragment.Scope != AuthorizationFragmentScopeApp {
		t.Fatalf("migrated fragment = %+v", fragment)
	}
	if len(fragment.Relationships) != 1 {
		t.Fatalf("relationships len = %d, want 1", len(fragment.Relationships))
	}
	if got := fragment.Relationships[0].Resource.Type; got != providerResourceTypeAppDynamic {
		t.Fatalf("relationship resource type = %q, want %q", got, providerResourceTypeAppDynamic)
	}
	if fragment.Relationships[0].Target.Resource == nil || fragment.Relationships[0].Target.Resource.Type != providerResourceTypeAppDynamic {
		t.Fatalf("relationship target resource = %+v", fragment.Relationships[0].Target.Resource)
	}
	if _, ok := fragment.ResourceTypes[providerResourceTypeAppDynamic]; !ok {
		t.Fatalf("migrated resource types missing %q: %+v", providerResourceTypeAppDynamic, fragment.ResourceTypes)
	}
	if _, ok := fragment.ResourceTypes[legacyProviderResourceTypePluginDynamic]; ok {
		t.Fatalf("migrated resource types still contain %q", legacyProviderResourceTypePluginDynamic)
	}

	record, err := svc.DB.ObjectStore(StoreAuthorizationDynamicFragments).Get(ctx, "app/github")
	if err != nil {
		t.Fatalf("Get(app/github): %v", err)
	}
	if _, ok := record["plugin"]; ok {
		t.Fatalf("migrated record still has legacy plugin field: %+v", record)
	}
	if got := record["app"]; got != "github" {
		t.Fatalf("record app = %v, want github", got)
	}
	if _, err := svc.DB.ObjectStore(StoreAuthorizationDynamicFragments).Get(ctx, "plugin/github"); !errors.Is(err, indexeddb.ErrNotFound) {
		t.Fatalf("Get(plugin/github) error = %v, want ErrNotFound", err)
	}
}
