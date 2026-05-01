package providerregistry

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"
	"gopkg.in/yaml.v3"
)

const (
	IndexSchema        = "gestaltd-provider-index"
	IndexSchemaVersion = 1
	MaxIndexBytes      = 10 << 20

	DefaultRepositoryName = "valon"
	DefaultRepositoryURL  = "https://raw.githubusercontent.com/valon-technologies/gestalt-providers/main/provider-index.yaml"
)

var (
	repoNameRe        = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	hostRe            = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)
	pathSegmentRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	exactVersionRe    = regexp.MustCompile(`^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
	errNoPackageMatch = errors.New("provider package not found")
)

type Repository struct {
	URL   string `yaml:"url"`
	Token string `yaml:"token,omitempty"`
}

type NamedRepository struct {
	Name  string
	URL   string
	Token string
}

type Index struct {
	Schema        string             `yaml:"schema"`
	SchemaVersion int                `yaml:"schemaVersion"`
	Packages      map[string]Package `yaml:"packages"`
}

type Package struct {
	DisplayName string             `yaml:"displayName,omitempty"`
	Description string             `yaml:"description,omitempty"`
	Versions    map[string]Version `yaml:"versions"`
}

type Version struct {
	Metadata  string   `yaml:"metadata"`
	Kind      string   `yaml:"kind"`
	Runtime   string   `yaml:"runtime"`
	Platforms []string `yaml:"platforms,omitempty"`
	Yanked    bool     `yaml:"yanked,omitempty"`
}

type ResolvedPackage struct {
	RepositoryName string
	RepositoryURL  string
	Package        string
	Version        string
	MetadataURL    string
	Kind           string
	Runtime        string
	Platforms      []string
	Yanked         bool
}

type ResolveRequest struct {
	Package           string
	VersionConstraint string
	RepositoryName    string
	Repositories      []NamedRepository
}

type Resolver struct {
	Client *http.Client
}

func ValidateRepositoryName(name string) error {
	name = strings.TrimSpace(name)
	if !repoNameRe.MatchString(name) {
		return fmt.Errorf("provider repository name %q must contain only lowercase letters, digits, dots, underscores, and hyphens", name)
	}
	return nil
}

func ValidatePackageAddress(address string) error {
	address = strings.TrimSpace(address)
	if address == "" {
		return fmt.Errorf("provider package address is required")
	}
	parts := strings.Split(address, "/")
	if len(parts) < 4 {
		return fmt.Errorf("provider package address %q must be host/owner/repo/path", address)
	}
	if !hostRe.MatchString(parts[0]) {
		return fmt.Errorf("provider package address %q has invalid host %q", address, parts[0])
	}
	for _, seg := range parts[1:] {
		if !pathSegmentRe.MatchString(seg) {
			return fmt.Errorf("provider package address %q has invalid segment %q", address, seg)
		}
	}
	return nil
}

func PackageName(address string) string {
	return path.Base(strings.TrimSpace(address))
}

func DefaultRepositories() []NamedRepository {
	return []NamedRepository{{
		Name: DefaultRepositoryName,
		URL:  DefaultRepositoryURL,
	}}
}

func (r *Resolver) Resolve(ctx context.Context, req ResolveRequest) (*ResolvedPackage, error) {
	pkg := strings.TrimSpace(req.Package)
	if err := ValidatePackageAddress(pkg); err != nil {
		return nil, err
	}
	repos := req.Repositories
	if len(repos) == 0 {
		repos = DefaultRepositories()
	}
	if repoName := strings.TrimSpace(req.RepositoryName); repoName != "" {
		if err := ValidateRepositoryName(repoName); err != nil {
			return nil, err
		}
		repos = filterRepositories(repos, repoName)
		if len(repos) == 0 {
			return nil, fmt.Errorf("provider repository %q is not configured", repoName)
		}
	}

	var matches []ResolvedPackage
	var errs []error
	for _, repo := range repos {
		index, err := FetchIndex(ctx, r.client(), repo.URL, repo.Token)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", repo.Name, err))
			continue
		}
		entry, ok := index.Packages[pkg]
		if !ok {
			continue
		}
		version, selected, err := SelectVersion(entry.Versions, req.VersionConstraint)
		if err != nil {
			return nil, fmt.Errorf("resolve %s from %s: %w", pkg, repo.Name, err)
		}
		metadataURL, err := ResolveMetadataURL(repo.URL, selected.Metadata)
		if err != nil {
			return nil, fmt.Errorf("resolve metadata URL for %s %s from %s: %w", pkg, version, repo.Name, err)
		}
		matches = append(matches, ResolvedPackage{
			RepositoryName: repo.Name,
			RepositoryURL:  repo.URL,
			Package:        pkg,
			Version:        version,
			MetadataURL:    metadataURL,
			Kind:           strings.TrimSpace(selected.Kind),
			Runtime:        strings.TrimSpace(selected.Runtime),
			Platforms:      slices.Clone(selected.Platforms),
			Yanked:         selected.Yanked,
		})
	}
	if len(matches) == 0 {
		if len(errs) > 0 {
			return nil, errors.Join(errs...)
		}
		return nil, fmt.Errorf("%w: %s", errNoPackageMatch, pkg)
	}
	if len(matches) > 1 && strings.TrimSpace(req.RepositoryName) == "" {
		names := make([]string, 0, len(matches))
		for i := range matches {
			names = append(names, matches[i].RepositoryName)
		}
		slices.Sort(names)
		return nil, fmt.Errorf("provider package %q is available from multiple repositories (%s); pass --repo or set source.repo", pkg, strings.Join(names, ", "))
	}
	return &matches[0], nil
}

func (r *Resolver) client() *http.Client {
	if r != nil && r.Client != nil {
		return r.Client
	}
	return http.DefaultClient
}

func filterRepositories(repos []NamedRepository, name string) []NamedRepository {
	out := repos[:0]
	for _, repo := range repos {
		if repo.Name == name {
			out = append(out, repo)
		}
	}
	return out
}

func FetchIndex(ctx context.Context, client *http.Client, rawURL, token string) (*Index, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("provider repository URL is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	switch parsed.Scheme {
	case "http", "https":
	case "file":
		return readIndexFile(parsed.Path)
	default:
		return nil, fmt.Errorf("provider repository URL must be http(s) or file URL")
	}
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/yaml, application/x-yaml, text/yaml, application/octet-stream")
	if token = strings.TrimSpace(token); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d fetching provider repository index from %s", resp.StatusCode, rawURL)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxIndexBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxIndexBytes {
		return nil, fmt.Errorf("provider repository index exceeds %d byte limit", MaxIndexBytes)
	}
	return DecodeIndex(data)
}

func readIndexFile(path string) (*Index, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) > MaxIndexBytes {
		return nil, fmt.Errorf("provider repository index exceeds %d byte limit", MaxIndexBytes)
	}
	return DecodeIndex(data)
}

func DecodeIndex(data []byte) (*Index, error) {
	var index Index
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&index); err != nil && err != io.EOF {
		return nil, fmt.Errorf("parse provider repository index: %w", err)
	}
	if err := ValidateIndex(&index); err != nil {
		return nil, err
	}
	return &index, nil
}

func ValidateIndex(index *Index) error {
	if index == nil {
		return fmt.Errorf("provider repository index is required")
	}
	if index.Schema != IndexSchema {
		return fmt.Errorf("unsupported provider repository index schema %q", index.Schema)
	}
	if index.SchemaVersion != IndexSchemaVersion {
		return fmt.Errorf("unsupported provider repository index schema version %d", index.SchemaVersion)
	}
	for pkg, entry := range index.Packages {
		if err := ValidatePackageAddress(pkg); err != nil {
			return err
		}
		if len(entry.Versions) == 0 {
			return fmt.Errorf("provider package %q has no versions", pkg)
		}
		for version, release := range entry.Versions {
			if _, err := semver.NewVersion(version); err != nil {
				return fmt.Errorf("provider package %q version %q is invalid: %w", pkg, version, err)
			}
			if strings.TrimSpace(release.Metadata) == "" {
				return fmt.Errorf("provider package %q version %q metadata is required", pkg, version)
			}
			if strings.TrimSpace(release.Kind) == "" {
				return fmt.Errorf("provider package %q version %q kind is required", pkg, version)
			}
			if strings.TrimSpace(release.Runtime) == "" {
				return fmt.Errorf("provider package %q version %q runtime is required", pkg, version)
			}
		}
	}
	return nil
}

func SelectVersion(versions map[string]Version, constraint string) (string, Version, error) {
	if len(versions) == 0 {
		return "", Version{}, fmt.Errorf("no versions available")
	}
	type candidate struct {
		raw     string
		version *semver.Version
		entry   Version
	}
	candidates := make([]candidate, 0, len(versions))
	for raw, entry := range versions {
		v, err := semver.NewVersion(raw)
		if err != nil {
			return "", Version{}, fmt.Errorf("invalid version %q: %w", raw, err)
		}
		candidates = append(candidates, candidate{raw: raw, version: v, entry: entry})
	}
	slices.SortFunc(candidates, func(a, b candidate) int {
		return -a.version.Compare(b.version)
	})

	constraint = strings.TrimSpace(constraint)
	if constraint == "" {
		for _, c := range candidates {
			if !c.entry.Yanked && c.version.Prerelease() == "" {
				return c.raw, c.entry, nil
			}
		}
		for _, c := range candidates {
			if !c.entry.Yanked {
				return c.raw, c.entry, nil
			}
		}
		return "", Version{}, fmt.Errorf("all versions are yanked")
	}
	c, err := semver.NewConstraint(constraint)
	if err != nil {
		return "", Version{}, fmt.Errorf("invalid version constraint %q: %w", constraint, err)
	}
	exact := exactVersionRe.MatchString(constraint)
	for _, candidate := range candidates {
		if candidate.entry.Yanked && (!exact || candidate.raw != constraint) {
			continue
		}
		if c.Check(candidate.version) {
			return candidate.raw, candidate.entry, nil
		}
	}
	return "", Version{}, fmt.Errorf("no versions match constraint %q", constraint)
}

func VersionSatisfiesConstraint(version, constraint string) bool {
	version = strings.TrimSpace(version)
	constraint = strings.TrimSpace(constraint)
	if version == "" {
		return false
	}
	if constraint == "" {
		return true
	}
	v, err := semver.NewVersion(version)
	if err != nil {
		return false
	}
	c, err := semver.NewConstraint(constraint)
	if err != nil {
		return false
	}
	return c.Check(v)
}

func ResolveMetadataURL(indexURL, metadata string) (string, error) {
	metadata = strings.TrimSpace(metadata)
	if metadata == "" {
		return "", fmt.Errorf("metadata URL is required")
	}
	parsed, err := url.Parse(metadata)
	if err != nil {
		return "", err
	}
	if parsed.IsAbs() {
		switch parsed.Scheme {
		case "http", "https", "file":
			return parsed.String(), nil
		default:
			return "", fmt.Errorf("metadata URL must be http(s), file, or relative")
		}
	}
	base, err := url.Parse(strings.TrimSpace(indexURL))
	if err != nil {
		return "", err
	}
	return base.ResolveReference(parsed).String(), nil
}

func UserRepositoryStorePath() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "gestalt", "provider-repositories.yaml")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "gestalt", "provider-repositories.yaml")
	}
	return ""
}

func UserRepositoryCacheDir() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")); xdg != "" {
		return filepath.Join(xdg, "gestalt", "provider-repositories")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".cache", "gestalt", "provider-repositories")
	}
	return ""
}

type RepositoryStore struct {
	Repositories map[string]Repository `yaml:"repositories,omitempty"`
}

func ReadRepositoryStore(path string) (*RepositoryStore, error) {
	if strings.TrimSpace(path) == "" {
		return &RepositoryStore{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &RepositoryStore{}, nil
		}
		return nil, err
	}
	var store RepositoryStore
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&store); err != nil && err != io.EOF {
		return nil, err
	}
	return &store, nil
}

func WriteRepositoryStore(path string, store *RepositoryStore) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("provider repository store path is required")
	}
	if store == nil {
		store = &RepositoryStore{}
	}
	for name := range store.Repositories {
		if err := ValidateRepositoryName(name); err != nil {
			return err
		}
	}
	data, err := yaml.Marshal(store)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
