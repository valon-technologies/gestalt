package pluginsource

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	HostGitHub      = "github.com"
	segmentCount    = 4
	assetPrefix     = "gestalt-plugin-"
	assetSuffix     = ".tar.gz"
	versionPrefix   = "v"
	pluginTagPrefix = "plugin/"
)

var segmentRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

type Source struct {
	Host   string
	Owner  string
	Repo   string
	Plugin string
}

func Parse(raw string) (Source, error) {
	if raw != strings.TrimSpace(raw) {
		return Source{}, fmt.Errorf("pluginsource: input contains leading or trailing whitespace")
	}

	parts := strings.Split(raw, "/")
	if len(parts) != segmentCount {
		return Source{}, fmt.Errorf("pluginsource: expected %d segments, got %d", segmentCount, len(parts))
	}

	host, owner, repo, plugin := parts[0], parts[1], parts[2], parts[3]

	if host != HostGitHub {
		return Source{}, fmt.Errorf("pluginsource: unsupported host %q (only %s is supported)", host, HostGitHub)
	}

	for _, seg := range []string{owner, repo, plugin} {
		if !segmentRe.MatchString(seg) {
			return Source{}, fmt.Errorf("pluginsource: invalid segment %q", seg)
		}
	}

	return Source{Host: host, Owner: owner, Repo: repo, Plugin: plugin}, nil
}

func (s Source) String() string {
	return s.Host + "/" + s.Owner + "/" + s.Repo + "/" + s.Plugin
}

func (s Source) AssetName(version string) string {
	return assetPrefix + s.Plugin + "_" + versionPrefix + version + assetSuffix
}

func (s Source) ReleaseTag(version string) string {
	return pluginTagPrefix + s.Plugin + "/" + versionPrefix + version
}

func (s Source) RepoSlug() string {
	return s.Owner + "/" + s.Repo
}
