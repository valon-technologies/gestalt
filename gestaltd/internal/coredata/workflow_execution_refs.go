package coredata

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
)

type WorkflowExecutionRefService struct {
	store indexeddb.ObjectStore
}

func NewWorkflowExecutionRefService(ds indexeddb.IndexedDB) *WorkflowExecutionRefService {
	return &WorkflowExecutionRefService{store: ds.ObjectStore(StoreWorkflowExecutionRefs)}
}

func (s *WorkflowExecutionRefService) Put(ctx context.Context, ref *coreworkflow.ExecutionReference) (*coreworkflow.ExecutionReference, error) {
	if ref == nil {
		return nil, fmt.Errorf("put workflow execution ref: ref is required")
	}

	id := strings.TrimSpace(ref.ID)
	providerName := strings.TrimSpace(ref.ProviderName)
	targetPlugin := strings.TrimSpace(ref.Target.PluginName)
	targetOperation := strings.TrimSpace(ref.Target.Operation)
	subjectID := strings.TrimSpace(ref.SubjectID)
	if id == "" || providerName == "" || targetPlugin == "" || targetOperation == "" || subjectID == "" {
		return nil, fmt.Errorf("put workflow execution ref: id, provider_name, target.plugin_name, target.operation, and subject_id are required")
	}

	permissionsJSON := ""
	if len(ref.Permissions) > 0 {
		raw, err := json.Marshal(ref.Permissions)
		if err != nil {
			return nil, fmt.Errorf("put workflow execution ref: marshal permissions: %w", err)
		}
		permissionsJSON = string(raw)
	}

	now := time.Now()
	createdAt := ref.CreatedAt
	if existing, err := s.store.Get(ctx, id); err == nil {
		if created := recTime(existing, "created_at"); !created.IsZero() {
			createdAt = &created
		}
	} else if err != indexeddb.ErrNotFound {
		return nil, fmt.Errorf("put workflow execution ref: %w", err)
	}
	if createdAt == nil || createdAt.IsZero() {
		createdAt = &now
	}

	rec := indexeddb.Record{
		"id":                id,
		"provider_name":     providerName,
		"target_plugin":     targetPlugin,
		"target_operation":  targetOperation,
		"target_connection": strings.TrimSpace(ref.Target.Connection),
		"target_instance":   strings.TrimSpace(ref.Target.Instance),
		"subject_id":        subjectID,
		"permissions_json":  permissionsJSON,
		"created_at":        *createdAt,
		"revoked_at":        timeOrNil(ref.RevokedAt),
	}
	if err := s.store.Put(ctx, rec); err != nil {
		return nil, fmt.Errorf("put workflow execution ref: %w", err)
	}
	return recordToWorkflowExecutionRef(rec), nil
}

func (s *WorkflowExecutionRefService) Get(ctx context.Context, id string) (*coreworkflow.ExecutionReference, error) {
	rec, err := s.store.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return nil, indexeddb.ErrNotFound
		}
		return nil, fmt.Errorf("get workflow execution ref: %w", err)
	}
	return recordToWorkflowExecutionRef(rec), nil
}

func (s *WorkflowExecutionRefService) ListBySubject(ctx context.Context, subjectID string) ([]*coreworkflow.ExecutionReference, error) {
	recs, err := s.store.Index("by_subject").GetAll(ctx, nil, strings.TrimSpace(subjectID))
	if err != nil {
		return nil, fmt.Errorf("list workflow execution refs by subject: %w", err)
	}
	refs := make([]*coreworkflow.ExecutionReference, 0, len(recs))
	for _, rec := range recs {
		refs = append(refs, recordToWorkflowExecutionRef(rec))
	}
	return refs, nil
}

func recordToWorkflowExecutionRef(rec indexeddb.Record) *coreworkflow.ExecutionReference {
	return &coreworkflow.ExecutionReference{
		ID:           recString(rec, "id"),
		ProviderName: recString(rec, "provider_name"),
		Target: coreworkflow.Target{
			PluginName: recString(rec, "target_plugin"),
			Operation:  recString(rec, "target_operation"),
			Connection: recString(rec, "target_connection"),
			Instance:   recString(rec, "target_instance"),
		},
		SubjectID:   recString(rec, "subject_id"),
		Permissions: recWorkflowExecutionRefPermissions(rec),
		CreatedAt:   recTimePtr(rec, "created_at"),
		RevokedAt:   recTimePtr(rec, "revoked_at"),
	}
}

func recWorkflowExecutionRefPermissions(rec indexeddb.Record) []core.AccessPermission {
	raw := recString(rec, "permissions_json")
	if raw == "" {
		return nil
	}
	var permissions []core.AccessPermission
	if err := json.Unmarshal([]byte(raw), &permissions); err != nil {
		return nil
	}
	return permissions
}

func timeOrNil(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return *value
}
