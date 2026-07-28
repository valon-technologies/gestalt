package coredata

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

const (
	DefaultAppVersionChangeRequestPageLimit = 50
	MaxAppVersionChangeRequestPageLimit     = 100
)

type AppVersionChangeRequestPage struct {
	Requests   []*core.AppVersionChangeRequest
	NextCursor string
}

type AppVersionChangeRequestService struct {
	store idb.ObjectStore
}

func NewAppVersionChangeRequestService(ds indexeddb.IndexedDB) *AppVersionChangeRequestService {
	return &AppVersionChangeRequestService{store: ds.ObjectStore(StoreAppVersionChangeRequests)}
}

func (s *AppVersionChangeRequestService) AppendRequest(ctx context.Context, request *core.AppVersionChangeRequest) (*core.AppVersionChangeRequest, error) {
	if request == nil {
		return nil, fmt.Errorf("append app version change request: request is required")
	}
	appName := strings.TrimSpace(request.App)
	if appName == "" {
		return nil, fmt.Errorf("append app version change request: app is required")
	}
	fromVersion := strings.TrimSpace(request.FromVersion)
	if fromVersion == "" {
		return nil, fmt.Errorf("append app version change request: from_version is required")
	}
	toVersion := strings.TrimSpace(request.ToVersion)
	if toVersion == "" {
		return nil, fmt.Errorf("append app version change request: to_version is required")
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	id := strings.TrimSpace(request.ID)
	if id == "" {
		id = uuid.NewString()
	}
	timestamp := request.Timestamp
	if timestamp.IsZero() {
		timestamp = now
	} else {
		timestamp = timestamp.UTC().Truncate(time.Millisecond)
	}
	rec := idb.Record{
		"id":            id,
		"app":           appName,
		"from_version":  fromVersion,
		"to_version":    toVersion,
		"actor":         strings.TrimSpace(request.Actor),
		"timestamp":     timestamp,
		"metadata_json": jsonValue(request.Metadata),
	}
	if request.FromVersionDeployableUntil != nil && !request.FromVersionDeployableUntil.IsZero() {
		rec["from_version_deployable_until"] = request.FromVersionDeployableUntil.UTC().Truncate(time.Millisecond)
	}
	if err := s.store.Add(ctx, rec); err != nil {
		return nil, fmt.Errorf("append app version change request: %w", err)
	}
	return recordToAppVersionChangeRequest(rec), nil
}

var indexedDBMaxTime = time.Date(2262, 1, 1, 0, 0, 0, 0, time.UTC)

func (s *AppVersionChangeRequestService) ListRequestsByApp(ctx context.Context, appName string) ([]*core.AppVersionChangeRequest, error) {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return nil, fmt.Errorf("list app version change requests: app is required")
	}
	query := idb.Bound(
		[]any{appName, time.Time{}},
		[]any{appName, indexedDBMaxTime},
		false,
		false,
	)
	recs, err := s.store.Index("by_app_timestamp").GetAll(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list app version change requests: %w", err)
	}
	out := make([]*core.AppVersionChangeRequest, 0, len(recs))
	for _, rec := range recs {
		out = append(out, recordToAppVersionChangeRequest(rec))
	}
	return out, nil
}

func (s *AppVersionChangeRequestService) ListRequestsByAppPage(ctx context.Context, appName string, limit int, cursor string) (*AppVersionChangeRequestPage, error) {
	requests, err := s.ListRequestsByApp(ctx, appName)
	if err != nil {
		return nil, err
	}
	sortChangeRequestsDesc(requests)

	cursorTime, cursorID, err := parseAppVersionChangeRequestCursor(cursor)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = DefaultAppVersionChangeRequestPageLimit
	}
	if limit > MaxAppVersionChangeRequestPageLimit {
		limit = MaxAppVersionChangeRequestPageLimit
	}

	filtered := make([]*core.AppVersionChangeRequest, 0, len(requests))
	for _, request := range requests {
		if request == nil {
			continue
		}
		if !cursorTime.IsZero() && !changeRequestBeforeCursor(request, cursorTime, cursorID) {
			continue
		}
		filtered = append(filtered, request)
	}

	page := &AppVersionChangeRequestPage{
		Requests: filtered,
	}
	if len(filtered) > limit {
		last := filtered[limit-1]
		page.Requests = filtered[:limit]
		page.NextCursor = formatAppVersionChangeRequestCursor(last.Timestamp, last.ID)
	}
	return page, nil
}

func (s *AppVersionChangeRequestService) LatestDesiredRevisionID(ctx context.Context, appName, desiredVersion string) (string, error) {
	requests, err := s.ListRequestsByApp(ctx, appName)
	if err != nil {
		return "", err
	}
	return latestDesiredRevisionID(requests, desiredVersion), nil
}

func (s *AppVersionChangeRequestService) LatestRevisionIDForVersion(ctx context.Context, appName, version string) (string, error) {
	requests, err := s.ListRequestsByApp(ctx, appName)
	if err != nil {
		return "", err
	}
	return latestDesiredRevisionID(requests, version), nil
}

func latestDesiredRevisionID(requests []*core.AppVersionChangeRequest, desiredVersion string) string {
	desiredVersion = strings.TrimSpace(desiredVersion)
	if desiredVersion == "" {
		return ""
	}
	var latestID string
	var latestTime time.Time
	for _, request := range requests {
		if request == nil || strings.TrimSpace(request.ToVersion) != desiredVersion {
			continue
		}
		if latestID == "" || request.Timestamp.After(latestTime) ||
			(request.Timestamp.Equal(latestTime) && request.ID > latestID) {
			latestID = request.ID
			latestTime = request.Timestamp
		}
	}
	return latestID
}

func sortChangeRequestsDesc(requests []*core.AppVersionChangeRequest) {
	sort.Slice(requests, func(i, j int) bool {
		left := requests[i]
		right := requests[j]
		if left == nil {
			return false
		}
		if right == nil {
			return true
		}
		if left.Timestamp.Equal(right.Timestamp) {
			return left.ID > right.ID
		}
		return left.Timestamp.After(right.Timestamp)
	})
}

func parseAppVersionChangeRequestCursor(cursor string) (time.Time, string, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return time.Time{}, "", nil
	}
	separator := strings.LastIndex(cursor, ":")
	if separator <= 0 {
		return time.Time{}, "", fmt.Errorf("invalid revision history cursor")
	}
	timestamp, err := time.Parse(time.RFC3339, cursor[:separator])
	if err != nil {
		return time.Time{}, "", fmt.Errorf("invalid revision history cursor: %w", err)
	}
	id := strings.TrimSpace(cursor[separator+1:])
	if id == "" {
		return time.Time{}, "", fmt.Errorf("invalid revision history cursor")
	}
	return timestamp.UTC(), id, nil
}

func formatAppVersionChangeRequestCursor(timestamp time.Time, id string) string {
	return timestamp.UTC().Format(time.RFC3339) + ":" + strings.TrimSpace(id)
}

func changeRequestBeforeCursor(request *core.AppVersionChangeRequest, cursorTime time.Time, cursorID string) bool {
	if request.Timestamp.After(cursorTime) {
		return false
	}
	if request.Timestamp.Before(cursorTime) {
		return true
	}
	return request.ID < cursorID
}

func (s *AppVersionChangeRequestService) HasKnownVersion(ctx context.Context, appName, version string) (bool, error) {
	appName = strings.TrimSpace(appName)
	version = strings.TrimSpace(version)
	if appName == "" || version == "" {
		return false, fmt.Errorf("has known app version: app and version are required")
	}
	query := idb.Bound(
		[]any{appName, version},
		[]any{appName, version},
		false,
		false,
	)
	recs, err := s.store.Index("by_app_to_version").GetAll(ctx, query)
	if err != nil {
		return false, fmt.Errorf("has known app version: %w", err)
	}
	return len(recs) > 0, nil
}

func recordToAppVersionChangeRequest(rec idb.Record) *core.AppVersionChangeRequest {
	var deployableUntil *time.Time
	if value := recTime(rec, "from_version_deployable_until"); !value.IsZero() {
		deployableUntil = &value
	}
	return &core.AppVersionChangeRequest{
		ID:                         recString(rec, "id"),
		App:                        recString(rec, "app"),
		FromVersion:                recString(rec, "from_version"),
		ToVersion:                  recString(rec, "to_version"),
		Actor:                      recString(rec, "actor"),
		Timestamp:                  recTime(rec, "timestamp"),
		FromVersionDeployableUntil: deployableUntil,
		Metadata:                   recAnyMap(rec, "metadata_json"),
	}
}
