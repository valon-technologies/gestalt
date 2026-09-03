package scim

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

// migrateLegacyState is deliberately one-shot and rerunnable. It copies
// committed, nondeleted resources and accepted create-only resources; pending
// updates/deletes are intentionally discarded because the provider is
// canonical after the cutover.
func (s *CompactService) migrateLegacyState(ctx context.Context) error {
	for _, name := range []string{coredata.StoreSCIMUsers, coredata.StoreSCIMGroups} {
		if _, err := s.db.ObjectStore(name).GetAll(ctx, nil); err != nil && !errors.Is(err, idb.ErrNotFound) {
			return fmt.Errorf("read legacy %s: %w", name, err)
		}
	}
	if err := s.migrateLegacyUsers(ctx); err != nil {
		return err
	}
	if err := s.migrateLegacyGroups(ctx); err != nil {
		return err
	}
	if err := s.verifyLegacyCopied(ctx); err != nil {
		return err
	}
	if err := s.verifyCompactState(ctx); err != nil {
		return err
	}
	for _, name := range []string{coredata.StoreSCIMProjectionIntents, coredata.StoreSCIMUsers, coredata.StoreSCIMGroups} {
		if err := s.db.DeleteObjectStore(ctx, name); err != nil && !errors.Is(err, idb.ErrNotFound) {
			return fmt.Errorf("drop legacy %s: %w", name, err)
		}
	}
	return nil
}

func (s *CompactService) migrateLegacyUsers(ctx context.Context) error {
	rows, err := s.db.ObjectStore(coredata.StoreSCIMUsers).GetAll(ctx, nil)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil
		}
		return err
	}
	for _, row := range rows {
		if recordBool(row, "deleted") {
			continue
		}
		if recordInt(row, "version") <= 0 {
			continue
		}
		var u persistedUser
		if err := decodeJSONValue(row["resource"], &u); err != nil {
			return fmt.Errorf("decode legacy user: %w", err)
		}
		if recordString(row, "id") == "" || recordString(row, "client_id") == "" || recordString(row, "core_user_id") == "" || strings.TrimSpace(u.UserName) == "" {
			return fmt.Errorf("legacy SCIM user is missing required identity fields")
		}
		intentResource := idb.Record{}
		for key, value := range row {
			intentResource[key] = value
		}
		if userID := recordString(row, "user_id"); userID != "" {
			intentResource["id"] = userID
		}
		if err := s.putMigratedUser(ctx, intentResource, u); err != nil {
			return err
		}
	}
	// Create-only intents have no committed row but contain enough information
	// to preserve the resource that the old service had accepted.
	intents, err := s.db.ObjectStore(coredata.StoreSCIMProjectionIntents).GetAll(ctx, nil)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil
		}
		return err
	}
	for _, row := range intents {
		if recordBool(row, "proposed_deleted") || recordInt(row, "base_version") != 0 {
			continue
		}
		var u persistedUser
		if err := decodeJSONValue(row["proposed"], &u); err != nil {
			return fmt.Errorf("decode legacy create-only user: %w", err)
		}
		if strings.TrimSpace(u.UserName) == "" {
			return fmt.Errorf("legacy create-only user is missing userName")
		}
		if recordString(row, "id") == "" && recordString(row, "user_id") == "" || recordString(row, "client_id") == "" || recordString(row, "core_user_id") == "" {
			return fmt.Errorf("legacy create-only user is missing required identity fields")
		}
		if err := s.putMigratedUser(ctx, row, u); err != nil {
			return err
		}
	}
	return nil
}
func (s *CompactService) putMigratedUser(ctx context.Context, row idb.Record, u persistedUser) error {
	cid := recordString(row, "client_id")
	if cid == "" {
		return nil
	}
	id := recordString(row, "user_id")
	if id == "" {
		id = recordString(row, "id")
	}
	if id == "" {
		return nil
	}
	coreID := recordString(row, "core_user_id")
	if coreID == "" {
		return nil
	}
	created, updated := recordTime(row, "created_at"), recordTime(row, "updated_at")
	if created.IsZero() {
		created = time.Now().UTC()
	}
	if updated.IsZero() {
		updated = created
	}
	r := storedResource{ID: id, ClientID: cid, ResourceType: "User", ExternalID: u.ExternalID, CoreUserID: coreID, UserName: normalize(u.UserName), Profile: userProfile{Name: u.Name, Emails: u.Emails}, CreatedAt: created, UpdatedAt: updated}
	record, err := resourceRecord(r)
	if err != nil {
		return fmt.Errorf("encode migrated User: %w", err)
	}
	return s.db.ObjectStore(coredata.StoreSCIMResources).Put(ctx, record)
}
func (s *CompactService) migrateLegacyGroups(ctx context.Context) error {
	rows, err := s.db.ObjectStore(coredata.StoreSCIMGroups).GetAll(ctx, nil)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil
		}
		return err
	}
	for _, row := range rows {
		if recordBool(row, "deleted") {
			continue
		}
		version := recordInt(row, "version")
		if version <= 0 && (recordBool(row, "pending_deleted") || row["pending_resource"] == nil) {
			continue
		}
		var g persistedGroup
		value := row["resource"]
		if version <= 0 {
			value = row["pending_resource"]
		}
		if err := decodeJSONValue(value, &g); err != nil {
			return fmt.Errorf("decode legacy Group: %w", err)
		}
		if strings.TrimSpace(g.DisplayName) == "" {
			return fmt.Errorf("legacy Group is missing displayName")
		}
		id, cid := recordString(row, "id"), recordString(row, "client_id")
		if id == "" || cid == "" {
			return fmt.Errorf("legacy Group is missing identity fields")
		}
		created, updated := recordTime(row, "created_at"), recordTime(row, "updated_at")
		if created.IsZero() {
			created = time.Now().UTC()
		}
		if updated.IsZero() {
			updated = created
		}
		r := storedResource{ID: id, ClientID: cid, ResourceType: "Group", ExternalID: g.ExternalID, DisplayName: g.DisplayName, CreatedAt: created, UpdatedAt: updated}
		record, err := resourceRecord(r)
		if err != nil {
			return fmt.Errorf("encode migrated Group: %w", err)
		}
		if err := s.db.ObjectStore(coredata.StoreSCIMResources).Put(ctx, record); err != nil {
			return err
		}
	}
	return nil
}

func (s *CompactService) verifyLegacyCopied(ctx context.Context) error {
	verifyUser := func(row idb.Record, resource any) error {
		id := recordString(row, "user_id")
		if id == "" {
			id = recordString(row, "id")
		}
		actual, err := s.db.ObjectStore(coredata.StoreSCIMResources).Get(ctx, id)
		if err != nil {
			return fmt.Errorf("legacy User %q was not migrated: %w", id, err)
		}
		var expected persistedUser
		if err := decodeJSONValue(resource, &expected); err != nil {
			return fmt.Errorf("decode legacy User during verification: %w", err)
		}
		if recordString(actual, "client_id") != recordString(row, "client_id") || recordString(actual, "resource_type") != "User" || recordString(actual, "core_user_id") != recordString(row, "core_user_id") || recordString(actual, "user_name") != normalize(expected.UserName) || recordString(actual, "external_id") != expected.ExternalID {
			return fmt.Errorf("legacy User %q migrated with incorrect identity or attributes", id)
		}
		var profile userProfile
		profileValue := actual["profile"]
		profilePresent := profileValue != nil
		if raw, ok := profileValue.([]byte); ok {
			profilePresent = len(bytes.TrimSpace(raw)) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
		}
		if raw, ok := profileValue.(json.RawMessage); ok {
			profilePresent = len(bytes.TrimSpace(raw)) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
		}
		if profilePresent {
			if err := decodeJSONValue(profileValue, &profile); err != nil || profile.Name != expected.Name || !reflect.DeepEqual(profile.Emails, expected.Emails) {
				return fmt.Errorf("legacy User %q migrated with incorrect profile", id)
			}
		} else if expected.Name != (Name{}) || len(expected.Emails) > 0 {
			return fmt.Errorf("legacy User %q migrated with incorrect profile", id)
		}
		return verifyMigrationTimes(actual, row, "User", id)
	}
	verifyGroup := func(row idb.Record, resource any) error {
		id := recordString(row, "id")
		actual, err := s.db.ObjectStore(coredata.StoreSCIMResources).Get(ctx, id)
		if err != nil {
			return fmt.Errorf("legacy Group %q was not migrated: %w", id, err)
		}
		var expected persistedGroup
		if err := decodeJSONValue(resource, &expected); err != nil {
			return fmt.Errorf("decode legacy Group during verification: %w", err)
		}
		if recordString(actual, "client_id") != recordString(row, "client_id") || recordString(actual, "resource_type") != "Group" || recordString(actual, "external_id") != expected.ExternalID || recordString(actual, "display_name") != expected.DisplayName {
			return fmt.Errorf("legacy Group %q migrated with incorrect identity or attributes", id)
		}
		return verifyMigrationTimes(actual, row, "Group", id)
	}
	users, err := s.db.ObjectStore(coredata.StoreSCIMUsers).GetAll(ctx, nil)
	if err == nil {
		for _, row := range users {
			if recordBool(row, "deleted") || recordInt(row, "version") <= 0 {
				continue
			}
			if err := verifyUser(row, row["resource"]); err != nil {
				return err
			}
		}
	} else if !errors.Is(err, idb.ErrNotFound) {
		return err
	}
	intents, err := s.db.ObjectStore(coredata.StoreSCIMProjectionIntents).GetAll(ctx, nil)
	if err == nil {
		for _, row := range intents {
			if recordBool(row, "proposed_deleted") || recordInt(row, "base_version") != 0 {
				continue
			}
			if err := verifyUser(row, row["proposed"]); err != nil {
				return err
			}
		}
	} else if !errors.Is(err, idb.ErrNotFound) {
		return err
	}
	groups, err := s.db.ObjectStore(coredata.StoreSCIMGroups).GetAll(ctx, nil)
	if err == nil {
		for _, row := range groups {
			if recordBool(row, "deleted") {
				continue
			}
			if recordInt(row, "version") <= 0 && (recordBool(row, "pending_deleted") || row["pending_resource"] == nil) {
				continue
			}
			resource := row["resource"]
			if recordInt(row, "version") <= 0 {
				resource = row["pending_resource"]
			}
			if err := verifyGroup(row, resource); err != nil {
				return err
			}
		}
	} else if !errors.Is(err, idb.ErrNotFound) {
		return err
	}
	return nil
}
func (s *CompactService) verifyCompactState(ctx context.Context) error {
	rows, err := s.db.ObjectStore(coredata.StoreSCIMResources).GetAll(ctx, nil)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if recordString(r, "id") == "" || recordString(r, "client_id") == "" || recordString(r, "resource_type") == "" {
			return fmt.Errorf("invalid migrated SCIM resource")
		}
	}
	return nil
}

func verifyMigrationTimes(actual, legacy idb.Record, typ, id string) error {
	for _, field := range []string{"created_at", "updated_at"} {
		want := recordTime(legacy, field)
		if !want.IsZero() && !recordTime(actual, field).Equal(want) {
			return fmt.Errorf("legacy %s %q migrated with incorrect %s", typ, id, field)
		}
	}
	return nil
}
