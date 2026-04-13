package override

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/valon-technologies/gestalt/server/internal/pluginsource"
)

const ManifestFile = "manifest.yaml"

type Resolver struct {
	Root string
	Next pluginsource.Resolver
}

func (r *Resolver) Resolve(ctx context.Context, src pluginsource.Source, version string) (*pluginsource.ResolvedPackage, error) {
	if pkg, ok, err := r.resolveLocal(src); err != nil {
		return nil, err
	} else if ok {
		return pkg, nil
	}
	if r.Next == nil {
		return nil, fmt.Errorf("pluginsource override: no fallback resolver configured for %s@%s", src.String(), version)
	}
	return r.Next.Resolve(ctx, src, version)
}

func (r *Resolver) ListPlatformArchives(ctx context.Context, src pluginsource.Source, version string) ([]pluginsource.PlatformArchive, error) {
	if _, ok, err := r.resolveLocal(src); err != nil {
		return nil, err
	} else if ok {
		return nil, nil
	}
	enumerator, ok := r.Next.(pluginsource.PlatformEnumerator)
	if !ok {
		return nil, nil
	}
	return enumerator.ListPlatformArchives(ctx, src, version)
}

func (r *Resolver) resolveLocal(src pluginsource.Source) (*pluginsource.ResolvedPackage, bool, error) {
	if r == nil || r.Root == "" {
		return nil, false, nil
	}

	localPath := filepath.Join(r.Root, src.Owner, src.Repo, filepath.FromSlash(src.PackagePath()))
	info, err := os.Stat(localPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("pluginsource override: stat %s: %w", localPath, err)
	}
	if !info.IsDir() {
		return nil, false, fmt.Errorf("pluginsource override: %s is not a directory", localPath)
	}

	manifestPath := filepath.Join(localPath, ManifestFile)
	if _, err := os.Stat(manifestPath); err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("pluginsource override: stat %s: %w", manifestPath, err)
	}

	return &pluginsource.ResolvedPackage{
		LocalPath: localPath,
		Cleanup:   func() {},
	}, true, nil
}
