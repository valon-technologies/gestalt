package core

import (
	"context"
	"errors"
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
	// listedSet is built by AppAccessDefaultsFor; literals that set only Listed
	// fall back to a scan.
	listedSet map[string]struct{}
}

// AppAccessDefaultsFor reads the defaults an app publishes.
func AppAccessDefaultsFor(prov Provider) AppAccessDefaults {
	if provider, ok := prov.(AppAccessDefaultsProvider); ok {
		if listed, configured := provider.DefaultAppAccessOperations(); configured {
			return AppAccessDefaults{Configured: true, Listed: listed, listedSet: trimmedSet(listed)}
		}
	}
	return AppAccessDefaults{}
}

// Includes reports whether the operation is on for a user with no choice.
func (d AppAccessDefaults) Includes(op catalog.CatalogOperation) bool {
	if d.Configured {
		id := strings.TrimSpace(op.ID)
		if d.listedSet != nil {
			_, ok := d.listedSet[id]
			return ok
		}
		return slices.ContainsFunc(d.Listed, func(listed string) bool { return strings.TrimSpace(listed) == id })
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
	return p.allowsID(strings.TrimSpace(op.ID), defaults.Includes(op))
}

// AllowsUnlisted is Allows for an operation the catalog does not list, whose
// default cannot be proven: only an explicit choice turns it on.
func (p *AppAccessProfile) AllowsUnlisted(id string) bool {
	if p == nil {
		return true
	}
	return p.allowsID(strings.TrimSpace(id), false)
}

func (p *AppAccessProfile) allowsID(id string, defaultOn bool) bool {
	if p.Legacy {
		return slices.Contains(p.LegacyEnabledOperations, id)
	}
	if slices.Contains(p.ExtraOperations, id) {
		return true
	}
	return defaultOn && !slices.Contains(p.DisabledOperations, id)
}

// Validate enforces that a profile is either legacy (only an enabled list) or
// relative (only disabled and extra decisions).
func (p *AppAccessProfile) Validate() error {
	if p == nil {
		return nil
	}
	if p.Legacy {
		if len(p.DisabledOperations) > 0 || len(p.ExtraOperations) > 0 {
			return errors.New("legacy app access profile must not hold disabled or extra operations")
		}
		return nil
	}
	if len(p.LegacyEnabledOperations) > 0 {
		return errors.New("relative app access profile must not hold legacy enabled operations")
	}
	return nil
}

// NewRelativeAppAccessProfile builds a profile of decisions relative to the
// app's defaults.
func NewRelativeAppAccessProfile(subjectID, app string, disabled, extra []string, updatedAt time.Time) *AppAccessProfile {
	return &AppAccessProfile{
		SubjectID:           subjectID,
		App:                 app,
		DisabledOperations:  MergeAppAccessIDs(disabled, nil),
		ExtraOperations:     MergeAppAccessIDs(extra, nil),
		DefaultsInitialized: true,
		UpdatedAt:           updatedAt,
	}
}

// NewLegacyAppAccessProfile builds a profile holding a full enabled list.
func NewLegacyAppAccessProfile(subjectID, app string, enabled []string, updatedAt time.Time) *AppAccessProfile {
	return &AppAccessProfile{
		SubjectID:               subjectID,
		App:                     app,
		LegacyEnabledOperations: MergeAppAccessIDs(enabled, nil),
		Legacy:                  true,
		DefaultsInitialized:     true,
		UpdatedAt:               updatedAt,
	}
}

// MergeAppAccessIDs returns the trimmed, sorted, deduplicated union of a and b.
func MergeAppAccessIDs(a, b []string) []string {
	merged := make([]string, 0, len(a)+len(b))
	for _, id := range slices.Concat(a, b) {
		if id = strings.TrimSpace(id); id != "" {
			merged = append(merged, id)
		}
	}
	return sortedUnique(merged)
}

// EnabledOperations returns the ids of the catalog operations the user has
// switched on, sorted.
func (p *AppAccessProfile) EnabledOperations(cat *catalog.Catalog, defaults AppAccessDefaults) []string {
	return p.catalogOperations(cat, defaults, true)
}

// DeniedOperations returns the ids of the catalog operations the user has not
// switched on, sorted.
func (p *AppAccessProfile) DeniedOperations(cat *catalog.Catalog, defaults AppAccessDefaults) []string {
	return p.catalogOperations(cat, defaults, false)
}

func (p *AppAccessProfile) catalogOperations(cat *catalog.Catalog, defaults AppAccessDefaults, allowed bool) []string {
	if cat == nil {
		return nil
	}
	ids := make([]string, 0, len(cat.Operations))
	for i := range cat.Operations {
		if p.Allows(cat.Operations[i], defaults) == allowed {
			ids = append(ids, cat.Operations[i].ID)
		}
	}
	slices.Sort(ids)
	return ids
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
// fetch, an admin hiding it) and comes back.
//
// A legacy profile's enabled ids carry forward as extras, which keeps them on.
// Every other operation of the static catalog that the page does not list was
// off under the legacy list, so it is recorded as an opt-out; otherwise it
// would fall back to its default and turn on.
func (p *AppAccessProfile) OverridesOutside(cat, static *catalog.Catalog) (disabled, extra []string) {
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
	if !p.Legacy {
		return outside(p.DisabledOperations), outside(p.ExtraOperations)
	}
	extra = outside(p.LegacyEnabledOperations)
	if static != nil {
		staticIDs := make([]string, 0, len(static.Operations))
		for i := range static.Operations {
			staticIDs = append(staticIDs, static.Operations[i].ID)
		}
		for _, id := range outside(staticIDs) {
			if !slices.Contains(p.LegacyEnabledOperations, id) {
				disabled = append(disabled, id)
			}
		}
	}
	return disabled, extra
}
