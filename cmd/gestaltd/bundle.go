package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func runBundle(args []string) error {
	fs := flag.NewFlagSet("gestaltd bundle", flag.ContinueOnError)
	fs.Usage = func() { printBundleUsage(fs.Output()) }
	configPath := fs.String("config", "", "path to config file")
	outputDir := fs.String("output", "", "output directory for bundled artifacts")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if *configPath == "" || *outputDir == "" {
		return fmt.Errorf("usage: gestaltd bundle --config PATH --output DIR")
	}

	absConfig, err := filepath.Abs(*configPath)
	if err != nil {
		return fmt.Errorf("resolving config path: %w", err)
	}
	absOutput, err := filepath.Abs(*outputDir)
	if err != nil {
		return fmt.Errorf("resolving output path: %w", err)
	}

	refs, err := collectLocalRefs(absConfig)
	if err != nil {
		return fmt.Errorf("collecting local references: %w", err)
	}

	sourceRoot, err := computeSourceRoot(absConfig, refs)
	if err != nil {
		return err
	}

	if err := copySourceTree(sourceRoot, absOutput, absOutput); err != nil {
		return fmt.Errorf("copying source tree: %w", err)
	}

	bundledConfig, err := filepath.Rel(sourceRoot, absConfig)
	if err != nil {
		return fmt.Errorf("computing bundled config path: %w", err)
	}
	bundledConfigPath := filepath.Join(absOutput, bundledConfig)

	if err := initConfig(bundledConfigPath); err != nil {
		return fmt.Errorf("hydrating bundle: %w", err)
	}

	log.Printf("bundle written to %s", absOutput)
	log.Printf("serve with: gestaltd serve --locked --config %s", bundledConfigPath)
	return nil
}

func collectLocalRefs(configPath string) ([]string, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var raw struct {
		Integrations map[string]struct {
			IconFile  string `yaml:"icon_file"`
			Upstreams []struct {
				URL string `yaml:"url"`
			} `yaml:"upstreams"`
			Plugin *struct {
				Package string `yaml:"package"`
			} `yaml:"plugin"`
		} `yaml:"integrations"`
		Runtimes map[string]struct {
			Plugin *struct {
				Package string `yaml:"package"`
			} `yaml:"plugin"`
		} `yaml:"runtimes"`
		UI struct {
			Plugin *struct {
				Package string `yaml:"package"`
			} `yaml:"plugin"`
		} `yaml:"ui"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing config for local refs: %w", err)
	}

	baseDir := filepath.Dir(configPath)
	var refs []string

	for name, intg := range raw.Integrations {
		if intg.IconFile != "" && !filepath.IsAbs(intg.IconFile) {
			resolved := filepath.Clean(filepath.Join(baseDir, intg.IconFile))
			if _, err := os.Stat(resolved); err != nil {
				return nil, fmt.Errorf("integration %q icon_file %q: %w", name, intg.IconFile, err)
			}
			refs = append(refs, resolved)
		}

		for _, us := range intg.Upstreams {
			if us.URL != "" && !isRemoteURL(us.URL) && !filepath.IsAbs(us.URL) {
				resolved := filepath.Clean(filepath.Join(baseDir, us.URL))
				if _, err := os.Stat(resolved); err != nil {
					return nil, fmt.Errorf("integration %q upstream url %q: %w", name, us.URL, err)
				}
				refs = append(refs, resolved)
			}
		}

		if intg.Plugin != nil && intg.Plugin.Package != "" {
			pkg := intg.Plugin.Package
			if !strings.HasPrefix(pkg, "https://") && !filepath.IsAbs(pkg) {
				resolved := filepath.Clean(filepath.Join(baseDir, pkg))
				if _, err := os.Stat(resolved); err != nil {
					return nil, fmt.Errorf("integration %q plugin.package %q: %w", name, pkg, err)
				}
				refs = append(refs, resolved)
			}
		}
	}

	for name, rt := range raw.Runtimes {
		if rt.Plugin != nil && rt.Plugin.Package != "" {
			pkg := rt.Plugin.Package
			if !strings.HasPrefix(pkg, "https://") && !filepath.IsAbs(pkg) {
				resolved := filepath.Clean(filepath.Join(baseDir, pkg))
				if _, err := os.Stat(resolved); err != nil {
					return nil, fmt.Errorf("runtime %q plugin.package %q: %w", name, pkg, err)
				}
				refs = append(refs, resolved)
			}
		}
	}

	if raw.UI.Plugin != nil && raw.UI.Plugin.Package != "" {
		pkg := raw.UI.Plugin.Package
		if !strings.HasPrefix(pkg, "https://") && !filepath.IsAbs(pkg) {
			resolved := filepath.Clean(filepath.Join(baseDir, pkg))
			if _, err := os.Stat(resolved); err != nil {
				return nil, fmt.Errorf("ui plugin.package %q: %w", pkg, err)
			}
			refs = append(refs, resolved)
		}
	}

	return refs, nil
}

func computeSourceRoot(configPath string, refs []string) (string, error) {
	all := append([]string{configPath}, refs...)
	root := filepath.Dir(all[0])
	for _, p := range all[1:] {
		for !strings.HasPrefix(p, root+string(filepath.Separator)) && p != root {
			parent := filepath.Dir(root)
			if parent == root {
				return "", fmt.Errorf("cannot compute common ancestor for paths: config and referenced files have no common directory")
			}
			root = parent
		}
	}

	for _, p := range refs {
		if !strings.HasPrefix(p, root) {
			return "", fmt.Errorf("local reference %q is outside the source tree rooted at %q", p, root)
		}
	}

	return root, nil
}

func copySourceTree(src, dst, excludeDir string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		absPath, _ := filepath.Abs(path)
		if absPath == excludeDir || strings.HasPrefix(absPath, excludeDir+string(filepath.Separator)) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		if rel == ".gestalt" || strings.HasPrefix(rel, ".gestalt"+string(filepath.Separator)) {
			return filepath.SkipDir
		}
		if filepath.Base(rel) == "gestalt.lock.json" {
			return nil
		}

		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		return bundleCopyFile(path, target)
	})
}

func bundleCopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func isRemoteURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func printBundleUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd bundle --config PATH --output DIR")
	writeUsageLine(w, "")
	writeUsageLine(w, "Prepare a self-contained bundle for production deployment.")
	writeUsageLine(w, "Copies the config tree, resolves providers and plugins, and writes")
	writeUsageLine(w, "lock state. The output is runnable with gestaltd serve --locked.")
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --config    Path to the config file")
	writeUsageLine(w, "  --output    Output directory for the bundle")
}
