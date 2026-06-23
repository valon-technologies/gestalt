package daemon

const defaultPlatforms = "darwin/amd64,darwin/arm64,linux/amd64,linux/arm64"
const allPlatformsValue = "all"
const defaultReleaseOutputDir = "dist/"

type releasePlatform struct {
	GOOS   string
	GOARCH string
}

type releaseBuildMode int

const (
	releaseBuildNone releaseBuildMode = iota
	releaseBuildDeclared
	releaseBuildImplicitGo
	releaseBuildPrebuilt
)

type releaseBuildTarget struct {
	Kind string
	Mode releaseBuildMode
}

type releaseArchive struct {
	Path   string
	SHA256 string
	Target string
}
