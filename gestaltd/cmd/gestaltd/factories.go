package main

import (
	"context"
	"fmt"
	"log/slog"
	"os/signal"
	"syscall"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	authprovider "github.com/valon-technologies/gestalt/server/internal/drivers/auth/provider"
	authorizationprovider "github.com/valon-technologies/gestalt/server/internal/drivers/authorization/provider"
	cacheprovider "github.com/valon-technologies/gestalt/server/internal/drivers/cache/provider"
	indexeddbprovider "github.com/valon-technologies/gestalt/server/internal/drivers/indexeddb/provider"
	s3provider "github.com/valon-technologies/gestalt/server/internal/drivers/s3/provider"
	secretsenv "github.com/valon-technologies/gestalt/server/internal/drivers/secrets/env"
	secretsfile "github.com/valon-technologies/gestalt/server/internal/drivers/secrets/file"
	secretsprovider "github.com/valon-technologies/gestalt/server/internal/drivers/secrets/provider"
	telemetrynoop "github.com/valon-technologies/gestalt/server/internal/drivers/telemetry/noop"
	telemetryotlp "github.com/valon-technologies/gestalt/server/internal/drivers/telemetry/otlp"
	telemetrystdout "github.com/valon-technologies/gestalt/server/internal/drivers/telemetry/stdout"
	workflowprovider "github.com/valon-technologies/gestalt/server/internal/drivers/workflow/provider"
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

func setupBootstrapWithConfigPaths(configPaths []string, state operator.StatePaths, locked bool) (*bootstrapEnv, error) {
	cfg, err := loadConfigForExecutionAtPathsWithStatePaths(configPaths, state, locked)
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
	factories.Audit = func(ctx context.Context, cfg config.ProviderEntry, telemetry core.TelemetryProvider) (core.AuditSink, func(context.Context) error, error) {
		if cfg.Source.IsMetadataURL() || cfg.Source.IsLocal() {
			return nil, nil, fmt.Errorf("provider-based audit providers are not yet supported")
		}
		switch cfg.Source.Builtin {
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
			return nil, nil, fmt.Errorf("unknown audit provider %q", cfg.Source.Builtin)
		}
	}
	factories.Auth = authprovider.Factory
	factories.Authorization = authorizationprovider.Factory
	factories.IndexedDB = indexeddbprovider.Factory
	factories.Cache = cacheprovider.Factory
	factories.S3 = s3provider.Factory
	factories.Workflow = workflowprovider.Factory
	factories.Secrets["env"] = secretsenv.Factory
	factories.Secrets["file"] = secretsfile.Factory
	factories.Secrets["provider"] = secretsprovider.Factory
	return factories
}

const gracefulShutdownTimeout = 15 * time.Second
