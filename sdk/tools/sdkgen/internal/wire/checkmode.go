package wire

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// rewriteTemplate copies a buf generate template into scratch with every
// plugin out directory redirected to an absolute path under scratch, so check
// mode renders without touching the tree. The template's relative out paths
// contain ".." segments, which is why buf's --output re-rooting cannot be
// used here. Returns the scratch template path and a map from each unique
// original out dir (relative to sdk/proto) to its scratch directory.
func rewriteTemplate(templatePath, scratch string) (string, map[string]string, error) {
	data, err := os.ReadFile(templatePath)
	if err != nil {
		return "", nil, err
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return "", nil, fmt.Errorf("parse %s: %w", templatePath, err)
	}
	plugins, ok := doc["plugins"].([]any)
	if !ok {
		return "", nil, fmt.Errorf("%s: no plugins list", templatePath)
	}
	outs := map[string]string{}
	for _, p := range plugins {
		plugin, ok := p.(map[string]any)
		if !ok {
			return "", nil, fmt.Errorf("%s: unexpected plugin entry shape", templatePath)
		}
		orig, ok := plugin["out"].(string)
		if !ok {
			return "", nil, fmt.Errorf("%s: plugin without string out", templatePath)
		}
		scratchDir, seen := outs[orig]
		if !seen {
			scratchDir = filepath.Join(scratch, fmt.Sprintf("out%d", len(outs)))
			outs[orig] = scratchDir
		}
		plugin["out"] = scratchDir
	}
	rewritten, err := yaml.Marshal(doc)
	if err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		return "", nil, err
	}
	scratchTemplate := filepath.Join(scratch, filepath.Base(templatePath))
	if err := os.WriteFile(scratchTemplate, rewritten, 0o644); err != nil {
		return "", nil, err
	}
	return scratchTemplate, outs, nil
}
