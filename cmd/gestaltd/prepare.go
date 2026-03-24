package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/provider"
)

const (
	preparedLockfileName = "gestalt.lock.json"
	preparedProvidersDir = ".gestalt/providers"
	preparedLockVersion  = 1
)

const (
	providerResolutionPrefer providerResolutionMode = iota
	providerResolutionRequire
	providerResolutionAuto
)

type providerResolutionMode int

type preparedPaths struct {
	configPath   string
	configDir    string
	lockfilePath string
	providersDir string
}

type preparedLockfile struct {
	Version   int                              `json:"version"`
	Providers map[string]preparedProviderEntry `json:"providers"`
}

type preparedProviderEntry struct {
	Fingerprint string `json:"fingerprint"`
	Provider    string `json:"provider"`
}

type preparedFingerprintInput struct {
	Type              string            `json:"type"`
	URL               string            `json:"url"`
	AllowedOperations map[string]string `json:"allowed_operations,omitempty"`
	DisplayName       string            `json:"display_name,omitempty"`
	Description       string            `json:"description,omitempty"`
	HasIcon           bool              `json:"has_icon,omitempty"`
	IconSHA256        string            `json:"icon_sha256,omitempty"`
	IconReadError     string            `json:"icon_read_error,omitempty"`
}

func runPrepare(args []string) error {
	fs := flag.NewFlagSet("gestaltd prepare", flag.ContinueOnError)
	fs.Usage = func() { printPrepareUsage(fs.Output()) }
	configPath := fs.String("config", "", "path to config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	return prepareConfig(*configPath)
}

func prepareConfig(configFlag string) error {
	configPath := resolveConfigPath(configFlag)
	_, err := prepareConfigAtPath(configPath)
	return err
}

func prepareConfigAtPath(configPath string) (*preparedLockfile, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("loading config: %v", err)
	}

	paths := preparePathsForConfig(configPath)
	if err := os.MkdirAll(paths.providersDir, 0755); err != nil {
		return nil, fmt.Errorf("creating providers dir: %w", err)
	}

	lock := &preparedLockfile{
		Version:   preparedLockVersion,
		Providers: make(map[string]preparedProviderEntry),
	}

	written, err := writePreparedArtifacts(context.Background(), cfg, paths)
	if err != nil {
		return nil, err
	}
	for name, entry := range written {
		lock.Providers[name] = entry
	}

	if err := writePreparedLockfile(paths.lockfilePath, lock); err != nil {
		return nil, err
	}

	log.Printf("prepared providers: %d", len(lock.Providers))
	log.Printf("wrote lockfile %s", paths.lockfilePath)
	return lock, nil
}

func loadConfigForExecution(configFlag string, mode providerResolutionMode) (string, *config.Config, map[string]string, error) {
	configPath := resolveConfigPath(configFlag)

	cfg, err := config.Load(configPath)
	if err != nil {
		return "", nil, nil, fmt.Errorf("loading config: %v", err)
	}

	preparedProviders, err := preparedProvidersForConfig(configPath, cfg, mode)
	if err != nil {
		return "", nil, nil, err
	}

	return configPath, cfg, preparedProviders, nil
}

func preparedProvidersForConfig(configPath string, cfg *config.Config, mode providerResolutionMode) (map[string]string, error) {
	if !configHasRemoteAPIUpstreams(cfg) {
		return nil, nil
	}

	paths := preparePathsForConfig(configPath)
	lock, err := readPreparedLockfile(paths.lockfilePath)
	if mode == providerResolutionAuto && (err != nil || !preparedLockMatchesConfig(cfg, paths, lock)) {
		lock, err = prepareConfigAtPath(configPath)
	}
	if err != nil {
		if mode == providerResolutionRequire {
			return nil, fmt.Errorf("remote REST/GraphQL upstreams require prepared artifacts; run `gestaltd prepare --config %s`: %w", configPath, err)
		}
		return nil, nil
	}

	preparedProviders := make(map[string]string)
	for name := range cfg.Integrations {
		intg := cfg.Integrations[name]
		upstream, hasRemote := remoteAPIUpstreamForPrepare(intg)
		if !hasRemote {
			continue
		}

		fingerprint, err := integrationFingerprint(name, intg, upstream)
		if err != nil {
			return nil, fmt.Errorf("fingerprinting integration %q: %w", name, err)
		}

		entry, ok := lock.Providers[name]
		if !ok || entry.Fingerprint != fingerprint {
			if mode == providerResolutionRequire {
				return nil, fmt.Errorf("prepared artifact for integration %q is missing or stale; run `gestaltd prepare --config %s`", name, configPath)
			}
			continue
		}

		absPath := resolveProviderPath(paths.configDir, entry.Provider)
		if _, statErr := os.Stat(absPath); statErr != nil {
			if mode == providerResolutionRequire {
				return nil, fmt.Errorf("prepared artifact for integration %q not found at %s; run `gestaltd prepare --config %s`", name, absPath, configPath)
			}
			continue
		}

		preparedProviders[name] = absPath
	}

	return preparedProviders, nil
}

func writePreparedArtifacts(ctx context.Context, cfg *config.Config, paths preparedPaths) (map[string]preparedProviderEntry, error) {
	written := make(map[string]preparedProviderEntry)
	for _, name := range slices.Sorted(maps.Keys(cfg.Integrations)) {
		intg := cfg.Integrations[name]
		upstream, hasRemote := remoteAPIUpstreamForPrepare(intg)
		if !hasRemote {
			continue
		}

		def, err := loadAPIUpstream(ctx, name, upstream, nil)
		if err != nil {
			return nil, fmt.Errorf("compiling provider %q: %w", name, err)
		}
		copied := *def
		def = &copied
		if err := provider.ApplyArtifactOverrides(def, intg); err != nil {
			return nil, fmt.Errorf("applying artifact overrides for %q: %w", name, err)
		}

		outPath := filepath.Join(paths.providersDir, name+".json")
		if err := writeJSONFile(outPath, def); err != nil {
			return nil, fmt.Errorf("writing provider %q: %w", name, err)
		}

		fingerprint, err := integrationFingerprint(name, intg, upstream)
		if err != nil {
			return nil, fmt.Errorf("fingerprinting integration %q: %w", name, err)
		}

		relPath, err := filepath.Rel(paths.configDir, outPath)
		if err != nil {
			return nil, fmt.Errorf("computing provider path for %q: %w", name, err)
		}
		written[name] = preparedProviderEntry{
			Fingerprint: fingerprint,
			Provider:    filepath.ToSlash(relPath),
		}
		log.Printf("wrote prepared provider %s", outPath)
	}
	return written, nil
}

func remoteAPIUpstreamForPrepare(intg config.IntegrationDef) (config.UpstreamDef, bool) {
	for i := range intg.Upstreams {
		us := &intg.Upstreams[i]
		switch us.Type {
		case config.UpstreamTypeREST, config.UpstreamTypeGraphQL:
			if us.URL != "" {
				return *us, true
			}
		}
	}
	return config.UpstreamDef{}, false
}

func configHasRemoteAPIUpstreams(cfg *config.Config) bool {
	for name := range cfg.Integrations {
		_, ok := remoteAPIUpstreamForPrepare(cfg.Integrations[name])
		if ok {
			return true
		}
	}
	return false
}

func resolveProviderPath(baseDir, provider string) string {
	if filepath.IsAbs(provider) {
		return provider
	}
	return filepath.Join(baseDir, filepath.FromSlash(provider))
}

func preparePathsForConfig(configPath string) preparedPaths {
	configDir := filepath.Dir(configPath)
	return preparedPaths{
		configPath:   configPath,
		configDir:    configDir,
		lockfilePath: filepath.Join(configDir, preparedLockfileName),
		providersDir: filepath.Join(configDir, filepath.FromSlash(preparedProvidersDir)),
	}
}

func writeJSONFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}

func readPreparedLockfile(path string) (*preparedLockfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lock preparedLockfile
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("parsing lockfile %s: %w", path, err)
	}
	if lock.Version != preparedLockVersion {
		return nil, fmt.Errorf("unsupported lockfile version %d", lock.Version)
	}
	if lock.Providers == nil {
		lock.Providers = make(map[string]preparedProviderEntry)
	}
	return &lock, nil
}

func writePreparedLockfile(path string, lock *preparedLockfile) error {
	if err := writeJSONFile(path, lock); err != nil {
		return fmt.Errorf("writing lockfile: %w", err)
	}
	return nil
}

func preparedLockMatchesConfig(cfg *config.Config, paths preparedPaths, lock *preparedLockfile) bool {
	if lock == nil || lock.Version != preparedLockVersion {
		return false
	}
	for name := range cfg.Integrations {
		intg := cfg.Integrations[name]
		upstream, ok := remoteAPIUpstreamForPrepare(intg)
		if !ok {
			continue
		}
		entry, found := lock.Providers[name]
		if !found {
			return false
		}
		fingerprint, err := integrationFingerprint(name, intg, upstream)
		if err != nil || entry.Fingerprint != fingerprint {
			return false
		}
		absPath := resolveProviderPath(paths.configDir, entry.Provider)
		if _, err := os.Stat(absPath); err != nil {
			return false
		}
	}
	return true
}

func integrationFingerprint(name string, intg config.IntegrationDef, upstream config.UpstreamDef) (string, error) {
	input := preparedFingerprintInput{
		Type:              upstream.Type,
		URL:               upstream.URL,
		AllowedOperations: map[string]string(upstream.AllowedOperations),
		DisplayName:       intg.DisplayName,
		Description:       intg.Description,
		HasIcon:           intg.IconFile != "",
	}
	if intg.IconFile != "" {
		data, err := os.ReadFile(intg.IconFile)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				input.IconReadError = err.Error()
			} else {
				input.IconReadError = os.ErrNotExist.Error()
			}
		} else {
			sum := sha256.Sum256(data)
			input.IconSHA256 = hex.EncodeToString(sum[:])
		}
	}
	payload, err := json.Marshal(struct {
		Name string `json:"name"`
		preparedFingerprintInput
	}{
		Name:                     name,
		preparedFingerprintInput: input,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}
