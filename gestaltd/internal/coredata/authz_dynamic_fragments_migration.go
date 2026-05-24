package coredata

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	legacyAuthorizationFragmentOwnerKindPlugin = "plugin"
	legacyAuthorizationFragmentScopePlugin     = "plugin"
	legacyProviderResourceTypePluginDynamic    = "plugin_dynamic"
	providerResourceTypeAppDynamic             = "app_dynamic"
)

var legacyAuthorizationDynamicFragmentsPluginSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_owner", KeyPath: []string{"owner_kind", "owner_id"}, Unique: true},
		{Name: "by_scope", KeyPath: []string{"scope"}},
		{Name: "by_plugin", KeyPath: []string{"plugin"}},
		{Name: "by_status", KeyPath: []string{"status"}},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "owner_kind", Type: indexeddb.TypeString, NotNull: true},
		{Name: "owner_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "scope", Type: indexeddb.TypeString, NotNull: true},
		{Name: "plugin", Type: indexeddb.TypeString},
		{Name: "version", Type: indexeddb.TypeInt},
		{Name: "status", Type: indexeddb.TypeString},
		{Name: "resource_types_json", Type: indexeddb.TypeString},
		{Name: "relationships_json", Type: indexeddb.TypeString},
		{Name: "audit_json", Type: indexeddb.TypeString},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}

func ensureAuthorizationDynamicFragmentsStore(ctx context.Context, ds indexeddb.IndexedDB) error {
	if err := ds.CreateObjectStore(ctx, StoreAuthorizationDynamicFragments, AuthorizationDynamicFragmentsSchema); err != nil {
		if status.Code(err) != codes.FailedPrecondition {
			return err
		}
		if legacyErr := ds.CreateObjectStore(ctx, StoreAuthorizationDynamicFragments, legacyAuthorizationDynamicFragmentsPluginSchema); legacyErr != nil {
			return err
		}
		if err := migrateAuthorizationDynamicFragmentsPluginSchema(ctx, ds); err != nil {
			return fmt.Errorf("migrate authz_dynamic_fragments plugin schema: %w", err)
		}
	}
	return nil
}

func migrateAuthorizationDynamicFragmentsPluginSchema(ctx context.Context, ds indexeddb.IndexedDB) error {
	store := ds.ObjectStore(StoreAuthorizationDynamicFragments)
	records, err := store.GetAll(ctx, nil)
	if err != nil {
		return fmt.Errorf("read legacy records: %w", err)
	}
	migrated := make([]indexeddb.Record, 0, len(records))
	for _, record := range records {
		next, err := migrateAuthorizationDynamicFragmentPluginRecord(record)
		if err != nil {
			return err
		}
		migrated = append(migrated, next)
	}

	if err := ds.DeleteObjectStore(ctx, StoreAuthorizationDynamicFragments); err != nil {
		return fmt.Errorf("delete legacy store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreAuthorizationDynamicFragments, AuthorizationDynamicFragmentsSchema); err != nil {
		return fmt.Errorf("create app schema store: %w", err)
	}
	store = ds.ObjectStore(StoreAuthorizationDynamicFragments)
	for _, record := range migrated {
		if err := store.Add(ctx, record); err != nil {
			return fmt.Errorf("write migrated record %q: %w", recString(record, "id"), err)
		}
	}
	return nil
}

func migrateAuthorizationDynamicFragmentPluginRecord(record indexeddb.Record) (indexeddb.Record, error) {
	next := make(indexeddb.Record, len(record)+1)
	for key, value := range record {
		next[key] = value
	}

	ownerKind := recString(next, "owner_kind")
	ownerID := recString(next, "owner_id")
	plugin := recString(next, "plugin")
	if plugin == "" && ownerKind == legacyAuthorizationFragmentOwnerKindPlugin {
		plugin = ownerID
	}
	if ownerKind == legacyAuthorizationFragmentOwnerKindPlugin {
		next["owner_kind"] = AuthorizationFragmentOwnerKindApp
	}
	if recString(next, "scope") == legacyAuthorizationFragmentScopePlugin {
		next["scope"] = AuthorizationFragmentScopeApp
	}
	if plugin != "" {
		next["app"] = plugin
	}
	if id := recString(next, "id"); strings.HasPrefix(id, "plugin/") {
		next["id"] = "app/" + strings.TrimPrefix(id, "plugin/")
	} else if ownerKind == legacyAuthorizationFragmentOwnerKindPlugin && ownerID != "" {
		next["id"] = "app/" + ownerID
	}
	delete(next, "plugin")

	resourceTypesJSON, err := migrateAuthorizationFragmentResourceTypesJSON(recString(next, "resource_types_json"))
	if err != nil {
		return nil, err
	}
	next["resource_types_json"] = resourceTypesJSON
	relationshipsJSON, err := migrateAuthorizationFragmentRelationshipsJSON(recString(next, "relationships_json"))
	if err != nil {
		return nil, err
	}
	next["relationships_json"] = relationshipsJSON
	return next, nil
}

func migrateAuthorizationFragmentResourceTypesJSON(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return raw, nil
	}
	var resourceTypes map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &resourceTypes); err != nil {
		return "", fmt.Errorf("decode legacy resource types: %w", err)
	}
	if value, ok := resourceTypes[legacyProviderResourceTypePluginDynamic]; ok {
		delete(resourceTypes, legacyProviderResourceTypePluginDynamic)
		resourceTypes[providerResourceTypeAppDynamic] = value
	}
	data, err := json.Marshal(resourceTypes)
	if err != nil {
		return "", fmt.Errorf("encode migrated resource types: %w", err)
	}
	return string(data), nil
}

func migrateAuthorizationFragmentRelationshipsJSON(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return raw, nil
	}
	var relationships []AuthorizationDynamicFragmentRelationship
	if err := json.Unmarshal([]byte(raw), &relationships); err != nil {
		return "", fmt.Errorf("decode legacy relationships: %w", err)
	}
	for i := range relationships {
		migrateAuthorizationFragmentRelationshipResource(&relationships[i].Resource)
		migrateAuthorizationFragmentRelationshipTarget(&relationships[i].Target)
	}
	data, err := json.Marshal(relationships)
	if err != nil {
		return "", fmt.Errorf("encode migrated relationships: %w", err)
	}
	return string(data), nil
}

func migrateAuthorizationFragmentRelationshipTarget(target *AuthorizationDynamicFragmentTarget) {
	if target == nil {
		return
	}
	if target.Resource != nil {
		migrateAuthorizationFragmentRelationshipResource(target.Resource)
	}
	if target.SubjectSet != nil {
		migrateAuthorizationFragmentRelationshipResource(&target.SubjectSet.Resource)
	}
}

func migrateAuthorizationFragmentRelationshipResource(resource *AuthorizationDynamicFragmentResource) {
	if resource != nil && resource.Type == legacyProviderResourceTypePluginDynamic {
		resource.Type = providerResourceTypeAppDynamic
	}
}
