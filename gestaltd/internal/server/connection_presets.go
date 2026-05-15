package server

import (
	"fmt"
	"maps"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

type resolvedConnectionPreset struct {
	ID                  string
	Instance            string
	ConnectionParams    map[string]string
	AuthorizationParams map[string]string
	ExpectedMetadata    map[string]string
}

func resolveConnectionPreset(conn config.ConnectionDef, hasConnectionDef bool, req startOAuthRequest) (*resolvedConnectionPreset, map[string]string, string, error) {
	mergedParams := maps.Clone(req.ConnectionParams)
	requestedInstance := strings.TrimSpace(req.Instance)
	presetID := strings.TrimSpace(req.Preset)
	if presetID == "" {
		return nil, mergedParams, requestedInstance, nil
	}
	if !config.SafeConnectionValue(presetID) {
		return nil, nil, "", fmt.Errorf("preset contains invalid characters")
	}
	if !hasConnectionDef {
		return nil, nil, "", fmt.Errorf("unknown connection preset: %s", presetID)
	}

	preset, ok := findConnectionPreset(conn.Presets, presetID)
	if !ok {
		return nil, nil, "", fmt.Errorf("unknown connection preset: %s", presetID)
	}
	if len(req.ConnectionParams) > 0 {
		return nil, nil, "", fmt.Errorf("preset %q does not accept client connection parameters", presetID)
	}
	instance := strings.TrimSpace(preset.Instance)
	if requestedInstance != "" && instance != "" && requestedInstance != instance {
		return nil, nil, "", fmt.Errorf("preset %q requires instance %q", presetID, instance)
	}
	if instance != "" {
		requestedInstance = instance
	}
	for key, value := range preset.ConnectionParams {
		if existing, ok := mergedParams[key]; ok && existing != value {
			return nil, nil, "", fmt.Errorf("preset %q requires connection parameter %q", presetID, key)
		}
		mergedParams[key] = value
	}

	return &resolvedConnectionPreset{
		ID:                  presetID,
		Instance:            instance,
		ConnectionParams:    maps.Clone(preset.ConnectionParams),
		AuthorizationParams: maps.Clone(preset.AuthorizationParams),
		ExpectedMetadata:    maps.Clone(preset.ExpectedMetadata),
	}, mergedParams, requestedInstance, nil
}

func findConnectionPreset(presets []config.ConnectionPresetDef, id string) (config.ConnectionPresetDef, bool) {
	for _, preset := range presets {
		if strings.TrimSpace(preset.ID) == id {
			return preset, true
		}
	}
	return config.ConnectionPresetDef{}, false
}

func presetID(preset *resolvedConnectionPreset) string {
	if preset == nil {
		return ""
	}
	return preset.ID
}

func presetExpectedMetadata(preset *resolvedConnectionPreset) map[string]string {
	if preset == nil {
		return nil
	}
	return maps.Clone(preset.ExpectedMetadata)
}
