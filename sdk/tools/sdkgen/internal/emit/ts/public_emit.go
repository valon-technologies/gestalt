package ts

import (
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
)

// EmitPublic renders the public TypeScript transport client under
// sdk/typescript/src/client/generated/.
func EmitPublic(schema *model.Schema) (*fileset.FileSet, error) {
	plan, err := publicsurface.PrepareEmit(schema)
	if err != nil {
		return nil, err
	}
	set := fileset.New()
	if len(plan.View.Services) == 0 {
		return set, nil
	}

	files := map[string]string{
		"methods.ts":         renderPublicMethods(plan.Methods),
		"types.ts":           renderPublicTypes(plan.View),
		"converters.ts":      renderPublicConverters(plan.View),
		"unary_transport.ts": renderPublicUnaryTransport(),
		"app_client.ts":      renderPublicAppClient(plan.Filtered.Services),
	}
	for path, content := range files {
		if err := set.Add(path, []byte(content)); err != nil {
			return nil, err
		}
	}
	return set, nil
}
