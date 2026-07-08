package gestalt

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	"github.com/valon-technologies/gestalt/sdk/go/migrations"
)

// Aliases to the migrations types in sdk/go/migrations.
//
//nolint:revive // grouped aliases documented at their canonical definitions
type (
	StoreDeclaration    = migrations.StoreDeclaration
	AddIndexDeclaration = migrations.AddIndexDeclaration
	IndexRef            = migrations.IndexRef
	SchemaDeclaration   = migrations.SchemaDeclaration
	SchemaRevision      = migrations.SchemaRevision
	BackfillTransform   = migrations.BackfillTransform
	BackfillRevision    = migrations.BackfillRevision
	MigrationRevision   = migrations.Revision
	MigrationRunOptions = migrations.RunOptions
	MigrationResult     = migrations.Result
	MigrationError      = migrations.MigrationError
)

// RunMigrations runs declared IndexedDB migrations against an open database.
func RunMigrations(ctx context.Context, db indexeddb.Database, opts migrations.RunOptions) (migrations.Result, error) {
	return migrations.Run(ctx, db, opts)
}

// RunMigrationsWithBinding opens an IndexedDB binding, runs migrations, and closes the database.
func RunMigrationsWithBinding(ctx context.Context, binding string, opts migrations.RunOptions) (migrations.Result, error) {
	db, err := IndexedDB(ctx, binding)
	if err != nil {
		return migrations.Result{}, fmt.Errorf("migrations: open indexeddb: %w", err)
	}
	defer db.Close()
	return migrations.Run(ctx, db, opts)
}
