package daemon

import (
	"context"
	"flag"
	"fmt"
	"io"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/operator"
	"github.com/valon-technologies/gestalt/server/internal/providerregistry"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"gopkg.in/yaml.v3"
)

type repeatedSetFlag []string

func (f *repeatedSetFlag) String() string { return strings.Join(*f, ",") }
func (f *repeatedSetFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

type boolValueFlag interface {
	IsBoolFlag() bool
}

func parseInterspersed(fs *flag.FlagSet, args []string) error {
	reordered := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positionals = append(positionals, arg)
			continue
		}
		reordered = append(reordered, arg)
		if strings.Contains(arg, "=") {
			continue
		}
		name := strings.TrimLeft(arg, "-")
		if f := fs.Lookup(name); f != nil {
			if boolFlag, ok := f.Value.(boolValueFlag); ok && boolFlag.IsBoolFlag() {
				continue
			}
		}
		if i+1 < len(args) {
			i++
			reordered = append(reordered, args[i])
		}
	}
	reordered = append(reordered, positionals...)
	return fs.Parse(reordered)
}

func runProviderRepo(args []string) error {
	if len(args) == 0 {
		printProviderRepoUsage(os.Stderr)
		return flag.ErrHelp
	}
	switch args[0] {
	case "-h", "--help", "help":
		printProviderRepoUsage(os.Stderr)
		return flag.ErrHelp
	case "add":
		return runProviderRepoAdd(args[1:])
	case "list":
		return runProviderRepoList(args[1:])
	case "remove":
		return runProviderRepoRemove(args[1:])
	case "update":
		return runProviderRepoUpdate(args[1:])
	default:
		return fmt.Errorf("unknown provider repo command %q", args[0])
	}
}

func runProviderRepoAdd(args []string) error {
	fs := flag.NewFlagSet("gestaltd provider repo add", flag.ContinueOnError)
	fs.Usage = func() { printProviderRepoAddUsage(fs.Output()) }
	var configPaths repeatedStringFlag
	fs.Var(&configPaths, "config", "path to project config to update")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("usage: gestaltd provider repo add NAME URL")
	}
	name, repoURL := strings.TrimSpace(fs.Arg(0)), strings.TrimSpace(fs.Arg(1))
	if err := providerregistry.ValidateRepositoryName(name); err != nil {
		return err
	}
	parsedRepoURL, err := url.Parse(repoURL)
	if err != nil {
		return fmt.Errorf("invalid provider repository URL %q: %w", repoURL, err)
	}
	if parsedRepoURL.Scheme == "" {
		return fmt.Errorf("invalid provider repository URL %q: scheme is required", repoURL)
	}
	if len(configPaths) > 0 {
		path := operator.ResolveConfigPaths(configPaths)[0]
		return editProjectProviderRepository(path, name, repoURL, false)
	}
	storePath := providerregistry.UserRepositoryStorePath()
	store, err := providerregistry.ReadRepositoryStore(storePath)
	if err != nil {
		return err
	}
	if store.Repositories == nil {
		store.Repositories = make(map[string]providerregistry.Repository)
	}
	store.Repositories[name] = providerregistry.Repository{URL: repoURL}
	return providerregistry.WriteRepositoryStore(storePath, store)
}

func runProviderRepoRemove(args []string) error {
	fs := flag.NewFlagSet("gestaltd provider repo remove", flag.ContinueOnError)
	var configPaths repeatedStringFlag
	fs.Var(&configPaths, "config", "path to project config to update")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: gestaltd provider repo remove NAME")
	}
	name := strings.TrimSpace(fs.Arg(0))
	if len(configPaths) > 0 {
		path := operator.ResolveConfigPaths(configPaths)[0]
		return removeProjectProviderRepository(path, name)
	}
	storePath := providerregistry.UserRepositoryStorePath()
	store, err := providerregistry.ReadRepositoryStore(storePath)
	if err != nil {
		return err
	}
	delete(store.Repositories, name)
	return providerregistry.WriteRepositoryStore(storePath, store)
}

func runProviderRepoList(args []string) error {
	fs := flag.NewFlagSet("gestaltd provider repo list", flag.ContinueOnError)
	var configPaths repeatedStringFlag
	fs.Var(&configPaths, "config", "path to config file")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	repos, err := loadProviderRepositories(operator.ResolveConfigPaths(configPaths))
	if err != nil {
		return err
	}
	for _, repo := range repos {
		fmt.Printf("%s\t%s\n", repo.Name, repo.URL)
	}
	return nil
}

func runProviderRepoUpdate(args []string) error {
	fs := flag.NewFlagSet("gestaltd provider repo update", flag.ContinueOnError)
	var configPaths repeatedStringFlag
	fs.Var(&configPaths, "config", "path to config file")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	repos, err := loadProviderRepositories(operator.ResolveConfigPaths(configPaths))
	if err != nil {
		return err
	}
	cacheDir := providerregistry.UserRepositoryCacheDir()
	if cacheDir == "" {
		return fmt.Errorf("provider repository cache directory is unavailable")
	}
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return err
	}
	for _, repo := range repos {
		index, err := providerregistry.FetchIndex(context.Background(), nil, repo.URL, repo.Token)
		if err != nil {
			return fmt.Errorf("update provider repo %s: %w", repo.Name, err)
		}
		data, err := yaml.Marshal(index)
		if err != nil {
			return err
		}
		path := filepath.Join(cacheDir, repo.Name+".yaml")
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, data, 0o600); err != nil {
			return err
		}
		if err := os.Rename(tmp, path); err != nil {
			return err
		}
		fmt.Printf("updated %s\n", repo.Name)
	}
	return nil
}

func runProviderSearch(args []string) error {
	fs := flag.NewFlagSet("gestaltd provider search", flag.ContinueOnError)
	var configPaths repeatedStringFlag
	repoName := fs.String("repo", "", "provider repository name")
	fs.Var(&configPaths, "config", "path to config file")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	query := strings.ToLower(strings.Join(fs.Args(), " "))
	repos, err := loadProviderRepositories(operator.ResolveConfigPaths(configPaths))
	if err != nil {
		return err
	}
	for _, repo := range filterProviderRepositories(repos, *repoName) {
		index, err := providerregistry.FetchIndex(context.Background(), nil, repo.URL, repo.Token)
		if err != nil {
			return fmt.Errorf("search provider repo %s: %w", repo.Name, err)
		}
		for _, pkg := range slices.Sorted(maps.Keys(index.Packages)) {
			entry := index.Packages[pkg]
			haystack := strings.ToLower(pkg + " " + entry.DisplayName + " " + entry.Description)
			if query == "" || strings.Contains(haystack, query) {
				fmt.Printf("%s\t%s\t%s\n", repo.Name, pkg, entry.DisplayName)
			}
		}
	}
	return nil
}

func runProviderInfo(args []string) error {
	fs := flag.NewFlagSet("gestaltd provider info", flag.ContinueOnError)
	var configPaths repeatedStringFlag
	repoName := fs.String("repo", "", "provider repository name")
	fs.Var(&configPaths, "config", "path to config file")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: gestaltd provider info PACKAGE")
	}
	pkg := fs.Arg(0)
	repos, err := loadProviderRepositories(operator.ResolveConfigPaths(configPaths))
	if err != nil {
		return err
	}
	for _, repo := range filterProviderRepositories(repos, *repoName) {
		index, err := providerregistry.FetchIndex(context.Background(), nil, repo.URL, repo.Token)
		if err != nil {
			return fmt.Errorf("info provider repo %s: %w", repo.Name, err)
		}
		entry, ok := index.Packages[pkg]
		if !ok {
			continue
		}
		fmt.Printf("%s\t%s\t%s\n", repo.Name, pkg, entry.DisplayName)
		for _, version := range slices.Sorted(maps.Keys(entry.Versions)) {
			release := entry.Versions[version]
			fmt.Printf("  %s\t%s\t%s\t%s\n", version, release.Kind, release.Runtime, release.Metadata)
		}
		return nil
	}
	return fmt.Errorf("provider package %q not found", pkg)
}

func runProviderAdd(args []string) error {
	fs := flag.NewFlagSet("gestaltd provider add", flag.ContinueOnError)
	var configPaths repeatedStringFlag
	var setFlags repeatedSetFlag
	name := fs.String("name", "", "provider entry name")
	kind := fs.String("kind", "", "provider kind")
	version := fs.String("version", "", "version constraint")
	repoName := fs.String("repo", "", "provider repository name")
	lockfilePath := fs.String("lockfile", "", "path to lockfile")
	noLock := fs.Bool("no-lock", false, "do not update lockfile")
	sync := fs.Bool("sync", false, "materialize prepared artifacts after locking")
	exactSource := fs.Bool("exact-source", false, "write resolved provider-release metadata URL instead of package source")
	dryRun := fs.Bool("dry-run", false, "print resolved package without editing")
	fs.Var(&configPaths, "config", "path to config file")
	fs.Var(&setFlags, "set", "set shallow entry field, e.g. path=/ui")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: gestaltd provider add PACKAGE")
	}
	paths := operator.ResolveConfigPaths(configPaths)
	primary, err := ensurePrimaryProviderConfig(paths)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		paths = []string{primary}
	} else {
		paths[0] = primary
	}
	cfg, err := config.LoadAllowMissingEnvPaths(paths)
	if err != nil {
		return err
	}
	repos, err := loadProviderRepositories(paths)
	if err != nil {
		return err
	}
	resolved, err := (&providerregistry.Resolver{}).Resolve(context.Background(), providerregistry.ResolveRequest{
		Package:           fs.Arg(0),
		VersionConstraint: *version,
		RepositoryName:    *repoName,
		Repositories:      repos,
	})
	if err != nil {
		return err
	}
	if *dryRun {
		fmt.Printf("%s\t%s\t%s\t%s\n", resolved.Package, resolved.Version, resolved.Kind, resolved.MetadataURL)
		return nil
	}
	apiVersion := config.ConfigAPIVersion
	if cfg != nil && strings.TrimSpace(cfg.APIVersion) != "" {
		apiVersion = strings.TrimSpace(cfg.APIVersion)
	}
	writePackageSource := !*exactSource
	entryKind := providermanifestv1.NormalizeKind(*kind)
	if entryKind == "" {
		entryKind = resolved.Kind
	}
	if entryKind == "telemetry" || entryKind == "audit" {
		return fmt.Errorf("provider add --kind %s is not supported yet; provider-backed %s is not supported at bootstrap", entryKind, entryKind)
	}
	entryName := strings.TrimSpace(*name)
	if entryName == "" {
		entryName = sanitizeDerivedPluginKey(providerregistry.PackageName(resolved.Package))
	}
	if entryName == "" {
		return fmt.Errorf("--name is required")
	}
	setValues := parseSetFlags(setFlags)
	if entryKind == providermanifestv1.KindUI && strings.TrimSpace(setValues["path"]) == "" {
		return fmt.Errorf("provider add for kind ui requires --set path=/mount")
	}
	if err := editProviderEntry(primary, apiVersion, entryKind, entryName, resolved, *version, *repoName, writePackageSource, setValues); err != nil {
		return err
	}
	if *noLock {
		return nil
	}
	state := operator.StatePaths{LockfilePath: *lockfilePath}
	if err := lockConfigWithStatePaths(configPaths, state, "", false); err != nil {
		return err
	}
	if *sync {
		return syncConfigWithStatePaths(configPaths, state, false)
	}
	return nil
}

func runProviderRemove(args []string) error {
	fs := flag.NewFlagSet("gestaltd provider remove", flag.ContinueOnError)
	var configPaths repeatedStringFlag
	kind := fs.String("kind", "plugin", "provider kind")
	lockfilePath := fs.String("lockfile", "", "path to lockfile")
	noLock := fs.Bool("no-lock", false, "do not update lockfile")
	fs.Var(&configPaths, "config", "path to config file")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: gestaltd provider remove NAME")
	}
	paths := operator.ResolveConfigPaths(configPaths)
	if len(paths) == 0 {
		return fmt.Errorf("no config file found")
	}
	if err := removeProviderEntry(paths[0], providermanifestv1.NormalizeKind(*kind), fs.Arg(0)); err != nil {
		return err
	}
	if !*noLock {
		return lockConfigWithStatePaths(configPaths, operator.StatePaths{LockfilePath: *lockfilePath}, "", false)
	}
	return nil
}

func runProviderUpgrade(args []string) error {
	fs := flag.NewFlagSet("gestaltd provider upgrade", flag.ContinueOnError)
	var configPaths repeatedStringFlag
	kind := fs.String("kind", "", "provider kind")
	lockfilePath := fs.String("lockfile", "", "path to lockfile")
	version := fs.String("version", "", "new version constraint")
	fs.Var(&configPaths, "config", "path to config file")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("usage: gestaltd provider upgrade [NAME]")
	}
	if *version != "" && fs.NArg() != 1 {
		return fmt.Errorf("--version requires a provider name")
	}
	if *version != "" {
		paths := operator.ResolveConfigPaths(configPaths)
		if len(paths) == 0 {
			return fmt.Errorf("no config file found")
		}
		if err := updateProviderVersionConstraint(paths[0], providermanifestv1.NormalizeKind(*kind), fs.Arg(0), *version); err != nil {
			return err
		}
	}
	return lockConfigWithStatePaths(configPaths, operator.StatePaths{LockfilePath: *lockfilePath}, "", false)
}

func loadProviderRepositories(configPaths []string) ([]providerregistry.NamedRepository, error) {
	byName := map[string]providerregistry.NamedRepository{}
	order := []string{providerregistry.DefaultRepositoryName}
	for _, repo := range providerregistry.DefaultRepositories() {
		byName[repo.Name] = repo
	}
	if storePath := providerregistry.UserRepositoryStorePath(); storePath != "" {
		store, err := providerregistry.ReadRepositoryStore(storePath)
		if err != nil {
			return nil, err
		}
		for _, name := range slices.Sorted(maps.Keys(store.Repositories)) {
			repo := store.Repositories[name]
			if _, ok := byName[name]; !ok {
				order = append(order, name)
			}
			byName[name] = providerregistry.NamedRepository{Name: name, URL: repo.URL, Token: repo.Token}
		}
	}
	if len(configPaths) > 0 {
		cfg, err := config.LoadAllowMissingEnvPaths(configPaths)
		if err != nil {
			return nil, err
		}
		if cfg != nil {
			for _, name := range slices.Sorted(maps.Keys(cfg.ProviderRepositories)) {
				repo := cfg.ProviderRepositories[name]
				if _, ok := byName[name]; !ok {
					order = append(order, name)
				}
				byName[name] = providerregistry.NamedRepository{Name: name, URL: repo.URL}
			}
		}
	}
	out := make([]providerregistry.NamedRepository, 0, len(order))
	for _, name := range order {
		out = append(out, byName[name])
	}
	return out, nil
}

func filterProviderRepositories(repos []providerregistry.NamedRepository, name string) []providerregistry.NamedRepository {
	name = strings.TrimSpace(name)
	if name == "" {
		return repos
	}
	out := repos[:0]
	for _, repo := range repos {
		if repo.Name == name {
			out = append(out, repo)
		}
	}
	return out
}

func ensurePrimaryProviderConfig(paths []string) (string, error) {
	if len(paths) == 0 {
		paths = operator.ResolveConfigPaths(nil)
	}
	if len(paths) == 0 || strings.TrimSpace(paths[0]) == "" {
		return "", fmt.Errorf("no config file found")
	}
	primary := paths[0]
	if _, err := os.Stat(primary); err == nil {
		return primary, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return operator.GenerateDefaultConfig(filepath.Dir(primary))
}

func parseSetFlags(flags []string) map[string]string {
	values := make(map[string]string, len(flags))
	for _, flag := range flags {
		key, value, ok := strings.Cut(flag, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return values
}

func editProjectProviderRepository(path, name, repoURL string, dryRun bool) error {
	root, doc, err := readConfigDocument(path)
	if err != nil {
		return err
	}
	setScalar(doc, "apiVersion", config.ConfigAPIVersion)
	repos := ensureMapping(doc, "providerRepositories")
	repoNode := yamlMapping(map[string]any{"url": repoURL})
	setNode(repos, name, repoNode)
	if dryRun {
		return nil
	}
	return writeConfigDocument(path, root)
}

func removeProjectProviderRepository(path, name string) error {
	root, doc, err := readConfigDocument(path)
	if err != nil {
		return err
	}
	if repos := mappingValueNodeLocal(doc, "providerRepositories"); repos != nil {
		deleteKey(repos, name)
	}
	return writeConfigDocument(path, root)
}

func editProviderEntry(path, apiVersion, kind, name string, resolved *providerregistry.ResolvedPackage, constraint, repoName string, packageSource bool, setValues map[string]string) error {
	root, doc, err := readConfigDocument(path)
	if err != nil {
		return err
	}
	setScalar(doc, "apiVersion", apiVersion)
	entry := map[string]any{}
	if packageSource {
		source := map[string]any{"package": resolved.Package}
		sourceRepoName := ""
		if repoName != "" {
			sourceRepoName = repoName
		} else if resolved.RepositoryName != providerregistry.DefaultRepositoryName {
			sourceRepoName = resolved.RepositoryName
		}
		if sourceRepoName != "" {
			source["repo"] = sourceRepoName
			if resolved.RepositoryURL != "" {
				repos := ensureMapping(doc, "providerRepositories")
				setNode(repos, sourceRepoName, yamlMapping(map[string]any{"url": resolved.RepositoryURL}))
			}
		}
		if constraint != "" {
			source["version"] = constraint
		}
		entry["source"] = source
	} else {
		entry["source"] = resolved.MetadataURL
	}
	if kind == providermanifestv1.KindUI {
		entry["path"] = setValues["path"]
	}
	target := providerEntryCollection(doc, kind)
	setNode(target, name, yamlMapping(entry))
	return writeConfigDocument(path, root)
}

func removeProviderEntry(path, kind, name string) error {
	root, doc, err := readConfigDocument(path)
	if err != nil {
		return err
	}
	deleteKey(providerEntryCollection(doc, kind), name)
	return writeConfigDocument(path, root)
}

func updateProviderVersionConstraint(path, kind, name, version string) error {
	root, doc, err := readConfigDocument(path)
	if err != nil {
		return err
	}
	targets := providerVersionTargets(doc, kind, name)
	if len(targets) == 0 {
		if kind != "" {
			return fmt.Errorf("provider %q of kind %q not found or is not a package source", name, kind)
		}
		return fmt.Errorf("provider %q not found or is not a package source", name)
	}
	if len(targets) > 1 {
		kinds := make([]string, 0, len(targets))
		for _, target := range targets {
			kinds = append(kinds, target.kind)
		}
		slices.Sort(kinds)
		return fmt.Errorf("provider %q is ambiguous across kinds %s; pass --kind", name, strings.Join(kinds, ", "))
	}
	setScalar(targets[0].source, "version", version)
	return writeConfigDocument(path, root)
}

type providerVersionTarget struct {
	kind   string
	source *yaml.Node
}

type providerEntryCollectionRef struct {
	kind string
	node *yaml.Node
}

func providerVersionTargets(doc *yaml.Node, kind, name string) []providerVersionTarget {
	collections := providerEntryCollectionsForVersionUpdate(doc, kind)
	targets := make([]providerVersionTarget, 0, 1)
	for _, collection := range collections {
		source := packageSourceInCollection(collection.node, name)
		if source == nil {
			continue
		}
		targets = append(targets, providerVersionTarget{
			kind:   collection.kind,
			source: source,
		})
	}
	return targets
}

func providerEntryCollectionsForVersionUpdate(doc *yaml.Node, kind string) []providerEntryCollectionRef {
	if kind != "" {
		return []providerEntryCollectionRef{{
			kind: kind,
			node: providerEntryCollectionNode(doc, kind),
		}}
	}
	providers := mappingValueNodeLocal(doc, "providers")
	runtimeProviders := mappingValueNodeLocal(mappingValueNodeLocal(doc, "runtime"), "providers")
	return []providerEntryCollectionRef{
		{kind: providermanifestv1.KindPlugin, node: mappingValueNodeLocal(doc, "plugins")},
		{kind: providermanifestv1.KindAuthentication, node: mappingValueNodeLocal(providers, configKindKey(providermanifestv1.KindAuthentication))},
		{kind: providermanifestv1.KindAuthorization, node: mappingValueNodeLocal(providers, configKindKey(providermanifestv1.KindAuthorization))},
		{kind: providermanifestv1.KindExternalCredentials, node: mappingValueNodeLocal(providers, configKindKey(providermanifestv1.KindExternalCredentials))},
		{kind: providermanifestv1.KindSecrets, node: mappingValueNodeLocal(providers, configKindKey(providermanifestv1.KindSecrets))},
		{kind: "telemetry", node: mappingValueNodeLocal(providers, configKindKey("telemetry"))},
		{kind: "audit", node: mappingValueNodeLocal(providers, configKindKey("audit"))},
		{kind: providermanifestv1.KindIndexedDB, node: mappingValueNodeLocal(providers, configKindKey(providermanifestv1.KindIndexedDB))},
		{kind: providermanifestv1.KindCache, node: mappingValueNodeLocal(providers, configKindKey(providermanifestv1.KindCache))},
		{kind: providermanifestv1.KindS3, node: mappingValueNodeLocal(providers, configKindKey(providermanifestv1.KindS3))},
		{kind: providermanifestv1.KindWorkflow, node: mappingValueNodeLocal(providers, configKindKey(providermanifestv1.KindWorkflow))},
		{kind: providermanifestv1.KindAgent, node: mappingValueNodeLocal(providers, configKindKey(providermanifestv1.KindAgent))},
		{kind: providermanifestv1.KindUI, node: mappingValueNodeLocal(providers, configKindKey(providermanifestv1.KindUI))},
		{kind: providermanifestv1.KindRuntime, node: runtimeProviders},
	}
}

func providerEntryCollectionNode(doc *yaml.Node, kind string) *yaml.Node {
	switch kind {
	case "", providermanifestv1.KindPlugin:
		return mappingValueNodeLocal(doc, "plugins")
	case providermanifestv1.KindRuntime:
		return mappingValueNodeLocal(mappingValueNodeLocal(doc, "runtime"), "providers")
	default:
		return mappingValueNodeLocal(mappingValueNodeLocal(doc, "providers"), configKindKey(kind))
	}
}

func packageSourceInCollection(collection *yaml.Node, name string) *yaml.Node {
	if collection == nil {
		return nil
	}
	entry := mappingValueNodeLocal(collection, name)
	if entry == nil {
		return nil
	}
	source := mappingValueNodeLocal(entry, "source")
	if source == nil || mappingValueNodeLocal(source, "package") == nil {
		return nil
	}
	return source
}

func providerEntryCollection(doc *yaml.Node, kind string) *yaml.Node {
	switch kind {
	case "", providermanifestv1.KindPlugin:
		return ensureMapping(doc, "plugins")
	case providermanifestv1.KindUI:
		return ensureMapping(ensureMapping(doc, "providers"), "ui")
	case providermanifestv1.KindRuntime:
		return ensureMapping(ensureMapping(doc, "runtime"), "providers")
	default:
		return ensureMapping(ensureMapping(doc, "providers"), configKindKey(kind))
	}
}

func configKindKey(kind string) string {
	switch kind {
	case providermanifestv1.KindAuthentication:
		return "authentication"
	case providermanifestv1.KindAuthorization:
		return "authorization"
	case providermanifestv1.KindExternalCredentials:
		return "externalCredentials"
	default:
		return kind
	}
}

func readConfigDocument(path string) (*yaml.Node, *yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, nil, err
	}
	doc := documentNode(&root)
	if doc.Kind == 0 {
		doc.Kind = yaml.MappingNode
		root.Kind = yaml.DocumentNode
		root.Content = []*yaml.Node{doc}
	}
	if doc.Kind != yaml.MappingNode {
		return nil, nil, fmt.Errorf("config %s must be a mapping document", path)
	}
	return &root, doc, nil
}

func writeConfigDocument(path string, root *yaml.Node) error {
	data, err := yaml.Marshal(root)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func documentNode(root *yaml.Node) *yaml.Node {
	if root.Kind == yaml.DocumentNode {
		if len(root.Content) == 0 {
			root.Content = []*yaml.Node{{Kind: yaml.MappingNode}}
		}
		return root.Content[0]
	}
	return root
}

func mappingValueNodeLocal(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func ensureMapping(node *yaml.Node, key string) *yaml.Node {
	if existing := mappingValueNodeLocal(node, key); existing != nil {
		if existing.Kind == yaml.MappingNode {
			return existing
		}
		existing.Kind = yaml.MappingNode
		existing.Tag = "!!map"
		existing.Value = ""
		existing.Content = nil
		return existing
	}
	child := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, child)
	return child
}

func setScalar(node *yaml.Node, key, value string) {
	setNode(node, key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
}

func setNode(node *yaml.Node, key string, value *yaml.Node) {
	if node.Kind != yaml.MappingNode {
		node.Kind = yaml.MappingNode
		node.Tag = "!!map"
		node.Content = nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content[i+1] = value
			return
		}
	}
	node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}

func deleteKey(node *yaml.Node, key string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content = append(node.Content[:i], node.Content[i+2:]...)
			return
		}
	}
}

func yamlMapping(value map[string]any) *yaml.Node {
	data, _ := yaml.Marshal(value)
	var node yaml.Node
	_ = yaml.Unmarshal(data, &node)
	return documentNode(&node)
}

func printProviderRepoUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd provider repo <command> [flags]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Commands:")
	writeUsageLine(w, "  add      Add a provider repository")
	writeUsageLine(w, "  list     List configured provider repositories")
	writeUsageLine(w, "  remove   Remove a provider repository")
	writeUsageLine(w, "  update   Fetch and cache provider repository indexes")
}

func printProviderRepoAddUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd provider repo add NAME URL [--config PATH]")
}
