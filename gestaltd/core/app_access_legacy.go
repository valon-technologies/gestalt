package core

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core/catalog"
)

// OperationHistory reports which operations an app exposed to users at a past
// moment. It is what lets a legacy allow-list be read exactly: an operation
// missing from the list was either switched off by the user or added after
// the list was saved, and only history can tell the two apart.
type OperationHistory interface {
	// OperationsAt returns the operation ids the app had at the given time.
	// known is false when history is unavailable for the app, for example an
	// app that is not sourced from the registry; callers must then keep the
	// legacy allow-list authoritative rather than guess.
	OperationsAt(ctx context.Context, app string, at time.Time) (ids []string, known bool, err error)
}

// ConvertLegacyAppAccessProfile rewrites a legacy allow-list as decisions
// relative to the app's defaults, preserving every choice the user made.
//
// existedAt is the set of operations the app had when the list was saved. A
// default-on operation absent from the list that existed then was switched
// off by the user and becomes an opt-out. One that did not exist then was
// added later and follows its default, which is the point of the conversion.
//
// The synthetic graphql capability is in neither the catalog nor history, so
// it is opted out whenever the list did not enable it.
//
// Decisions about ids outside the current catalog are carried over so they
// survive an operation that later returns.
func ConvertLegacyAppAccessProfile(profile *AppAccessProfile, cat *catalog.Catalog, defaults AppAccessDefaults, existedAt []string) *AppAccessProfile {
	if profile == nil || !profile.Legacy || cat == nil {
		return profile
	}
	return convertLegacy(profile, cat, defaults, trimmedSet(profile.LegacyEnabledOperations), trimmedSet(existedAt))
}

func convertLegacy(profile *AppAccessProfile, cat *catalog.Catalog, defaults AppAccessDefaults, enabled, existed map[string]struct{}) *AppAccessProfile {
	inCatalog := make(map[string]catalog.CatalogOperation, len(cat.Operations))
	ids := make(map[string]struct{}, len(cat.Operations)+len(enabled)+len(existed)+1)
	for i := range cat.Operations {
		id := strings.TrimSpace(cat.Operations[i].ID)
		inCatalog[id] = cat.Operations[i]
		ids[id] = struct{}{}
	}
	for id := range enabled {
		ids[id] = struct{}{}
	}
	for id := range existed {
		ids[id] = struct{}{}
	}
	ids[GraphQLCapabilityID] = struct{}{}

	var disabled, extra []string
	for id := range ids {
		_, isEnabled := enabled[id]
		_, didExist := existed[id]
		switch classifyLegacyOperation(id, inCatalog, defaults, isEnabled, didExist) {
		case legacyOptOut:
			disabled = append(disabled, id)
		case legacyOptIn:
			extra = append(extra, id)
		}
	}
	converted := *profile
	converted.Legacy = false
	converted.LegacyEnabledOperations = nil
	converted.DisabledOperations = sortedUnique(disabled)
	converted.ExtraOperations = sortedUnique(extra)
	return &converted
}

type legacyDecision int

const (
	legacyFollowsDefault legacyDecision = iota
	legacyOptOut
	legacyOptIn
)

// classifyLegacyOperation decides what a legacy list said about one id. Ids
// outside the catalog have no provable default, so any choice is carried over.
// The synthetic graphql capability is in neither catalog nor history and is
// opted out whenever the list did not enable it.
func classifyLegacyOperation(id string, inCatalog map[string]catalog.CatalogOperation, defaults AppAccessDefaults, enabled, existed bool) legacyDecision {
	op, listed := inCatalog[id]
	switch {
	case !listed && enabled:
		return legacyOptIn
	case !listed && (existed || id == GraphQLCapabilityID):
		return legacyOptOut
	case !listed:
		return legacyFollowsDefault
	case defaults.Includes(op) && !enabled && existed:
		return legacyOptOut
	case !defaults.Includes(op) && enabled:
		return legacyOptIn
	}
	return legacyFollowsDefault
}

// LegacyKeptReason says why ResolveLegacyAppAccessProfile returned a legacy
// profile unconverted, so callers can log or count it.
type LegacyKeptReason string

const (
	// LegacyConverted means the profile was converted; nothing was kept.
	LegacyConverted LegacyKeptReason = ""
	// LegacyKeptNotLegacy means there was nothing to convert.
	LegacyKeptNotLegacy LegacyKeptReason = "not_legacy"
	// LegacyKeptNoCatalog means the app's catalog was unavailable.
	LegacyKeptNoCatalog LegacyKeptReason = "no_catalog"
	// LegacyKeptNoHistory means no history source exists for the app.
	LegacyKeptNoHistory LegacyKeptReason = "no_history"
	// LegacyKeptHistoryError means the history lookup failed.
	LegacyKeptHistoryError LegacyKeptReason = "history_error"
	// LegacyKeptHistoryIncomplete means history lacks an operation the list
	// enabled and the catalog still has, so it cannot describe the save time.
	LegacyKeptHistoryIncomplete LegacyKeptReason = "history_incomplete"
)

// ResolveLegacyAppAccessProfile is the read-time view of a legacy profile.
// The conversion is intentionally never persisted: the store has no
// compare-and-swap, so writing it back could overwrite a concurrent user save.
// The stored allow-list stays until the user saves.
//
// The profile is returned unchanged, with the reason and any history error for
// the caller to log, whenever the conversion cannot be done exactly.
func ResolveLegacyAppAccessProfile(ctx context.Context, history OperationHistory, profile *AppAccessProfile, cat *catalog.Catalog, defaults AppAccessDefaults) (*AppAccessProfile, LegacyKeptReason, error) {
	switch {
	case profile == nil || !profile.Legacy:
		return profile, LegacyKeptNotLegacy, nil
	case cat == nil:
		return profile, LegacyKeptNoCatalog, nil
	case history == nil:
		return profile, LegacyKeptNoHistory, nil
	}
	existedAt, known, err := history.OperationsAt(ctx, profile.App, profile.UpdatedAt)
	if err != nil {
		return profile, LegacyKeptHistoryError, err
	}
	if !known {
		return profile, LegacyKeptNoHistory, nil
	}
	enabled, existed := trimmedSet(profile.LegacyEnabledOperations), trimmedSet(existedAt)
	if !historyCoversEnabled(cat, enabled, existed) {
		return profile, LegacyKeptHistoryIncomplete, nil
	}
	return convertLegacy(profile, cat, defaults, enabled, existed), LegacyConverted, nil
}

func historyCoversEnabled(cat *catalog.Catalog, enabled, existed map[string]struct{}) bool {
	for i := range cat.Operations {
		id := strings.TrimSpace(cat.Operations[i].ID)
		if _, isEnabled := enabled[id]; !isEnabled {
			continue
		}
		if _, ok := existed[id]; !ok {
			return false
		}
	}
	return true
}

func trimmedSet(ids []string) map[string]struct{} {
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			set[id] = struct{}{}
		}
	}
	return set
}

func sortedUnique(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}
