package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/fsnotify/fsnotify"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

type providerLocalWatchPlan struct {
	Files     []string
	Roots     []string
	WatchDirs []string
}

type providerLocalWatchPlanBuilder struct {
	files map[string]struct{}
	roots map[string]struct{}
}

func newProviderLocalWatchPlan(configPaths []string, cfg *config.Config) (providerLocalWatchPlan, error) {
	builder := newProviderLocalWatchPlanBuilder()
	for _, configPath := range configPaths {
		if err := builder.addRequiredPath(configPath); err != nil {
			return providerLocalWatchPlan{}, fmt.Errorf("watch config %q: %w", configPath, err)
		}
	}

	for _, sourcePath := range providerLocalSourcePaths(cfg) {
		if err := builder.addSourcePath(sourcePath); err != nil {
			return providerLocalWatchPlan{}, err
		}
	}
	for _, metadataPath := range providerLocalReleaseMetadataPaths(cfg) {
		if err := builder.addRequiredFile(metadataPath); err != nil {
			return providerLocalWatchPlan{}, fmt.Errorf("watch release metadata %q: %w", metadataPath, err)
		}
	}
	return builder.build()
}

func providerLocalSourcePaths(cfg *config.Config) []string {
	return providerLocalEntryPaths(cfg, func(entry *config.ProviderEntry) (string, bool) {
		if entry.HasLocalSource() {
			return entry.SourcePath(), true
		}
		return "", false
	})
}

func providerLocalReleaseMetadataPaths(cfg *config.Config) []string {
	return providerLocalEntryPaths(cfg, func(entry *config.ProviderEntry) (string, bool) {
		if entry.HasLocalReleaseSource() {
			return entry.SourceReleasePath(), true
		}
		return "", false
	})
}

func providerLocalEntryPaths(cfg *config.Config, entryPath func(*config.ProviderEntry) (string, bool)) []string {
	if cfg == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var paths []string
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		cleaned := filepath.Clean(path)
		if _, ok := seen[cleaned]; ok {
			return
		}
		seen[cleaned] = struct{}{}
		paths = append(paths, cleaned)
	}
	addProviderEntries := func(entries map[string]*config.ProviderEntry) {
		for _, entry := range entries {
			if entry != nil {
				if path, ok := entryPath(entry); ok {
					add(path)
				}
			}
		}
	}
	addProviderEntries(cfg.Apps)
	addProviderEntries(cfg.Providers.Authentication)
	addProviderEntries(cfg.Providers.Authorization)
	addProviderEntries(cfg.Providers.ExternalCredentials)
	addProviderEntries(cfg.Providers.Secrets)
	addProviderEntries(cfg.Providers.Telemetry)
	addProviderEntries(cfg.Providers.Audit)
	addProviderEntries(cfg.Providers.IndexedDB)
	addProviderEntries(cfg.Providers.Cache)
	addProviderEntries(cfg.Providers.S3)
	addProviderEntries(cfg.Providers.Workflow)
	addProviderEntries(cfg.Providers.Agent)
	for _, entry := range cfg.Runtime.Providers {
		if entry != nil {
			if path, ok := entryPath(&entry.ProviderEntry); ok {
				add(path)
			}
		}
	}
	for _, entry := range cfg.Providers.UI {
		if entry != nil {
			if path, ok := entryPath(&entry.ProviderEntry); ok {
				add(path)
			}
		}
	}
	slices.Sort(paths)
	return paths
}

func newProviderLocalWatchPlanBuilder() *providerLocalWatchPlanBuilder {
	return &providerLocalWatchPlanBuilder{
		files: map[string]struct{}{},
		roots: map[string]struct{}{},
	}
}

func (b *providerLocalWatchPlanBuilder) addSourcePath(sourcePath string) error {
	manifestPath, err := watchSourceManifestPath(sourcePath)
	if err != nil {
		return fmt.Errorf("watch source %q: %w", sourcePath, err)
	}
	return b.addRequiredRoot(filepath.Dir(manifestPath))
}

func watchSourceManifestPath(sourcePath string) (string, error) {
	sourcePath = strings.TrimSpace(sourcePath)
	if sourcePath == "" {
		return "", fmt.Errorf("source path is required")
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		return "", err
	}
	manifestPath := sourcePath
	if info.IsDir() {
		manifestPath, err = providerpkg.FindManifestFile(sourcePath)
		if err != nil {
			return "", err
		}
	} else if !providerpkg.IsManifestFile(sourcePath) {
		return "", fmt.Errorf("source %q must point to a provider manifest file or directory", sourcePath)
	}
	return canonicalPath(manifestPath)
}

func (b *providerLocalWatchPlanBuilder) addRequiredPath(path string) error {
	canonical, err := canonicalPath(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return b.addRequiredRoot(canonical)
	}
	return b.addRequiredFile(canonical)
}

func (b *providerLocalWatchPlanBuilder) addRequiredFile(path string) error {
	canonical, err := canonicalPath(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", canonical)
	}
	b.files[canonical] = struct{}{}
	return nil
}

func (b *providerLocalWatchPlanBuilder) addRequiredRoot(path string) error {
	canonical, err := canonicalPath(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", canonical)
	}
	b.roots[canonical] = struct{}{}
	return nil
}

func (b *providerLocalWatchPlanBuilder) build() (providerLocalWatchPlan, error) {
	plan := providerLocalWatchPlan{
		Files: sortedMapKeys(b.files),
		Roots: sortedMapKeys(b.roots),
	}
	watchDirs, err := plan.collectWatchDirs()
	if err != nil {
		return providerLocalWatchPlan{}, err
	}
	plan.WatchDirs = watchDirs
	return plan, nil
}

func sortedMapKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func (p providerLocalWatchPlan) collectWatchDirs() ([]string, error) {
	dirs := map[string]struct{}{}
	addDir := func(path string) error {
		canonical, err := canonicalPath(path)
		if err != nil {
			return err
		}
		info, err := os.Stat(canonical)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", canonical)
		}
		dirs[canonical] = struct{}{}
		return nil
	}
	for _, file := range p.Files {
		if err := addDir(filepath.Dir(file)); err != nil {
			return nil, fmt.Errorf("watch parent for %s: %w", file, err)
		}
	}
	for _, root := range p.Roots {
		if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if p.shouldSkipPath(path, d) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.IsDir() {
				return nil
			}
			dirs[filepath.Clean(path)] = struct{}{}
			return nil
		}); err != nil {
			return nil, fmt.Errorf("collect watch dirs for %s: %w", root, err)
		}
	}
	return sortedMapKeys(dirs), nil
}

func (p providerLocalWatchPlan) includesEventPath(path string) bool {
	canonical, err := canonicalPath(path)
	if err != nil {
		return false
	}
	for _, file := range p.Files {
		if canonical == file {
			return true
		}
	}
	for _, root := range p.Roots {
		if providerLocalWatchPathWithinRoot(root, canonical) {
			return true
		}
	}
	return false
}

func (p providerLocalWatchPlan) includesRootPath(path string) bool {
	canonical, err := canonicalPath(path)
	if err != nil {
		return false
	}
	for _, root := range p.Roots {
		if providerLocalWatchPathWithinRoot(root, canonical) {
			return true
		}
	}
	return false
}

func (p providerLocalWatchPlan) shouldSkipPath(_ string, d os.DirEntry) bool {
	if d.IsDir() {
		_, excluded := providerLocalWatchExcludedDirs[d.Name()]
		return excluded
	}
	return false
}

var providerLocalWatchExcludedDirs = map[string]struct{}{
	".git":         {},
	".gestalt":     {},
	".gestaltd":    {},
	"node_modules": {},
	"target":       {},
}

func providerLocalWatchPathWithinRoot(root, target string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func newProviderLocalFSWatcher(plan providerLocalWatchPlan) (*fsnotify.Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create filesystem watcher: %w", err)
	}
	for _, dir := range plan.WatchDirs {
		if err := watcher.Add(dir); err != nil {
			_ = watcher.Close()
			return nil, fmt.Errorf("watch %s: %w", dir, err)
		}
	}
	return watcher, nil
}

func addProviderLocalCreatedWatchDirs(watcher *fsnotify.Watcher, plan providerLocalWatchPlan, path string) {
	if !plan.includesRootPath(path) {
		return
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return
	}
	_ = filepath.WalkDir(path, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if plan.shouldSkipPath(path, d) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			_ = watcher.Add(path)
		}
		return nil
	})
}
