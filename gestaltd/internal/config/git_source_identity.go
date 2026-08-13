package config

import (
	"net/url"
	"path"
	"strings"
)

// GitSourceIdentity is the checkout identity of a GitHub git source: the
// repository, the configured ref, and the app directory (parent of the
// manifest). It is not an install locator and not a published commit.
type GitSourceIdentity struct {
	Repo   GitHubRepo
	Ref    string
	AppDir string
}

// Identity is the GitHub checkout identity for this git source. False when
// the repo is not github.com or the ref is missing. The ref is the configured
// value, not the lowercased install-locator form from NormalizedLocationParts.
func (g GitSourceDef) Identity() (GitSourceIdentity, bool) {
	repo, err := ParseGitHubRepo(g.Repo)
	if err != nil {
		return GitSourceIdentity{}, false
	}
	ref := strings.TrimSpace(g.Ref)
	if ref == "" {
		return GitSourceIdentity{}, false
	}
	appDir := path.Dir(normalizeGitLocationManifestPath(g.Path))
	if appDir == "." {
		appDir = ""
	}
	return GitSourceIdentity{Repo: repo, Ref: ref, AppDir: appDir}, true
}

// TreeURL is the GitHub website projection of Identity. Empty when this
// source has no GitHub identity. The ref is escaped as a single path
// segment, so a slash in the ref becomes %2F.
func (g GitSourceDef) TreeURL() string {
	id, ok := g.Identity()
	if !ok {
		return ""
	}
	return id.TreeURL()
}

// TreeURL is https://github.com/<owner>/<name>/tree/<ref>/<appDir>.
func (id GitSourceIdentity) TreeURL() string {
	if id.Repo.Owner == "" || id.Repo.Name == "" || id.Ref == "" {
		return ""
	}
	p := "/" + url.PathEscape(id.Repo.Owner) + "/" + url.PathEscape(id.Repo.Name) +
		"/tree/" + url.PathEscape(id.Ref)
	if dir := strings.Trim(id.AppDir, "/"); dir != "" {
		for _, seg := range strings.Split(dir, "/") {
			if seg == "" || seg == "." {
				continue
			}
			p += "/" + url.PathEscape(seg)
		}
	}
	return "https://github.com" + p
}
