package coredata

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
)

const (
	appInstallEventMetaRegistry           = "registry"
	appInstallEventMetaMaterializedPath   = "materialized_path"
	appInstallEventMetaVersionConstraint  = "version_constraint"
	appInstallEventMetaSourceRef          = "source_ref"
	appInstallEventMetaProviderReleaseURL = "provider_release_url"
	appInstallEventMetaArtifactChecksums  = "artifact_checksums"
	appInstallEventMetaInstalledAt        = "installed_at"
)

// PromotedInstallationMetadata builds the metadata payload for a promoted event.
func PromotedInstallationMetadata(installation *core.AppInstallation, materializedPath string) map[string]any {
	if installation == nil {
		return nil
	}
	metadata := map[string]any{
		appInstallEventMetaRegistry:           strings.TrimSpace(installation.Registry),
		appInstallEventMetaMaterializedPath:   strings.TrimSpace(materializedPath),
		appInstallEventMetaVersionConstraint:  strings.TrimSpace(installation.VersionConstraint),
		appInstallEventMetaSourceRef:          strings.TrimSpace(installation.SourceRef),
		appInstallEventMetaProviderReleaseURL: strings.TrimSpace(installation.ProviderReleaseURL),
	}
	if len(installation.ArtifactChecksums) > 0 {
		metadata[appInstallEventMetaArtifactChecksums] = installation.ArtifactChecksums
	}
	if !installation.InstalledAt.IsZero() {
		metadata[appInstallEventMetaInstalledAt] = installation.InstalledAt.UTC().Format(time.RFC3339Nano)
	}
	return metadata
}

// HeadInstallation returns the latest promoted install state for an app, projected
// from app_installation_events.
func (s *AppInstallationEventService) HeadInstallation(ctx context.Context, appName string) (*core.AppInstallation, error) {
	if s == nil {
		return nil, fmt.Errorf("head app installation: event service is not configured")
	}
	events, err := s.ListEventsByApp(ctx, appName)
	if err != nil {
		return nil, err
	}
	head := headInstallationFromEvents(events)
	if head == nil {
		return nil, core.ErrNotFound
	}
	return head, nil
}

// HeadEvent returns the latest head-changing event for an app.
func (s *AppInstallationEventService) HeadEvent(ctx context.Context, appName string) (*core.AppInstallationEvent, error) {
	if s == nil {
		return nil, fmt.Errorf("head app installation event: event service is not configured")
	}
	events, err := s.ListEventsByApp(ctx, appName)
	if err != nil {
		return nil, err
	}
	head := headEventFromEvents(events)
	if head == nil {
		return nil, core.ErrNotFound
	}
	return head, nil
}

// ListPromotionHistory returns promoted install records for an app in timestamp order.
func (s *AppInstallationEventService) ListPromotionHistory(ctx context.Context, appName string) ([]*core.AppInstallation, error) {
	if s == nil {
		return nil, fmt.Errorf("list promotion history: event service is not configured")
	}
	events, err := s.ListEventsByApp(ctx, appName)
	if err != nil {
		return nil, err
	}
	out := make([]*core.AppInstallation, 0)
	for _, event := range events {
		if event == nil || strings.TrimSpace(event.Type) != core.AppInstallationEventTypePromoted {
			continue
		}
		out = append(out, installationFromPromotedEvent(event))
	}
	return out, nil
}

// ListHeadInstallations returns the latest promoted install per app, projected from events.
func (s *AppInstallationEventService) ListHeadInstallations(ctx context.Context) ([]*core.AppInstallation, error) {
	if s == nil {
		return nil, fmt.Errorf("list head app installations: event service is not configured")
	}
	recs, err := s.store.GetAll(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("list head app installations: %w", err)
	}
	apps := make(map[string]struct{})
	for _, rec := range recs {
		appName := strings.TrimSpace(recString(rec, "installation_id"))
		if appName != "" {
			apps[appName] = struct{}{}
		}
	}
	out := make([]*core.AppInstallation, 0, len(apps))
	for appName := range apps {
		head, err := s.HeadInstallation(ctx, appName)
		if errors.Is(err, core.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, head)
	}
	return out, nil
}

func headInstallationFromEvents(events []*core.AppInstallationEvent) *core.AppInstallation {
	headEvent := headEventFromEvents(events)
	if headEvent == nil {
		return nil
	}
	return installationFromHeadEvent(eventsByID(events), headEvent)
}

func headEventFromEvents(events []*core.AppInstallationEvent) *core.AppInstallationEvent {
	var head *core.AppInstallationEvent
	for _, event := range events {
		if event == nil {
			continue
		}
		switch strings.TrimSpace(event.Type) {
		case core.AppInstallationEventTypePromoted, core.AppInstallationEventTypeRollback:
			head = event
		case core.AppInstallationEventTypeUninstallRequested:
			head = nil
		}
	}
	return head
}

func eventsByID(events []*core.AppInstallationEvent) map[string]*core.AppInstallationEvent {
	byID := make(map[string]*core.AppInstallationEvent, len(events))
	for _, event := range events {
		if event == nil {
			continue
		}
		id := strings.TrimSpace(event.ID)
		if id == "" {
			continue
		}
		byID[id] = event
	}
	return byID
}

func installationFromHeadEvent(eventsByID map[string]*core.AppInstallationEvent, headEvent *core.AppInstallationEvent) *core.AppInstallation {
	if headEvent == nil {
		return nil
	}
	switch strings.TrimSpace(headEvent.Type) {
	case core.AppInstallationEventTypePromoted:
		return installationFromPromotedEvent(headEvent)
	case core.AppInstallationEventTypeRollback:
		return installationFromRollbackEvent(eventsByID, headEvent)
	default:
		return nil
	}
}

func installationFromRollbackEvent(eventsByID map[string]*core.AppInstallationEvent, rollback *core.AppInstallationEvent) *core.AppInstallation {
	if rollback == nil {
		return nil
	}
	targetVersion := strings.TrimSpace(rollback.FromVersion)
	for current := eventsByID[strings.TrimSpace(rollback.SupersedesEventID)]; current != nil; current = eventsByID[strings.TrimSpace(current.SupersedesEventID)] {
		if strings.TrimSpace(current.Type) != core.AppInstallationEventTypePromoted {
			continue
		}
		if strings.TrimSpace(current.ToVersion) != targetVersion {
			continue
		}
		installation := installationFromPromotedEvent(current)
		if installation == nil {
			return nil
		}
		installation.PreviousResolvedVersion = strings.TrimSpace(rollback.ToVersion)
		activeSince := rollback.Timestamp.UTC().Truncate(time.Millisecond)
		installation.ActiveSince = &activeSince
		installation.UpdatedAt = activeSince
		return installation
	}
	return nil
}

func installationFromPromotedEvent(event *core.AppInstallationEvent) *core.AppInstallation {
	if event == nil {
		return nil
	}
	activeSince := event.Timestamp.UTC().Truncate(time.Millisecond)
	installation := &core.AppInstallation{
		AppName:                 strings.TrimSpace(event.InstallationID),
		VersionConstraint:       strings.TrimSpace(event.ToVersion),
		ResolvedVersion:         strings.TrimSpace(event.ToVersion),
		PreviousResolvedVersion: strings.TrimSpace(event.FromVersion),
		RolloutStatus:           core.AppInstallationRolloutStatusPromoted,
		ActiveSince:             &activeSince,
		InstalledBy:             strings.TrimSpace(event.Actor),
		UpdatedAt:               activeSince,
	}
	if metadata := event.Metadata; metadata != nil {
		if v := strings.TrimSpace(stringMeta(metadata, appInstallEventMetaVersionConstraint)); v != "" {
			installation.VersionConstraint = v
		}
		installation.Registry = stringMeta(metadata, appInstallEventMetaRegistry)
		installation.SourceRef = stringMeta(metadata, appInstallEventMetaSourceRef)
		installation.ProviderReleaseURL = stringMeta(metadata, appInstallEventMetaProviderReleaseURL)
		installation.ArtifactChecksums = stringMapMeta(metadata, appInstallEventMetaArtifactChecksums)
		if installedAt, ok := timeMeta(metadata, appInstallEventMetaInstalledAt); ok {
			installation.InstalledAt = installedAt
		}
	}
	if installation.InstalledAt.IsZero() {
		installation.InstalledAt = activeSince
	}
	return installation
}

// InstallationFromPromotedEvent projects one promoted event into an AppInstallation.
func InstallationFromPromotedEvent(event *core.AppInstallationEvent) *core.AppInstallation {
	return installationFromPromotedEvent(event)
}

func stringMeta(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func stringMapMeta(metadata map[string]any, key string) map[string]string {
	if metadata == nil {
		return nil
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case map[string]string:
		if len(typed) == 0 {
			return nil
		}
		out := make(map[string]string, len(typed))
		for k, v := range typed {
			out[k] = v
		}
		return out
	case map[string]any:
		if len(typed) == 0 {
			return nil
		}
		out := make(map[string]string, len(typed))
		for k, v := range typed {
			out[k] = fmt.Sprint(v)
		}
		return out
	default:
		return nil
	}
}

func timeMeta(metadata map[string]any, key string) (time.Time, bool) {
	raw := stringMeta(metadata, key)
	if raw == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return time.Time{}, false
		}
	}
	return parsed.UTC().Truncate(time.Millisecond), true
}
