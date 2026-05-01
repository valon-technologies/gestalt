package providerpkg

import "github.com/valon-technologies/gestalt/server/services/plugins/packageio"

func PlatformString(goos, goarch string) string {
	return packageio.PlatformString(goos, goarch)
}

func PlatformArchiveSuffix(goos, goarch string) string {
	return packageio.PlatformArchiveSuffix(goos, goarch)
}

func CurrentPlatformString() string {
	return packageio.CurrentPlatformString()
}

func ParsePlatformString(s string) (goos, goarch string, err error) {
	return packageio.ParsePlatformString(s)
}
