package bootstrap

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/plugins/providerpkg"
)

type hostedProcessLaunch struct {
	command string
	args    []string
	cleanup func()
}

func prepareHostedProcessLaunch(kind, name string, entry *config.ProviderEntry, command string, args []string, cleanup func(), runtimeConfig config.EffectiveHostedRuntime) (hostedProcessLaunch, error) {
	if hostedRuntimeUsesImageEntrypoint(runtimeConfig) {
		command, args, err := hostedRuntimeImageEntrypoint(kind, name, entry)
		if err != nil {
			return hostedProcessLaunch{}, err
		}
		return hostedProcessLaunch{
			command: command,
			args:    args,
			cleanup: cleanup,
		}, nil
	}

	launch := hostedProcessLaunch{
		command: strings.TrimSpace(command),
		args:    slices.Clone(args),
		cleanup: cleanup,
	}
	if launch.command == "" {
		return hostedProcessLaunch{}, fmt.Errorf("hosted runtime command is required")
	}
	return launch, nil
}

func hostedRuntimeUsesImageEntrypoint(runtimeConfig config.EffectiveHostedRuntime) bool {
	hasRuntimeImageOrTemplate := strings.TrimSpace(runtimeConfig.Image) != "" || strings.TrimSpace(runtimeConfig.Template) != ""
	if runtimeConfig.Provider != nil {
		return runtimeConfig.Provider.Driver != config.RuntimeProviderDriverLocal && hasRuntimeImageOrTemplate
	}
	return hasRuntimeImageOrTemplate
}

func hostedRuntimeImageEntrypoint(kind, name string, entry *config.ProviderEntry) (string, []string, error) {
	label := strings.TrimSpace(kind)
	if label == "" {
		label = "provider"
	}
	if trimmedName := strings.TrimSpace(name); trimmedName != "" {
		label = fmt.Sprintf("%s %q", label, trimmedName)
	}
	if entry == nil || entry.ResolvedManifest == nil {
		return "", nil, fmt.Errorf("%s manifest is required for hosted runtime image launch", label)
	}
	entrypoint := providerpkg.EntrypointForKind(entry.ResolvedManifest, kind)
	if entrypoint == nil {
		return "", nil, fmt.Errorf("%s manifest does not define an entrypoint for hosted runtime image launch", label)
	}
	command := path.Clean(strings.TrimSpace(entrypoint.ArtifactPath))
	if command == "." || command == "" {
		return "", nil, fmt.Errorf("%s manifest entrypoint artifactPath is required for hosted runtime image launch", label)
	}
	if path.IsAbs(command) || command == ".." || strings.HasPrefix(command, "../") {
		return "", nil, fmt.Errorf("%s manifest entrypoint artifactPath must be relative for hosted runtime image launch", label)
	}
	return "./" + command, slices.Clone(entrypoint.Args), nil
}
