package gestalt

import (
	"context"
	"fmt"
	"strings"
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
	if _, err := RunMigrationsWithBinding(ctx, binding, opts); err != nil {
		return fmt.Errorf("migrations: run: %w", err)
	}
	return nil
}
