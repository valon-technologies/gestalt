package pluginpkg

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	pluginmanifestv1 "github.com/valon-technologies/gestalt/sdk/pluginmanifest/v1"
)

func ReadPackageManifest(packagePath string) ([]byte, *pluginmanifestv1.Manifest, error) {
	file, err := os.Open(packagePath)
	if err != nil {
		return nil, nil, fmt.Errorf("open package %q: %w", packagePath, err)
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return nil, nil, fmt.Errorf("open gzip stream: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("read tar stream: %w", err)
		}
		if hdr.FileInfo().IsDir() {
			continue
		}
		name := path.Clean(strings.TrimPrefix(hdr.Name, "./"))
		if name != ManifestFile {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", ManifestFile, err)
		}
		manifest, err := DecodeManifest(data)
		if err != nil {
			return nil, nil, err
		}
		return data, manifest, nil
	}

	return nil, nil, fmt.Errorf("package %q does not contain %s", packagePath, ManifestFile)
}

func ReadManifestFile(path string) ([]byte, *pluginmanifestv1.Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read manifest %q: %w", path, err)
	}
	manifest, err := DecodeManifest(data)
	if err != nil {
		return nil, nil, err
	}
	return data, manifest, nil
}

func LoadManifestFromPath(inputPath string) ([]byte, *pluginmanifestv1.Manifest, string, error) {
	info, err := os.Stat(inputPath)
	if err != nil {
		return nil, nil, "", fmt.Errorf("stat %q: %w", inputPath, err)
	}
	if info.IsDir() {
		manifestPath := filepath.Join(inputPath, ManifestFile)
		data, manifest, err := ReadManifestFile(manifestPath)
		return data, manifest, manifestPath, err
	}
	if filepath.Base(inputPath) == ManifestFile {
		data, manifest, err := ReadManifestFile(inputPath)
		return data, manifest, inputPath, err
	}
	data, manifest, err := ReadPackageManifest(inputPath)
	return data, manifest, inputPath, err
}

func CreatePackageFromDir(sourceDir, outputPath string) error {
	sourceDir = filepath.Clean(sourceDir)
	if _, err := ValidatePackageDir(sourceDir); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	out, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create package %q: %w", outputPath, err)
	}
	defer func() {
		_ = out.Close()
	}()

	gzw := gzip.NewWriter(out)
	defer func() {
		_ = gzw.Close()
	}()

	tw := tar.NewWriter(gzw)
	defer func() {
		_ = tw.Close()
	}()

	var files []string
	err = filepath.WalkDir(sourceDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(sourceDir, p)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk source dir: %w", err)
	}
	slices.Sort(files)

	for _, rel := range files {
		absPath := filepath.Join(sourceDir, filepath.FromSlash(rel))
		info, err := os.Stat(absPath)
		if err != nil {
			return fmt.Errorf("stat %s: %w", rel, err)
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("build tar header for %s: %w", rel, err)
		}
		hdr.Name = rel
		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("write tar header for %s: %w", rel, err)
		}
		f, err := os.Open(absPath)
		if err != nil {
			return fmt.Errorf("open %s: %w", rel, err)
		}
		if _, err := io.Copy(tw, f); err != nil {
			_ = f.Close()
			return fmt.Errorf("write %s: %w", rel, err)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("close %s: %w", rel, err)
		}
	}

	return nil
}

func ExtractPackage(packagePath, destDir string) error {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create destination dir: %w", err)
	}

	file, err := os.Open(packagePath)
	if err != nil {
		return fmt.Errorf("open package %q: %w", packagePath, err)
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("open gzip stream: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar stream: %w", err)
		}

		rel, err := archiveEntryPath(hdr.Name)
		if err != nil {
			return err
		}
		target := filepath.Join(destDir, filepath.FromSlash(rel))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)); err != nil {
				return fmt.Errorf("create directory %s: %w", rel, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("create parent dir for %s: %w", rel, err)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode))
			if err != nil {
				return fmt.Errorf("create file %s: %w", rel, err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				return fmt.Errorf("extract file %s: %w", rel, err)
			}
			if err := f.Close(); err != nil {
				return fmt.Errorf("close file %s: %w", rel, err)
			}
		default:
			return fmt.Errorf("unsupported tar entry type for %s", rel)
		}
	}
}

func ValidatePackageDir(sourceDir string) (*pluginmanifestv1.Manifest, error) {
	_, manifest, err := loadManifestFromDir(sourceDir)
	if err != nil {
		return nil, err
	}
	for _, artifact := range manifest.Artifacts {
		path := filepath.Join(sourceDir, filepath.FromSlash(artifact.Path))
		sum, err := fileSHA256(path)
		if err != nil {
			return nil, fmt.Errorf("validate artifact %s: %w", artifact.Path, err)
		}
		if sum != artifact.SHA256 {
			return nil, fmt.Errorf("artifact %s sha256 %s does not match manifest %s", artifact.Path, sum, artifact.SHA256)
		}
	}
	if manifest.Provider != nil && manifest.Provider.ConfigSchemaPath != "" {
		schemaPath := filepath.Join(sourceDir, filepath.FromSlash(manifest.Provider.ConfigSchemaPath))
		if _, err := os.Stat(schemaPath); err != nil {
			return nil, fmt.Errorf("validate provider config schema %s: %w", manifest.Provider.ConfigSchemaPath, err)
		}
	}
	return manifest, nil
}

func loadManifestFromDir(sourceDir string) ([]byte, *pluginmanifestv1.Manifest, error) {
	return ReadManifestFile(filepath.Join(sourceDir, ManifestFile))
}

func archiveEntryPath(name string) (string, error) {
	cleaned := path.Clean(strings.TrimPrefix(name, "./"))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("archive entry %q escapes the package root", name)
	}
	return cleaned, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}
