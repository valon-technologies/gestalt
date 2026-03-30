package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/drivers/auth/google"
	"github.com/valon-technologies/gestalt/server/internal/drivers/auth/local"
	authnone "github.com/valon-technologies/gestalt/server/internal/drivers/auth/none"
	"github.com/valon-technologies/gestalt/server/internal/drivers/auth/oidc"
	"github.com/valon-technologies/gestalt/server/internal/drivers/bindings/proxy"
	"github.com/valon-technologies/gestalt/server/internal/drivers/bindings/webhook"
	dynamodbstore "github.com/valon-technologies/gestalt/server/internal/drivers/datastore/dynamodb"
	"github.com/valon-technologies/gestalt/server/internal/drivers/datastore/firestore"
	"github.com/valon-technologies/gestalt/server/internal/drivers/datastore/mongodb"
	"github.com/valon-technologies/gestalt/server/internal/drivers/datastore/mysql"
	"github.com/valon-technologies/gestalt/server/internal/drivers/datastore/oracle"
	"github.com/valon-technologies/gestalt/server/internal/drivers/datastore/postgres"
	"github.com/valon-technologies/gestalt/server/internal/drivers/datastore/sqlite"
	"github.com/valon-technologies/gestalt/server/internal/drivers/datastore/sqlserver"
	secretsenv "github.com/valon-technologies/gestalt/server/internal/drivers/secrets/env"
	secretsfile "github.com/valon-technologies/gestalt/server/internal/drivers/secrets/file"
	secretsgcp "github.com/valon-technologies/gestalt/server/internal/drivers/secrets/gcp"
	telemetrynoop "github.com/valon-technologies/gestalt/server/internal/drivers/telemetry/noop"
	telemetryotlp "github.com/valon-technologies/gestalt/server/internal/drivers/telemetry/otlp"
	telemetrystdout "github.com/valon-technologies/gestalt/server/internal/drivers/telemetry/stdout"
)

type bootstrapEnv struct {
	Ctx    context.Context
	Stop   context.CancelFunc
	Config *config.Config
	Result *bootstrap.Result
}

func setupBootstrap(configFlag string, locked bool) (*bootstrapEnv, error) {
	_, cfg, preparedProviders, err := loadConfigForExecution(configFlag, locked)
	if err != nil {
		return nil, err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	factories := buildFactories(preparedProviders)

	result, err := bootstrap.Bootstrap(ctx, cfg, factories)
	if err != nil {
		stop()
		return nil, fmt.Errorf("bootstrap: %v", err)
	}
	logDatastoreWarnings(result.Datastore)

	if err := result.Datastore.Migrate(ctx); err != nil {
		_ = result.Datastore.Close()
		if closer, ok := result.SecretManager.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
		stop()
		return nil, fmt.Errorf("running datastore migrations: %v", err)
	}

	return &bootstrapEnv{
		Ctx:    ctx,
		Stop:   stop,
		Config: cfg,
		Result: result,
	}, nil
}

func (e *bootstrapEnv) Close() {
	e.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
	defer cancel()
	_ = e.Result.Close(ctx)
}

func buildFactories(preparedProviders map[string]string) *bootstrap.FactoryRegistry {
	factories := bootstrap.NewFactoryRegistry()
	factories.Telemetry["noop"] = telemetrynoop.Factory
	factories.Telemetry["stdout"] = telemetrystdout.Factory
	factories.Telemetry["otlp"] = telemetryotlp.Factory
	factories.Auth["google"] = google.Factory
	factories.Auth["local"] = local.Factory
	factories.Auth["none"] = authnone.Factory
	factories.Auth["oidc"] = oidc.Factory
	factories.Datastores["sqlite"] = sqlite.Factory
	factories.Datastores["postgres"] = postgres.Factory
	factories.Datastores["mysql"] = mysql.Factory
	factories.Datastores["dynamodb"] = dynamodbstore.Factory
	factories.Datastores["mongodb"] = mongodb.Factory
	factories.Datastores["oracle"] = oracle.Factory
	factories.Datastores["firestore"] = firestore.Factory
	factories.Datastores["sqlserver"] = sqlserver.Factory
	factories.DefaultProvider = defaultProviderFactory(preparedProviders)
	factories.Bindings["webhook"] = webhook.Factory
	factories.Bindings["proxy"] = proxy.Factory
	factories.Secrets["env"] = secretsenv.Factory
	factories.Secrets["file"] = secretsfile.Factory
	factories.Secrets["gcp_secret_manager"] = secretsgcp.Factory
	return factories
}

func resolveConfigPath(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if envPath := os.Getenv("GESTALT_CONFIG"); envPath != "" {
		return envPath
	}
	if _, err := os.Stat("config.yaml"); err == nil {
		return "config.yaml"
	}
	if p := defaultLocalConfigPath(); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "/etc/gestalt/config.yaml"
}

func logDatastoreWarnings(ds core.Datastore) {
	type warner interface {
		Warnings() []string
	}
	if w, ok := ds.(warner); ok {
		for _, msg := range w.Warnings() {
			slog.Warn(msg)
		}
	}
}

const gracefulShutdownTimeout = 15 * time.Second
