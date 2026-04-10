package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"gopkg.in/yaml.v3"
)

func runConfig(args []string) error {
	if len(args) == 0 {
		printConfigUsage(os.Stderr)
		return flag.ErrHelp
	}

	switch args[0] {
	case "-h", "--help", "help":
		printConfigUsage(os.Stderr)
		return flag.ErrHelp
	case "resolve":
		return runConfigResolve(args[1:])
	default:
		return fmt.Errorf("unknown config command %q", args[0])
	}
}

type resolvedPlugin struct {
	Source      string                       `yaml:"source,omitempty"`
	DisplayName string                      `yaml:"displayName,omitempty"`
	Connections map[string]resolvedConnection `yaml:"connections,omitempty"`
}

type resolvedConnection struct {
	Mode   string                  `yaml:"mode,omitempty"`
	Auth   resolvedConnectionAuth  `yaml:"auth,omitempty"`
	Params map[string]resolvedParam `yaml:"params,omitempty"`
}

type resolvedConnectionAuth struct {
	Type             string   `yaml:"type,omitempty"`
	AuthorizationURL string   `yaml:"authorizationUrl,omitempty"`
	TokenURL         string   `yaml:"tokenUrl,omitempty"`
	ClientID         string   `yaml:"clientId,omitempty"`
	ClientSecret     string   `yaml:"clientSecret,omitempty"`
	Scopes           []string `yaml:"scopes,omitempty"`
	PKCE             bool     `yaml:"pkce,omitempty"`
}

type resolvedParam struct {
	Required    bool   `yaml:"required,omitempty"`
	Description string `yaml:"description,omitempty"`
	From        string `yaml:"from,omitempty"`
}

var sensitiveFieldNames = []string{"secret", "key", "token", "password"}

func isSensitiveField(name string) bool {
	lower := strings.ToLower(name)
	for _, s := range sensitiveFieldNames {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

const redactedValue = "***"

func redactString(fieldName, value string) string {
	if value == "" {
		return ""
	}
	if isSensitiveField(fieldName) {
		return redactedValue
	}
	return value
}

func buildResolvedConnectionAuth(auth config.ConnectionAuthDef) resolvedConnectionAuth {
	return resolvedConnectionAuth{
		Type:             string(auth.Type),
		AuthorizationURL: auth.AuthorizationURL,
		TokenURL:         auth.TokenURL,
		ClientID:         redactString("clientId", auth.ClientID),
		ClientSecret:     redactString("clientSecret", auth.ClientSecret),
		Scopes:           auth.Scopes,
		PKCE:             auth.PKCE,
	}
}

func buildResolvedParams(params map[string]config.ConnectionParamDef) map[string]resolvedParam {
	if len(params) == 0 {
		return nil
	}
	out := make(map[string]resolvedParam, len(params))
	for name, p := range params {
		out[name] = resolvedParam{
			Required:    p.Required,
			Description: p.Description,
			From:        redactString(name, p.From),
		}
	}
	return out
}

func buildResolvedConnection(conn config.ConnectionDef) resolvedConnection {
	return resolvedConnection{
		Mode:   string(conn.Mode),
		Auth:   buildResolvedConnectionAuth(conn.Auth),
		Params: buildResolvedParams(conn.ConnectionParams),
	}
}

func runConfigResolve(args []string) error {
	fs := flag.NewFlagSet("gestaltd config resolve", flag.ContinueOnError)
	fs.Usage = func() { printConfigResolveUsage(fs.Output()) }
	configPath := fs.String("config", "", "path to config file")
	artifactsDir := fs.String("artifacts-dir", "", "path to writable prepared-artifacts directory")
	pluginFilter := fs.String("plugin", "", "show only this plugin")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	_, cfg, err := loadConfigForExecutionWithArtifactsDir(*configPath, *artifactsDir, true)
	if err != nil {
		return err
	}

	output := make(map[string]resolvedPlugin)

	names := make([]string, 0, len(cfg.Plugins))
	for name := range cfg.Plugins {
		names = append(names, name)
	}
	slices.Sort(names)

	for _, name := range names {
		if *pluginFilter != "" && name != *pluginFilter {
			continue
		}

		intg := cfg.Plugins[name]
		if intg.Plugin == nil {
			continue
		}

		rp := resolvedPlugin{
			Source:      intg.Plugin.SourceRef(),
			DisplayName: intg.DisplayName,
			Connections: make(map[string]resolvedConnection),
		}

		var manifestPlugin = intg.Plugin.ManifestPlugin()

		pluginConn := config.EffectivePluginConnectionDef(intg.Plugin, manifestPlugin)
		rp.Connections[config.PluginConnectionName] = buildResolvedConnection(pluginConn)

		if manifestPlugin != nil {
			for connName := range manifestPlugin.Connections {
				conn, ok := config.EffectiveNamedConnectionDef(intg.Plugin, manifestPlugin, connName)
				if ok {
					rp.Connections[connName] = buildResolvedConnection(conn)
				}
			}
		}
		if intg.Plugin.Connections != nil {
			for connName := range intg.Plugin.Connections {
				if _, exists := rp.Connections[connName]; exists {
					continue
				}
				conn, ok := config.EffectiveNamedConnectionDef(intg.Plugin, manifestPlugin, connName)
				if ok {
					rp.Connections[connName] = buildResolvedConnection(conn)
				}
			}
		}

		output[name] = rp
	}

	if *pluginFilter != "" && len(output) == 0 {
		return fmt.Errorf("plugin %q not found", *pluginFilter)
	}

	data, err := yaml.Marshal(output)
	if err != nil {
		return fmt.Errorf("marshaling output: %w", err)
	}
	_, err = os.Stdout.Write(data)
	return err
}

func printConfigUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd config <command> [flags]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Commands:")
	writeUsageLine(w, "  resolve     Print the effective merged configuration for each plugin")
}

func printConfigResolveUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd config resolve [--config PATH] [--artifacts-dir PATH] [--plugin NAME]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Print the effective merged configuration for each plugin as YAML.")
	writeUsageLine(w, "Shows the runtime-merged result of manifest defaults and config overrides,")
	writeUsageLine(w, "with sensitive fields redacted.")
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --config          Path to the config file")
	writeUsageLine(w, "  --artifacts-dir   Path to writable prepared-artifacts directory")
	writeUsageLine(w, "  --plugin          Show only the named plugin")
}
