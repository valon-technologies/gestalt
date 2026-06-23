package packageio

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
	"time"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func ReadPackageManifest(packagePath string) (_ []byte, _ *providermanifestv1.Manifest, err error) {
	return ReadPackageManifestIn(packagePath, ManifestFiles)
}

func ReadPackageManifestIn(packagePath string, names []string) (_ []byte, _ *providermanifestv1.Manifest, err error) {
	// Collect every candidate manifest in a single pass instead of walking
	// (and fully decompressing) the archive once per candidate name. Packages
	// using a manifest later in the priority list (e.g. manifest.yaml) would
	// otherwise pay a full decompression for each earlier miss.
	wanted := make(map[string]struct{}, len(names))
	for _, name := range names {
		wanted[name] = struct{}{}
	}
	found := make(map[string][]byte, len(names))
	if err := walkPackageArchive(packagePath, func(entry packageArchiveEntry) error {
		if entry.Header.Typeflag == tar.TypeDir {
			return nil
		}
		if _, ok := wanted[entry.Path]; !ok {
			return nil
		}
		data, err := io.ReadAll(entry.Reader)
		if err != nil {
			return fmt.Errorf("read %s: %w", entry.Path, err)
		}
		found[entry.Path] = data
		return nil
	}); err != nil {
		return nil, nil, err
	}
	for _, name := range names {
		data, ok := found[name]
		if !ok {
			continue
		}
		manifest, err := DecodeManifestFormat(data, ManifestFormatFromPath(name))
		if err != nil {
			return nil, nil, err
		}
		return data, manifest, nil
	}
	return nil, nil, fmt.Errorf("package %q does not contain a manifest (%s)", packagePath, strings.Join(names, ", "))
}

func InspectPackage(packagePath string) (*providermanifestv1.Manifest, error) {
	return InspectPackageIn(packagePath, ManifestFiles)
}

func InspectPackageIn(packagePath string, names []string) (_ *providermanifestv1.Manifest, err error) {
	rootManifests := make(map[string]struct{})
	rootManifestData := make(map[string][]byte)
	fileSums := make(map[string]string)
	var staticCatalogData []byte
	staticCatalogFound := false
	if err := walkPackageArchive(packagePath, func(entry packageArchiveEntry) error {
		if entry.Header.Typeflag == tar.TypeDir {
			return nil
		}
		isRootManifest := !strings.Contains(entry.Path, "/") && IsManifestFileIn(entry.Path, names)
		keepData := isRootManifest || entry.Path == StaticCatalogFile
		sum := sha256.New()
		var data []byte
		var err error
		if keepData {
			data, err = io.ReadAll(io.TeeReader(entry.Reader, sum))
		} else {
			_, err = io.Copy(sum, entry.Reader)
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", entry.Path, err)
		}
		fileSums[entry.Path] = hex.EncodeToString(sum.Sum(nil))
		if isRootManifest {
			rootManifests[entry.Path] = struct{}{}
			rootManifestData[entry.Path] = data
		}
		if entry.Path == StaticCatalogFile {
			staticCatalogData = data
			staticCatalogFound = true
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := validateSingleRootManifest(packagePath, rootManifests); err != nil {
		return nil, err
	}

	manifestName := ""
	for name := range rootManifests {
		manifestName = name
	}
	manifest, err := DecodeManifestFormat(rootManifestData[manifestName], ManifestFormatFromPath(manifestName))
	if err != nil {
		return nil, err
	}
	if err := validatePackageManifestReferences(manifest, fileSums); err != nil {
		return nil, err
	}
	if err := validatePackageStaticCatalog(manifest, staticCatalogData, staticCatalogFound); err != nil {
		return nil, err
	}
	return manifest, nil
}

func ReadManifestFile(p string) ([]byte, *providermanifestv1.Manifest, error) {
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

func ReadSourceManifestFile(p string) ([]byte, *providermanifestv1.Manifest, error) {
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

func LoadManifestFromPath(inputPath string) ([]byte, *providermanifestv1.Manifest, string, error) {
	return LoadManifestFromPathIn(inputPath, ManifestFiles)
}

func LoadManifestFromPathIn(inputPath string, names []string) ([]byte, *providermanifestv1.Manifest, string, error) {
	info, err := os.Stat(inputPath)
	if err != nil {
		return nil, nil, "", fmt.Errorf("stat %q: %w", inputPath, err)
	}
	if info.IsDir() {
		manifestPath, err := FindManifestFileIn(inputPath, names)
		if err != nil {
			return nil, nil, "", err
		}
		data, manifest, err := ReadManifestFile(manifestPath)
		return data, manifest, manifestPath, err
	}
	if IsManifestFileIn(inputPath, names) {
		data, manifest, err := ReadManifestFile(inputPath)
		return data, manifest, inputPath, err
	}
	data, manifest, err := ReadPackageManifestIn(inputPath, names)
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
	gzw.ModTime = time.Unix(0, 0)
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
		hdr.ModTime = time.Unix(0, 0)
		hdr.AccessTime = time.Unix(0, 0)
		hdr.ChangeTime = time.Unix(0, 0)
		hdr.Uid = 0
		hdr.Gid = 0
		hdr.Uname = ""
		hdr.Gname = ""
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

	rootManifests := make(map[string]struct{})
	if err := walkPackageArchive(packagePath, func(entry packageArchiveEntry) error {
		if entry.Header.Typeflag == tar.TypeReg && !strings.Contains(entry.Path, "/") && IsManifestFileIn(entry.Path, ManifestFiles) {
			rootManifests[entry.Path] = struct{}{}
		}
		target := filepath.Join(destDir, filepath.FromSlash(entry.Path))
		switch entry.Header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(entry.Header.Mode)); err != nil {
				return fmt.Errorf("create directory %s: %w", entry.Path, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("create parent dir for %s: %w", entry.Path, err)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(entry.Header.Mode))
			if err != nil {
				return fmt.Errorf("create file %s: %w", entry.Path, err)
			}
			if _, err := io.Copy(f, entry.Reader); err != nil {
				_ = f.Close()
				return fmt.Errorf("extract file %s: %w", entry.Path, err)
			}
			if err := f.Close(); err != nil {
				return fmt.Errorf("close file %s: %w", entry.Path, err)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return validateSingleRootManifest(packagePath, rootManifests)
}

func validateSingleRootManifest(packagePath string, rootManifests map[string]struct{}) error {
	if len(rootManifests) == 0 {
		return fmt.Errorf("package %q does not contain a root provider manifest", packagePath)
	}
	if len(rootManifests) == 1 {
		return nil
	}
	names := make([]string, 0, len(rootManifests))
	for name := range rootManifests {
		names = append(names, name)
	}
	slices.Sort(names)
	return fmt.Errorf("package %q contains multiple root provider manifests: %s", packagePath, strings.Join(names, ", "))
}

func ReadArchiveEntry(packagePath, wanted string) (_ []byte, err error) {
	var found []byte
	if err := walkPackageArchive(packagePath, func(entry packageArchiveEntry) error {
		if entry.Header.Typeflag == tar.TypeDir || entry.Path != wanted {
			return nil
		}
		if found != nil {
			return fmt.Errorf("archive entry %q appears more than once", wanted)
		}
		var err error
		found, err = io.ReadAll(entry.Reader)
		if err != nil {
			return fmt.Errorf("read %s: %w", wanted, err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if found == nil {
		return nil, fmt.Errorf("package %q does not contain %s", packagePath, wanted)
	}
	return found, nil
}

type packageArchiveEntry struct {
	Header *tar.Header
	Path   string
	Reader io.Reader
}

func walkPackageArchive(packagePath string, visit func(packageArchiveEntry) error) (err error) {
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
			break
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

		switch hdr.Typeflag {
		case tar.TypeDir, tar.TypeReg:
		default:
			return fmt.Errorf("unsupported tar entry type for %s", rel)
		}

		if err := visit(packageArchiveEntry{Header: hdr, Path: rel, Reader: tr}); err != nil {
			return err
		}
	}
	return nil
}

func ValidatePackageDir(sourceDir string) (*providermanifestv1.Manifest, error) {
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
	staticCatalog, err := ReadStaticCatalog(sourceDir, "")
	if err != nil {
		return nil, err
	}
	if staticCatalog == nil && StaticCatalogRequired(manifest) {
		return nil, fmt.Errorf("validate provider static catalog %s: file does not exist", StaticCatalogFile)
	}
	return manifest, nil
}

func validatePackageManifestReferences(manifest *providermanifestv1.Manifest, fileSums map[string]string) error {
	for _, artifact := range manifest.Artifacts {
		rel, err := packageReferencePath(artifact.Path)
		if err != nil {
			return fmt.Errorf("validate artifact %s: %w", artifact.Path, err)
		}
		sum, ok := fileSums[rel]
		if !ok {
			return fmt.Errorf("validate artifact %s: file does not exist", artifact.Path)
		}
		if sum != artifact.SHA256 {
			return fmt.Errorf("artifact %s sha256 %s does not match manifest %s", artifact.Path, sum, artifact.SHA256)
		}
	}
	for _, ref := range LocalPackageReferences(manifest) {
		rel, err := packageReferencePath(ref.Path)
		if err != nil {
			return fmt.Errorf("validate %s %s: %w", ref.Description, ref.Path, err)
		}
		if _, ok := fileSums[rel]; !ok {
			return fmt.Errorf("validate %s %s: file does not exist", ref.Description, ref.Path)
		}
	}
	return nil
}

func validatePackageStaticCatalog(manifest *providermanifestv1.Manifest, data []byte, found bool) error {
	if !found {
		if StaticCatalogRequired(manifest) {
			return fmt.Errorf("validate provider static catalog %s: file does not exist", StaticCatalogFile)
		}
		return nil
	}

	var cat catalog.Catalog
	if err := decodeStrict(data, ManifestFormatFromPath(StaticCatalogFile), "static catalog", &cat); err != nil {
		return err
	}
	if err := cat.Validate(); err != nil {
		return fmt.Errorf("validate static catalog %q: %w", StaticCatalogFile, err)
	}
	return nil
}

func packageReferencePath(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("path is required")
	}
	return archiveEntryPath(value)
}

func loadManifestFromDir(sourceDir string) ([]byte, *providermanifestv1.Manifest, error) {
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
	if cleaned == "." || path.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
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
