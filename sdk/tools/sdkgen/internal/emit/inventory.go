package emit

import (
	"encoding/json"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
)

// AddModuleInventory records the complete generated module set in a stable,
// machine-readable file. Consumers can use it for packaging and import
// audits without duplicating emitter-specific path rules.
func AddModuleInventory(set *fileset.FileSet, target Target) error {
	paths := set.Files()
	modules := make([]string, 0, len(paths))
	for _, file := range paths {
		modules = append(modules, file.Path)
	}
	payload := struct {
		Target  Target   `json:"target"`
		Modules []string `json:"modules"`
	}{Target: target, Modules: modules}
	content, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return set.Add("sdkgen.module-inventory.json", content)
}
