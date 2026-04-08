package pluginsource

import "context"

type Auth struct {
	Token string
}

type ResolveRequest struct {
	Source  Source
	Version string
	Auth    *Auth
}

type ResolvedPackage struct {
	LocalPath     string
	Cleanup       func()
	ArchiveSHA256 string
	ResolvedURL   string
}

type Resolver interface {
	Resolve(ctx context.Context, req ResolveRequest) (*ResolvedPackage, error)
}
