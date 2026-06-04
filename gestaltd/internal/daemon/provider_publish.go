package daemon

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/operator"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
	"github.com/valon-technologies/gestalt/server/services/apps/source"
)

const providerPublishPlanSchema = "gestaltd.provider.publish.plan.v1"
const providerPublishFileKindArchive = "archive"
const providerPublishFileKindValidation = "validation"
const providerPublishFileKindMetadata = "metadata"
const providerPublishFormatText = "text"
const providerPublishFormatJSON = "json"
const republishCorruptObjectGuidance = "delete the object or entire snapshot SHA prefix and republish"

var fullGitSHARe = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

type providerPublishSourceRefInfo = operator.SnapshotSourceRefPath

type providerPublishDistDirs []string

func (d *providerPublishDistDirs) String() string {
	return strings.Join(*d, ",")
}

func (d *providerPublishDistDirs) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("--dist-dir requires a value")
	}
	*d = append(*d, value)
	return nil
}

type providerPublishFile struct {
	Kind       string `json:"kind"`
	Target     string `json:"target,omitempty"`
	LocalPath  string `json:"localPath"`
	StorageURL string `json:"storageUrl"`
	PublicURL  string `json:"publicUrl"`
	SHA256     string `json:"sha256"`
}

type providerPublishPlan struct {
	Schema            string                `json:"schema"`
	PublishRepository string                `json:"publishRepository"`
	SourceRepository  string                `json:"sourceRepository"`
	SourceRef         string                `json:"sourceRef"`
	ProviderDir       string                `json:"providerDir"`
	ManifestPath      string                `json:"manifestPath"`
	Version           string                `json:"version"`
	Metadata          providerPublishFile   `json:"metadata"`
	Validation        []providerPublishFile `json:"validation"`
	Artifacts         []providerPublishFile `json:"artifacts"`
	Files             []providerPublishFile `json:"files"`
}

func runProviderPublish(args []string) (err error) {
	fs := flag.NewFlagSet("gestaltd provider publish", flag.ContinueOnError)
	fs.Usage = func() { printProviderPublishUsage(fs.Output()) }
	repoName := fs.String("repo", "", "provider snapshot repository name")
	manifestPath := fs.String("manifest", "", "source manifest path")
	version := fs.String("version", "", "semantic version guard")
	ref := fs.String("ref", "", "full source commit SHA")
	dryRun := fs.Bool("dry-run", false, "print the publish plan without uploading")
	format := fs.String("format", providerPublishFormatText, "dry-run output format: text or json")
	var distDirs providerPublishDistDirs
	var configPaths repeatedStringFlag
	fs.Var(&distDirs, "dist-dir", "directory containing release archives (repeatable)")
	fs.Var(&configPaths, "config", "path to config file (repeat to layer overrides)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if strings.TrimSpace(*repoName) == "" {
		return fmt.Errorf("--repo is required")
	}
	if err := validateProviderPublishFormat(*format, *dryRun); err != nil {
		return err
	}
	if strings.TrimSpace(*manifestPath) == "" {
		return fmt.Errorf("--manifest is required")
	}
	if len(distDirs) == 0 {
		return fmt.Errorf("--dist-dir is required")
	}
	if strings.TrimSpace(*version) == "" {
		return fmt.Errorf("--version is required")
	}
	if err := source.ValidateVersion(*version); err != nil {
		return fmt.Errorf("invalid --version: %w", err)
	}
	sourceRef := strings.ToLower(strings.TrimSpace(*ref))
	if !fullGitSHARe.MatchString(sourceRef) {
		return fmt.Errorf("--ref must be a 40-character commit SHA")
	}

	cfg, err := config.LoadAllowMissingEnvPaths(operator.ResolveConfigPaths(configPaths))
	if err != nil {
		return err
	}
	repo, err := providerPublishRepository(cfg, *repoName)
	if err != nil {
		return err
	}
	_, sourceManifest, err := providerpkg.ReadSourceManifestFile(*manifestPath)
	if err != nil {
		return fmt.Errorf("read --manifest: %w", err)
	}
	releaseManifest, releaseVersion, releaseArchives, err := collectReleaseArchivesFromDirs([]string(distDirs), *version)
	if err != nil {
		return err
	}
	if err := validateProviderPublishManifest(sourceManifest, releaseManifest, releaseVersion, *version); err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp("", "gestalt-provider-publish-*")
	if err != nil {
		return fmt.Errorf("create publish temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	if err := writeProviderReleaseMetadata(tmpDir, releaseManifest, releaseVersion, releaseArchives, false); err != nil {
		return fmt.Errorf("write release metadata: %w", err)
	}

	files, sourceInfo, err := providerPublishFiles(repo, *manifestPath, sourceRef, releaseArchives, tmpDir)
	if err != nil {
		return err
	}
	if *dryRun {
		if *format == providerPublishFormatJSON {
			return printProviderPublishPlanJSON(*repoName, sourceInfo, *version, files)
		}
		printProviderPublishPlan(files)
		return nil
	}
	if err := preflightProviderPublishFiles(files); err != nil {
		return err
	}
	return uploadProviderPublishFiles(files, sourceRef)
}

func validateProviderPublishFormat(format string, dryRun bool) error {
	switch strings.TrimSpace(format) {
	case providerPublishFormatText:
		return nil
	case providerPublishFormatJSON:
		if !dryRun {
			return fmt.Errorf("--format json requires --dry-run")
		}
		return nil
	default:
		return fmt.Errorf("--format must be %q or %q", providerPublishFormatText, providerPublishFormatJSON)
	}
}

func providerPublishRepository(cfg *config.Config, name string) (config.ProviderSnapshotRepositoryConfig, error) {
	if cfg == nil {
		return config.ProviderSnapshotRepositoryConfig{}, fmt.Errorf("config did not load provider snapshot repositories")
	}
	repo, ok := cfg.ProviderSnapshotRepositories[strings.TrimSpace(name)]
	if !ok {
		return config.ProviderSnapshotRepositoryConfig{}, fmt.Errorf("providerSnapshotRepositories.%s is not configured", name)
	}
	publish := repo.Publish
	if publish.PathLayout == "" && !publish.Immutable && publish.Storage.Kind == "" && publish.Storage.URL == "" {
		return config.ProviderSnapshotRepositoryConfig{}, fmt.Errorf("providerSnapshotRepositories.%s.publish is required", name)
	}
	return repo, nil
}

func validateProviderPublishManifest(sourceManifest, releaseManifest *providermanifestv1.Manifest, releaseVersion, versionGuard string) error {
	if sourceManifest == nil {
		return fmt.Errorf("--manifest is required")
	}
	if releaseManifest == nil {
		return fmt.Errorf("release manifest is required")
	}
	if sourceManifest.Source != releaseManifest.Source {
		return fmt.Errorf("--manifest package %q does not match release package %q", sourceManifest.Source, releaseManifest.Source)
	}
	if sourceManifest.Kind != releaseManifest.Kind {
		return fmt.Errorf("--manifest kind %q does not match release kind %q", sourceManifest.Kind, releaseManifest.Kind)
	}
	if releaseVersion != versionGuard {
		return fmt.Errorf("release version %q does not match --version %q", releaseVersion, versionGuard)
	}
	return nil
}

func providerPublishFiles(repo config.ProviderSnapshotRepositoryConfig, manifestPath, sourceRef string, archives []releaseArchive, metadataDir string) ([]providerPublishFile, providerPublishSourceRefInfo, error) {
	sourceInfo, err := providerPublishSourceRef(manifestPath, sourceRef)
	if err != nil {
		return nil, providerPublishSourceRefInfo{}, err
	}
	storageRoot := strings.TrimRight(strings.TrimSpace(repo.Publish.Storage.URL), "/")
	publicRoot := strings.TrimRight(strings.TrimSpace(repo.URL), "/")
	if publicRoot == "" {
		return nil, providerPublishSourceRefInfo{}, fmt.Errorf("provider snapshot repository url is required")
	}

	sortedArchives := append([]releaseArchive(nil), archives...)
	sort.Slice(sortedArchives, func(i, j int) bool {
		return filepath.Base(sortedArchives[i].Path) < filepath.Base(sortedArchives[j].Path)
	})
	files := make([]providerPublishFile, 0, len(sortedArchives)+3)
	for _, archive := range sortedArchives {
		file, err := newProviderPublishFile(providerPublishFileKindArchive, archive.Target, archive.Path, storageRoot, publicRoot, sourceInfo.RelRoot, filepath.Base(archive.Path))
		if err != nil {
			return nil, providerPublishSourceRefInfo{}, err
		}
		files = append(files, file)
	}
	for _, name := range []string{providerrelease.ValidationManifestFile, providerrelease.ValidationCatalogFile} {
		localPath := filepath.Join(metadataDir, name)
		if _, err := os.Stat(localPath); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, providerPublishSourceRefInfo{}, fmt.Errorf("stat %s: %w", localPath, err)
		}
		file, err := newProviderPublishFile(providerPublishFileKindValidation, "", localPath, storageRoot, publicRoot, sourceInfo.RelRoot, name)
		if err != nil {
			return nil, providerPublishSourceRefInfo{}, err
		}
		files = append(files, file)
	}
	file, err := newProviderPublishFile(providerPublishFileKindMetadata, "", filepath.Join(metadataDir, providerrelease.MetadataFile), storageRoot, publicRoot, sourceInfo.RelRoot, providerrelease.MetadataFile)
	if err != nil {
		return nil, providerPublishSourceRefInfo{}, err
	}
	files = append(files, file)
	return files, sourceInfo, nil
}

func providerPublishSourceRef(manifestPath, sourceRef string) (providerPublishSourceRefInfo, error) {
	absManifestPath, err := filepath.Abs(manifestPath)
	if err != nil {
		return providerPublishSourceRefInfo{}, fmt.Errorf("resolve --manifest: %w", err)
	}
	if evaluatedManifestPath, err := filepath.EvalSymlinks(absManifestPath); err == nil {
		absManifestPath = evaluatedManifestPath
	}
	manifestDir := filepath.Dir(absManifestPath)
	rootOut, err := runProviderPublishCommand("git", "-C", manifestDir, "rev-parse", "--show-toplevel")
	if err != nil {
		return providerPublishSourceRefInfo{}, fmt.Errorf("resolve git root for --manifest: %w", err)
	}
	gitRoot := strings.TrimSpace(rootOut)
	if absGitRoot, err := filepath.Abs(gitRoot); err == nil {
		gitRoot = absGitRoot
	}
	if evaluatedGitRoot, err := filepath.EvalSymlinks(gitRoot); err == nil {
		gitRoot = evaluatedGitRoot
	}
	relManifestPath, err := filepath.Rel(gitRoot, absManifestPath)
	if err != nil {
		return providerPublishSourceRefInfo{}, fmt.Errorf("resolve manifest path relative to git root: %w", err)
	}
	if relManifestPath == "." || strings.HasPrefix(relManifestPath, ".."+string(filepath.Separator)) || filepath.IsAbs(relManifestPath) {
		return providerPublishSourceRefInfo{}, fmt.Errorf("--manifest must be inside the current git checkout")
	}
	providerDir := path.Dir(filepath.ToSlash(relManifestPath))
	if providerDir == "." {
		providerDir = ""
	}
	remoteOut, err := runProviderPublishCommand("git", "-C", gitRoot, "remote", "get-url", "origin")
	if err != nil {
		return providerPublishSourceRefInfo{}, fmt.Errorf("resolve git origin for --manifest: %w", err)
	}
	sourceInfo, err := operator.NewSnapshotSourceRefPath(strings.TrimSpace(remoteOut), sourceRef, filepath.ToSlash(relManifestPath))
	if err != nil {
		return providerPublishSourceRefInfo{}, err
	}
	if sourceInfo.ProviderDir != providerDir || sourceInfo.ManifestPath != filepath.ToSlash(relManifestPath) {
		return providerPublishSourceRefInfo{}, fmt.Errorf("resolved snapshot path does not match --manifest")
	}
	return sourceInfo, nil
}

func newProviderPublishFile(kind, target, localPath, storageRoot, publicRoot, relRoot, filename string) (providerPublishFile, error) {
	digest, err := sha256File(localPath)
	if err != nil {
		return providerPublishFile{}, err
	}
	rel := path.Join(relRoot, filename)
	return providerPublishFile{
		Kind:       kind,
		Target:     target,
		LocalPath:  localPath,
		StorageURL: storageRoot + "/" + rel,
		PublicURL:  publicRoot + "/" + rel,
		SHA256:     digest,
	}, nil
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func printProviderPublishPlan(files []providerPublishFile) {
	for _, file := range files {
		_, _ = fmt.Fprintf(os.Stdout, "dry-run upload %s -> %s sha256=%s\n", file.LocalPath, file.StorageURL, file.SHA256)
	}
	if len(files) > 0 {
		_, _ = fmt.Fprintf(os.Stdout, "provider-release metadata: %s\n", files[len(files)-1].PublicURL)
	}
}

func printProviderPublishPlanJSON(publishRepository string, sourceInfo providerPublishSourceRefInfo, version string, files []providerPublishFile) error {
	plan, err := newProviderPublishPlan(publishRepository, sourceInfo, version, files)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(plan)
}

func newProviderPublishPlan(publishRepository string, sourceInfo providerPublishSourceRefInfo, version string, files []providerPublishFile) (providerPublishPlan, error) {
	plan := providerPublishPlan{
		Schema:            providerPublishPlanSchema,
		PublishRepository: publishRepository,
		SourceRepository:  sourceInfo.SourceRepository,
		SourceRef:         sourceInfo.SourceRef,
		ProviderDir:       sourceInfo.ProviderDir,
		ManifestPath:      sourceInfo.ManifestPath,
		Version:           version,
		Artifacts:         []providerPublishFile{},
		Validation:        []providerPublishFile{},
		Files:             make([]providerPublishFile, 0, len(files)),
	}
	for _, file := range files {
		plan.Files = append(plan.Files, file)
		switch file.Kind {
		case providerPublishFileKindArchive:
			plan.Artifacts = append(plan.Artifacts, file)
		case providerPublishFileKindValidation:
			plan.Validation = append(plan.Validation, file)
		case providerPublishFileKindMetadata:
			plan.Metadata = file
		}
	}
	if plan.Metadata.PublicURL == "" {
		return providerPublishPlan{}, fmt.Errorf("provider publish plan is missing %s", providerrelease.MetadataFile)
	}
	if !providerPublishPlanHasFile(plan.Validation, providerrelease.ValidationManifestFile) {
		return providerPublishPlan{}, fmt.Errorf("provider publish plan is missing %s", providerrelease.ValidationManifestFile)
	}
	return plan, nil
}

func providerPublishPlanHasFile(files []providerPublishFile, name string) bool {
	for _, file := range files {
		if filepath.Base(file.LocalPath) == name {
			return true
		}
	}
	return false
}

func preflightProviderPublishFiles(files []providerPublishFile) error {
	for _, file := range files {
		if err := validateProviderPublishMetadataFile(file); err != nil {
			return err
		}
		exists, err := providerPublishObjectExists(file.StorageURL)
		if err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("%s already exists; %s", file.StorageURL, republishCorruptObjectGuidance)
		}
	}
	return nil
}

func validateProviderPublishMetadataFile(file providerPublishFile) error {
	if file.Kind != providerPublishFileKindMetadata {
		return nil
	}
	if err := providerrelease.ValidateLocalBundle(file.LocalPath); err != nil {
		return fmt.Errorf("provider release metadata %s: %w", file.LocalPath, err)
	}
	return nil
}

func uploadProviderPublishFiles(files []providerPublishFile, sourceRef string) error {
	for _, kind := range []string{providerPublishFileKindArchive, providerPublishFileKindValidation, providerPublishFileKindMetadata} {
		for _, file := range files {
			if file.Kind != kind {
				continue
			}
			if err := uploadProviderPublishFile(file, sourceRef); err != nil {
				return err
			}
		}
	}
	return nil
}

func uploadProviderPublishFile(file providerPublishFile, sourceRef string) error {
	metadata := fmt.Sprintf("source-ref=%s,sha256=%s", sourceRef, file.SHA256)
	if _, err := runProviderPublishCommand("gcloud", "storage", "cp", "--if-generation-match=0", "--custom-metadata="+metadata, file.LocalPath, file.StorageURL); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "uploaded %s\n", file.StorageURL)
	return nil
}

func providerPublishObjectExists(storageURL string) (bool, error) {
	_, err := runProviderPublishCommand("gcloud", "storage", "objects", "describe", storageURL)
	if err != nil {
		if providerPublishObjectNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func providerPublishObjectNotFound(err error) bool {
	var cmdErr *providerPublishCommandError
	if !errors.As(err, &cmdErr) {
		return false
	}
	text := strings.ToLower(cmdErr.Stdout + "\n" + cmdErr.Stderr + "\n" + cmdErr.Err.Error())
	return strings.Contains(text, "not found") ||
		strings.Contains(text, "no urls matched") ||
		strings.Contains(text, "404")
}

type providerPublishCommandError struct {
	Name   string
	Args   []string
	Err    error
	Stdout string
	Stderr string
}

func (e *providerPublishCommandError) Error() string {
	return fmt.Sprintf("%s %s failed: %v\n%s%s", e.Name, strings.Join(e.Args, " "), e.Err, e.Stdout, e.Stderr)
}

func (e *providerPublishCommandError) Unwrap() error {
	return e.Err
}

func runProviderPublishCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", &providerPublishCommandError{
			Name:   name,
			Args:   append([]string(nil), args...),
			Err:    err,
			Stdout: stdout.String(),
			Stderr: stderr.String(),
		}
	}
	return stdout.String(), nil
}

func printProviderPublishUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd provider publish --repo NAME --manifest PATH --version VERSION --ref SHA --dist-dir DIR [--dist-dir DIR...]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Publish finalized provider release artifacts to a configured artifact repository.")
	writeUsageLine(w, "Reads .tar.gz archives from each --dist-dir, validates one release bundle,")
	writeUsageLine(w, "generates provider-release.yaml and validation sidecars, then uploads archives")
	writeUsageLine(w, "and sidecars before provider-release.yaml. Destination paths use the configured repository")
	writeUsageLine(w, "publish.pathLayout and the manifest source plus --ref.")
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --config    Path to config file (repeat to layer overrides)")
	writeUsageLine(w, "  --dist-dir  Directory containing release archives (repeatable)")
	writeUsageLine(w, "  --dry-run   Print the upload plan without writing")
	writeUsageLine(w, "  --format    Dry-run output format: text or json (default: text)")
	writeUsageLine(w, "  --manifest  Source manifest path")
	writeUsageLine(w, "  --ref       Full source commit SHA")
	writeUsageLine(w, "  --repo      providerSnapshotRepositories entry with publish settings")
	writeUsageLine(w, "  --version   Semantic version guard")
}
