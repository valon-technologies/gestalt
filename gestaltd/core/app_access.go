package core

import (
	"context"
	"time"
)

// AppAccessProfile is the user-owned allow list for one app. Workspace
// authorization remains a separate ceiling: enabling an operation here never
// grants access the workspace has not granted.
type AppAccessProfile struct {
	SubjectID           string
	App                 string
	EnabledOperations   []string
	DefaultsInitialized bool
	UpdatedAt           time.Time
}

// AppAccessProfileStore persists the interactive app capabilities a user has
// selected. The store deliberately has an idempotent defaulting operation so
// reconnecting an account cannot overwrite a user's choices.
type AppAccessProfileStore interface {
	GetAppAccessProfile(ctx context.Context, subjectID, app string) (*AppAccessProfile, error)
	EnsureAppAccessDefaults(ctx context.Context, subjectID, app string, operations []string) (*AppAccessProfile, error)
	SetAppAccessOperations(ctx context.Context, subjectID, app string, operations []string) (*AppAccessProfile, error)
}
