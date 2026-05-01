package source

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

const (
	HostGitHub      = "github.com"
	minSegmentCount = 4
	assetPrefix     = "gestalt-plugin-"
	assetSuffix     = ".tar.gz"
	versionPrefix   = "v"
)

var segmentRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

type Source struct {
	Host  string
	Owner string
	Repo  string
	Path  string
	Token string
}

func Parse(raw string) (Source, error) {
	if raw != strings.TrimSpace(raw) {
		return Source{}, fmt.Errorf("plugin source: input contains leading or trailing whitespace")
	}

	parts := strings.Split(raw, "/")
	if len(parts) < minSegmentCount {
		return Source{}, fmt.Errorf("plugin source: expected at least %d segments, got %d", minSegmentCount, len(parts))
	}

	host, owner, repo := parts[0], parts[1], parts[2]

	if host != HostGitHub {
		return Source{}, fmt.Errorf("plugin source: unsupported host %q (only %s is supported)", host, HostGitHub)
	}

	for _, seg := range parts[1:] {
		if !segmentRe.MatchString(seg) {
			return Source{}, fmt.Errorf("plugin source: invalid segment %q", seg)
		}
	}

	packagePath := strings.Join(parts[3:], "/")

	return Source{Host: host, Owner: owner, Repo: repo, Path: packagePath}, nil
}

func (s Source) PackagePath() string {
	return s.Path
}

func (s Source) PluginName() string {
	return path.Base(s.Path)
}

func (s Source) String() string {
	return s.Host + "/" + s.Owner + "/" + s.Repo + "/" + s.PackagePath()
}

func (s Source) AssetName(version string) string {
	return assetPrefix + s.PluginName() + "_" + versionPrefix + version + assetSuffix
}

func (s Source) ReleaseTag(version string) string {
	return s.PackagePath() + "/" + versionPrefix + version
}

func (s Source) RepoSlug() string {
	return s.Owner + "/" + s.Repo
}
