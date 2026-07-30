package config

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const (
	maxRootAppPrompts         = 5
	maxRootAppPromptTextRunes = 280
)

// AppPrompt is a deployer-owned prompt definition for the root app. The stable
// ID makes the prompt reusable by UI and future protocol surfaces.
type AppPrompt struct {
	ID   string `yaml:"id"`
	Text string `yaml:"text"`
}

// RootAppPrompts returns the prompt definitions configured on the app mounted
// at /. The root app is the Home/default app, but its config key is
// deployment-defined (for example, apps.home), so lookup is based on the
// canonical root mount rather than a hard-coded app name.
func RootAppPrompts(apps map[string]*ProviderEntry) (map[string][]AppPrompt, error) {
	rootName := ""
	var rootPrompts map[string][]AppPrompt
	for name, entry := range apps {
		if entry == nil || entry.Static == nil || strings.TrimSpace(entry.Static.Mount) != "/" {
			continue
		}
		if rootName != "" {
			return nil, fmt.Errorf("only one app can be mounted at /; remove the / mount from apps.%s or apps.%s", rootName, name)
		}
		rootName = name
		prompts, err := appPrompts(entry.Config)
		if err != nil {
			return nil, fmt.Errorf("apps.%s.config.prompts: %w", name, err)
		}
		rootPrompts = prompts
	}
	return rootPrompts, nil
}

func validateRootAppPromptConfig(entry *ProviderEntry) error {
	if entry == nil || entry.Static == nil || strings.TrimSpace(entry.Static.Mount) != "/" {
		return nil
	}
	_, err := appPrompts(entry.Config)
	return err
}

func appPrompts(node yaml.Node) (map[string][]AppPrompt, error) {
	promptsNode := mappingValueNode(&node, "prompts")
	if promptsNode == nil || promptsNode.Tag == "!!null" {
		return nil, nil
	}
	promptsNode = documentValueNode(promptsNode)
	if promptsNode.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("must be a mapping of app names to prompt lists")
	}

	result := make(map[string][]AppPrompt, len(promptsNode.Content)/2)
	for i := 0; i+1 < len(promptsNode.Content); i += 2 {
		appName := strings.TrimSpace(promptsNode.Content[i].Value)
		if appName == "" {
			return nil, fmt.Errorf("app name must not be empty")
		}
		if _, exists := result[appName]; exists {
			return nil, fmt.Errorf("duplicate app name %q", appName)
		}

		listNode := documentValueNode(promptsNode.Content[i+1])
		if listNode == nil || listNode.Kind != yaml.SequenceNode {
			return nil, fmt.Errorf("%s must be a list", appName)
		}
		if len(listNode.Content) > maxRootAppPrompts {
			return nil, fmt.Errorf("%s has %d prompts; maximum is %d", appName, len(listNode.Content), maxRootAppPrompts)
		}

		appPrompts := make([]AppPrompt, 0, len(listNode.Content))
		seenIDs := make(map[string]struct{}, len(listNode.Content))
		for index, promptNode := range listNode.Content {
			var prompt AppPrompt
			if err := decodeYAMLNodeKnownFields(promptNode, &prompt); err != nil {
				return nil, fmt.Errorf("%s[%d]: %w", appName, index, err)
			}
			prompt.ID = strings.TrimSpace(prompt.ID)
			prompt.Text = strings.TrimSpace(prompt.Text)
			if prompt.ID == "" {
				return nil, fmt.Errorf("%s[%d].id must not be empty", appName, index)
			}
			if _, exists := seenIDs[prompt.ID]; exists {
				return nil, fmt.Errorf("%s has duplicate prompt id %q", appName, prompt.ID)
			}
			seenIDs[prompt.ID] = struct{}{}
			if prompt.Text == "" {
				return nil, fmt.Errorf("%s[%d].text must not be empty", appName, index)
			}
			if utf8.RuneCountInString(prompt.Text) > maxRootAppPromptTextRunes {
				return nil, fmt.Errorf("%s[%d].text exceeds %d characters", appName, index, maxRootAppPromptTextRunes)
			}
			appPrompts = append(appPrompts, prompt)
		}
		if len(appPrompts) > 0 {
			result[appName] = appPrompts
		}
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}
