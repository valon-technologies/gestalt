package server

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

const (
	uiReadinessEnabledEnv          = "GESTALTD_UI_READINESS_ENABLED"
	releaseIDEnv                   = "GESTALTD_RELEASE_ID"
	uiQualificationBearerTokenEnv  = "GESTALTD_UI_QUALIFICATION_BEARER_TOKEN"
	defaultUIReadinessRecheckEvery = 5 * time.Second
)

type uiReadinessSettings struct {
	enabled         bool
	releaseID       string
	probeBearer     string
	extraProbePaths []string
	recheckInterval time.Duration
}

func resolveUIReadinessSettings(cfg *config.Config, gestaltdVersion, sourceVersion string) uiReadinessSettings {
	settings := uiReadinessSettings{
		recheckInterval: defaultUIReadinessRecheckEvery,
	}
	if cfg != nil && cfg.Server.UIReadiness != nil {
		uiCfg := cfg.Server.UIReadiness
		if uiCfg.Enabled != nil {
			settings.enabled = *uiCfg.Enabled
		}
		settings.releaseID = strings.TrimSpace(uiCfg.ReleaseID)
		settings.probeBearer = strings.TrimSpace(uiCfg.ProbeBearerToken)
		settings.extraProbePaths = append([]string(nil), uiCfg.ExtraProbePaths...)
		if interval, err := uiCfg.RecheckIntervalDuration(); err == nil && interval > 0 {
			settings.recheckInterval = interval
		}
	}
	if raw := strings.TrimSpace(os.Getenv(uiReadinessEnabledEnv)); raw != "" {
		if parsed, err := strconv.ParseBool(raw); err == nil {
			settings.enabled = parsed
		}
	}
	if settings.releaseID == "" {
		settings.releaseID = strings.TrimSpace(os.Getenv(releaseIDEnv))
	}
	if settings.releaseID == "" {
		settings.releaseID = strings.TrimSpace(gestaltdVersion) + "@" + strings.TrimSpace(sourceVersion)
	}
	if settings.probeBearer == "" {
		settings.probeBearer = strings.TrimSpace(os.Getenv(uiQualificationBearerTokenEnv))
	}
	return settings
}
