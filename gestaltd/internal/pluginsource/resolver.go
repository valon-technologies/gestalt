package pluginsource

import "context"

type ResolvedPackage struct {
	LocalPath     string
	Cleanup       func()
	ArchiveSHA256 string
	ResolvedURL   string
}

type Resolver interface {
	Resolve(ctx context.Context, src Source, version string) (*ResolvedPackage, error)
}
