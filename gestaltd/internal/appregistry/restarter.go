package appregistry

import "context"

// AppRestarter stops and starts a configured app provider during fleet install
// convergence. When version is non-empty and a registry install exists on disk,
// StartApp mounts that materialized package instead of the deploy-time pin.
type AppRestarter interface {
	Restartable(app string) (bool, error)
	StopApp(ctx context.Context, app string) error
	StartApp(ctx context.Context, app, version string) error
	AbortRestarts()
}
