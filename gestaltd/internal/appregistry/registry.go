package appregistry

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/providerregistry"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/packageio"
	"github.com/valon-technologies/gestalt/server/services/apps/source"
)

const (
	IndexSchemaVersion  = 1
	EntrySchemaVersion  = 1
	IndexFileName       = "index.json"
	appSourcePathPrefix = "apps/"
)

var (
	ErrRegistryEntryConflict = errors.New("registry entry conflict")
	ErrIndexVersionConflict  = errors.New("app registry index version conflict")
)

type Index struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Apps          map[string]AppVersions `json:"apps"`
}

type AppVersions struct {
	DisplayName string                  `json:"displayName,omitempty"`
	Description string                  `json:"description,omitempty"`
	Versions    map[string]IndexVersion `json:"versions"`
}

type IndexVersion struct {
	Metadata          string            `json:"metadata"`
	Platforms         []string          `json:"platforms,omitempty"`
	PublishedAt       time.Time         `json:"publishedAt"`
	PublishStartedAt  *time.Time        `json:"publishStartedAt,omitempty"`
	SourceRef         string            `json:"sourceRef,omitempty"`
	Repository        string            `json:"repository,omitempty"`
	Publication       *Publication      `json:"publication,omitempty"`
	PublicationKind   PublicationKind   `json:"publicationKind,omitempty"`
	PublishID         string            `json:"publishId,omitempty"`
	BuilderVersion    string            `json:"builderVersion,omitempty"`
	DeclarationDigest string            `json:"declarationDigest,omitempty"`
	LocalSource       *LocalSourceState `json:"localSource,omitempty"`
}

type Entry struct {
	SchemaVersion     int                 `json:"schemaVersion"`
	App               string              `json:"app"`
	Version           string              `json:"version"`
	SourceRef         string              `json:"sourceRef,omitempty"`
	ManifestPath      string              `json:"manifestPath"`
	Repository        string              `json:"repository,omitempty"`
	Publication       *Publication        `json:"publication,omitempty"`
	PublicationKind   PublicationKind     `json:"publicationKind,omitempty"`
	PublishID         string              `json:"publishId,omitempty"`
	BuilderVersion    string              `json:"builderVersion,omitempty"`
	DeclarationDigest string              `json:"declarationDigest,omitempty"`
	LocalSource       *LocalSourceState   `json:"localSource,omitempty"`
	Artifacts         map[string]Artifact `json:"artifacts"`
	Interface         Interface           `json:"interface,omitempty"`
	Requires          Requires            `json:"requires,omitempty"`
	Compatibility     Compatibility       `json:"compatibility,omitempty"`
	PublishedAt       time.Time           `json:"publishedAt"`
	PublishStartedAt  *time.Time          `json:"publishStartedAt,omitempty"`
}

type Publication struct {
	WorkflowRunURL     string                  `json:"workflowRunUrl"`
	TriggerPullRequest *PublicationPullRequest `json:"triggerPullRequest,omitempty"`
	TriggerCommit      *PublicationCommit      `json:"triggerCommit,omitempty"`
}

type PublicationPullRequest struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	Title  string `json:"title,omitempty"`
}

type PublicationCommit struct {
	SHA string `json:"sha"`
	URL string `json:"url"`
}

type Artifact struct {
	URL       string `json:"url"`
	PublicURL string `json:"publicUrl"`
	SHA256    string `json:"sha256"`
}

type Interface struct {
	Operations map[string]OperationContract `json:"operations,omitempty"`
}

type OperationContract struct {
	InputSchema  json.RawMessage `json:"inputSchema,omitempty"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
}

type Requires struct {
	Apps map[string]AppRequirement `json:"apps,omitempty"`
}

type AppRequirement struct {
	Version    string                          `json:"version,omitempty"`
	Operations map[string]OperationRequirement `json:"operations,omitempty"`
}

type OperationRequirement struct {
	InputSchemaHash string `json:"inputSchemaHash,omitempty"`
}

type Compatibility struct {
	MinGestaltdVersion string `json:"minGestaltdVersion,omitempty"`
}

type PublishArtifact struct {
	Target     string
	LocalPath  string
	Filename   string
	StorageURL string
	PublicURL  string
	SHA256     string
}

func parseAppSource(raw string) (appName, repository string, err error) {
	src, err := source.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", err
	}
	if !strings.HasPrefix(src.Path, appSourcePathPrefix) {
		return "", "", fmt.Errorf("app source path must be apps/{app}, got %q", src.Path)
	}
	appName = strings.TrimSpace(strings.TrimPrefix(src.Path, appSourcePathPrefix))
	if appName == "" || strings.Contains(appName, "/") {
		return "", "", fmt.Errorf("app source path must be apps/{app}, got %q", src.Path)
	}
	repository = src.Host + "/" + src.Owner + "/" + src.Repo
	return appName, repository, nil
}

func AppSourceAddress(repository, appName string) string {
	return strings.TrimRight(strings.TrimSpace(repository), "/") + "/" + appSourcePathPrefix + strings.Trim(strings.TrimSpace(appName), "/")
}

func AppNameFromManifestSource(source string) (string, error) {
	appName, _, err := parseAppSource(source)
	return appName, err
}

func RepositoryFromManifestSource(source string) (string, error) {
	_, repository, err := parseAppSource(source)
	return repository, err
}

// RequirementAppName normalizes a requires.apps map key to the short fleet app name.
// Manifest dependencies may use full source addresses (github.com/acme/apps/base)
// while fleet catalog entries use short names (base).
func RequirementAppName(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", fmt.Errorf("dependency app name is required")
	}
	if appName, err := AppNameFromManifestSource(key); err == nil {
		return appName, nil
	}
	if err := providerregistry.ValidateRepositoryName(key); err != nil {
		return "", fmt.Errorf("invalid dependency app name %q: %w", key, err)
	}
	return key, nil
}

func requirementForApp(requires Requires, targetApp string) (AppRequirement, bool) {
	targetApp = strings.TrimSpace(targetApp)
	if targetApp == "" {
		return AppRequirement{}, false
	}
	if requirement, ok := requires.Apps[targetApp]; ok {
		return requirement, true
	}
	for key, requirement := range requires.Apps {
		appName, err := RequirementAppName(key)
		if err != nil || appName != targetApp {
			continue
		}
		return requirement, true
	}
	return AppRequirement{}, false
}

func BuildEntry(input BuildEntryInput) (Entry, error) {
	if err := ValidatePublishInputWithOptions(input.Manifest, input.Version, input.SourceRef, PublishValidationOptions{
		PublicationKind: input.PublicationKind,
	}); err != nil {
		return Entry{}, err
	}
	if input.Release == nil {
		return Entry{}, fmt.Errorf("provider release metadata is required")
	}
	appName, repository, err := parseAppSource(input.Manifest.Source)
	if err != nil {
		return Entry{}, err
	}
	artifacts, err := buildArtifacts(input.Artifacts)
	if err != nil {
		return Entry{}, err
	}
	requires := RequiresFromRelease(input.Release)
	compatibility := CompatibilityFromRelease(input.Release)
	publishedAt := input.PublishedAt
	if publishedAt.IsZero() {
		publishedAt = time.Now().UTC()
	}
	entry := Entry{
		SchemaVersion:     EntrySchemaVersion,
		App:               appName,
		Version:           strings.TrimSpace(input.Version),
		SourceRef:         strings.ToLower(strings.TrimSpace(input.SourceRef)),
		ManifestPath:      strings.TrimSpace(input.ManifestPath),
		Repository:        repository,
		Publication:       clonePublication(input.Publication),
		PublicationKind:   input.PublicationKind,
		PublishID:         strings.TrimSpace(input.PublishID),
		BuilderVersion:    strings.TrimSpace(input.BuilderVersion),
		DeclarationDigest: strings.TrimSpace(input.DeclarationDigest),
		LocalSource:       cloneLocalSourceState(input.LocalSource),
		Artifacts:         artifacts,
		Interface:         InterfaceFromRelease(input.Release),
		Requires:          requires,
		Compatibility:     compatibility,
		PublishedAt:       publishedAt.UTC(),
	}
	if !input.PublishStartedAt.IsZero() {
		startedAt := input.PublishStartedAt.UTC()
		entry.PublishStartedAt = &startedAt
	}
	if err := validateEntry(&entry); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

type BuildEntryInput struct {
	Manifest          *providermanifestv1.Manifest
	Version           string
	SourceRef         string
	ManifestPath      string
	Publication       *Publication
	PublicationKind   PublicationKind
	PublishID         string
	BuilderVersion    string
	DeclarationDigest string
	LocalSource       *LocalSourceState
	Release           *providerrelease.Metadata
	Artifacts         []PublishArtifact
	PublishedAt       time.Time
	PublishStartedAt  time.Time
}

func buildArtifacts(artifacts []PublishArtifact) (map[string]Artifact, error) {
	if len(artifacts) == 0 {
		return nil, fmt.Errorf("at least one artifact is required")
	}
	out := make(map[string]Artifact, len(artifacts))
	for _, artifact := range artifacts {
		target := strings.TrimSpace(artifact.Target)
		if target == "" {
			return nil, fmt.Errorf("artifact target is required")
		}
		if err := validateArtifactPlatform(target); err != nil {
			return nil, err
		}
		if strings.TrimSpace(artifact.SHA256) == "" {
			return nil, fmt.Errorf("artifact sha256 is required for target %q", target)
		}
		if strings.TrimSpace(artifact.StorageURL) == "" || strings.TrimSpace(artifact.PublicURL) == "" {
			return nil, fmt.Errorf("artifact URLs are required for target %q", target)
		}
		if _, ok := out[target]; ok {
			return nil, fmt.Errorf("duplicate artifact target %q", target)
		}
		out[target] = Artifact{
			URL:       artifact.StorageURL,
			PublicURL: artifact.PublicURL,
			SHA256:    artifact.SHA256,
		}
	}
	return out, nil
}

func validateArtifactPlatform(target string) error {
	if _, _, err := packageio.ParsePlatformString(target); err != nil {
		return fmt.Errorf("artifact platform %q: %w", target, err)
	}
	return nil
}

func InterfaceFromRelease(release *providerrelease.Metadata) Interface {
	if release == nil || release.StaticValidation == nil || release.StaticValidation.Catalog == nil {
		return Interface{}
	}
	operations := make(map[string]OperationContract)
	ops := release.StaticValidation.Catalog.Operations
	for i := range ops {
		op := ops[i]
		id := strings.TrimSpace(op.ID)
		if id == "" {
			continue
		}
		operations[id] = OperationContract{
			InputSchema:  cloneRawJSON(op.InputSchema),
			OutputSchema: cloneRawJSON(op.OutputSchema),
		}
	}
	if len(operations) == 0 {
		return Interface{}
	}
	return Interface{Operations: operations}
}

func RequiresFromRelease(release *providerrelease.Metadata) Requires {
	if release == nil || release.StaticValidation == nil || release.StaticValidation.Requires == nil {
		return Requires{}
	}
	return requiresFromProviderRelease(*release.StaticValidation.Requires)
}

func CompatibilityFromRelease(release *providerrelease.Metadata) Compatibility {
	if release == nil || release.StaticValidation == nil || release.StaticValidation.Compatibility == nil {
		return Compatibility{}
	}
	return Compatibility{MinGestaltdVersion: release.StaticValidation.Compatibility.MinGestaltdVersion}
}

func requiresFromProviderRelease(requires providerrelease.Requires) Requires {
	if len(requires.Apps) == 0 {
		return Requires{}
	}
	apps := make(map[string]AppRequirement, len(requires.Apps))
	for appName, req := range requires.Apps {
		ops := make(map[string]OperationRequirement, len(req.Operations))
		for opName, op := range req.Operations {
			ops[opName] = OperationRequirement{InputSchemaHash: op.InputSchemaHash}
		}
		apps[appName] = AppRequirement{
			Version:    req.Version,
			Operations: ops,
		}
	}
	return Requires{Apps: apps}
}

func normalizeEntryPublicationKindForEquality(entry Entry) Entry {
	if entry.PublicationKind == "" {
		entry.PublicationKind = PublicationKindGitHub
	}
	return entry
}

// EntriesEqualIgnoringPublishedAt reports whether two entries are identical except
// for publishedAt and publishStartedAt, which are ignored for idempotent republish checks.
func EntriesEqualIgnoringPublishedAt(a, b Entry) bool {
	a = normalizeEntryPublicationKindForEquality(a)
	b = normalizeEntryPublicationKindForEquality(b)
	a.PublishedAt = time.Time{}
	b.PublishedAt = time.Time{}
	a.PublishStartedAt = nil
	b.PublishStartedAt = nil
	aData, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bData, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return bytes.Equal(aData, bData)
}

// EntryFileEquivalentIgnoringPublishedAt compares a local entry JSON file with
// existing registry bytes, ignoring publishedAt differences.
func EntryFileEquivalentIgnoringPublishedAt(localPath string, existing []byte) (bool, error) {
	localData, err := os.ReadFile(localPath)
	if err != nil {
		return false, fmt.Errorf("read local entry %s: %w", localPath, err)
	}
	localEntry, err := DecodeEntry(localData)
	if err != nil {
		return false, fmt.Errorf("decode local entry: %w", err)
	}
	existingEntry, err := DecodeEntry(existing)
	if err != nil {
		return false, fmt.Errorf("decode existing entry: %w", err)
	}
	return EntriesEqualIgnoringPublishedAt(*localEntry, *existingEntry), nil
}

func validateEntry(entry *Entry) error {
	if entry == nil {
		return fmt.Errorf("registry entry is required")
	}
	if entry.SchemaVersion != EntrySchemaVersion {
		return fmt.Errorf("unsupported registry entry schema version %d", entry.SchemaVersion)
	}
	if strings.TrimSpace(entry.App) == "" {
		return fmt.Errorf("registry entry app is required")
	}
	if err := source.ValidateVersion(strings.TrimSpace(entry.Version)); err != nil {
		return fmt.Errorf("registry entry version: %w", err)
	}
	if err := validateEntrySourceRef(entry); err != nil {
		return err
	}
	if strings.TrimSpace(entry.ManifestPath) == "" {
		return fmt.Errorf("registry entry manifestPath is required")
	}
	if err := validateEntryRepositoryField(entry); err != nil {
		return fmt.Errorf("registry entry repository: %w", err)
	}
	if err := validatePublication(entry.Publication); err != nil {
		return fmt.Errorf("registry entry publication: %w", err)
	}
	if err := validateEntryPublicationMetadata(entry); err != nil {
		return err
	}
	if len(entry.Artifacts) == 0 {
		return fmt.Errorf("registry entry artifacts are required")
	}
	for target, artifact := range entry.Artifacts {
		if err := validateArtifactPlatform(target); err != nil {
			return fmt.Errorf("registry entry artifact: %w", err)
		}
		if strings.TrimSpace(artifact.URL) == "" || strings.TrimSpace(artifact.PublicURL) == "" {
			return fmt.Errorf("registry entry artifact %q URLs are required", target)
		}
		if strings.TrimSpace(artifact.SHA256) == "" {
			return fmt.Errorf("registry entry artifact %q sha256 is required", target)
		}
	}
	return nil
}

func validateIndex(index *Index) error {
	if index == nil {
		return fmt.Errorf("app registry index is required")
	}
	if index.SchemaVersion != IndexSchemaVersion {
		return fmt.Errorf("unsupported app registry index schema version %d", index.SchemaVersion)
	}
	if index.Apps == nil {
		return fmt.Errorf("app registry index apps are required")
	}
	for appName, app := range index.Apps {
		if strings.TrimSpace(appName) == "" {
			return fmt.Errorf("app registry index app name is required")
		}
		if len(app.Versions) == 0 {
			return fmt.Errorf("app registry index app %q has no versions", appName)
		}
		for version := range app.Versions {
			release := app.Versions[version]
			if err := source.ValidateVersion(version); err != nil {
				return fmt.Errorf("app registry index app %q version %q is invalid: %w", appName, version, err)
			}
			if strings.TrimSpace(release.Metadata) == "" {
				return fmt.Errorf("app registry index app %q version %q metadata is required", appName, version)
			}
			if release.PublishedAt.IsZero() {
				return fmt.Errorf("app registry index app %q version %q publishedAt is required", appName, version)
			}
			if err := validateIndexVersionSourceRef(appName, version, release); err != nil {
				return err
			}
			if err := validatePublication(release.Publication); err != nil {
				return fmt.Errorf("app registry index app %q version %q publication: %w", appName, version, err)
			}
		}
	}
	return nil
}

func DecodeIndex(data []byte) (*Index, error) {
	var index Index
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("decode app registry index: %w", err)
	}
	if err := validateIndex(&index); err != nil {
		return nil, err
	}
	return &index, nil
}

func DecodeEntry(data []byte) (*Entry, error) {
	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("decode app registry entry: %w", err)
	}
	if err := validateEntry(&entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

func NewEmptyIndex() *Index {
	return &Index{
		SchemaVersion: IndexSchemaVersion,
		Apps:          map[string]AppVersions{},
	}
}

func artifactPlatforms(artifacts map[string]Artifact) []string {
	platforms := make([]string, 0, len(artifacts))
	for target := range artifacts {
		platforms = append(platforms, target)
	}
	return platforms
}

func applyAppIndexAppMetadata(app *AppVersions, displayName, description string) bool {
	changed := false
	if displayName = strings.TrimSpace(displayName); displayName != "" && app.DisplayName != displayName {
		app.DisplayName = displayName
		changed = true
	}
	if description = strings.TrimSpace(description); description != "" && app.Description != description {
		app.Description = description
		changed = true
	}
	return changed
}

func indexVersionFromEntry(entry Entry, metadataPath string) IndexVersion {
	platforms := artifactPlatforms(entry.Artifacts)
	sort.Strings(platforms)
	indexVersion := IndexVersion{
		Metadata:          strings.TrimSpace(metadataPath),
		Platforms:         platforms,
		PublishedAt:       entry.PublishedAt.UTC(),
		SourceRef:         strings.TrimSpace(entry.SourceRef),
		Repository:        strings.TrimSpace(entry.Repository),
		Publication:       clonePublication(entry.Publication),
		PublicationKind:   entry.PublicationKind,
		PublishID:         strings.TrimSpace(entry.PublishID),
		BuilderVersion:    strings.TrimSpace(entry.BuilderVersion),
		DeclarationDigest: strings.TrimSpace(entry.DeclarationDigest),
		LocalSource:       cloneLocalSourceState(entry.LocalSource),
	}
	if entry.PublishStartedAt != nil && !entry.PublishStartedAt.IsZero() {
		startedAt := entry.PublishStartedAt.UTC()
		indexVersion.PublishStartedAt = &startedAt
	}
	return indexVersion
}

func indexVersionsEqual(a, b IndexVersion) bool {
	aData, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bData, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return bytes.Equal(aData, bData)
}

// indexVersionsEqualIgnoringPublishedAt reports whether two index rows name the
// same published version. publishedAt and publishStartedAt are facts of the
// first writer, not identity, so matching republishes keep the stored clock.
func indexVersionsEqualIgnoringPublishedAt(a, b IndexVersion) bool {
	a.PublishedAt = time.Time{}
	b.PublishedAt = time.Time{}
	a.PublishStartedAt = nil
	b.PublishStartedAt = nil
	return indexVersionsEqual(a, b)
}

// UpsertAppIndex updates the per-app index for a published version. The second
// return value reports whether the index was modified. A matching republish
// (same declaration identity, different clock) keeps the first writer's
// publishedAt and is not a conflict.
func UpsertAppIndex(index *Index, entry Entry, metadataPath string, displayName, description string) (*Index, bool, error) {
	if index == nil {
		index = &Index{
			SchemaVersion: IndexSchemaVersion,
			Apps:          map[string]AppVersions{},
		}
	}
	if index.SchemaVersion == 0 {
		index.SchemaVersion = IndexSchemaVersion
	}
	if index.Apps == nil {
		index.Apps = map[string]AppVersions{}
	}
	appName := strings.TrimSpace(entry.App)
	app := index.Apps[appName]
	if app.Versions == nil {
		app.Versions = map[string]IndexVersion{}
	}
	if existing, ok := app.Versions[entry.Version]; ok {
		if strings.TrimSpace(existing.Metadata) != strings.TrimSpace(metadataPath) {
			return nil, false, fmt.Errorf("app %q version %q is already indexed", appName, entry.Version)
		}
		expected := indexVersionFromEntry(entry, metadataPath)
		if !indexVersionsEqualIgnoringPublishedAt(existing, expected) {
			return nil, false, fmt.Errorf("app %q version %q index identity mismatch: %w; %s", appName, entry.Version, ErrIndexVersionConflict, RepublishCorruptObjectGuidance)
		}
		changed := applyAppIndexAppMetadata(&app, displayName, description)
		if !changed {
			return index, false, nil
		}
		index.Apps[appName] = app
		if err := validateIndex(index); err != nil {
			return nil, false, err
		}
		return index, true, nil
	}
	applyAppIndexAppMetadata(&app, displayName, description)
	app.Versions[entry.Version] = indexVersionFromEntry(entry, metadataPath)
	index.Apps[appName] = app
	if err := validateIndex(index); err != nil {
		return nil, false, err
	}
	return index, true, nil
}

func AppArtifactPrefix(appName, version string) string {
	return path.Join("apps", appName, "artifacts", version)
}

type PublishLayout struct {
	AppName        string
	ArtifactPrefix string
	EntryPath      string
	IndexPath      string
}

func ResolvePublishLayout(source, version string) (PublishLayout, error) {
	appName, _, err := parseAppSource(source)
	if err != nil {
		return PublishLayout{}, err
	}
	return PublishLayout{
		AppName:        appName,
		ArtifactPrefix: AppArtifactPrefix(appName, version),
		EntryPath:      AppVersionEntryPath(appName, version),
		IndexPath:      AppIndexPath(appName),
	}, nil
}

func AppVersionEntryPath(appName, version string) string {
	return path.Join("apps", appName, "versions", version+".json")
}

func AppIndexPath(appName string) string {
	return path.Join("apps", appName, IndexFileName)
}

func GlobalIndexPath() string {
	return IndexFileName
}

func PublicURL(publicRoot, rel string) string {
	return strings.TrimRight(strings.TrimSpace(publicRoot), "/") + "/" + strings.TrimLeft(rel, "/")
}

func StorageURL(storageRoot, rel string) string {
	return strings.TrimRight(strings.TrimSpace(storageRoot), "/") + "/" + strings.TrimLeft(rel, "/")
}

func validateEntryRepository(repository, appName string) error {
	repository = strings.TrimSpace(repository)
	appName = strings.TrimSpace(appName)
	if repository == "" {
		return fmt.Errorf("is required")
	}
	parsedApp, parsedRepository, err := parseAppSource(AppSourceAddress(repository, appName))
	if err != nil {
		return err
	}
	if parsedRepository != repository {
		return fmt.Errorf("must be host/owner/repo")
	}
	if parsedApp != appName {
		return fmt.Errorf("does not match app %q", appName)
	}
	return nil
}

func InputSchemaHash(schema json.RawMessage) string {
	if len(schema) == 0 {
		return ""
	}
	sum := sha256.Sum256(schema)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func cloneRawJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	out := make(json.RawMessage, len(raw))
	copy(out, raw)
	return out
}

func clonePublication(value *Publication) *Publication {
	if value == nil {
		return nil
	}
	out := *value
	if value.TriggerPullRequest != nil {
		trigger := *value.TriggerPullRequest
		out.TriggerPullRequest = &trigger
	}
	if value.TriggerCommit != nil {
		trigger := *value.TriggerCommit
		out.TriggerCommit = &trigger
	}
	return &out
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}

func CatalogOperationContracts(cat *catalog.Catalog) map[string]OperationContract {
	if cat == nil {
		return nil
	}
	out := make(map[string]OperationContract, len(cat.Operations))
	for i := range cat.Operations {
		op := cat.Operations[i]
		id := strings.TrimSpace(op.ID)
		if id == "" {
			continue
		}
		out[id] = OperationContract{
			InputSchema:  cloneRawJSON(op.InputSchema),
			OutputSchema: cloneRawJSON(op.OutputSchema),
		}
	}
	return out
}
