package config

import (
	"strings"
	"testing"
	"time"
)

func TestAppRegistryHeartbeatConfigDefaults(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `server: {}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	assertAppRegistryDuration(t, "heartbeat interval", cfg.Server.AppRegistry.HeartbeatIntervalDuration, 15*time.Second)
	assertAppRegistryDuration(t, "heartbeat TTL", cfg.Server.AppRegistry.HeartbeatTTLDuration, 45*time.Second)
	assertAppRegistryDuration(t, "healthy stability window", cfg.Server.AppRegistry.HealthyStabilityWindowDuration, 30*time.Second)
	assertAppRegistryDuration(t, "heartbeat retention", cfg.Server.AppRegistry.HeartbeatRetentionDuration, 24*time.Hour)
	if cfg.Server.AppRegistry.RolloutMode != AppRegistryRolloutModeEnrollment {
		t.Fatalf("rollout mode = %q, want %q", cfg.Server.AppRegistry.RolloutMode, AppRegistryRolloutModeEnrollment)
	}
}

func TestAppRegistryHeartbeatConfigAcceptsExplicitValues(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
server:
  appRegistry:
    heartbeatInterval: 10s
    heartbeatTtl: 35s
    healthyStabilityWindow: 20s
    heartbeatRetention: 12h
    rolloutMode: heartbeat
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	assertAppRegistryDuration(t, "heartbeat interval", cfg.Server.AppRegistry.HeartbeatIntervalDuration, 10*time.Second)
	assertAppRegistryDuration(t, "heartbeat TTL", cfg.Server.AppRegistry.HeartbeatTTLDuration, 35*time.Second)
	assertAppRegistryDuration(t, "healthy stability window", cfg.Server.AppRegistry.HealthyStabilityWindowDuration, 20*time.Second)
	assertAppRegistryDuration(t, "heartbeat retention", cfg.Server.AppRegistry.HeartbeatRetentionDuration, 12*time.Hour)
	if cfg.Server.AppRegistry.RolloutMode != AppRegistryRolloutModeHeartbeat {
		t.Fatalf("rollout mode = %q, want heartbeat", cfg.Server.AppRegistry.RolloutMode)
	}
}

func TestAppRegistryHeartbeatConfigValidation(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		config  string
		wantErr string
	}{
		{name: "interval positive", config: "heartbeatInterval: 0s", wantErr: "heartbeatInterval"},
		{name: "ttl positive", config: "heartbeatTtl: -1s", wantErr: "heartbeatTtl"},
		{name: "ttl exceeds interval", config: "heartbeatInterval: 15s\n    heartbeatTtl: 15s", wantErr: "must be greater than heartbeatInterval"},
		{name: "stability positive", config: "healthyStabilityWindow: 0s", wantErr: "healthyStabilityWindow"},
		{name: "retention positive", config: "heartbeatRetention: invalid", wantErr: "heartbeatRetention"},
		{name: "rollout mode", config: "rolloutMode: other", wantErr: "rolloutMode"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := mustWriteConfigFile(t, "server:\n  appRegistry:\n    "+tc.config+"\n")
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Load error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func assertAppRegistryDuration(t *testing.T, name string, load func() (time.Duration, error), want time.Duration) {
	t.Helper()
	got, err := load()
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if got != want {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
}
