package appregistry

import "github.com/valon-technologies/gestalt/server/services/apps/packageio"

func packageioParsePlatform(platform string) (string, string, error) {
	return packageio.ParsePlatformString(platform)
}
