package providerpkg

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

type runtimeManifestMetadata struct {
	SecuritySchemes map[string]*providermanifestv1.WebhookSecurityScheme `json:"securitySchemes,omitempty" yaml:"securitySchemes,omitempty"`
	Webhooks        map[string]*providermanifestv1.WebhookDef            `json:"webhooks,omitempty" yaml:"webhooks,omitempty"`
}

func ApplySourceRuntimeManifestMetadata(manifestPath string, manifest *providermanifestv1.Manifest) error {
	if manifest == nil || manifest.Kind != providermanifestv1.KindPlugin {
		return nil
	}
	metadata, err := loadSourceRuntimeManifestMetadata(filepath.Dir(manifestPath))
	if err != nil {
		return err
	}
	if metadata == nil {
		return nil
	}
	if manifest.Spec == nil {
		manifest.Spec = &providermanifestv1.Spec{}
	}
	if err := mergeRuntimeManifestMetadata(manifest.Spec, metadata); err != nil {
		return err
	}
	return nil
}

func loadSourceRuntimeManifestMetadata(rootDir string) (*runtimeManifestMetadata, error) {
	tempFile, err := os.CreateTemp("", "gestalt-runtime-manifest-metadata-*.json")
	if err != nil {
		return nil, fmt.Errorf("create runtime manifest metadata file: %w", err)
	}
	path := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("close runtime manifest metadata file: %w", err)
	}
	defer func() { _ = os.Remove(path) }()

	if err := generateSourceRuntimeManifestMetadata(rootDir, path); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read runtime manifest metadata: %w", err)
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil, nil
	}

	var metadata runtimeManifestMetadata
	if err := decodeStrict(data, runtimeManifestMetadataFormat(data), "runtime manifest metadata", &metadata); err != nil {
		return nil, err
	}
	if len(metadata.SecuritySchemes) == 0 && len(metadata.Webhooks) == 0 {
		return nil, nil
	}
	return &metadata, nil
}

func runtimeManifestMetadataFormat(data []byte) string {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return ManifestFormatJSON
	}
	switch data[0] {
	case '{', '[':
		return ManifestFormatJSON
	default:
		return ManifestFormatYAML
	}
}

func generateSourceRuntimeManifestMetadata(rootDir, metadataPath string) error {
	command, args, cleanup, err := SourceProviderExecutionCommand(rootDir, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		if errors.Is(err, ErrNoSourceProviderPackage) {
			return nil
		}
		return fmt.Errorf("prepare synthesized source provider for runtime manifest metadata: %w", err)
	}
	if cleanup != nil {
		defer cleanup()
	}

	cmd := exec.Command(command, args...)
	cmd.Env = append(os.Environ(), envWriteManifestMetadata+"="+metadataPath)
	execEnv, err := SourceProviderExecutionEnv(rootDir, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return fmt.Errorf("prepare synthesized source provider environment for runtime manifest metadata: %w", err)
	}
	for key, value := range execEnv {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		msg := bytes.TrimSpace(output.Bytes())
		if len(msg) == 0 {
			return fmt.Errorf("generate runtime manifest metadata: %w", err)
		}
		return fmt.Errorf("generate runtime manifest metadata: %w\n%s", err, msg)
	}
	return nil
}

func mergeRuntimeManifestMetadata(spec *providermanifestv1.Spec, metadata *runtimeManifestMetadata) error {
	if spec == nil || metadata == nil {
		return nil
	}
	if len(metadata.SecuritySchemes) > 0 {
		if spec.SecuritySchemes == nil {
			spec.SecuritySchemes = map[string]*providermanifestv1.WebhookSecurityScheme{}
		}
		for name, scheme := range metadata.SecuritySchemes {
			name = strings.TrimSpace(name)
			if _, exists := spec.SecuritySchemes[name]; exists {
				return fmt.Errorf("runtime manifest metadata duplicates spec.securitySchemes.%s", name)
			}
			spec.SecuritySchemes[name] = scheme
		}
	}
	if len(metadata.Webhooks) > 0 {
		if spec.Webhooks == nil {
			spec.Webhooks = map[string]*providermanifestv1.WebhookDef{}
		}
		for name, webhook := range metadata.Webhooks {
			name = strings.TrimSpace(name)
			if _, exists := spec.Webhooks[name]; exists {
				return fmt.Errorf("runtime manifest metadata duplicates spec.webhooks.%s", name)
			}
			spec.Webhooks[name] = webhook
		}
	}
	return nil
}
