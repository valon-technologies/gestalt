package config

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

const DefaultRemoteName = "default"

// RemoteConfig names one upstream gestaltd origin.
type RemoteConfig struct {
	URL     string `yaml:"url"`
	Token   string `yaml:"token,omitempty"`
	Default bool   `yaml:"default,omitempty"`
}

// EntryPlacementRemote returns the canonical remote name for provider placement.
// An empty string means the entry builds locally.
func EntryPlacementRemote(entry *ProviderEntry) string {
	if entry == nil {
		return ""
	}
	return strings.TrimSpace(entry.Remote)
}

// EntryBuildsLocal reports whether an entry should be built in-process locally.
func EntryBuildsLocal(entry *ProviderEntry) bool {
	if entry == nil {
		return false
	}
	if entry.DevActive || entry.Local {
		return true
	}
	return EntryPlacementRemote(entry) == ""
}

// ReferencedRemoteNames returns the sorted unique remote names referenced by
// configured provider entries.
func (cfg *Config) ReferencedRemoteNames() []string {
	if cfg == nil {
		return nil
	}
	seen := make(map[string]struct{})
	add := func(entry *ProviderEntry) {
		if entry == nil || entry.DevActive || entry.Local {
			return
		}
		if name := EntryPlacementRemote(entry); name != "" {
			seen[name] = struct{}{}
		}
	}
	for _, entry := range cfg.Apps {
		add(entry)
	}
	for _, entries := range []map[string]*ProviderEntry{
		cfg.Providers.Identity,
		cfg.Providers.Authorization,
		cfg.Providers.IndexedDB,
		cfg.Providers.Workflow,
		cfg.Providers.Agent,
	} {
		for _, entry := range entries {
			add(entry)
		}
	}
	if len(seen) == 0 {
		return nil
	}
	return slices.Sorted(maps.Keys(seen))
}

// DefaultRemoteEntry returns the named default remote configuration.
func (cfg *Config) DefaultRemoteEntry() (name string, remote *RemoteConfig, ok bool) {
	if cfg == nil || len(cfg.Server.Remotes) == 0 {
		return "", nil, false
	}
	var soleName string
	var soleRemote *RemoteConfig
	soleCount := 0
	for name, remote := range cfg.Server.Remotes {
		if remote == nil {
			continue
		}
		if remote.Default {
			return name, remote, true
		}
		soleName = name
		soleRemote = remote
		soleCount++
	}
	if soleCount == 1 {
		return soleName, soleRemote, true
	}
	return "", nil, false
}

func canonicalizeLegacyRemoteConfig(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	legacyURL := strings.TrimSpace(cfg.Server.Remote)
	if legacyURL == "" {
		return nil
	}
	hadNamedRemotes := len(cfg.Server.Remotes) > 0
	if cfg.Server.Remotes == nil {
		cfg.Server.Remotes = make(map[string]*RemoteConfig)
	}
	// If named remotes already exist, fold the legacy scalar into an existing
	// default entry only — never synthesize or promote one. This lets
	// validateRemotesConfig catch the "no default" case for multi-remote
	// configs instead of masking it.
	if hadNamedRemotes {
		if _, existing, ok := cfg.DefaultRemoteEntry(); ok && existing != nil {
			if strings.TrimSpace(existing.URL) == "" {
				existing.URL = legacyURL
			}
			if strings.TrimSpace(existing.Token) == "" {
				existing.Token = strings.TrimSpace(cfg.Server.RemoteToken)
			}
		}
		return nil
	}
	cfg.Server.Remotes[DefaultRemoteName] = &RemoteConfig{
		URL:     legacyURL,
		Token:   strings.TrimSpace(cfg.Server.RemoteToken),
		Default: true,
	}
	dev := cfg.Server.Dev
	stampDefaultRemote(cfg.Apps, DefaultRemoteName, dev)
	for _, entries := range []map[string]*ProviderEntry{
		cfg.Providers.Identity,
		cfg.Providers.Authorization,
		cfg.Providers.IndexedDB,
		cfg.Providers.Workflow,
		cfg.Providers.Agent,
	} {
		stampDefaultRemote(entries, DefaultRemoteName, dev)
	}
	return nil
}

// stampDefaultRemote sets remote to defaultName on every entry that does not
// already name a remote and is not explicitly local. Dev-active entries are
// normally skipped (they run locally and are not remote-delegated), but when
// dev is true they are stamped too so the reverse-tunnel publisher includes
// them in its publication plan — the remote then forwards operations and UI
// traffic back through the tunnel to the dev machine.
func stampDefaultRemote(entries map[string]*ProviderEntry, defaultName string, dev bool) {
	for _, entry := range entries {
		if entry == nil || entry.Local {
			continue
		}
		if entry.DevActive && !dev {
			continue
		}
		if strings.TrimSpace(entry.Remote) != "" {
			continue
		}
		entry.Remote = defaultName
	}
}

// canonicalizeRemotes folds the legacy server.remote scalar into
// server.remotes, stamps remote: default onto entries that should delegate,
// backfills the default remote's token from server.remoteToken, and trims
// scalar fields. Called from CanonicalizeStructure so every consumer
// (Load, bootstrap, serve) sees placement-ready remotes.
func canonicalizeRemotes(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	if err := canonicalizeLegacyRemoteConfig(cfg); err != nil {
		return err
	}
	if _, defaultRemote, ok := cfg.DefaultRemoteEntry(); ok && defaultRemote != nil {
		if strings.TrimSpace(defaultRemote.Token) == "" {
			defaultRemote.Token = strings.TrimSpace(cfg.Server.RemoteToken)
		}
	}
	trimmed := make(map[string]*RemoteConfig, len(cfg.Server.Remotes))
	for name, remote := range cfg.Server.Remotes {
		if remote == nil {
			continue
		}
		remote.URL = strings.TrimRight(strings.TrimSpace(remote.URL), "/")
		remote.Token = strings.TrimSpace(remote.Token)
		trimmed[strings.TrimSpace(name)] = remote
	}
	cfg.Server.Remotes = trimmed
	cfg.Server.Remote = strings.TrimRight(strings.TrimSpace(cfg.Server.Remote), "/")
	cfg.Server.RemoteToken = strings.TrimSpace(cfg.Server.RemoteToken)
	return nil
}

// validateRemotesConfig validates the canonicalized remotes config: required
// URLs, origin checks, at most one default, known remote references.
func validateRemotesConfig(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	defaultCount := 0
	remoteCount := 0
	for name, remote := range cfg.Server.Remotes {
		if remote == nil {
			return fmt.Errorf("config validation: server.remotes.%s is required", name)
		}
		remoteCount++
		if remote.Default {
			defaultCount++
		}
		if remote.URL == "" {
			return fmt.Errorf("config validation: server.remotes.%s.url is required", name)
		}
		if err := validateHTTPOriginURL("server.remotes."+name+".url", remote.URL); err != nil {
			return err
		}
	}
	if defaultCount > 1 {
		return fmt.Errorf("config validation: at most one server.remotes entry may set default: true")
	}
	if remoteCount > 1 && defaultCount == 0 {
		return fmt.Errorf("config validation: exactly one server.remotes entry must set default: true when multiple remotes are configured")
	}
	for _, name := range cfg.ReferencedRemoteNames() {
		if cfg.Server.Remotes[name] == nil {
			return fmt.Errorf("config validation: remote %q is not defined under server.remotes", name)
		}
	}
	return nil
}

func validateProviderEntryRemote(subject string, entry *ProviderEntry) error {
	if entry == nil {
		return nil
	}
	remote := strings.TrimSpace(entry.Remote)
	if entry.Local && remote != "" {
		return fmt.Errorf("config validation: %s cannot set both local: true and remote", subject)
	}
	return nil
}

func validateUnsupportedRemotePlacement(subject string, entry *ProviderEntry) error {
	if err := validateProviderEntryRemote(subject, entry); err != nil {
		return err
	}
	if entry != nil && strings.TrimSpace(entry.Remote) != "" {
		return fmt.Errorf("config validation: %s does not support remote placement", subject)
	}
	return nil
}
