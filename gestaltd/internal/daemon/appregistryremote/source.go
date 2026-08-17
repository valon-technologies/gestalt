package appregistryremote

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
)

var (
	fullGitSHARe        = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
	errNotGitRepository = errors.New("not a git repository")
)

type commandRunner interface {
	Run(name string, args ...string) (string, error)
}

type shellCommandRunner struct{}

func (shellCommandRunner) Run(name string, args ...string) (string, error) {
	return runShellCommand(name, args...)
}

// resolvePublishManifest locates apps/{app}/manifest.yaml from git root or cwd.
func resolvePublishManifest(appName string, runner commandRunner) (absPath, relPath string, err error) {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return "", "", fmt.Errorf("app name is required")
	}
	if gitRoot, gitErr := gitRootFromWorkingDirectory(runner); gitErr == nil {
		path, resolveErr := resolveAppPublishManifestFromGitRoot(gitRoot, appName)
		if resolveErr == nil {
			rel, relErr := filepath.Rel(gitRoot, path)
			if relErr != nil {
				return "", "", fmt.Errorf("relative manifest path from git root: %w", relErr)
			}
			return path, filepath.ToSlash(rel), nil
		}
	} else if !errors.Is(gitErr, errNotGitRepository) {
		return "", "", gitErr
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("resolve working directory: %w", err)
	}
	path, err := resolveAppPublishManifestFromRoot(cwd, appName)
	if err != nil {
		return "", "", err
	}
	rel, err := filepath.Rel(cwd, path)
	if err != nil {
		return "", "", fmt.Errorf("relative manifest path from working directory: %w", err)
	}
	return path, filepath.ToSlash(rel), nil
}

func resolveAppPublishManifestFromGitRoot(gitRoot, appName string) (string, error) {
	return resolveAppPublishManifestFromRoot(gitRoot, appName)
}

func resolveAppPublishManifestFromRoot(root, appName string) (string, error) {
	appName = strings.TrimSpace(appName)
	wantRel := filepath.ToSlash(filepath.Join("apps", appName, "manifest.yaml"))
	var matches []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != "manifest.yaml" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == wantRel || strings.HasSuffix(rel, "/"+wantRel) {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("search apps/%s/manifest.yaml under %s: %w", appName, root, err)
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no apps/%s/manifest.yaml found; verify manifest.source and repository layout", appName)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("multiple apps/%s/manifest.yaml files found: %s", appName, strings.Join(matches, ", "))
	}
}

func gitRootFromWorkingDirectory(runner commandRunner) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	rootOut, err := runner.Run("git", "-C", cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		if isNotGitRepositoryError(err) {
			return "", errNotGitRepository
		}
		return "", fmt.Errorf("resolve git root from %s: %w", cwd, err)
	}
	gitRoot := strings.TrimSpace(rootOut)
	if absGitRoot, err := filepath.Abs(gitRoot); err == nil {
		gitRoot = absGitRoot
	}
	if evaluatedGitRoot, err := filepath.EvalSymlinks(gitRoot); err == nil {
		gitRoot = evaluatedGitRoot
	}
	return gitRoot, nil
}

func isNotGitRepositoryError(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "not a git repository")
}

func collectLocalSourceState(manifestPath string, runner commandRunner) *appregistry.LocalSourceState {
	manifestDir := filepath.Dir(manifestPath)
	if _, err := runner.Run("git", "-C", manifestDir, "rev-parse", "--show-toplevel"); err != nil {
		return nil
	}
	commitOut, err := runner.Run("git", "-C", manifestDir, "rev-parse", "HEAD")
	if err != nil {
		return nil
	}
	commitSHA := strings.ToLower(strings.TrimSpace(commitOut))
	if !fullGitSHARe.MatchString(commitSHA) {
		return nil
	}
	state := &appregistry.LocalSourceState{CommitSHA: commitSHA}
	statusOut, err := runner.Run("git", "-C", manifestDir, "status", "--porcelain")
	if err != nil {
		return state
	}
	for _, line := range strings.Split(statusOut, "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) < 3 {
			continue
		}
		code := line[:2]
		if strings.TrimSpace(code) == "" {
			continue
		}
		if code == "??" {
			state.Untracked = true
			continue
		}
		state.Dirty = true
	}
	return state
}

func validateRequiredPlatforms(platforms map[string]struct{}, required []string) error {
	var missing []string
	for _, platform := range required {
		platform = strings.TrimSpace(platform)
		if platform == "" {
			continue
		}
		if _, ok := platforms[platform]; !ok {
			missing = append(missing, platform)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("%w: missing %s", appregistry.ErrPublishRequiredPlatform, strings.Join(missing, ", "))
}
