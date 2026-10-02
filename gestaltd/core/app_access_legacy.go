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
	enabled := trimmedSet(profile.LegacyEnabledOperations)
	existed := trimmedSet(existedAt)
	inCatalog := make(map[string]struct{}, len(cat.Operations))

	converted := *profile
	converted.Legacy = false
	converted.LegacyEnabledOperations = nil
	converted.DisabledOperations = nil
	converted.ExtraOperations = nil
	for i := range cat.Operations {
		op := cat.Operations[i]
		id := strings.TrimSpace(op.ID)
		inCatalog[id] = struct{}{}
		_, isEnabled := enabled[id]
		_, didExist := existed[id]
		switch isDefault := defaults.Includes(op); {
		case isDefault && !isEnabled && didExist:
			converted.DisabledOperations = append(converted.DisabledOperations, id)
		case !isDefault && isEnabled:
			converted.ExtraOperations = append(converted.ExtraOperations, id)
		}
	}
	for id := range existed {
		if _, inList := enabled[id]; inList {
			continue
		}
		if _, known := inCatalog[id]; !known {
			converted.DisabledOperations = append(converted.DisabledOperations, id)
		}
	}
	for id := range enabled {
		if _, known := inCatalog[id]; !known {
			converted.ExtraOperations = append(converted.ExtraOperations, id)
		}
	}
	if _, wasEnabled := enabled[GraphQLCapabilityID]; !wasEnabled {
		converted.DisabledOperations = append(converted.DisabledOperations, GraphQLCapabilityID)
	}
	converted.DisabledOperations = sortedUnique(converted.DisabledOperations)
	converted.ExtraOperations = sortedUnique(converted.ExtraOperations)
	return &converted
}

// ResolveLegacyAppAccessProfile is the read-time view of a legacy profile. It
// never stores the result: the stored allow-list stays until the user saves,
// so resolving cannot race a save or write on a read path.
//
// The profile is returned unchanged, with any history error alongside it for
// the caller to log, whenever the conversion cannot be done exactly. That
// includes history that lacks an operation the list enabled and the catalog
// still has, since such history cannot describe this app at the save time.
func ResolveLegacyAppAccessProfile(ctx context.Context, history OperationHistory, profile *AppAccessProfile, cat *catalog.Catalog, defaults AppAccessDefaults) (*AppAccessProfile, error) {
	if profile == nil || !profile.Legacy || cat == nil || history == nil {
		return profile, nil
	}
	existedAt, known, err := history.OperationsAt(ctx, profile.App, profile.UpdatedAt)
	if err != nil || !known || !historyCoversEnabled(profile, cat, existedAt) {
		return profile, err
	}
	return ConvertLegacyAppAccessProfile(profile, cat, defaults, existedAt), nil
}

func historyCoversEnabled(profile *AppAccessProfile, cat *catalog.Catalog, existedAt []string) bool {
	existed := trimmedSet(existedAt)
	enabled := trimmedSet(profile.LegacyEnabledOperations)
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
