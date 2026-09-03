package scim_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/scim"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

// These schemas model stores created by the pre-compaction implementation.
// They belong to this migration fixture, not to production coredata: new
// binaries create only scim_resources and discover legacy stores by name.
var legacyUsersSchema = idb.ObjectStoreOptions{
	Indexes: []idb.IndexSchema{
		{Name: "by_client", KeyPath: []string{"client_id"}},
		{Name: "by_core_user", KeyPath: []string{"core_user_id"}},
		{Name: "by_user_name_key", KeyPath: []string{"user_name_key"}, Unique: true},
		{Name: "by_external_id_key", KeyPath: []string{"external_id_key"}, Unique: true},
		{Name: "by_email_key", KeyPath: []string{"email_key"}, Unique: true},
	},
	Columns: []idb.ColumnDef{
		{Name: "id", Type: idb.TypeString, PrimaryKey: true},
		{Name: "client_id", Type: idb.TypeString, NotNull: true},
		{Name: "core_user_id", Type: idb.TypeString, NotNull: true},
		{Name: "authoritative_domain", Type: idb.TypeString},
		{Name: "user_name_key", Type: idb.TypeString},
		{Name: "external_id_key", Type: idb.TypeString},
		{Name: "email_key", Type: idb.TypeString},
		{Name: "active", Type: idb.TypeBool, NotNull: true},
		{Name: "deleted", Type: idb.TypeBool, NotNull: true},
		{Name: "version", Type: idb.TypeInt, NotNull: true},
		{Name: "resource", Type: idb.TypeJSON, NotNull: true},
		{Name: "applied_relationships", Type: idb.TypeJSON},
		{Name: "last_operation_fingerprint", Type: idb.TypeString},
		{Name: "created_at", Type: idb.TypeTime, NotNull: true},
		{Name: "updated_at", Type: idb.TypeTime, NotNull: true},
		{Name: "deleted_at", Type: idb.TypeTime},
	},
}

var legacyProjectionIntentsSchema = idb.ObjectStoreOptions{
	Indexes: []idb.IndexSchema{
		{Name: "by_user", KeyPath: []string{"user_id"}, Unique: true},
		{Name: "by_core_user", KeyPath: []string{"core_user_id"}},
		{Name: "by_client", KeyPath: []string{"client_id"}},
		{Name: "by_next_attempt", KeyPath: []string{"next_attempt_at"}},
	},
	Columns: []idb.ColumnDef{
		{Name: "id", Type: idb.TypeString, PrimaryKey: true},
		{Name: "user_id", Type: idb.TypeString, NotNull: true},
		{Name: "client_id", Type: idb.TypeString, NotNull: true},
		{Name: "core_user_id", Type: idb.TypeString, NotNull: true},
		{Name: "proposed", Type: idb.TypeJSON, NotNull: true},
		{Name: "proposed_deleted", Type: idb.TypeBool, NotNull: true},
		{Name: "base_version", Type: idb.TypeInt, NotNull: true},
		{Name: "next_version", Type: idb.TypeInt, NotNull: true},
		{Name: "created_at", Type: idb.TypeTime, NotNull: true},
		{Name: "updated_at", Type: idb.TypeTime, NotNull: true},
	},
}

var legacyGroupsSchema = idb.ObjectStoreOptions{
	Indexes: []idb.IndexSchema{{Name: "by_client", KeyPath: []string{"client_id"}}},
	Columns: []idb.ColumnDef{
		{Name: "id", Type: idb.TypeString, PrimaryKey: true},
		{Name: "client_id", Type: idb.TypeString, NotNull: true},
		{Name: "version", Type: idb.TypeInt, NotNull: true},
		{Name: "deleted", Type: idb.TypeBool, NotNull: true},
		{Name: "resource", Type: idb.TypeJSON, NotNull: true},
		{Name: "created_at", Type: idb.TypeTime, NotNull: true},
		{Name: "updated_at", Type: idb.TypeTime, NotNull: true},
	},
}

func TestSCIMMigrationPreservesCommittedAndCreateOnlyResources(t *testing.T) {
	t.Parallel()
	db := &coretesting.StubIndexedDB{}
	services, err := coredata.New(db)
	if err != nil {
		t.Fatal(err)
	}
	coreUser, err := services.Users.FindOrCreateUser(context.Background(), "migrated@valon.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, store := range []struct {
		name   string
		schema idb.ObjectStoreOptions
	}{
		{coredata.StoreSCIMUsers, legacyUsersSchema},
		{coredata.StoreSCIMProjectionIntents, legacyProjectionIntentsSchema},
		{coredata.StoreSCIMGroups, legacyGroupsSchema},
	} {
		if _, err := db.CreateObjectStore(context.Background(), store.name, store.schema); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	resource, _ := json.Marshal(map[string]any{"userName": "Migrated@Valon.com", "active": true})
	// Simulate a crash after the compact row was created but before the legacy
	// migration completed. The rerunnable migration must replace this partial
	// row with the complete legacy representation.
	if err := db.ObjectStore(coredata.StoreSCIMResources).Put(context.Background(), idb.Record{
		"id": "legacy-user", "client_id": "rippling", "resource_type": "User", "core_user_id": coreUser.ID,
		"user_name": "partial@valon.com", "created_at": now, "updated_at": now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.ObjectStore(coredata.StoreSCIMUsers).Add(context.Background(), idb.Record{
		"id": "legacy-user", "client_id": "rippling", "core_user_id": coreUser.ID, "version": int64(1), "deleted": false,
		"resource": json.RawMessage(resource), "created_at": now, "updated_at": now,
	}); err != nil {
		t.Fatal(err)
	}
	groupResource, _ := json.Marshal(map[string]any{"displayName": "Legacy Group"})
	if err := db.ObjectStore(coredata.StoreSCIMGroups).Add(context.Background(), idb.Record{
		"id": "legacy-group", "client_id": "rippling", "version": int64(0), "deleted": false,
		"resource": json.RawMessage([]byte(`{}`)), "pending_resource": json.RawMessage(groupResource), "pending_version": int64(1), "pending_deleted": false,
		"created_at": now, "updated_at": now,
	}); err != nil {
		t.Fatal(err)
	}
	createOnlyCoreUser, err := services.Users.FindOrCreateUser(context.Background(), "create-only@valon.com")
	if err != nil {
		t.Fatal(err)
	}
	createOnly, _ := json.Marshal(map[string]any{"userName": "CreateOnly@Valon.com", "active": true})
	if err := db.ObjectStore(coredata.StoreSCIMProjectionIntents).Add(context.Background(), idb.Record{
		"id": "create-only-intent", "user_id": "create-only-user", "client_id": "rippling", "core_user_id": createOnlyCoreUser.ID,
		"base_version": int64(0), "proposed_deleted": false, "proposed": json.RawMessage(createOnly),
	}); err != nil {
		t.Fatal(err)
	}

	authorization := newRecordingAuthorization()
	props, _ := structpb.NewStruct(map[string]any{"managedBy": "scim", "scimClientId": "rippling", "scimUserId": "legacy-user"})
	legacyTuple := &proto.RelationshipTuple{Target: &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{Type: "subject", Id: "user:" + coreUser.ID}}}, Relation: "member", Resource: &proto.Resource{Type: "group", Id: "legacy-group"}}
	authorization.setRelationship(&proto.Relationship{Tuple: legacyTuple, Properties: props, SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME})
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil)})
	_, _, handler := newSCIMService(t, db, authorization, cfg)
	if response := scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/legacy-user", testCurrentToken, nil); response.Code != http.StatusOK {
		t.Fatalf("migrated User GET = %d %s", response.Code, response.Body.String())
	}
	if response := scimRequest(t, handler, http.MethodGet, "/scim/v2/Groups/legacy-group", testCurrentToken, nil); response.Code != http.StatusOK {
		t.Fatalf("migrated Group GET = %d %s", response.Code, response.Body.String())
	}
	if response := scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/create-only-user", testCurrentToken, nil); response.Code != http.StatusOK {
		t.Fatalf("migrated create-only User GET = %d %s", response.Code, response.Body.String())
	}
	if relationship := authorization.relationshipForUser(coreUser.ID); relationship == nil || relationship.GetProperties() == nil {
		t.Fatalf("legacy relationship was not preserved as ordinary state = %#v", relationship)
	}
	for _, name := range []string{coredata.StoreSCIMUsers, coredata.StoreSCIMProjectionIntents, coredata.StoreSCIMGroups} {
		if db.HasObjectStore(name) {
			t.Fatalf("legacy store %q remains", name)
		}
	}
	if _, err := scim.NewService(services.DB, authorization, "https://gestalt.example", cfg); err != nil {
		t.Fatalf("rerunning completed migration: %v", err)
	}
}

func TestSCIMMigrationRejectsMalformedEligibleResourceBeforeDroppingLegacyStores(t *testing.T) {
	t.Parallel()
	db := &coretesting.StubIndexedDB{}
	services, err := coredata.New(db)
	if err != nil {
		t.Fatal(err)
	}
	coreUser, err := services.Users.FindOrCreateUser(context.Background(), "malformed@valon.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateObjectStore(context.Background(), coredata.StoreSCIMUsers, legacyUsersSchema); err != nil {
		t.Fatal(err)
	}
	if err := db.ObjectStore(coredata.StoreSCIMUsers).Add(context.Background(), idb.Record{
		"id": "malformed-user", "client_id": "rippling", "core_user_id": coreUser.ID,
		"version": int64(1), "deleted": false, "resource": json.RawMessage(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil)})
	if _, err := scim.NewService(services.DB, newRecordingAuthorization(), "https://gestalt.example", cfg); err == nil {
		t.Fatal("malformed eligible resource unexpectedly allowed startup")
	}
	if !db.HasObjectStore(coredata.StoreSCIMUsers) {
		t.Fatal("legacy store was dropped after failed verification")
	}
}
