package agent

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
)

var scpGitURLPattern = regexp.MustCompile(`^[^@\s/\\]+@([^:\s]+):(.+)$`)

func CloneWorkspace(src *Workspace) *Workspace {
	if src == nil {
		return nil
	}
	out := &Workspace{
		Checkouts: make([]WorkspaceGitCheckout, len(src.Checkouts)),
		CWD:       src.CWD,
	}
	copy(out.Checkouts, src.Checkouts)
	return out
}

func ClonePreparedWorkspace(src *PreparedWorkspace) *PreparedWorkspace {
	if src == nil {
		return nil
	}
	return &PreparedWorkspace{Root: src.Root, CWD: src.CWD}
}

func NormalizeWorkspace(src *Workspace) (*Workspace, error) {
	if src == nil {
		return nil, nil
	}
	out := CloneWorkspace(src)
	for i := range out.Checkouts {
		out.Checkouts[i].URL = strings.TrimSpace(out.Checkouts[i].URL)
		out.Checkouts[i].Ref = strings.TrimSpace(out.Checkouts[i].Ref)
		normalized, err := normalizeWorkspaceRelPath(out.Checkouts[i].Path)
		if err != nil {
			return nil, fmt.Errorf("checkout[%d].path: %w", i, err)
		}
		out.Checkouts[i].Path = normalized
	}
	cwd, err := normalizeWorkspaceRelPath(out.CWD)
	if err != nil {
		return nil, fmt.Errorf("cwd: %w", err)
	}
	out.CWD = cwd
	if err := ValidateWorkspace(out); err != nil {
		return nil, err
	}
	return out, nil
}

func ValidateWorkspace(workspace *Workspace) error {
	if workspace == nil {
		return nil
	}
	if len(workspace.Checkouts) == 0 {
		return fmt.Errorf("workspace.checkouts is required")
	}
	paths := make([]string, 0, len(workspace.Checkouts))
	for i, checkout := range workspace.Checkouts {
		if strings.TrimSpace(checkout.URL) == "" {
			return fmt.Errorf("workspace.checkouts[%d].url is required", i)
		}
		normalized, err := normalizeWorkspaceRelPath(checkout.Path)
		if err != nil {
			return fmt.Errorf("workspace.checkouts[%d].path: %w", i, err)
		}
		for _, existing := range paths {
			if workspacePathOverlaps(existing, normalized) {
				return fmt.Errorf("workspace.checkouts[%d].path overlaps %q", i, existing)
			}
		}
		paths = append(paths, normalized)
		if _, err := CanonicalGitRepositoryIdentity(checkout.URL); err != nil {
			return fmt.Errorf("workspace.checkouts[%d].url: %w", i, err)
		}
	}
	cwd, err := normalizeWorkspaceRelPath(workspace.CWD)
	if err != nil {
		return fmt.Errorf("workspace.cwd: %w", err)
	}
	for _, checkoutPath := range paths {
		if workspacePathContains(checkoutPath, cwd) {
			return nil
		}
	}
	return fmt.Errorf("workspace.cwd must be inside one checkout path")
}

func normalizeWorkspaceRelPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("relative path is required")
	}
	if strings.Contains(value, `\`) {
		return "", fmt.Errorf("backslash separators are not allowed")
	}
	if strings.HasPrefix(value, "/") {
		return "", fmt.Errorf("absolute paths are not allowed")
	}
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("path must stay inside the workspace")
	}
	return cleaned, nil
}

func workspacePathOverlaps(a, b string) bool {
	return workspacePathContains(a, b) || workspacePathContains(b, a)
}

func workspacePathContains(parent, child string) bool {
	parent = strings.Trim(path.Clean(parent), "/")
	child = strings.Trim(path.Clean(child), "/")
	return parent == child || strings.HasPrefix(child, parent+"/")
}

func CanonicalGitRepositoryIdentity(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("git URL is required")
	}
	if match := scpGitURLPattern.FindStringSubmatch(raw); match != nil {
		return canonicalGitIdentity(match[1], match[2])
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse git URL: %w", err)
	}
	if parsed.Scheme == "" {
		if strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, ".") {
			return "", fmt.Errorf("local git paths are not allowed; use file:// with an explicit allowlist")
		}
		return canonicalGitIdentity("", raw)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("git URL query and fragment are not allowed")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("git URL userinfo is not allowed")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https", "http", "ssh", "git":
		return canonicalGitIdentity(parsed.Host, parsed.Path)
	case "file":
		if parsed.Host != "" && parsed.Host != "localhost" {
			return "", fmt.Errorf("file git URLs must be local")
		}
		localPath := path.Clean(parsed.Path)
		if localPath == "." || !strings.HasPrefix(localPath, "/") {
			return "", fmt.Errorf("file git URL path must be absolute")
		}
		return "file://" + strings.TrimSuffix(localPath, ".git"), nil
	default:
		return "", fmt.Errorf("git URL scheme %q is not supported", parsed.Scheme)
	}
}

func canonicalGitIdentity(host, repoPath string) (string, error) {
	host = strings.ToLower(strings.TrimSpace(host))
	repoPath = strings.TrimSpace(repoPath)
	if repoPath == "" {
		return "", fmt.Errorf("git repository path is required")
	}
	repoPath = strings.TrimPrefix(repoPath, "/")
	repoPath = strings.TrimSuffix(path.Clean(repoPath), ".git")
	if repoPath == "." || repoPath == "" || strings.HasPrefix(repoPath, "../") {
		return "", fmt.Errorf("git repository path is invalid")
	}
	if host == "" {
		return repoPath, nil
	}
	return host + "/" + repoPath, nil
}
