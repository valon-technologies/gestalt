package coredata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type AppAllowedOperationsService struct {
	store idb.ObjectStore
}

func NewAppAllowedOperationsService(ds indexeddb.IndexedDB) *AppAllowedOperationsService {
	return &AppAllowedOperationsService{store: ds.ObjectStore(StoreAppAllowedOperations)}
}

// operationPermissions is the persisted wire shape. Runtime policy cannot
// replace aliases, schemas, GraphQL documents, or any other provider metadata.
type operationPermissions struct {
	AllowedRoles []string `json:"allowedRoles"`
}

func (s *AppAllowedOperationsService) GetAppOperationPolicy(ctx context.Context, app string) (core.AppOperationPolicy, error) {
	record, err := s.store.Get(ctx, app)
	if errors.Is(err, idb.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get app operation policy: %w", err)
	}
	// Keep the existing storage format so deployments need no data migration.
	var operations map[string]operationPermissions
	if err := json.Unmarshal([]byte(recString(record, "operations_json")), &operations); err != nil {
		return nil, fmt.Errorf("decode operation permissions: %w", err)
	}
	var removed []string
	if err := json.Unmarshal([]byte(recString(record, "removed_json")), &removed); err != nil {
		return nil, fmt.Errorf("decode removed operations: %w", err)
	}
	policy := make(core.AppOperationPolicy, len(operations)+len(removed))
	for _, id := range removed {
		policy[id] = nil
	}
	// Existing records used operations-over-removed precedence.
	for id, permissions := range operations {
		policy[id] = permissions.AllowedRoles
	}
	return policy, nil
}

// Patch applies only supplied permission changes. An empty patch is a no-op,
// not a reset of other administrators' previously saved choices.
func (s *AppAllowedOperationsService) Patch(ctx context.Context, app string, patch core.AppOperationPolicy) error {
	if len(patch) == 0 {
		return nil
	}
	policy, err := s.GetAppOperationPolicy(ctx, app)
	if err != nil {
		return err
	}
	if policy == nil {
		policy = make(core.AppOperationPolicy)
	}
	maps.Copy(policy, patch)
	operations := make(map[string]operationPermissions)
	removed := make([]string, 0)
	for id, roles := range policy {
		if len(roles) == 0 {
			removed = append(removed, id)
		} else {
			operations[id] = operationPermissions{AllowedRoles: roles}
		}
	}
	slices.Sort(removed)
	operationsJSON, err := json.Marshal(operations)
	if err != nil {
		return err
	}
	removedJSON, err := json.Marshal(removed)
	if err != nil {
		return err
	}
	return s.store.Put(ctx, idb.Record{
		"id": app, "app": app,
		"operations_json": string(operationsJSON),
		"removed_json":    string(removedJSON),
		"updated_at":      time.Now().UTC().Truncate(time.Millisecond),
	})
}
