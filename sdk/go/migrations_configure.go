package gestalt

import (
	"context"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/go/migrations"
)

// ConfigureMigrations runs declared migrations before provider configuration when
// the provider implements [MigrationsProvider].
func ConfigureMigrations(ctx context.Context, provider Provider, name string, config map[string]any) error {
	mp, ok := provider.(MigrationsProvider)
	if !ok {
		return nil
	}
	opts, binding, err := mp.MigrationOptions(ctx, name, config)
	if err != nil {
		return fmt.Errorf("migrations: resolve options: %w", err)
	}
	if strings.TrimSpace(binding) == "" {
		if raw, ok := config["indexeddb"]; ok {
			if s, ok := raw.(string); ok {
				binding = strings.TrimSpace(s)
			}
		}
	}
	db, err := IndexedDB(ctx, binding)
	if err != nil {
		return fmt.Errorf("migrations: open indexeddb: %w", err)
	}
	defer db.Close()
	if _, err := migrations.Run(ctx, db, migrations.RunOptions{
		Revisions:      opts.Revisions,
		LedgerStore:    opts.LedgerStore,
		AppName:        name,
		WorkflowClient: opts.WorkflowClient,
	}); err != nil {
		return fmt.Errorf("migrations: run: %w", err)
	}
	return nil
}
