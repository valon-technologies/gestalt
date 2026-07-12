package appregistry

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

// Converger lazily materializes catalog-known app versions on this instance.
type Converger struct {
	Installer *Installer
	Catalog   *coredata.AppVersionCatalogService

	mu       sync.Mutex
	inflight map[string]struct{}
}

func NewConverger(installer *Installer, catalog *coredata.AppVersionCatalogService) *Converger {
	if installer == nil || catalog == nil {
		return nil
	}
	return &Converger{
		Installer: installer,
		Catalog:   catalog,
		inflight:  make(map[string]struct{}),
	}
}

// ConvergeOnce materializes any catalog-known versions that are missing locally.
func (c *Converger) ConvergeOnce(ctx context.Context) error {
	if c == nil {
		return nil
	}
	if c.Installer == nil || c.Catalog == nil {
		return fmt.Errorf("app registry converger is not configured")
	}
	known, err := c.Catalog.ListAllKnownVersions(ctx)
	if err != nil {
		return fmt.Errorf("list catalog known versions: %w", err)
	}
	var firstErr error
	for _, installation := range known {
		if err := c.convergeInstallation(ctx, installation); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// StartBackground runs ConvergeOnce without blocking server startup.
func (c *Converger) StartBackground(ctx context.Context) {
	if c == nil {
		return
	}
	go func() {
		if err := c.ConvergeOnce(context.WithoutCancel(ctx)); err != nil {
			slog.Warn("app registry catalog convergence finished with errors", "error", err)
			return
		}
		slog.Info("app registry catalog convergence complete")
	}()
}

func (c *Converger) convergeInstallation(ctx context.Context, installation *core.AppInstallation) error {
	if installation == nil {
		return nil
	}
	appName := strings.TrimSpace(installation.AppName)
	version := strings.TrimSpace(installation.Version)
	if appName == "" || version == "" {
		return nil
	}
	key := appName + "\x00" + version
	if !c.beginInflight(key) {
		return nil
	}
	defer c.endInflight(key)

	materializedPath := LocalMaterializedPath(c.Installer.ArtifactsDir, appName, version)
	if IsLocallyMaterialized(materializedPath) {
		return nil
	}

	path, err := c.Installer.materializeKnownVersion(ctx, installation)
	if err != nil {
		slog.Warn("app registry local materialization failed",
			"app", appName,
			"version", version,
			"registry", installation.Registry,
			"error", err,
		)
		return fmt.Errorf("materialize %s@%s: %w", appName, version, err)
	}
	slog.Info("app registry local materialization complete",
		"app", appName,
		"version", version,
		"path", path,
	)
	return nil
}

func (c *Converger) beginInflight(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.inflight == nil {
		c.inflight = make(map[string]struct{})
	}
	if _, ok := c.inflight[key]; ok {
		return false
	}
	c.inflight[key] = struct{}{}
	return true
}

func (c *Converger) endInflight(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.inflight, key)
}
