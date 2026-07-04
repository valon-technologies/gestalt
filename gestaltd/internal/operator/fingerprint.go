package operator

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

type fingerprintSkipper struct {
	matcher        gitignore.Matcher
	repoRoot       string
	outputAbs      string
	staticBuildAbs string
}

func newFingerprintSkipper(sourceDir, outputAbs, staticBuildAbs string) (fingerprintSkipper, error) {
	matcher, repoRoot, err := gitignoreMatcherForDir(sourceDir)
	if err != nil {
		return fingerprintSkipper{}, err
	}
	return fingerprintSkipper{
		matcher:        matcher,
		repoRoot:       repoRoot,
		outputAbs:      outputAbs,
		staticBuildAbs: filepath.Clean(staticBuildAbs),
	}, nil
}

func (s fingerprintSkipper) shouldSkip(path string, d os.DirEntry) bool {
	cleanPath := filepath.Clean(path)
	if s.outputAbs != "" && pathWithinRoot(s.outputAbs, cleanPath) {
		return true
	}
	if s.staticBuildAbs != "" && pathWithinRoot(s.staticBuildAbs, cleanPath) {
		return true
	}
	if d != nil && d.IsDir() && d.Name() == gitDirName {
		return true
	}
	components := gitignorePathComponents(s.repoRoot, cleanPath)
	if len(components) == 0 {
		return false
	}
	return s.matcher.Match(components, d != nil && d.IsDir())
}

type digestCollector struct {
	sourceDir string
	digests   []string
	seen      map[string]struct{}
}

func newDigestCollector(sourceDir string) *digestCollector {
	return &digestCollector{sourceDir: sourceDir, seen: map[string]struct{}{}}
}

func (c *digestCollector) addFile(path string) error {
	rel, err := filepath.Rel(c.sourceDir, path)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	if _, ok := c.seen[rel]; ok {
		return nil
	}
	c.seen[rel] = struct{}{}
	sum, err := providerpkg.FileSHA256(path)
	if err != nil {
		return err
	}
	c.digests = append(c.digests, rel+"="+sum)
	return nil
}

func (c *digestCollector) joined() string {
	slices.Sort(c.digests)
	combined := sha256.Sum256([]byte(strings.Join(c.digests, "\n")))
	return hex.EncodeToString(combined[:])
}

func (c *digestCollector) joinedOver(baseDigest string) string {
	slices.Sort(c.digests)
	combined := sha256.Sum256([]byte(baseDigest + "\n" + strings.Join(c.digests, "\n")))
	return hex.EncodeToString(combined[:])
}

func walkDigestFiles(root string, skipper fingerprintSkipper, c *digestCollector) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if skipper.shouldSkip(path, d) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		return c.addFile(path)
	})
}

func fingerprintLocalBuildInputs(sourceDir, manifestPath string, manifest *providermanifestv1.Manifest, build *providerpkg.ResolvedSourceBuild) (string, error) {
	c := newDigestCollector(sourceDir)

	if err := c.addFile(manifestPath); err != nil {
		return "", fmt.Errorf("digest manifest: %w", err)
	}
	if err := fingerprintLocalPackageSupportFiles(sourceDir, manifest, c.addFile); err != nil {
		return "", err
	}

	outputAbs := ""
	if outputRel, _, err := providerpkg.SourceBuildOutput(manifest); err == nil && outputRel != "" {
		outputAbs = filepath.Clean(filepath.Join(sourceDir, filepath.FromSlash(outputRel)))
	}
	skipper, err := newFingerprintSkipper(sourceDir, outputAbs, providerpkg.SourceStaticBuildDir(manifestPath))
	if err != nil {
		return "", err
	}

	var inputs []string
	for _, command := range build.Commands {
		inputs = append(inputs, command.Inputs...)
	}
	inputs = append(inputs, providerpkg.SourceInstallInputs(manifest)...)
	if len(inputs) == 0 {
		for _, rootAbs := range providerpkg.BuildWorkdirAbsPaths(sourceDir, build.Commands) {
			if err := walkDigestFiles(rootAbs, skipper, c); err != nil {
				return "", fmt.Errorf("digest build inputs under %q: %w", rootAbs, err)
			}
		}
	} else {
		for _, input := range inputs {
			inputAbs := filepath.Clean(filepath.Join(sourceDir, filepath.FromSlash(input)))
			info, err := os.Stat(inputAbs)
			if err != nil {
				return "", fmt.Errorf("stat build input %q: %w", input, err)
			}
			if !info.IsDir() {
				if err := c.addFile(inputAbs); err != nil {
					return "", fmt.Errorf("digest build input %q: %w", input, err)
				}
				continue
			}
			if err := walkDigestFiles(inputAbs, skipper, c); err != nil {
				return "", fmt.Errorf("digest build input %q: %w", input, err)
			}
		}
	}

	return c.joined(), nil
}

func foldInstallInputsDigest(sourceDir, manifestPath, baseDigest string, installInputs []string) (string, error) {
	skipper, err := newFingerprintSkipper(sourceDir, "", providerpkg.SourceStaticBuildDir(manifestPath))
	if err != nil {
		return "", err
	}
	c := newDigestCollector(sourceDir)
	for _, input := range installInputs {
		inputAbs := filepath.Clean(filepath.Join(sourceDir, filepath.FromSlash(input)))
		info, err := os.Stat(inputAbs)
		if err != nil {
			return "", fmt.Errorf("stat install input %q: %w", input, err)
		}
		if info.IsDir() {
			if err := walkDigestFiles(inputAbs, skipper, c); err != nil {
				return "", fmt.Errorf("digest install input %q: %w", input, err)
			}
			continue
		}
		if err := c.addFile(inputAbs); err != nil {
			return "", fmt.Errorf("digest install input %q: %w", input, err)
		}
	}
	return c.joinedOver(baseDigest), nil
}

func fingerprintLocalPackageSupportFiles(sourceDir string, manifest *providermanifestv1.Manifest, addFile func(string) error) error {
	for _, ref := range providerpkg.LocalPackageReferences(manifest) {
		path := filepath.Clean(filepath.Join(sourceDir, filepath.FromSlash(ref.Path)))
		if err := addFile(path); err != nil {
			return fmt.Errorf("digest %s: %w", ref.Description, err)
		}
	}

	staticCatalogPath := providerpkg.StaticCatalogPath(sourceDir)
	if _, err := os.Stat(staticCatalogPath); err == nil {
		if err := addFile(staticCatalogPath); err != nil {
			return fmt.Errorf("digest provider static catalog: %w", err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("digest provider static catalog: %w", err)
	}
	return nil
}
