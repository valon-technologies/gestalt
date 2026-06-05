package operator

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

func writeMaterializedCacheEntryArchive(entryDir, outputPath string) error {
	out, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create materialized cache archive: %w", err)
	}
	defer func() { _ = out.Close() }()

	tw := tar.NewWriter(out)
	defer func() { _ = tw.Close() }()

	if err := filepath.WalkDir(entryDir, func(absPath string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if absPath == entryDir {
			return nil
		}
		rel, err := filepath.Rel(entryDir, absPath)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if _, err := validateMaterializedCacheArchivePath(rel); err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("materialized cache archive does not support symlink %s", rel)
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat materialized cache archive path %s: %w", rel, err)
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("materialized cache archive does not support non-regular file %s", rel)
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("build materialized cache archive header for %s: %w", rel, err)
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
			return fmt.Errorf("write materialized cache archive header for %s: %w", rel, err)
		}
		if info.IsDir() {
			return nil
		}
		f, err := os.Open(absPath)
		if err != nil {
			return fmt.Errorf("open materialized cache archive path %s: %w", rel, err)
		}
		if _, err := io.Copy(tw, f); err != nil {
			_ = f.Close()
			return fmt.Errorf("write materialized cache archive path %s: %w", rel, err)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("close materialized cache archive path %s: %w", rel, err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("walk materialized cache entry: %w", err)
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("close materialized cache tar stream: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close materialized cache archive: %w", err)
	}
	return nil
}

func extractMaterializedCacheEntryArchive(reader io.Reader, destDir string) error {
	tr := tar.NewReader(reader)
	seen := map[string]struct{}{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read materialized cache tar stream: %w", err)
		}
		rel, err := validateMaterializedCacheArchivePath(hdr.Name)
		if err != nil {
			return err
		}
		if _, ok := seen[rel]; ok {
			return fmt.Errorf("materialized cache archive path %q appears more than once", rel)
		}
		seen[rel] = struct{}{}

		target := filepath.Join(destDir, filepath.FromSlash(rel))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)&0o777); err != nil {
				return fmt.Errorf("create materialized cache archive directory %s: %w", rel, err)
			}
			if err := os.Chmod(target, os.FileMode(hdr.Mode)&0o777); err != nil {
				return err
			}
		case tar.TypeReg:
			if hdr.Size < 0 {
				return fmt.Errorf("materialized cache archive file %s has invalid size", rel)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create parent for materialized cache archive file %s: %w", rel, err)
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return fmt.Errorf("create materialized cache archive file %s: %w", rel, err)
			}
			_, copyErr := io.CopyN(out, tr, hdr.Size)
			closeErr := out.Close()
			if copyErr != nil {
				return fmt.Errorf("extract materialized cache archive file %s: %w", rel, copyErr)
			}
			if closeErr != nil {
				return fmt.Errorf("close materialized cache archive file %s: %w", rel, closeErr)
			}
			if err := os.Chmod(target, os.FileMode(hdr.Mode)&0o777); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported materialized cache archive entry type for %s", rel)
		}
	}
	if _, ok := seen[materializedCacheEntryFile]; !ok {
		return fmt.Errorf("materialized cache archive missing %s", materializedCacheEntryFile)
	}
	return nil
}

func validateMaterializedCacheArchivePath(name string) (string, error) {
	if name != strings.TrimSpace(name) || name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") {
		return "", fmt.Errorf("invalid materialized cache archive path %q", name)
	}
	name = strings.TrimSuffix(name, "/")
	clean := path.Clean(name)
	if clean == "." || clean != name || strings.HasPrefix(clean, "../") || clean == ".." {
		return "", fmt.Errorf("invalid materialized cache archive path %q", name)
	}
	if clean == materializedCacheEntryFile {
		return clean, nil
	}
	if clean == materializedCacheRootDir {
		return clean, nil
	}
	if strings.HasPrefix(clean, materializedCacheRootDir+"/") {
		rel, err := validateMaterializedCachePath(strings.TrimPrefix(clean, materializedCacheRootDir+"/"))
		if err != nil {
			return "", err
		}
		return materializedCacheRootDir + "/" + rel, nil
	}
	return "", fmt.Errorf("invalid materialized cache archive path %q", name)
}
