package appregistry

import "context"

// AppRestarter stops and starts a configured app provider during fleet install
// convergence. Step 9 exercises restart plumbing only; later steps swap binaries.
type AppRestarter interface {
	Restartable(app string) (bool, error)
	StopApp(ctx context.Context, app string) error
	StartApp(ctx context.Context, app string) error
	AbortRestarts()
}
