package pluginpkg

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func ReadPackageManifest(packagePath string) (_ []byte, _ *pluginmanifestv1.Manifest, err error) {
	var firstErr error
	for _, name := range ManifestFiles {
		data, err := ReadArchiveEntry(packagePath, name)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		format := ManifestFormatFromPath(name)
		manifest, err := DecodeManifestFormat(data, format)
		if err != nil {
			return nil, nil, err
		}
		return data, manifest, nil
	}
	return nil, nil, firstErr
}

func ReadManifestFile(p string) ([]byte, *pluginmanifestv1.Manifest, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, nil, fmt.Errorf("read manifest %q: %w", p, err)
	}
	format := ManifestFormatFromPath(p)
	manifest, err := DecodeManifestFormat(data, format)
	if err != nil {
		return nil, nil, err
	}
	return data, manifest, nil
}

func ReadSourceManifestFile(p string) ([]byte, *pluginmanifestv1.Manifest, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, nil, fmt.Errorf("read manifest %q: %w", p, err)
	}
	format := ManifestFormatFromPath(p)
	manifest, err := DecodeSourceManifestFormat(data, format)
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
		manifestPath, err := FindManifestFile(inputPath)
		if err != nil {
			return nil, nil, "", err
		}
		data, manifest, err := ReadManifestFile(manifestPath)
		return data, manifest, manifestPath, err
	}
	if IsManifestFile(inputPath) {
		data, manifest, err := ReadManifestFile(inputPath)
		return data, manifest, inputPath, err
	}
	data, manifest, err := ReadPackageManifest(inputPath)
	return data, manifest, inputPath, err
}

func CopyPackageDir(sourceDir, destDir string) error {
	sourceDir = filepath.Clean(sourceDir)
	if _, err := ValidatePackageDir(sourceDir); err != nil {
		return err
	}
	destDir = filepath.Clean(destDir)
	if isPathWithinDir(sourceDir, destDir) {
		return fmt.Errorf("output directory %q must not be inside source directory %q", destDir, sourceDir)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	return filepath.WalkDir(sourceDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(sourceDir, p)
		if err != nil {
			return err
		}
		target := filepath.Join(destDir, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		src, err := os.Open(p)
		if err != nil {
			return err
		}
		defer func() { _ = src.Close() }()
		dst, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
		if err != nil {
			return err
		}
		if _, err := io.Copy(dst, src); err != nil {
			_ = dst.Close()
			return err
		}
		return dst.Close()
	})
}

func CreatePackageFromDir(sourceDir, outputPath string) (err error) {
	sourceDir = filepath.Clean(sourceDir)
	if _, err := ValidatePackageDir(sourceDir); err != nil {
		return err
	}
	outputPath = filepath.Clean(outputPath)
	if isPathWithinDir(sourceDir, outputPath) {
		return fmt.Errorf("output archive %q must not be inside source directory %q", outputPath, sourceDir)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	out, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create package %q: %w", outputPath, err)
	}
	defer joinCloseError(&err, fmt.Sprintf("close package %q", outputPath), out)

	gzw := gzip.NewWriter(out)
	defer joinCloseError(&err, "close gzip stream", gzw)

	tw := tar.NewWriter(gzw)
	defer joinCloseError(&err, "close tar stream", tw)

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

func isPathWithinDir(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func ExtractPackage(packagePath, destDir string) (err error) {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create destination dir: %w", err)
	}

	file, err := os.Open(packagePath)
	if err != nil {
		return fmt.Errorf("open package %q: %w", packagePath, err)
	}
	defer joinCloseError(&err, fmt.Sprintf("close package %q", packagePath), file)

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("open gzip stream: %w", err)
	}
	defer joinCloseError(&err, "close gzip stream", gzr)

	tr := tar.NewReader(gzr)
	seen := make(map[string]struct{})
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
		if _, ok := seen[rel]; ok {
			return fmt.Errorf("archive entry %q appears more than once", rel)
		}
		seen[rel] = struct{}{}
		target := filepath.Join(destDir, filepath.FromSlash(rel))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)); err != nil {
				return fmt.Errorf("create directory %s: %w", rel, err)
			}
		case tar.TypeReg:
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

func ReadArchiveEntry(packagePath, wanted string) (_ []byte, err error) {
	file, err := os.Open(packagePath)
	if err != nil {
		return nil, fmt.Errorf("open package %q: %w", packagePath, err)
	}
	defer joinCloseError(&err, fmt.Sprintf("close package %q", packagePath), file)

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("open gzip stream: %w", err)
	}
	defer joinCloseError(&err, "close gzip stream", gzr)

	tr := tar.NewReader(gzr)
	var found []byte
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar stream: %w", err)
		}
		if hdr.FileInfo().IsDir() {
			continue
		}
		name, err := archiveEntryPath(hdr.Name)
		if err != nil {
			return nil, err
		}
		if name != wanted {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("archive entry %q appears more than once", wanted)
		}
		found, err = io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", wanted, err)
		}
	}
	if found == nil {
		return nil, fmt.Errorf("package %q does not contain %s", packagePath, wanted)
	}
	return found, nil
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
	for _, ref := range LocalPackageReferences(manifest) {
		refPath := filepath.Join(sourceDir, filepath.FromSlash(ref.Path))
		if _, err := os.Stat(refPath); err != nil {
			return nil, fmt.Errorf("validate %s %s: %w", ref.Description, ref.Path, err)
		}
	}
	return manifest, nil
}

func loadManifestFromDir(sourceDir string) ([]byte, *pluginmanifestv1.Manifest, error) {
	p, err := FindManifestFile(sourceDir)
	if err != nil {
		return nil, nil, err
	}
	return ReadManifestFile(p)
}

func archiveEntryPath(name string) (string, error) {
	if strings.Contains(name, "\\") {
		return "", fmt.Errorf("archive entry %q must use forward slashes", name)
	}
	cleaned := path.Clean(strings.TrimPrefix(name, "./"))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("archive entry %q escapes the package root", name)
	}
	return cleaned, nil
}

func fileSHA256(path string) (_ string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer joinCloseError(&err, fmt.Sprintf("close %q", path), f)
	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func joinCloseError(errp *error, label string, closer io.Closer) {
	if closeErr := closer.Close(); closeErr != nil {
		*errp = errors.Join(*errp, fmt.Errorf("%s: %w", label, closeErr))
	}
}
