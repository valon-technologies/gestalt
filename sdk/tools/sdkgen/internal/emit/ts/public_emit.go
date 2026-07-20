package ts

import (
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
)

// EmitPublic renders the public TypeScript transport client into the caller's
// package-owned generated directory using the supplied import paths.
func EmitPublic(schema *model.Schema, imports PublicImports) (*fileset.FileSet, error) {
	plan, err := publicsurface.PrepareEmit(schema)
	if err != nil {
		return nil, err
	}
	return EmitPublicPlan(plan, imports)
}

// EmitPublicPlan renders a prepared public emit plan.
func EmitPublicPlan(plan *publicsurface.EmitPlan, imports PublicImports) (*fileset.FileSet, error) {
	set := fileset.New()
	if plan == nil || len(plan.View.Services) == 0 {
		return set, nil
	}

	files := map[string]string{
		"methods.ts":              renderPublicMethods(plan.Methods),
		"types.ts":                renderPublicTypes(plan.View, imports),
		"converters.ts":           renderPublicConverters(plan.View, imports),
		"transport.ts":             renderPublicTransport(),
		"gateway_error.ts":        renderPublicGatewayError(imports),
		"rest_request_mapping.ts": renderPublicRestRequestMapping(),
		"transport_support.ts":    renderPublicTransportSupport(imports),
	}
	for _, svc := range plan.Filtered.Services {
		fileName := publicServiceClientFile(svc.Name)
		files[fileName] = renderPublicServiceClient(svc, imports)
	}
	for path, content := range files {
		if err := set.Add(path, []byte(content)); err != nil {
			return nil, err
		}
	}
	return set, nil
}
