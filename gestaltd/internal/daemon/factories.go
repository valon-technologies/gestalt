package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os/signal"
	"syscall"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/operator"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	telemetrynoop "github.com/valon-technologies/gestalt/server/services/observability/drivers/noop"
	telemetryotlp "github.com/valon-technologies/gestalt/server/services/observability/drivers/otlp"
	telemetrystdout "github.com/valon-technologies/gestalt/server/services/observability/drivers/stdout"
	"github.com/valon-technologies/gestalt/server/services/providerdrivers"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	secretsenv "github.com/valon-technologies/gestalt/server/services/secrets/drivers/env"
	secretsfile "github.com/valon-technologies/gestalt/server/services/secrets/drivers/file"
	"gopkg.in/yaml.v3"
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
		if cfg.HasReleaseMetadataSource() || cfg.HasLocalSource() {
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
	factories.Auth = func(node yaml.Node, deps bootstrap.Deps) (core.AuthenticationProvider, error) {
		defaultCallbackURL := ""
		if deps.BaseURL != "" {
			defaultCallbackURL = deps.BaseURL + config.AuthCallbackPath
		}
		return providerdrivers.AuthenticationFactory(node, providerdrivers.AuthenticationDeps{
			DefaultCallbackURL: defaultCallbackURL,
			SessionKey:         deps.EncryptionKey,
		})
	}
	factories.Authorization = func(node yaml.Node, hostServices []runtimehost.HostService, _ bootstrap.Deps) (core.AuthorizationProvider, error) {
		return providerdrivers.AuthorizationFactory(node, hostServices)
	}
	factories.ExternalCredentials = func(ctx context.Context, name string, node yaml.Node, hostServices []runtimehost.HostService, _ bootstrap.Deps) (core.ExternalCredentialProvider, error) {
		return providerdrivers.ExternalCredentialsFactory(ctx, name, node, hostServices)
	}
	factories.IndexedDB = providerdrivers.IndexedDBFactory
	factories.Cache = providerdrivers.CacheFactory
	factories.S3 = providerdrivers.S3Factory
	factories.Workflow = func(ctx context.Context, name string, node yaml.Node, hostServices []runtimehost.HostService, deps bootstrap.Deps) (coreworkflow.Provider, error) {
		return providerdrivers.WorkflowFactory(ctx, name, node, hostServices, providerdrivers.WorkflowDeps{
			EgressDefaultAction: deps.Egress.DefaultAction,
		})
	}
	factories.Agent = func(ctx context.Context, name string, node yaml.Node, hostServices []runtimehost.HostService, deps bootstrap.Deps) (coreagent.Provider, error) {
		return providerdrivers.AgentFactory(ctx, name, node, hostServices, providerdrivers.AgentDeps{
			EgressDefaultAction: deps.Egress.DefaultAction,
			Telemetry:           deps.Telemetry,
		})
	}
	factories.Secrets["env"] = secretsenv.Factory
	factories.Secrets["file"] = secretsfile.Factory
	factories.Secrets["provider"] = providerdrivers.SecretsProviderFactory
	return factories
}

const gracefulShutdownTimeout = 15 * time.Second
