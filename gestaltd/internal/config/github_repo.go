package config

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
)

// GitHubRepo is the owner/name of a github.com repository.
type GitHubRepo struct {
	Owner string
	Name  string
}

// ParseGitHubRepo extracts owner and name from an http(s), ssh, or
// git@github.com: URL. Other hosts fail; callers decide whether that is
// omitted or an error.
func ParseGitHubRepo(raw string) (GitHubRepo, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return GitHubRepo{}, fmt.Errorf("github repo URL is empty")
	}
	const sshPrefix = "git@github.com:"
	if strings.HasPrefix(raw, sshPrefix) {
		return gitHubRepoFromPath(strings.TrimPrefix(raw, sshPrefix))
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return GitHubRepo{}, fmt.Errorf("parse github repo URL: %w", err)
	}
	if !isGitHubHost(parsed.Host) {
		return GitHubRepo{}, fmt.Errorf("not a github.com repository: %q", raw)
	}
	switch parsed.Scheme {
	case "http", "https", "ssh":
		return gitHubRepoFromPath(parsed.Path)
	default:
		return GitHubRepo{}, fmt.Errorf("not a github.com repository: %q", raw)
	}
}

func isGitHubHost(host string) bool {
	switch strings.ToLower(host) {
	case "github.com", "www.github.com":
		return true
	default:
		return false
	}
}

func gitHubRepoFromPath(p string) (GitHubRepo, error) {
	clean := strings.Trim(path.Clean(p), "/")
	parts := strings.Split(clean, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || parts[0] == "." {
		return GitHubRepo{}, fmt.Errorf("github repo path must be <owner>/<repo>")
	}
	name := strings.TrimSuffix(parts[1], ".git")
	if name == "" {
		return GitHubRepo{}, fmt.Errorf("github repo path must be <owner>/<repo>")
	}
	return GitHubRepo{Owner: parts[0], Name: name}, nil
}

const gitHubSnapshotRemoteErr = "source.git snapshots require https://github.com/<owner>/<repo>[.git]"

// ParseGitHubSnapshotRemote extracts owner and name from a GitHub remote that
// snapshots accept: https://github.com/<owner>/<repo> or git@github.com:<owner>/<repo>.
// Other forms that ParseGitHubRepo accepts (http, ssh://, www.github.com) fail here.
func ParseGitHubSnapshotRemote(raw string) (GitHubRepo, error) {
	raw = strings.TrimSpace(raw)
	if !gitHubSnapshotRemoteForm(raw) {
		return GitHubRepo{}, errors.New(gitHubSnapshotRemoteErr)
	}
	repo, err := ParseGitHubRepo(raw)
	if err != nil {
		return GitHubRepo{}, errors.New(gitHubSnapshotRemoteErr)
	}
	return repo, nil
}

func gitHubSnapshotRemoteForm(raw string) bool {
	if strings.HasPrefix(raw, "git@github.com:") {
		return true
	}
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && strings.EqualFold(parsed.Host, "github.com")
}
