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

// PlatformArchive identifies a single platform-specific archive in a release.
type PlatformArchive struct {
	Platform string // e.g. "darwin/arm64", "linux/amd64/musl", "generic"
	URL      string // download URL for the archive
}

// PlatformEnumerator discovers all platform-specific archives available for a
// plugin release without downloading them. Resolvers that implement this
// interface enable multi-platform lockfiles.
type PlatformEnumerator interface {
	ListPlatformArchives(ctx context.Context, src Source, version string) ([]PlatformArchive, error)
}
