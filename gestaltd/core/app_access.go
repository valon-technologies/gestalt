package core

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core/catalog"
)

// GraphQLCapabilityID is the reserved app capability for raw GraphQL
// requests, which do not correspond to one catalog operation.
const GraphQLCapabilityID = "graphql"

// AppAccessProfile is the user-owned allow list for one app. Workspace
// authorization remains a separate ceiling: enabling an operation here never
// grants access the workspace has not granted.
//
// A profile records the user's decisions relative to the app's defaults, not
// a copy of what was enabled when they first connected. An operation the app
// adds later therefore follows its default until the user chooses otherwise.
type AppAccessProfile struct {
	SubjectID string
	App       string
	// DisabledOperations are default-on operations the user switched off.
	DisabledOperations []string
	// ExtraOperations are operations that are not default-on that the user
	// switched on.
	ExtraOperations []string
	// LegacyEnabledOperations is set only on profiles written before decisions
	// were stored relative to defaults. It is the full list that was enabled at
	// the last save and stays authoritative until the user next saves.
	LegacyEnabledOperations []string
	Legacy                  bool
	DefaultsInitialized     bool
	UpdatedAt               time.Time
}

// AppAccessProfileStore persists the interactive app capabilities a user has
// selected. The store deliberately has an idempotent defaulting operation so
// reconnecting an account cannot overwrite a user's choices.
type AppAccessProfileStore interface {
	GetAppAccessProfile(ctx context.Context, subjectID, app string) (*AppAccessProfile, error)
	EnsureAppAccessDefaults(ctx context.Context, subjectID, app string) (*AppAccessProfile, error)
	SetAppAccessOverrides(ctx context.Context, subjectID, app string, disabled, extra []string) (*AppAccessProfile, error)
}

// AppAccessDefaults says which operations an app turns on for a user who has
// made no choice.
type AppAccessDefaults struct {
	// Configured is true when the app publishes an explicit list, which may be
	// empty. Otherwise every operation that is visible by default is on.
	Configured bool
	Listed     []string
}

// AppAccessDefaultsFor reads the defaults an app publishes.
func AppAccessDefaultsFor(prov Provider) AppAccessDefaults {
	if provider, ok := prov.(AppAccessDefaultsProvider); ok {
		if listed, configured := provider.DefaultAppAccessOperations(); configured {
			return AppAccessDefaults{Configured: true, Listed: listed}
		}
	}
	return AppAccessDefaults{}
}

// Includes reports whether the operation is on for a user with no choice.
func (d AppAccessDefaults) Includes(op catalog.CatalogOperation) bool {
	if d.Configured {
		return slices.ContainsFunc(d.Listed, func(id string) bool { return strings.TrimSpace(id) == strings.TrimSpace(op.ID) })
	}
	return catalog.OperationVisibleByDefault(op)
}

// Operations returns the default-on operation ids of the catalog, sorted.
func (d AppAccessDefaults) Operations(cat *catalog.Catalog) []string {
	if cat == nil {
		return nil
	}
	operations := make([]string, 0, len(cat.Operations))
	for i := range cat.Operations {
		if d.Includes(cat.Operations[i]) {
			operations = append(operations, cat.Operations[i].ID)
		}
	}
	slices.Sort(operations)
	return operations
}

// Allows reports whether the user has the operation switched on. A nil
// profile means the user has made no choice, so everything is allowed.
func (p *AppAccessProfile) Allows(op catalog.CatalogOperation, defaults AppAccessDefaults) bool {
	if p == nil {
		return true
	}
	id := strings.TrimSpace(op.ID)
	if p.Legacy {
		return slices.Contains(p.LegacyEnabledOperations, id)
	}
	if slices.Contains(p.ExtraOperations, id) {
		return true
	}
	return defaults.Includes(op) && !slices.Contains(p.DisabledOperations, id)
}

// EnabledOperations returns the ids of the catalog operations the user has
// switched on, sorted.
func (p *AppAccessProfile) EnabledOperations(cat *catalog.Catalog, defaults AppAccessDefaults) []string {
	if cat == nil {
		return nil
	}
	enabled := make([]string, 0, len(cat.Operations))
	for i := range cat.Operations {
		if p.Allows(cat.Operations[i], defaults) {
			enabled = append(enabled, cat.Operations[i].ID)
		}
	}
	slices.Sort(enabled)
	return enabled
}

// AppAccessOverrides turns the operations a user wants on into decisions
// relative to the defaults. Ids outside the catalog are ignored.
func AppAccessOverrides(enabled []string, cat *catalog.Catalog, defaults AppAccessDefaults) (disabled, extra []string) {
	if cat == nil {
		return nil, nil
	}
	want := make(map[string]struct{}, len(enabled))
	for _, id := range enabled {
		want[strings.TrimSpace(id)] = struct{}{}
	}
	for i := range cat.Operations {
		op := cat.Operations[i]
		_, on := want[op.ID]
		switch isDefault := defaults.Includes(op); {
		case isDefault && !on:
			disabled = append(disabled, op.ID)
		case !isDefault && on:
			extra = append(extra, op.ID)
		}
	}
	slices.Sort(disabled)
	slices.Sort(extra)
	return disabled, extra
}

// OverridesOutside returns the decisions the profile holds for operations the
// catalog does not list right now. A save only sees the operations the page
// offered, so carrying these forward keeps an opt-out from reverting to the
// default when its operation is temporarily absent (a failed session-catalog
// fetch, an admin hiding it) and comes back. A legacy profile's enabled ids
// carry forward as extras, which keeps them on.
func (p *AppAccessProfile) OverridesOutside(cat *catalog.Catalog) (disabled, extra []string) {
	if p == nil {
		return nil, nil
	}
	listed := make(map[string]struct{})
	if cat != nil {
		for i := range cat.Operations {
			listed[cat.Operations[i].ID] = struct{}{}
		}
	}
	outside := func(ids []string) []string {
		var kept []string
		for _, id := range ids {
			if _, ok := listed[id]; !ok {
				kept = append(kept, id)
			}
		}
		return kept
	}
	if p.Legacy {
		return nil, outside(p.LegacyEnabledOperations)
	}
	return outside(p.DisabledOperations), outside(p.ExtraOperations)
}
