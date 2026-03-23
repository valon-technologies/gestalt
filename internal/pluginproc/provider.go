package pluginproc

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
	"github.com/valon-technologies/gestalt/internal/config"
	"gopkg.in/yaml.v3"
)

const closeGracePeriod = 2 * time.Second

var (
	_ core.Provider        = (*baseProvider)(nil)
	_ core.CatalogProvider = (*baseProvider)(nil)
	_ io.Closer            = (*baseProvider)(nil)
	_ core.Provider        = (*manualProvider)(nil)
	_ core.ManualProvider  = (*manualProvider)(nil)
	_ core.Provider        = (*oauthProvider)(nil)
	_ core.OAuthProvider   = (*oauthProvider)(nil)
)

type baseProvider struct {
	name           string
	displayName    string
	description    string
	connectionMode core.ConnectionMode
	operations     []core.Operation
	cat            *catalog.Catalog
	session        *pluginSession
	requestTimeout time.Duration
}

func New(ctx context.Context, name string, intg config.IntegrationDef, plugin config.PluginDef) (core.Provider, error) {
	if len(plugin.Command) == 0 {
		return nil, fmt.Errorf("integration %s: plugin.command is required", name)
	}

	startupTimeout, err := parseDuration(plugin.StartupTimeout, defaultStartupTimeoutSec*time.Second)
	if err != nil {
		return nil, fmt.Errorf("integration %s: invalid plugin.startup_timeout: %w", name, err)
	}
	requestTimeout, err := parseDuration(plugin.RequestTimeout, defaultRequestTimeoutSec*time.Second)
	if err != nil {
		return nil, fmt.Errorf("integration %s: invalid plugin.request_timeout: %w", name, err)
	}

	sess, err := startSession(name, plugin)
	if err != nil {
		return nil, fmt.Errorf("integration %s: starting plugin: %w", name, err)
	}

	initCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()

	initResult := InitializeResult{}
	if err := sess.client.Call(initCtx, methodInitialize, InitializeParams{
		ProtocolVersion: ProtocolVersion,
		HostInfo: HostInfo{
			Name: "gestalt",
		},
		Integration: IntegrationInfo{
			Name:   name,
			Config: decodeConfigNode(plugin.Config),
		},
	}, &initResult); err != nil {
		_ = sess.Close()
		return nil, fmt.Errorf("initialize plugin: %w", err)
	}

	if initResult.ProtocolVersion != ProtocolVersion {
		_ = sess.Close()
		return nil, fmt.Errorf("initialize plugin: unsupported protocol version %q", initResult.ProtocolVersion)
	}
	if len(initResult.Provider.Operations) == 0 {
		_ = sess.Close()
		return nil, fmt.Errorf("initialize plugin: provider returned no operations")
	}

	displayName := firstNonEmpty(intg.DisplayName, initResult.Provider.DisplayName, name)
	description := firstNonEmpty(intg.Description, initResult.Provider.Description)
	connMode, err := normalizeConnectionMode(intg.ConnectionMode, initResult.Provider.ConnectionMode)
	if err != nil {
		_ = sess.Close()
		return nil, fmt.Errorf("integration %s: %w", name, err)
	}

	ops, cat, err := filterManifest(name, initResult.Provider.Operations, initResult.Provider.Catalog, plugin.AllowedOperations)
	if err != nil {
		_ = sess.Close()
		return nil, fmt.Errorf("integration %s: %w", name, err)
	}
	if cat != nil {
		cat.Name = name
		if cat.DisplayName == "" {
			cat.DisplayName = displayName
		}
		if cat.Description == "" {
			cat.Description = description
		}
	}

	base := &baseProvider{
		name:           name,
		displayName:    displayName,
		description:    description,
		connectionMode: connMode,
		operations:     ops,
		cat:            cat,
		session:        sess,
		requestTimeout: requestTimeout,
	}

	authType := inferAuthType(initResult.Provider.Auth, initResult.Capabilities)
	switch authType {
	case "", AuthTypeNone:
		return base, nil
	case AuthTypeManual:
		return &manualProvider{baseProvider: base}, nil
	case AuthTypeOAuth2:
		return &oauthProvider{baseProvider: base}, nil
	default:
		_ = sess.Close()
		return nil, fmt.Errorf("initialize plugin: unknown auth type %q", authType)
	}
}

func (p *baseProvider) Name() string                        { return p.name }
func (p *baseProvider) DisplayName() string                 { return p.displayName }
func (p *baseProvider) Description() string                 { return p.description }
func (p *baseProvider) ConnectionMode() core.ConnectionMode { return p.connectionMode }
func (p *baseProvider) ListOperations() []core.Operation    { return p.operations }
func (p *baseProvider) Catalog() *catalog.Catalog           { return p.cat }

func (p *baseProvider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	callCtx, cancel := withOptionalTimeout(ctx, p.requestTimeout)
	defer cancel()

	var result core.OperationResult
	if err := p.session.client.Call(callCtx, methodProviderExecute, ExecuteParams{
		Operation: operation,
		Params:    params,
		Token:     token,
	}, &result); err != nil {
		return nil, fmt.Errorf("plugin execute %s/%s: %w", p.name, operation, err)
	}
	return &result, nil
}

func (p *baseProvider) Close() error {
	return p.session.Close()
}

type manualProvider struct {
	*baseProvider
}

func (p *manualProvider) SupportsManualAuth() bool { return true }

type oauthProvider struct {
	*baseProvider
}

func (p *oauthProvider) AuthorizationURL(state string, scopes []string) string {
	authURL, _, _ := p.StartOAuth(state, scopes)
	return authURL
}

func (p *oauthProvider) StartOAuth(state string, scopes []string) (authURL string, verifier string, err error) {
	callCtx, cancel := withOptionalTimeout(context.Background(), p.requestTimeout)
	defer cancel()

	var result AuthStartResult
	if err := p.session.client.Call(callCtx, methodAuthStart, AuthStartParams{
		State:  state,
		Scopes: scopes,
	}, &result); err != nil {
		return "", "", fmt.Errorf("plugin %s: auth.start: %w", p.name, err)
	}
	return result.AuthURL, result.Verifier, nil
}

func (p *oauthProvider) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	return p.ExchangeCodeWithVerifier(ctx, code, "")
}

func (p *oauthProvider) ExchangeCodeWithVerifier(ctx context.Context, code, verifier string) (*core.TokenResponse, error) {
	callCtx, cancel := withOptionalTimeout(ctx, p.requestTimeout)
	defer cancel()

	var result core.TokenResponse
	if err := p.session.client.Call(callCtx, methodAuthExchangeCode, AuthExchangeCodeParams{
		Code:     code,
		Verifier: verifier,
	}, &result); err != nil {
		return nil, fmt.Errorf("plugin auth exchange code: %w", err)
	}
	return &result, nil
}

func (p *oauthProvider) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	callCtx, cancel := withOptionalTimeout(ctx, p.requestTimeout)
	defer cancel()

	var result core.TokenResponse
	if err := p.session.client.Call(callCtx, methodAuthRefreshToken, AuthRefreshTokenParams{
		RefreshToken: refreshToken,
	}, &result); err != nil {
		return nil, fmt.Errorf("plugin auth refresh token: %w", err)
	}
	return &result, nil
}

type pluginSession struct {
	client *Client
	cmd    *exec.Cmd
	waitCh chan error
}

func startSession(name string, cfg config.PluginDef) (*pluginSession, error) {
	cmd := exec.Command(cfg.Command[0], cfg.Command[1:]...)
	cmd.Dir = cfg.Cwd
	cmd.Env = append(os.Environ(), flattenEnv(cfg.Env)...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start process: %w", err)
	}

	go streamLogs(name, stderr)

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
		close(waitCh)
	}()

	return &pluginSession{
		client: newClient(stdout, stdin),
		cmd:    cmd,
		waitCh: waitCh,
	}, nil
}

func (s *pluginSession) Close() error {
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), closeGracePeriod)
	err := s.client.Call(shutdownCtx, methodShutdown, map[string]any{}, nil)
	shutdownCancel()
	if err != nil && !errors.Is(err, os.ErrClosed) && !errors.Is(err, context.DeadlineExceeded) {
		return s.kill(err)
	}

	select {
	case err := <-s.waitCh:
		return err
	case <-time.After(closeGracePeriod):
	}

	if s.cmd.Process != nil {
		_ = s.cmd.Process.Signal(os.Interrupt)
	}

	select {
	case err := <-s.waitCh:
		return err
	case <-time.After(closeGracePeriod):
	}

	return s.kill(nil)
}

func (s *pluginSession) kill(firstErr error) error {
	if s.cmd.Process != nil {
		if err := s.cmd.Process.Kill(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := <-s.waitCh; err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func streamLogs(name string, r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		log.Printf("plugin %s: %s", name, line)
	}
}

func filterManifest(name string, ops []core.Operation, cat *catalog.Catalog, allowed config.AllowedOps) ([]core.Operation, *catalog.Catalog, error) {
	if allowed == nil {
		return ops, cat, nil
	}
	if len(allowed) == 0 {
		return nil, nil, fmt.Errorf("plugin.allowed_operations cannot be empty; omit the field to allow all")
	}

	opSet := make(map[string]struct{}, len(ops))
	for _, op := range ops {
		opSet[op.Name] = struct{}{}
	}
	for opName := range allowed {
		if _, ok := opSet[opName]; !ok {
			return nil, nil, fmt.Errorf("plugin.allowed_operations contains unknown operation %q", opName)
		}
	}

	filteredOps := make([]core.Operation, 0, len(allowed))
	for _, op := range ops {
		if desc, ok := allowed[op.Name]; ok {
			if desc != "" {
				op.Description = desc
			}
			filteredOps = append(filteredOps, op)
		}
	}

	if cat == nil {
		return filteredOps, nil, nil
	}

	filteredCat := *cat
	filteredCat.Name = firstNonEmpty(filteredCat.Name, name)
	filteredCat.Operations = make([]catalog.CatalogOperation, 0, len(allowed))
	for i := range cat.Operations {
		op := &cat.Operations[i]
		if desc, ok := allowed[op.ID]; ok {
			entry := *op
			if desc != "" {
				entry.Description = desc
			}
			filteredCat.Operations = append(filteredCat.Operations, entry)
		}
	}
	return filteredOps, &filteredCat, nil
}

func inferAuthType(cfg *ProviderAuthConfig, caps PluginCapabilities) string {
	switch {
	case cfg != nil && cfg.Type != "":
		return cfg.Type
	case caps.ManualAuth:
		return AuthTypeManual
	case caps.OAuth:
		return AuthTypeOAuth2
	default:
		return ""
	}
}

func normalizeConnectionMode(override, manifest string) (core.ConnectionMode, error) {
	mode := firstNonEmpty(override, manifest, string(core.ConnectionModeUser))
	switch core.ConnectionMode(mode) {
	case core.ConnectionModeNone, core.ConnectionModeUser, core.ConnectionModeIdentity, core.ConnectionModeEither:
		return core.ConnectionMode(mode), nil
	default:
		return "", fmt.Errorf("unknown connection_mode %q", mode)
	}
}

func withOptionalTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return ctx, func() {}
	}
	if deadline, ok := ctx.Deadline(); ok {
		if time.Until(deadline) <= timeout {
			return ctx, func() {}
		}
	}
	return context.WithTimeout(ctx, timeout)
}

func parseDuration(raw string, fallback time.Duration) (time.Duration, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	return time.ParseDuration(raw)
}

func flattenEnv(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for key, value := range env {
		out = append(out, key+"="+value)
	}
	return out
}

func decodeConfigNode(node yaml.Node) any {
	if node.Kind == 0 {
		return nil
	}

	var v any
	if err := node.Decode(&v); err != nil {
		return nil
	}
	return normalizeYAMLValue(v)
}

func normalizeYAMLValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for key, value := range t {
			out[key] = normalizeYAMLValue(value)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(t))
		for key, value := range t {
			out[fmt.Sprint(key)] = normalizeYAMLValue(value)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, value := range t {
			out[i] = normalizeYAMLValue(value)
		}
		return out
	default:
		return v
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
