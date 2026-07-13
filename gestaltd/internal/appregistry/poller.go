package appregistry

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

const DefaultCatalogPollInterval = time.Minute

type CatalogPoller struct {
	ChangeRequests   *coredata.AppVersionChangeRequestService
	Materializations *coredata.AppInstanceMaterializationService
	InstanceID       string
	Interval         time.Duration
	Now              func() time.Time

	startOnce sync.Once
	passMu    sync.Mutex
	mu        sync.Mutex
	inflight  map[string]struct{}
}

type CatalogPollerConfig struct {
	ChangeRequests   *coredata.AppVersionChangeRequestService
	Materializations *coredata.AppInstanceMaterializationService
	InstanceID       string
	Interval         time.Duration
	Now              func() time.Time
}

func NewCatalogPoller(cfg CatalogPollerConfig) *CatalogPoller {
	return &CatalogPoller{
		ChangeRequests:   cfg.ChangeRequests,
		Materializations: cfg.Materializations,
		InstanceID:       strings.TrimSpace(cfg.InstanceID),
		Interval:         cfg.Interval,
		Now:              cfg.Now,
		inflight:         make(map[string]struct{}),
	}
}

func ResolveInstanceID() string {
	host, err := os.Hostname()
	if err == nil {
		host = strings.TrimSpace(host)
		if host != "" {
			return host
		}
	}
	return uuid.NewString()
}

func (p *CatalogPoller) Start(ctx context.Context) {
	if p == nil || p.ChangeRequests == nil || p.Materializations == nil {
		return
	}
	p.startOnce.Do(func() {
		if strings.TrimSpace(p.InstanceID) == "" {
			p.InstanceID = ResolveInstanceID()
		}
		go p.loop(ctx)
	})
}

func (p *CatalogPoller) loop(ctx context.Context) {
	p.runOnce(ctx)
	ticker := time.NewTicker(p.pollInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.runOnce(context.WithoutCancel(ctx))
		}
	}
}

func (p *CatalogPoller) runOnce(ctx context.Context) {
	if !p.passMu.TryLock() {
		return
	}
	defer p.passMu.Unlock()

	if err := p.ReconcileOnce(ctx); err != nil {
		slog.Warn("app registry catalog poll finished with errors", "error", err)
	}
}

func (p *CatalogPoller) ReconcileOnce(ctx context.Context) error {
	if p == nil {
		return nil
	}
	if p.ChangeRequests == nil || p.Materializations == nil {
		return fmt.Errorf("app registry catalog poller is not configured")
	}
	instanceID := strings.TrimSpace(p.InstanceID)
	if instanceID == "" {
		return fmt.Errorf("app registry catalog poller: instance id is required")
	}

	known, err := p.ChangeRequests.ListAllKnownVersions(ctx)
	if err != nil {
		return fmt.Errorf("list catalog known versions: %w", err)
	}

	var firstErr error
	for _, installation := range known {
		if err := p.reconcileInstallation(ctx, instanceID, installation); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (p *CatalogPoller) reconcileInstallation(ctx context.Context, instanceID string, installation *core.AppInstallation) error {
	if installation == nil {
		return nil
	}
	appName := strings.TrimSpace(installation.AppName)
	version := strings.TrimSpace(installation.Version)
	if appName == "" || version == "" {
		return nil
	}
	key := appName + "\x00" + version
	if !p.beginInflight(key) {
		return nil
	}
	defer p.endInflight(key)

	alreadyAcked, err := p.Materializations.HasAcknowledged(ctx, instanceID, appName, version)
	if err != nil {
		return fmt.Errorf("check ack for %s@%s: %w", appName, version, err)
	}
	if alreadyAcked {
		return nil
	}

	acknowledgedAt := p.now()
	if _, err := p.Materializations.Acknowledge(ctx, &core.AppInstanceMaterialization{
		InstanceID:     instanceID,
		App:            appName,
		Version:        version,
		AcknowledgedAt: acknowledgedAt,
	}); err != nil {
		return fmt.Errorf("acknowledge %s@%s: %w", appName, version, err)
	}
	return nil
}

func (p *CatalogPoller) beginInflight(key string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.inflight == nil {
		p.inflight = make(map[string]struct{})
	}
	if _, ok := p.inflight[key]; ok {
		return false
	}
	p.inflight[key] = struct{}{}
	return true
}

func (p *CatalogPoller) endInflight(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.inflight, key)
}

func (p *CatalogPoller) pollInterval() time.Duration {
	if p != nil && p.Interval > 0 {
		return p.Interval
	}
	return DefaultCatalogPollInterval
}

func (p *CatalogPoller) now() time.Time {
	if p != nil && p.Now != nil {
		return p.Now().UTC().Truncate(time.Millisecond)
	}
	return time.Now().UTC().Truncate(time.Millisecond)
}
