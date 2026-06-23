package operator

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

const gitDirName = ".git"

// gitignoreMatcherForDir builds a matcher from every .gitignore between the
// enclosing git repo root (nearest ancestor containing a .git entry) and dir,
// plus nested .gitignore files under dir. Returns (matcher, repoRoot); when no
// enclosing repo exists, repoRoot is dir and the matcher has no patterns, so
// nothing is excluded by gitignore.
//
// go-git's ReadPatterns walks the entire tree under repoRoot to collect nested
// .gitignore files, so this is O(checkout) per distinct repoRoot on the first
// call and O(1) after: matchers are cached per repoRoot for the process lifetime.
// That bounds the cost for a daemon fingerprinting many providers in one repo or
// re-fingerprinting on watch loops. No git binary is invoked; the gestaltd
// runtime image ships none.
func gitignoreMatcherForDir(dir string) (gitignore.Matcher, string, error) {
	repoRoot, ok := enclosingGitRepoRoot(dir)
	if !ok {
		return gitignore.NewMatcher(nil), dir, nil
	}
	matcher, err := cachedGitignoreMatcher(repoRoot)
	if err != nil {
		return nil, repoRoot, err
	}
	return matcher, repoRoot, nil
}

var gitignoreMatcherCache sync.Map

func cachedGitignoreMatcher(repoRoot string) (gitignore.Matcher, error) {
	if v, ok := gitignoreMatcherCache.Load(repoRoot); ok {
		return v.(gitignore.Matcher), nil
	}
	fs := osfs.New(repoRoot)
	patterns, err := gitignore.ReadPatterns(fs, nil)
	if err != nil {
		return nil, err
	}
	matcher := gitignore.NewMatcher(patterns)
	gitignoreMatcherCache.Store(repoRoot, matcher)
	return matcher, nil
}

// enclosingGitRepoRoot walks up from dir to the nearest ancestor containing a
// .git entry (a directory in a normal checkout or a file in a worktree). It
// returns false at the filesystem root if no ancestor qualifies.
func enclosingGitRepoRoot(dir string) (string, bool) {
	dir = filepath.Clean(dir)
	for {
		if _, err := os.Stat(filepath.Join(dir, gitDirName)); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// gitignorePathComponents splits path into the slash-separated components
// relative to repoRoot, as required by gitignore.Matcher.Match. An empty or
// unmappable path yields no components.
func gitignorePathComponents(repoRoot, path string) []string {
	rel, err := filepath.Rel(repoRoot, path)
	if err != nil {
		return nil
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == "" {
		return nil
	}
	return strings.Split(rel, "/")
}
