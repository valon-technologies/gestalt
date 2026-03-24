package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"

	"github.com/valon-technologies/gestalt/internal/config"
	"gopkg.in/yaml.v3"
)

const (
	bundleConfigName   = "config.yaml"
	bundleProvidersDir = "providers"
)

func bundleConfig(configFlag, outputDir string) error {
	configPath := resolveConfigPath(configFlag)

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %v", err)
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("creating output dir: %w", err)
	}

	providersDir := filepath.Join(outputDir, bundleProvidersDir)
	if err := os.MkdirAll(providersDir, 0755); err != nil {
		return fmt.Errorf("creating providers dir: %w", err)
	}

	written, err := writeProviderArtifacts(context.Background(), cfg, providersDir)
	if err != nil {
		return err
	}

	configOut := filepath.Join(outputDir, bundleConfigName)
	if err := writeBundledConfig(configPath, configOut, written); err != nil {
		return err
	}

	log.Printf("wrote bundled config %s", configOut)
	log.Printf("bundle ready: gestaltd --config %s", configOut)
	return nil
}

func writeBundledConfig(sourcePath, outputPath string, written map[string]string) error {
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("reading source config: %w", err)
	}

	var doc yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&doc); err != nil {
		return fmt.Errorf("parsing source config YAML: %w", err)
	}
	if len(doc.Content) == 0 {
		return fmt.Errorf("source config is empty")
	}

	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("source config root must be a mapping")
	}

	deleteMappingKey(root, "provider_dirs")

	integrationsNode := mappingValue(root, "integrations")
	if integrationsNode != nil && integrationsNode.Kind == yaml.MappingNode {
		for i := 0; i < len(integrationsNode.Content); i += 2 {
			name := integrationsNode.Content[i].Value
			if _, ok := written[name]; !ok {
				continue
			}

			intgNode := integrationsNode.Content[i+1]
			rewriteBundledIntegration(name, intgNode)
		}
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("creating bundled config: %w", err)
	}

	enc := yaml.NewEncoder(f)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		_ = enc.Close()
		_ = f.Close()
		return fmt.Errorf("writing bundled config: %w", err)
	}
	if err := enc.Close(); err != nil {
		_ = f.Close()
		return fmt.Errorf("finalizing bundled config: %w", err)
	}
	return f.Close()
}

func rewriteBundledIntegration(name string, intgNode *yaml.Node) {
	if intgNode == nil || intgNode.Kind != yaml.MappingNode {
		return
	}

	deleteMappingKey(intgNode, "icon_file")

	upstreamsNode := mappingValue(intgNode, "upstreams")
	if upstreamsNode == nil || upstreamsNode.Kind != yaml.SequenceNode {
		return
	}

	providerPath := path.Join(bundleProvidersDir, name+".json")
	for _, upstreamNode := range upstreamsNode.Content {
		if upstreamNode == nil || upstreamNode.Kind != yaml.MappingNode {
			continue
		}
		switch mappingString(upstreamNode, "type") {
		case config.UpstreamTypeREST, config.UpstreamTypeGraphQL:
			deleteMappingKey(upstreamNode, "url")
			setMappingString(upstreamNode, "provider", providerPath)
		}
	}
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func mappingString(node *yaml.Node, key string) string {
	value := mappingValue(node, key)
	if value == nil {
		return ""
	}
	return value.Value
}

func setMappingString(node *yaml.Node, key, value string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content[i+1].Kind = yaml.ScalarNode
			node.Content[i+1].Tag = "!!str"
			node.Content[i+1].Value = value
			return
		}
	}

	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
	)
}

func deleteMappingKey(node *yaml.Node, key string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content = append(node.Content[:i], node.Content[i+2:]...)
			return
		}
	}
}
