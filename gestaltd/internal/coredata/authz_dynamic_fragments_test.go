package coredata_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

func TestAuthorizationDynamicFragmentServiceUpsertsOwnerRecord(t *testing.T) {
	t.Parallel()

	svc, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	ctx := context.Background()
	relationship := coredata.AuthorizationDynamicFragmentRelationship{
		Subject:  coredata.AuthorizationDynamicFragmentSubject{Type: "subject", ID: "user:alice"},
		Relation: "viewer",
		Resource: coredata.AuthorizationDynamicFragmentResource{Type: "app_dynamic", ID: "github"},
	}

	fragment, err := svc.AuthzFragments.UpsertRelationship(ctx, coredata.AuthorizationAppFragmentOwner("github"), relationship, coredata.AuthorizationDynamicFragmentAuditMetadata{Reason: "test"})
	if err != nil {
		t.Fatalf("UpsertRelationship: %v", err)
	}
	if fragment.ID != "app/github" || fragment.Owner.Kind != coredata.AuthorizationFragmentOwnerKindApp || fragment.Owner.App != "github" {
		t.Fatalf("fragment owner = %#v id=%q, want app/github", fragment.Owner, fragment.ID)
	}
	if fragment.Version != 1 {
		t.Fatalf("version = %d, want 1", fragment.Version)
	}

	relationship.Relation = "admin"
	updated, err := svc.AuthzFragments.ReplaceSubjectResourceRelationships(ctx, coredata.AuthorizationAppFragmentOwner("github"), relationship, coredata.AuthorizationDynamicFragmentAuditMetadata{Reason: "role_change"})
	if err != nil {
		t.Fatalf("ReplaceSubjectResourceRelationships role change: %v", err)
	}
	if updated.Version != 2 {
		t.Fatalf("version after role change = %d, want 2", updated.Version)
	}
	if len(updated.Relationships) != 1 || updated.Relationships[0].Relation != "admin" {
		t.Fatalf("relationships = %#v, want only admin role", updated.Relationships)
	}

	deleted, updated, err := svc.AuthzFragments.DeleteRelationship(ctx, coredata.AuthorizationAppFragmentOwner("github"), relationship, coredata.AuthorizationDynamicFragmentAuditMetadata{Reason: "delete"})
	if err != nil {
		t.Fatalf("DeleteRelationship: %v", err)
	}
	if !deleted {
		t.Fatal("DeleteRelationship deleted = false, want true")
	}
	if updated != nil {
		t.Fatalf("fragment after deleting only relationship = %#v, want deleted fragment", updated)
	}
	if _, err := svc.AuthzFragments.GetFragmentByOwner(ctx, coredata.AuthorizationAppFragmentOwner("github")); err != core.ErrNotFound {
		t.Fatalf("GetFragmentByOwner after delete err = %v, want ErrNotFound", err)
	}
}

func TestAuthorizationDynamicFragmentServiceDeleteMissingOwnerDoesNotCreateFragment(t *testing.T) {
	t.Parallel()

	svc, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	ctx := context.Background()
	deleted, fragment, err := svc.AuthzFragments.DeleteRelationship(ctx, coredata.AuthorizationAppFragmentOwner("github"), coredata.AuthorizationDynamicFragmentRelationship{
		Subject:  coredata.AuthorizationDynamicFragmentSubject{Type: "subject", ID: "user:alice"},
		Relation: "viewer",
		Resource: coredata.AuthorizationDynamicFragmentResource{Type: "app_dynamic", ID: "github"},
	}, coredata.AuthorizationDynamicFragmentAuditMetadata{Reason: "delete_missing"})
	if err != nil {
		t.Fatalf("DeleteRelationship missing owner: %v", err)
	}
	if deleted || fragment != nil {
		t.Fatalf("DeleteRelationship missing owner = deleted %v fragment %#v, want false nil", deleted, fragment)
	}
	if _, err := svc.AuthzFragments.GetFragmentByOwner(ctx, coredata.AuthorizationAppFragmentOwner("github")); err != core.ErrNotFound {
		t.Fatalf("GetFragmentByOwner err = %v, want ErrNotFound", err)
	}
}

func TestAuthorizationDynamicFragmentServiceDeleteSubjectResourceRelationships(t *testing.T) {
	t.Parallel()

	svc, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	ctx := context.Background()
	owner := coredata.AuthorizationAppFragmentOwner("github")
	directMember := coredata.AuthorizationDynamicFragmentRelationship{
		Subject:  coredata.AuthorizationDynamicFragmentSubject{Type: "subject", ID: "service_account:bot"},
		Relation: "viewer",
		Resource: coredata.AuthorizationDynamicFragmentResource{Type: "app_dynamic", ID: "github"},
	}
	targetedMember := directMember
	targetedMember.Relation = "editor"
	targetedMember.Target.Subject = &coredata.AuthorizationDynamicFragmentSubject{Type: "subject", ID: "service_account:bot"}
	if _, err := svc.AuthzFragments.UpsertRelationship(ctx, owner, directMember, coredata.AuthorizationDynamicFragmentAuditMetadata{Reason: "direct"}); err != nil {
		t.Fatalf("UpsertRelationship direct: %v", err)
	}
	if _, err := svc.AuthzFragments.UpsertRelationship(ctx, owner, targetedMember, coredata.AuthorizationDynamicFragmentAuditMetadata{Reason: "targeted"}); err != nil {
		t.Fatalf("UpsertRelationship targeted: %v", err)
	}

	deleted, fragment, err := svc.AuthzFragments.DeleteSubjectResourceRelationships(ctx, owner, directMember.Subject, directMember.Resource, coredata.AuthorizationDynamicFragmentAuditMetadata{Reason: "delete_member"})
	if err != nil {
		t.Fatalf("DeleteSubjectResourceRelationships: %v", err)
	}
	if !deleted {
		t.Fatal("DeleteSubjectResourceRelationships deleted = false, want true")
	}
	if fragment != nil {
		t.Fatalf("fragment after deleting only member relationships = %#v, want deleted fragment", fragment)
	}
	if _, err := svc.AuthzFragments.GetFragmentByOwner(ctx, owner); err != core.ErrNotFound {
		t.Fatalf("GetFragmentByOwner after delete err = %v, want ErrNotFound", err)
	}
}

func TestAuthorizationDynamicFragmentRelationshipKeyUsesTrimmedPropertyValues(t *testing.T) {
	t.Parallel()

	svc, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	ctx := context.Background()
	relationship := coredata.AuthorizationDynamicFragmentRelationship{
		Subject:  coredata.AuthorizationDynamicFragmentSubject{Type: "subject", ID: "user:alice"},
		Relation: "viewer",
		Resource: coredata.AuthorizationDynamicFragmentResource{Type: "app_dynamic", ID: "github"},
		Properties: map[string]string{
			" source ": " provider ",
		},
	}

	if _, err := svc.AuthzFragments.UpsertRelationship(ctx, coredata.AuthorizationAppFragmentOwner("github"), relationship, coredata.AuthorizationDynamicFragmentAuditMetadata{Reason: "first"}); err != nil {
		t.Fatalf("UpsertRelationship first: %v", err)
	}
	relationship.Properties[" source "] = " write_path "
	fragment, err := svc.AuthzFragments.UpsertRelationship(ctx, coredata.AuthorizationAppFragmentOwner("github"), relationship, coredata.AuthorizationDynamicFragmentAuditMetadata{Reason: "second"})
	if err != nil {
		t.Fatalf("UpsertRelationship second: %v", err)
	}
	if len(fragment.Relationships) != 2 {
		t.Fatalf("relationships = %#v, want distinct entries for distinct trimmed property values", fragment.Relationships)
	}
}

func TestAuthorizationDynamicFragmentRelationshipKeyIncludesSubjectAndTarget(t *testing.T) {
	t.Parallel()

	svc, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	ctx := context.Background()
	relationship := coredata.AuthorizationDynamicFragmentRelationship{
		Subject:  coredata.AuthorizationDynamicFragmentSubject{Type: "subject", ID: "user:alice"},
		Relation: "viewer",
		Resource: coredata.AuthorizationDynamicFragmentResource{Type: "app/github/repository", ID: "gestalt"},
		Target: coredata.AuthorizationDynamicFragmentTarget{
			Resource: &coredata.AuthorizationDynamicFragmentResource{Type: "team", ID: "servicing"},
		},
	}

	if _, err := svc.AuthzFragments.UpsertRelationship(ctx, coredata.AuthorizationAppFragmentOwner("github"), relationship, coredata.AuthorizationDynamicFragmentAuditMetadata{Reason: "first"}); err != nil {
		t.Fatalf("UpsertRelationship first: %v", err)
	}
	relationship.Subject.ID = "user:bob"
	fragment, err := svc.AuthzFragments.UpsertRelationship(ctx, coredata.AuthorizationAppFragmentOwner("github"), relationship, coredata.AuthorizationDynamicFragmentAuditMetadata{Reason: "second"})
	if err != nil {
		t.Fatalf("UpsertRelationship second: %v", err)
	}
	if len(fragment.Relationships) != 2 {
		t.Fatalf("relationships = %#v, want distinct entries for distinct subjects with same target", fragment.Relationships)
	}

	deleted, fragment, err := svc.AuthzFragments.DeleteRelationship(ctx, coredata.AuthorizationAppFragmentOwner("github"), relationship, coredata.AuthorizationDynamicFragmentAuditMetadata{Reason: "delete_second"})
	if err != nil {
		t.Fatalf("DeleteRelationship second: %v", err)
	}
	if !deleted || len(fragment.Relationships) != 1 || fragment.Relationships[0].Subject.ID != "user:alice" {
		t.Fatalf("after delete deleted=%v relationships=%#v, want only alice relationship", deleted, fragment.Relationships)
	}
}

func TestAuthorizationDynamicFragmentServiceRejectsVersionMismatch(t *testing.T) {
	t.Parallel()

	svc, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	ctx := context.Background()
	fragment, err := svc.AuthzFragments.PutFragment(ctx, &coredata.AuthorizationDynamicFragment{
		Owner: coredata.AuthorizationGlobalFragmentOwner(),
		Relationships: []coredata.AuthorizationDynamicFragmentRelationship{{
			Subject:  coredata.AuthorizationDynamicFragmentSubject{Type: "subject", ID: "user:alice"},
			Relation: "admin",
			Resource: coredata.AuthorizationDynamicFragmentResource{Type: "admin_dynamic", ID: "global"},
		}},
	}, coredata.AuthorizationDynamicFragmentUpdate{})
	if err != nil {
		t.Fatalf("PutFragment: %v", err)
	}
	stale := fragment.Version - 1
	_, err = svc.AuthzFragments.PutFragment(ctx, fragment, coredata.AuthorizationDynamicFragmentUpdate{ExpectedVersion: &stale})
	if err == nil || !strings.Contains(err.Error(), "version mismatch") {
		t.Fatalf("PutFragment stale version error = %v, want version mismatch", err)
	}
}

func TestAuthorizationDynamicFragmentResourceTypeValidationUsesTrimmedRelationKeys(t *testing.T) {
	t.Parallel()

	svc, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	ctx := context.Background()
	_, err = svc.AuthzFragments.PutFragment(ctx, &coredata.AuthorizationDynamicFragment{
		Owner: coredata.AuthorizationGlobalFragmentOwner(),
		ResourceTypes: map[string]json.RawMessage{
			"team": json.RawMessage(`{"relations":{" viewer ":{"subjectTypes":["subject"]}},"actions":{"view":{"relations":["viewer"]}}}`),
		},
	}, coredata.AuthorizationDynamicFragmentUpdate{})
	if err != nil {
		t.Fatalf("PutFragment with trimmed action relation reference: %v", err)
	}

	_, err = svc.AuthzFragments.PutFragment(ctx, &coredata.AuthorizationDynamicFragment{
		Owner: coredata.AuthorizationGlobalFragmentOwner(),
		ResourceTypes: map[string]json.RawMessage{
			"team": json.RawMessage(`{"relations":{"viewer":{"subjectTypes":["subject"]}," viewer ":{"subjectTypes":["subject"]}}}`),
		},
	}, coredata.AuthorizationDynamicFragmentUpdate{})
	if err == nil || !strings.Contains(err.Error(), `duplicate key after trimming "viewer"`) {
		t.Fatalf("PutFragment duplicate trimmed relation key error = %v, want duplicate key error", err)
	}
}

func TestAuthorizationDynamicFragmentsStoreCreatedWithCoreData(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	svc, err := coredata.New(db)
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	if svc.AuthzFragments == nil {
		t.Fatal("AuthzFragments = nil")
	}
	if !db.HasObjectStore(coredata.StoreAuthorizationDynamicFragments) {
		t.Fatalf("missing object store %q", coredata.StoreAuthorizationDynamicFragments)
	}
	if _, err := svc.AuthzFragments.GetFragment(context.Background(), "missing"); err != core.ErrNotFound {
		t.Fatalf("GetFragment missing error = %v, want core.ErrNotFound", err)
	}
}
