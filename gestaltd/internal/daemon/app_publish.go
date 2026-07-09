package daemon

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/providerregistry"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

const appPublishPlanSchema = "gestaltd.app.publish.plan.v1"

type appPublishDistDirs []string

func (d *appPublishDistDirs) String() string {
	return strings.Join(*d, ",")
}

func (d *appPublishDistDirs) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("--dist-dir requires a value")
	}
	*d = append(*d, value)
	return nil
}

type appPublishPlan struct {
	Schema          string             `json:"schema"`
	RegistryName    string             `json:"registryName"`
	AppName         string             `json:"appName"`
	DisplayName     string             `json:"displayName,omitempty"`
	Description     string             `json:"description,omitempty"`
	Version         string             `json:"version"`
	Entry           appregistry.Entry  `json:"entry"`
	EntryObject     appPublishObject   `json:"entryObject"`
	IndexObject     appPublishObject   `json:"indexObject"`
	ArtifactObjects []appPublishObject `json:"artifactObjects"`
}

const appPublishIndexUpdateAttempts = 5

type appPublishObject struct {
	Kind       string `json:"kind"`
	Target     string `json:"target,omitempty"`
	LocalPath  string `json:"localPath"`
	StorageURL string `json:"storageUrl"`
	PublicURL  string `json:"publicUrl"`
	SHA256     string `json:"sha256,omitempty"`
}

func runAppPublish(args []string) (err error) {
	fs := flag.NewFlagSet("gestaltd app publish", flag.ContinueOnError)
	fs.Usage = func() { printAppPublishUsage(fs.Output()) }
	registryName := fs.String("registry", "", "logical registry name recorded in publish output")
	bucket := fs.String("bucket", "", "GCS bucket name for registry uploads")
	appName := fs.String("app", "", "app name under apps/{app}/manifest.yaml")
	version := fs.String("version", "", "semantic version guard")
	ref := fs.String("ref", "", "full source commit SHA")
	dryRun := fs.Bool("dry-run", false, "print the publish plan as JSON without uploading")
	var distDirs appPublishDistDirs
	fs.Var(&distDirs, "dist-dir", "directory containing release archives (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if strings.TrimSpace(*registryName) == "" {
		return fmt.Errorf("--registry is required")
	}
	if strings.TrimSpace(*bucket) == "" {
		return fmt.Errorf("--bucket is required")
	}
	if strings.TrimSpace(*appName) == "" {
		return fmt.Errorf("--app is required")
	}
	if err := providerregistry.ValidateRepositoryName(*appName); err != nil {
		return fmt.Errorf("--app: %w", err)
	}
	if len(distDirs) == 0 {
		return fmt.Errorf("--dist-dir is required")
	}
	if strings.TrimSpace(*version) == "" {
		return fmt.Errorf("--version is required")
	}
	sourceRef := strings.ToLower(strings.TrimSpace(*ref))
	if !fullGitSHARe.MatchString(sourceRef) {
		return fmt.Errorf("--ref must be a 40-character commit SHA")
	}

	registry, err := config.NewGCSAppRegistry(*bucket)
	if err != nil {
		return fmt.Errorf("--bucket: %w", err)
	}
	manifestPath, err := resolveAppPublishManifest(*appName)
	if err != nil {
		return err
	}
	_, sourceManifest, err := readAppPublishManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", manifestPath, err)
	}
	manifestApp, err := appregistry.AppNameFromManifestSource(sourceManifest.Source)
	if err != nil {
		return fmt.Errorf("%s: invalid manifest source: %w", manifestPath, err)
	}
	if manifestApp != strings.TrimSpace(*appName) {
		return fmt.Errorf("%s: manifest source app %q does not match --app %q; update manifest source or pass the matching --app name", manifestPath, manifestApp, strings.TrimSpace(*appName))
	}
	releaseManifest, releaseVersion, releaseArchives, err := collectReleaseArchivesFromDirs([]string(distDirs), *version)
	if err != nil {
		return err
	}
	if err := validateProviderPublishManifest(sourceManifest, releaseManifest, releaseVersion, *version); err != nil {
		return err
	}
	if err := appregistry.ValidatePublishInput(sourceManifest, *version, sourceRef); err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp("", "gestalt-app-publish-*")
	if err != nil {
		return fmt.Errorf("create publish temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	if err := writeProviderReleaseMetadata(tmpDir, releaseManifest, releaseVersion, releaseArchives, nil, false); err != nil {
		return fmt.Errorf("write release metadata: %w", err)
	}

	releaseMetadataBytes, err := readProviderReleaseMetadata(filepath.Join(tmpDir, providerrelease.MetadataFile))
	if err != nil {
		return err
	}
	releaseMetadata, err := providerrelease.Decode(releaseMetadataBytes)
	if err != nil {
		return fmt.Errorf("decode provider release metadata: %w", err)
	}

	sourceInfo, err := providerPublishSourceRef(manifestPath, sourceRef)
	if err != nil {
		return err
	}

	plan, err := buildAppPublishPlan(appPublishPlanInput{
		RegistryName: *registryName,
		Registry:     registry,
		DisplayName:  sourceManifest.DisplayName,
		Description:  sourceManifest.Description,
		Version:      *version,
		SourceRef:    sourceRef,
		ManifestPath: sourceInfo.ManifestPath,
		Manifest:     sourceManifest,
		Release:      releaseMetadata,
		Archives:     releaseArchives,
		MetadataPath: filepath.Join(tmpDir, providerrelease.MetadataFile),
	})
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(plan.EntryObject.LocalPath) }()

	if *dryRun {
		return printAppPublishPlanJSON(plan)
	}
	if err := preflightAppPublishPlan(plan); err != nil {
		return err
	}
	return uploadAppPublishPlan(plan, sourceRef)
}

func readProviderReleaseMetadata(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return data, nil
}

func readAppPublishManifest(manifestPath string) ([]byte, *providermanifestv1.Manifest, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, nil, err
	}
	_, manifest, err := providerpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		return nil, nil, err
	}
	return data, manifest, nil
}

const appPublishManifestFile = "manifest.yaml"

func resolveAppPublishManifest(appName string) (string, error) {
	gitRoot, err := gitRootFromWorkingDirectory()
	if err != nil {
		return "", err
	}
	return resolveAppPublishManifestFromGitRoot(gitRoot, appName)
}

func resolveAppPublishManifestFromGitRoot(gitRoot, appName string) (string, error) {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return "", fmt.Errorf("--app is required")
	}
	wantRel := filepath.ToSlash(filepath.Join("apps", appName, appPublishManifestFile))
	var matches []string
	err := filepath.WalkDir(gitRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != appPublishManifestFile {
			return nil
		}
		rel, err := filepath.Rel(gitRoot, path)
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
		return "", fmt.Errorf("search apps/%s/manifest.yaml under %s: %w", appName, gitRoot, err)
	}
	switch len(matches) {
	case 0:
		msg := fmt.Sprintf("no apps/%s/manifest.yaml under git root %s", appName, gitRoot)
		if hint := appPublishManifestNotFoundHint(gitRoot, appName); hint != "" {
			msg += "; " + hint
		}
		return "", fmt.Errorf("%s", msg)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("multiple apps/%s/manifest.yaml files under git root %s: %s; ensure only one matching app directory exists", appName, gitRoot, strings.Join(matches, ", "))
	}
}

func appPublishManifestNotFoundHint(gitRoot, appName string) string {
	wantDir := filepath.ToSlash(filepath.Join("apps", appName))
	var appDirs []string
	err := filepath.WalkDir(gitRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() || entry.Name() != appName {
			return nil
		}
		rel, err := filepath.Rel(gitRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == wantDir || strings.HasSuffix(rel, "/"+wantDir) {
			appDirs = append(appDirs, path)
		}
		return nil
	})
	if err != nil {
		return ""
	}
	switch len(appDirs) {
	case 0:
		return "verify --app and run from the repository checkout that contains apps/{app}/manifest.yaml"
	case 1:
		manifestYAML := filepath.Join(appDirs[0], appPublishManifestFile)
		if _, err := os.Stat(manifestYAML); err == nil {
			return ""
		}
		if _, err := os.Stat(filepath.Join(appDirs[0], "manifest.json")); err == nil {
			return fmt.Sprintf("found %s but app registry publish requires manifest.yaml", filepath.Join(appDirs[0], "manifest.json"))
		}
		return fmt.Sprintf("found app directory %s but no manifest.yaml inside it", appDirs[0])
	default:
		return "multiple app directories match this name; narrow the repository layout so only one apps/{app} directory exists"
	}
}

func gitRootFromWorkingDirectory() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	rootOut, err := runProviderPublishCommand("git", "-C", cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("app publish must run inside a git repository checkout (from %s): %w", cwd, err)
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

type appPublishPlanInput struct {
	RegistryName string
	Registry     config.AppRegistryConfig
	DisplayName  string
	Description  string
	Version      string
	SourceRef    string
	ManifestPath string
	Manifest     *providermanifestv1.Manifest
	Release      *providerrelease.Metadata
	Archives     []releaseArchive
	MetadataPath string
}

func buildAppPublishPlan(input appPublishPlanInput) (appPublishPlan, error) {
	storageRoot, err := input.Registry.StorageURL()
	if err != nil {
		return appPublishPlan{}, err
	}
	publicRoot, err := input.Registry.PublicURL()
	if err != nil {
		return appPublishPlan{}, err
	}
	storageRoot = strings.TrimRight(storageRoot, "/")
	publicRoot = strings.TrimRight(publicRoot, "/")

	layout, err := appregistry.ResolvePublishLayout(input.Manifest.Source, input.Version)
	if err != nil {
		return appPublishPlan{}, err
	}

	artifactPrefix := layout.ArtifactPrefix
	sortedArchives := append([]releaseArchive(nil), input.Archives...)
	sort.Slice(sortedArchives, func(i, j int) bool {
		return filepath.Base(sortedArchives[i].Path) < filepath.Base(sortedArchives[j].Path)
	})

	publishArtifacts := make([]appregistry.PublishArtifact, 0, len(sortedArchives))
	artifactObjects := make([]appPublishObject, 0, len(sortedArchives))
	for _, archive := range sortedArchives {
		filename := filepath.Base(archive.Path)
		rel := path.Join(artifactPrefix, filename)
		digest, err := sha256File(archive.Path)
		if err != nil {
			return appPublishPlan{}, err
		}
		publishArtifacts = append(publishArtifacts, appregistry.PublishArtifact{
			Target:     archive.Target,
			LocalPath:  archive.Path,
			Filename:   filename,
			StorageURL: appregistry.StorageURL(storageRoot, rel),
			PublicURL:  appregistry.PublicURL(publicRoot, rel),
			SHA256:     digest,
		})
		artifactObjects = append(artifactObjects, appPublishObject{
			Kind:       providerPublishFileKindArchive,
			Target:     archive.Target,
			LocalPath:  archive.Path,
			StorageURL: appregistry.StorageURL(storageRoot, rel),
			PublicURL:  appregistry.PublicURL(publicRoot, rel),
			SHA256:     digest,
		})
	}

	entry, err := appregistry.BuildEntry(appregistry.BuildEntryInput{
		Manifest:     input.Manifest,
		Version:      input.Version,
		SourceRef:    input.SourceRef,
		ManifestPath: input.ManifestPath,
		Release:      input.Release,
		Artifacts:    publishArtifacts,
	})
	if err != nil {
		return appPublishPlan{}, err
	}

	entryRel := layout.EntryPath
	entryData, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return appPublishPlan{}, err
	}
	entryPath, err := writeTempJSON("gestalt-app-entry-*", entryData)
	if err != nil {
		return appPublishPlan{}, err
	}
	entryDigest, err := sha256File(entryPath)
	if err != nil {
		return appPublishPlan{}, err
	}

	indexRel := layout.IndexPath
	return appPublishPlan{
		Schema:       appPublishPlanSchema,
		RegistryName: input.RegistryName,
		AppName:      entry.App,
		DisplayName:  input.DisplayName,
		Description:  input.Description,
		Version:      input.Version,
		Entry:        entry,
		EntryObject: appPublishObject{
			Kind:       "entry",
			LocalPath:  entryPath,
			StorageURL: appregistry.StorageURL(storageRoot, entryRel),
			PublicURL:  appregistry.PublicURL(publicRoot, entryRel),
			SHA256:     entryDigest,
		},
		IndexObject: appPublishObject{
			Kind:       "index",
			StorageURL: appregistry.StorageURL(storageRoot, indexRel),
			PublicURL:  appregistry.PublicURL(publicRoot, indexRel),
		},
		ArtifactObjects: artifactObjects,
	}, nil
}

func writeTempJSON(pattern string, data []byte) (string, error) {
	file, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	path := file.Name()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func preflightAppPublishPlan(plan appPublishPlan) error {
	if err := preflightAppRegistryIndex(plan); err != nil {
		return err
	}
	for _, object := range append([]appPublishObject{plan.EntryObject}, plan.ArtifactObjects...) {
		if err := preflightAppPublishObject(object); err != nil {
			return err
		}
	}
	return nil
}

func preflightAppRegistryIndex(plan appPublishPlan) error {
	_, existing, err := downloadAppRegistryObject(plan.IndexObject.StorageURL)
	if err != nil {
		return err
	}
	var index *appregistry.Index
	if len(existing) == 0 {
		index = appregistry.NewEmptyIndex()
	} else {
		index, err = appregistry.DecodeIndex(existing)
		if err != nil {
			return fmt.Errorf("decode existing app index: %w", err)
		}
	}
	metadataPath := appregistry.AppVersionEntryPath(plan.AppName, plan.Version)
	_, _, err = appregistry.UpsertAppIndex(index, plan.Entry, metadataPath, plan.DisplayName, plan.Description)
	return err
}

func preflightAppPublishObject(object appPublishObject) error {
	matches, err := appPublishObjectMatchesExisting(object)
	if err != nil {
		return err
	}
	if matches {
		return nil
	}
	generation, _, err := describeAppPublishObject(object.StorageURL)
	if err != nil {
		return err
	}
	if generation == 0 {
		return nil
	}
	return fmt.Errorf("%s already exists; %s", object.StorageURL, republishCorruptObjectGuidance)
}

func uploadAppPublishPlan(plan appPublishPlan, sourceRef string) error {
	if err := uploadAppPublishImmutableObjects(plan, sourceRef); err != nil {
		return err
	}
	return uploadAppRegistryIndex(plan, sourceRef)
}

func uploadAppPublishImmutableObjects(plan appPublishPlan, sourceRef string) error {
	for _, object := range plan.ArtifactObjects {
		if err := uploadAppPublishObjectIfNeeded(object, sourceRef); err != nil {
			return err
		}
	}
	return uploadAppPublishObjectIfNeeded(plan.EntryObject, sourceRef)
}

func uploadAppPublishObjectIfNeeded(object appPublishObject, sourceRef string) error {
	matches, err := appPublishObjectMatchesExisting(object)
	if err != nil {
		return err
	}
	if matches {
		_, _ = fmt.Fprintf(os.Stdout, "skipped existing %s\n", object.StorageURL)
		return nil
	}
	return uploadAppPublishObject(object, sourceRef)
}

func appPublishObjectMatchesExisting(object appPublishObject) (bool, error) {
	generation, existingSHA, err := describeAppPublishObject(object.StorageURL)
	if err != nil {
		return false, err
	}
	if generation == 0 {
		return false, nil
	}
	if object.SHA256 != "" && existingSHA == object.SHA256 {
		return true, nil
	}
	if object.Kind == "entry" && object.LocalPath != "" {
		_, existing, err := downloadAppRegistryObject(object.StorageURL)
		if err != nil {
			return false, err
		}
		return appregistry.EntryFileEquivalentIgnoringPublishedAt(object.LocalPath, existing)
	}
	return false, nil
}

func uploadAppPublishObject(object appPublishObject, sourceRef string) error {
	metadata := fmt.Sprintf("source-ref=%s", sourceRef)
	if object.SHA256 != "" {
		metadata += ",sha256=" + object.SHA256
	}
	if _, err := runProviderPublishCommand("gcloud", "storage", "cp", "--if-generation-match=0", "--custom-metadata="+metadata, object.LocalPath, object.StorageURL); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "uploaded %s\n", object.StorageURL)
	return nil
}

func uploadAppRegistryIndex(plan appPublishPlan, sourceRef string) error {
	indexPath := plan.IndexObject.StorageURL
	for attempt := 1; attempt <= appPublishIndexUpdateAttempts; attempt++ {
		generation, existing, err := downloadAppRegistryObject(indexPath)
		if err != nil {
			return err
		}
		var index *appregistry.Index
		if len(existing) == 0 {
			index = appregistry.NewEmptyIndex()
		} else {
			index, err = appregistry.DecodeIndex(existing)
			if err != nil {
				return fmt.Errorf("decode existing app index: %w", err)
			}
		}
		metadataPath := appregistry.AppVersionEntryPath(plan.AppName, plan.Version)
		updated, changed, err := appregistry.UpsertAppIndex(index, plan.Entry, metadataPath, plan.DisplayName, plan.Description)
		if err != nil {
			return err
		}
		if !changed {
			_, _ = fmt.Fprintf(os.Stdout, "skipped unchanged index for %s %s\n", plan.AppName, plan.Version)
			return nil
		}
		data, err := json.MarshalIndent(updated, "", "  ")
		if err != nil {
			return err
		}
		tmpPath, err := writeTempJSON("gestalt-app-index-*", append(data, '\n'))
		if err != nil {
			return err
		}
		if err := uploadAppRegistryIndexFile(tmpPath, indexPath, sourceRef, generation); err != nil {
			_ = os.Remove(tmpPath)
			if appPublishPreconditionFailed(err) && attempt < appPublishIndexUpdateAttempts {
				continue
			}
			return err
		}
		_ = os.Remove(tmpPath)
		_, _ = fmt.Fprintf(os.Stdout, "updated %s\n", indexPath)
		return nil
	}
	return fmt.Errorf("update %s: exceeded retry limit after concurrent index updates", indexPath)
}

func uploadAppRegistryIndexFile(localPath, storageURL, sourceRef string, generation int64) error {
	args := []string{
		"storage", "cp",
		"--custom-metadata=source-ref=" + sourceRef,
		localPath,
		storageURL,
	}
	if generation == 0 {
		args = append(args, "--if-generation-match=0")
	} else {
		args = append(args, "--if-generation-match="+strconv.FormatInt(generation, 10))
	}
	_, err := runProviderPublishCommand("gcloud", args...)
	return err
}

func appPublishPreconditionFailed(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "precondition") ||
		strings.Contains(text, "generation") ||
		strings.Contains(text, "412")
}

func downloadAppRegistryObject(storageURL string) (int64, []byte, error) {
	generation, _, err := describeAppPublishObject(storageURL)
	if err != nil {
		return 0, nil, err
	}
	if generation == 0 {
		return 0, nil, nil
	}
	out, err := runProviderPublishCommand("gcloud", "storage", "cat", storageURL)
	if err != nil {
		return 0, nil, err
	}
	return generation, []byte(out), nil
}

type gcsObjectGeneration int64

func (g *gcsObjectGeneration) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		*g = 0
		return nil
	}
	if data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		value = strings.TrimSpace(value)
		if value == "" {
			*g = 0
			return nil
		}
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("parse generation %q: %w", value, err)
		}
		*g = gcsObjectGeneration(parsed)
		return nil
	}
	var parsed int64
	if err := json.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("parse generation: %w", err)
	}
	*g = gcsObjectGeneration(parsed)
	return nil
}

type appPublishObjectDescription struct {
	Generation gcsObjectGeneration `json:"generation"`
	Metadata   map[string]string   `json:"metadata"`
}

func describeAppPublishObject(storageURL string) (int64, string, error) {
	out, err := runProviderPublishCommand("gcloud", "storage", "objects", "describe", storageURL, "--format=json")
	if err != nil {
		if providerPublishObjectNotFound(err) {
			return 0, "", nil
		}
		return 0, "", err
	}
	var described appPublishObjectDescription
	if err := json.Unmarshal([]byte(out), &described); err != nil {
		return 0, "", fmt.Errorf("parse object metadata for %s: %w", storageURL, err)
	}
	return int64(described.Generation), strings.TrimSpace(described.Metadata["sha256"]), nil
}

func printAppPublishPlanJSON(plan appPublishPlan) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(plan)
}

func printAppPublishUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd app publish --registry NAME --bucket BUCKET --app APP --version VERSION --ref SHA --dist-dir DIR [--dist-dir DIR...]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Publish an installable app version to a GCS app registry bucket.")
	writeUsageLine(w, "Resolves the source manifest at apps/{app}/manifest.yaml under the git root.")
	writeUsageLine(w, "Builds immutable version metadata and artifacts under apps/{app}/ in the bucket.")
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --app        App name (manifest at apps/{app}/manifest.yaml)")
	writeUsageLine(w, "  --bucket     GCS bucket name or gs:// URL")
	writeUsageLine(w, "  --dist-dir   Directory containing release archives (repeatable)")
	writeUsageLine(w, "  --dry-run    Print the upload plan as JSON without writing")
	writeUsageLine(w, "  --ref        Full source commit SHA")
	writeUsageLine(w, "  --registry   Logical registry name recorded in publish output")
	writeUsageLine(w, "  --version    Semantic version guard")
}
