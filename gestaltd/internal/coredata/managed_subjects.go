package coredata

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

const ManagedSubjectKindServiceAccount = "service_account"

type ManagedSubjectService struct {
	store indexeddb.ObjectStore
}

func NewManagedSubjectService(ds indexeddb.IndexedDB) *ManagedSubjectService {
	return &ManagedSubjectService{store: ds.ObjectStore(StoreManagedSubjects)}
}

func (s *ManagedSubjectService) CreateManagedSubject(ctx context.Context, subject *core.ManagedSubject) (*core.ManagedSubject, error) {
	if subject == nil {
		return nil, fmt.Errorf("create managed subject: subject is required")
	}
	subjectID := strings.TrimSpace(subject.SubjectID)
	if subjectID == "" {
		return nil, fmt.Errorf("create managed subject: subject_id is required")
	}
	kind := strings.TrimSpace(subject.Kind)
	if kind == "" {
		kind, _, _ = core.ParseSubjectID(subjectID)
	}
	if kind != ManagedSubjectKindServiceAccount {
		return nil, fmt.Errorf("create managed subject: unsupported kind %q", kind)
	}
	if parsedKind, _, ok := core.ParseSubjectID(subjectID); !ok || parsedKind != kind {
		return nil, fmt.Errorf("create managed subject: subject_id must be a canonical %s subject ID", ManagedSubjectKindServiceAccount)
	}

	now := time.Now().UTC().Truncate(time.Second)
	rec := indexeddb.Record{
		"id":                    subjectID,
		"subject_id":            subjectID,
		"kind":                  kind,
		"display_name":          strings.TrimSpace(subject.DisplayName),
		"description":           strings.TrimSpace(subject.Description),
		"credential_subject_id": subjectID,
		"created_by_subject_id": strings.TrimSpace(subject.CreatedBySubjectID),
		"deleted":               false,
		"created_at":            now,
		"updated_at":            now,
	}
	if err := s.store.Add(ctx, rec); err != nil {
		if errors.Is(err, indexeddb.ErrAlreadyExists) {
			return nil, core.ErrAlreadyRegistered
		}
		return nil, fmt.Errorf("create managed subject: %w", err)
	}
	return recordToManagedSubject(rec), nil
}

func (s *ManagedSubjectService) GetManagedSubject(ctx context.Context, subjectID string) (*core.ManagedSubject, error) {
	rec, err := s.store.Get(ctx, strings.TrimSpace(subjectID))
	if err != nil {
		if errors.Is(err, indexeddb.ErrNotFound) {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get managed subject: %w", err)
	}
	subject := recordToManagedSubject(rec)
	if subject.DeletedAt != nil {
		return nil, core.ErrNotFound
	}
	return subject, nil
}

func (s *ManagedSubjectService) ListManagedSubjects(ctx context.Context, kind string) ([]*core.ManagedSubject, error) {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = ManagedSubjectKindServiceAccount
	}
	if kind != ManagedSubjectKindServiceAccount {
		return nil, fmt.Errorf("list managed subjects: unsupported kind %q", kind)
	}
	recs, err := s.store.Index("by_kind_deleted").GetAll(ctx, nil, kind, false)
	if err != nil {
		return nil, fmt.Errorf("list managed subjects: %w", err)
	}
	out := make([]*core.ManagedSubject, 0, len(recs))
	for _, rec := range recs {
		out = append(out, recordToManagedSubject(rec))
	}
	return out, nil
}

func (s *ManagedSubjectService) UpdateManagedSubject(ctx context.Context, subjectID string, update func(*core.ManagedSubject) error) (*core.ManagedSubject, error) {
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		return nil, fmt.Errorf("update managed subject: subject_id is required")
	}
	if update == nil {
		return nil, fmt.Errorf("update managed subject: update is required")
	}
	subject, err := s.GetManagedSubject(ctx, subjectID)
	if err != nil {
		return nil, err
	}
	if err := update(subject); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Second)
	rec := managedSubjectToRecord(subject)
	rec["updated_at"] = now
	rec["deleted"] = subject.DeletedAt != nil
	if err := s.store.Put(ctx, rec); err != nil {
		return nil, fmt.Errorf("update managed subject: %w", err)
	}
	return recordToManagedSubject(rec), nil
}

func (s *ManagedSubjectService) DeleteManagedSubject(ctx context.Context, subjectID string) (*core.ManagedSubject, error) {
	subjectID = strings.TrimSpace(subjectID)
	rec, err := s.store.Get(ctx, subjectID)
	if err != nil {
		if errors.Is(err, indexeddb.ErrNotFound) {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("delete managed subject: %w", err)
	}
	subject := recordToManagedSubject(rec)
	if subject.DeletedAt != nil {
		return subject, nil
	}
	now := time.Now().UTC().Truncate(time.Second)
	subject.DeletedAt = &now
	subject.UpdatedAt = now
	rec = managedSubjectToRecord(subject)
	if err := s.store.Put(ctx, rec); err != nil {
		return nil, fmt.Errorf("delete managed subject: %w", err)
	}
	return recordToManagedSubject(rec), nil
}

func (s *ManagedSubjectService) RemoveManagedSubjectForRollback(ctx context.Context, subjectID string) error {
	if err := s.store.Delete(ctx, strings.TrimSpace(subjectID)); err != nil {
		return fmt.Errorf("rollback managed subject: %w", err)
	}
	return nil
}

func recordToManagedSubject(rec indexeddb.Record) *core.ManagedSubject {
	return &core.ManagedSubject{
		SubjectID:           recString(rec, "subject_id"),
		Kind:                recString(rec, "kind"),
		DisplayName:         recString(rec, "display_name"),
		Description:         recString(rec, "description"),
		CredentialSubjectID: recString(rec, "credential_subject_id"),
		CreatedBySubjectID:  recString(rec, "created_by_subject_id"),
		CreatedAt:           recTime(rec, "created_at"),
		UpdatedAt:           recTime(rec, "updated_at"),
		DeletedAt:           recTimePtr(rec, "deleted_at"),
	}
}

func managedSubjectToRecord(subject *core.ManagedSubject) indexeddb.Record {
	return indexeddb.Record{
		"id":                    strings.TrimSpace(subject.SubjectID),
		"subject_id":            strings.TrimSpace(subject.SubjectID),
		"kind":                  strings.TrimSpace(subject.Kind),
		"display_name":          strings.TrimSpace(subject.DisplayName),
		"description":           strings.TrimSpace(subject.Description),
		"credential_subject_id": strings.TrimSpace(subject.CredentialSubjectID),
		"created_by_subject_id": strings.TrimSpace(subject.CreatedBySubjectID),
		"deleted":               subject.DeletedAt != nil,
		"created_at":            subject.CreatedAt,
		"updated_at":            subject.UpdatedAt,
		"deleted_at":            subject.DeletedAt,
	}
}
