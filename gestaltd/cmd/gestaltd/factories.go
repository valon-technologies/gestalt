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
	authplugin "github.com/valon-technologies/gestalt/server/internal/drivers/auth/plugin"
	datastoreplugin "github.com/valon-technologies/gestalt/server/internal/drivers/datastore/plugin"
	secretsaws "github.com/valon-technologies/gestalt/server/internal/drivers/secrets/aws"
	secretsazure "github.com/valon-technologies/gestalt/server/internal/drivers/secrets/azure"
	secretsenv "github.com/valon-technologies/gestalt/server/internal/drivers/secrets/env"
	secretsfile "github.com/valon-technologies/gestalt/server/internal/drivers/secrets/file"
	secretsgoogle "github.com/valon-technologies/gestalt/server/internal/drivers/secrets/google"
	secretskeychain "github.com/valon-technologies/gestalt/server/internal/drivers/secrets/keychain"
	secretsvault "github.com/valon-technologies/gestalt/server/internal/drivers/secrets/vault"
	telemetrynoop "github.com/valon-technologies/gestalt/server/internal/drivers/telemetry/noop"
	telemetryotlp "github.com/valon-technologies/gestalt/server/internal/drivers/telemetry/otlp"
	telemetrystdout "github.com/valon-technologies/gestalt/server/internal/drivers/telemetry/stdout"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/operator"
)

type bootstrapEnv struct {
	Ctx    context.Context
	Stop   context.CancelFunc
	Config *config.Config
	Result *bootstrap.Result

	prevLogger *slog.Logger
}

func setupBootstrapWithArtifactsDir(configFlag, artifactsDir string, locked bool) (*bootstrapEnv, error) {
	_, cfg, err := loadConfigForExecutionWithArtifactsDir(configFlag, artifactsDir, locked)
	if err != nil {
		return nil, err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	factories := buildFactories()

	result, err := bootstrap.Bootstrap(ctx, cfg, factories)
	if err != nil {
		stop()
		return nil, fmt.Errorf("bootstrap: %v", err)
	}
	prevLogger := slog.Default()
	if logger := result.Telemetry.Logger(); logger != nil {
		slog.SetDefault(logger)
	}
	restoreLoggerOnError := true
	defer func() {
		if restoreLoggerOnError {
			slog.SetDefault(prevLogger)
		}
	}()
	logDatastoreWarnings(result.Datastore)

	if err := result.Datastore.Migrate(ctx); err != nil {
		stop()
		closeCtx, cancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
		defer cancel()
		_ = result.Close(closeCtx)
		return nil, fmt.Errorf("running datastore migrations: %v", err)
	}
	restoreLoggerOnError = false

	return &bootstrapEnv{
		Ctx:        ctx,
		Stop:       stop,
		Config:     cfg,
		Result:     result,
		prevLogger: prevLogger,
	}, nil
}

func (e *bootstrapEnv) Close() {
	e.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
	defer cancel()
	_ = e.Result.Close(ctx)
	if e.prevLogger != nil {
		slog.SetDefault(e.prevLogger)
	}
}

func buildFactories() *bootstrap.FactoryRegistry {
	factories := bootstrap.NewFactoryRegistry()
	factories.Telemetry["noop"] = telemetrynoop.Factory
	factories.Telemetry["stdout"] = telemetrystdout.Factory
	factories.Telemetry["otlp"] = telemetryotlp.Factory
	factories.Audit = func(ctx context.Context, cfg config.AuditConfig, telemetry core.TelemetryProvider) (core.AuditSink, func(context.Context) error, error) {
		switch cfg.Provider {
		case "", "inherit":
			return invocation.NewLoggerAuditSink(telemetry.Logger()), nil, nil
		case "noop":
			return invocation.NewLoggerAuditSink(telemetrynoop.New().Logger()), nil, nil
		case "stdout":
			var stdoutCfg struct {
				Level  string `yaml:"level"`
				Format string `yaml:"format"`
			}
			if cfg.Config.Kind != 0 {
				if err := cfg.Config.Decode(&stdoutCfg); err != nil {
					return nil, nil, fmt.Errorf("stdout audit: parsing config: %w", err)
				}
			}
			return invocation.NewLevelAwareLoggerAuditSink(telemetrystdout.NewLogger(stdoutCfg.Level, stdoutCfg.Format)), nil, nil
		case "otlp":
			logger, closeFn, err := telemetryotlp.NewAuditLogger(ctx, cfg.Config)
			if err != nil {
				return nil, nil, err
			}
			return invocation.NewLevelAwareLoggerAuditSink(logger), closeFn, nil
		default:
			return nil, nil, fmt.Errorf("unknown audit provider %q", cfg.Provider)
		}
	}
	factories.Auth["plugin"] = authplugin.Factory
	factories.Datastores["plugin"] = datastoreplugin.Factory
	factories.Secrets["env"] = secretsenv.Factory
	factories.Secrets["file"] = secretsfile.Factory
	factories.Secrets["google_secret_manager"] = secretsgoogle.Factory
	factories.Secrets["aws_secrets_manager"] = secretsaws.Factory
	factories.Secrets["vault"] = secretsvault.Factory
	factories.Secrets["azure_key_vault"] = secretsazure.Factory
	factories.Secrets["keychain"] = secretskeychain.Factory
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
	for _, p := range operator.LocalConfigPaths() {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p := operator.DefaultLocalConfigPath(); p != "" {
		return p
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
