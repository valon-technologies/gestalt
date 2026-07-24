# `gestaltd/internal/appregistry/registry.go`

Public interface for the app registry package as introduced in [gestalt#2709](https://github.com/valon-technologies/gestalt/pull/2709) commit 1. JSON shapes are defined in [models.md](./models.md). Deploy reader config is in [config.md](./config.md); `gestaltd app registry publish` passes `--bucket` on the CLI.

## PublishedVersion construction

```go
func AppSourceAddress(repository, appName string) string
// Reconstruct the manifest source address from repository and app.
//
// Example input:  "github.com/valon-technologies/valon-tools", "g-issues"
// Example output: "github.com/valon-technologies/valon-tools/apps/g-issues"

func ValidatePublishInput(manifest *providermanifestv1.Manifest, version, sourceRef string) error
// Require app kind, semver version, a 40-character lowercase sourceRef, and manifest source.
//
// Example input:  manifest{Kind: "app", Source: "github.com/valon-technologies/valon-tools/apps/g-issues"},
//                 "0.0.0-snapshot.gabc123", "abc123def456abc123def456abc123def456abcd"
// Example output: nil

func BuildPublishedVersion(input BuildPublishedVersionInput) (PublishedVersion, error)
// Assemble and validate a PublishedVersion from manifest, release metadata, and publish artifacts.
// Contract fields (interface, requires, compatibility) are read from release metadata.
//
// Example input:  BuildPublishedVersionInput{
//                   Manifest: manifest for g-issues,
//                   Version: "0.0.0-snapshot.gabc123",
//                   SourceRef: "abc123def456abc123def456abc123def456abcd",
//                   ManifestPath: "valon-tools/apps/g-issues/manifest.yaml",
//                   Release: provider release metadata with catalog snapshot,
//                   Artifacts: [{Target: "linux/amd64", StorageURL: "gs://...", SHA256: "..."}],
//                 }
// Example output: PublishedVersion{
//                   SchemaVersion: 1,
//                   App: "g-issues",
//                   Repository: "github.com/valon-technologies/valon-tools",
//                   Version: "0.0.0-snapshot.gabc123",
//                   Artifacts: {"linux/amd64": {URL: "gs://...", PublicURL: "https://...", SHA256: "..."}},
//                   Interface: {Operations: {"issues.list": {...}}},
//                 }, nil

func InterfaceFromRelease(release *providerrelease.Metadata) Interface
// Copy operation contracts from the provider release catalog snapshot.
//
// Example input:  release.StaticValidation.Catalog.Operations = [
//                   {ID: "issues.list", InputSchema: {"type":"object"}},
//                 ]
// Example output: Interface{
//                   Operations: {"issues.list": {InputSchema: {"type":"object"}}},
//                 }

func RequiresFromRelease(release *providerrelease.Metadata) Requires
// Copy requires from provider release staticValidation.requires.
//
// Example input:  release with staticValidation.requires.apps.slack.version = "^1.4.0"
// Example output: Requires{Apps: {"slack": {Version: "^1.4.0"}}}

func CompatibilityFromRelease(release *providerrelease.Metadata) Compatibility
// Copy compatibility from provider release staticValidation.compatibility.
//
// Example input:  release with staticValidation.compatibility.minGestaltdVersion = "0.20.0"
// Example output: Compatibility{MinGestaltdVersion: "0.20.0"}
```

## Validation and decoding

```go
func DecodePublishedVersion(data []byte) (*PublishedVersion, error)
// Unmarshal JSON and validate the published version.
//
// Example input:  []byte(`{"schemaVersion":1,"app":"g-issues","version":"0.0.1",...}`)
// Example output: &PublishedVersion{SchemaVersion: 1, App: "g-issues", Version: "0.0.1", ...}, nil

func DecodeIndex(data []byte) (*Index, error)
// Unmarshal JSON and validate the index.
//
// Example input:  []byte(`{"schemaVersion":1,"apps":{"g-issues":{"versions":{...}}}}`)
// Example output: &Index{SchemaVersion: 1, Apps: {...}}, nil

func NewEmptyIndex() *Index
// Return a valid empty per-app index.
//
// Example input:  (none)
// Example output: &Index{SchemaVersion: 1, Apps: {}}
```

## Index mutation

```go
func UpsertAppIndex(index *Index, publishedVersion PublishedVersion, metadataPath, displayName, description string) (*Index, bool, error)
// Update the per-app index for a published version.
//
// Example input:  NewEmptyIndex(),
//                 PublishedVersion{App: "g-issues", Version: "0.0.1", Artifacts: {"linux/amd64": {...}}, ...},
//                 "apps/g-issues/versions/0.0.1.json",
//                 "g-issues", "Issues workspace"
// Example output: Index with apps["g-issues"].versions["0.0.1"].metadata =
//                 "apps/g-issues/versions/0.0.1.json", nil
//
// Example input:  index that already contains g-issues@0.0.1 at the same metadataPath
// Example output: same index unchanged, nil
//
// Example input:  index that already contains g-issues@0.0.1 at a different metadataPath
// Example output: nil, error("app \"g-issues\" version \"0.0.1\" is already indexed")
```

## Paths and URLs

```go
func ResolvePublishLayout(source, version string) (PublishLayout, error)
// Resolve registry app id and relative object paths from manifest source.
//
// Example input:  "github.com/valon-technologies/valon-tools/apps/g-issues", "0.0.1"
// Example output: PublishLayout{
//                   AppName: "g-issues",
//                   ArtifactPrefix: "apps/g-issues/artifacts/0.0.1",
//                   PublishedVersionPath: "apps/g-issues/versions/0.0.1.json",
//                   IndexPath: "apps/g-issues/index.json",
//                 }, nil

func AppArtifactPrefix(appName, version string) string
// Return the artifact directory for one published version.
//
// Example input:  "g-issues", "0.0.0-snapshot.gabc123"
// Example output: "apps/g-issues/artifacts/0.0.0-snapshot.gabc123"

func PublishedVersionPath(appName, version string) string
// Return the relative path to a published version JSON document.
//
// Example input:  "g-issues", "0.0.0-snapshot.gabc123"
// Example output: "apps/g-issues/versions/0.0.0-snapshot.gabc123.json"

func AppIndexPath(appName string) string
// Return the relative path to a per-app index JSON document.
//
// Example input:  "g-issues"
// Example output: "apps/g-issues/index.json"

func GlobalIndexPath() string
// Return the relative path to a future global index JSON document.
//
// Example input:  (none)
// Example output: "index.json"

func StorageURL(storageRoot, rel string) string
// Join a GCS storage root with a relative registry path.
//
// Example input:  "gs://gestalt-app-registry", "apps/g-issues/index.json"
// Example output: "gs://gestalt-app-registry/apps/g-issues/index.json"

func PublicURL(publicRoot, rel string) string
// Join a public HTTPS root with a relative registry path.
//
// Example input:  "https://storage.googleapis.com/gestalt-app-registry",
//                 "apps/g-issues/versions/0.0.1.json"
// Example output: "https://storage.googleapis.com/gestalt-app-registry/apps/g-issues/versions/0.0.1.json"
```

## Utilities

```go
func InputSchemaHash(schema json.RawMessage) string
// Return sha256:{hex} for an operation input schema.
//
// Example input:  json.RawMessage(`{"type":"object"}`)
// Example output: "sha256:a2c799262a3ce3c19ef5cdd983bf3d12b43ab3c426227091b909dcb7054738c0"

func CatalogOperationContracts(cat *catalog.Catalog) map[string]OperationContract
// Convert a catalog.Catalog to map[string]OperationContract.
//
// Example input:  &catalog.Catalog{
//                   Operations: []catalog.CatalogOperation{
//                     {ID: "issues.list", InputSchema: {"type":"object"}},
//                   },
//                 }
// Example output: map[string]OperationContract{
//                   "issues.list": {InputSchema: {"type":"object"}},
//                 }
```
